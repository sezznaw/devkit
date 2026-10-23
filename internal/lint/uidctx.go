package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 规则 rpc-uid-from-ctx：玩家网关调得到的 RPC 方法，不能按请求体里的 uid 办事。
//
// 玩家网关用 kitexx.MustUID(ctx) 取登录者，再把它填进 RPC 请求的 uid 字段；RPC 服务的 handler
// 只看 req.Uid，于是「请求体里的 uid 就是登录者」这件事全靠网关自觉——网关一旦被攻破，就能拿任意
// uid 调这些方法（看别人的资料、改别人的头像）。登录者的身份其实随 metainfo 一路传到了 RPC 服务：
// kitexx.UID / MustUID / LoginUID 在 RPC 服务里能拿到同一个 uid，kitexx.CheckMemberUID(ctx, uid)
// 校验「声称是会员的调用者必须等于目标 uid」（玩家与后台共用的方法用它）。方法体里出现这四个
// 之一，就算从 ctx 取了登录者。
//
// 判定（只对 RPC 服务，且项目在 idl/devkit.yaml 配了 lint.player_gateways）：
//
//	handler/ 下每个 RPC 方法（rpcMethod 正则认方法行，方法体到下一个列首 func 或文件尾），
//	方法体的代码（去掉注释）读了请求的 uid 字段（req.Uid、req.GetUid()、req.MemberUid …），
//	且没有调 kitexx.(UID|MustUID|LoginUID|CheckMemberUID)(，
//	且任一 conf/*.yaml 的 callers.rules 里放行它的 allow 含玩家网关之一或 "*"
//	→ 报在 handler 文件的方法行。
//
// 豁免：IDL 里该方法上方的注释块含 @target-uid（这个 uid 是被操作的对象而不是登录者，比如后台
// 替代理建会员），或 handler 方法行（或它的上一行）写 //devkit:lint-ignore rpc-uid-from-ctx。

// reqUIDRead：读请求体的 uid 字段。\b 保证 req.UidList 这类不算。
var reqUIDRead = regexp.MustCompile(`\breq\.(Get)?(Uid|UID|MemberUid|MemberUID)\b`)

// ctxUIDCall：从 ctx 取登录者的四个 helper，出现任意一个就算方法自己核对了身份。
var ctxUIDCall = regexp.MustCompile(`\bkitexx\.(UID|MustUID|LoginUID|CheckMemberUID)\(`)

// targetUIDTag：IDL 方法注释里的豁免标记——这个方法的 uid 是被操作的对象，不是登录者。
var targetUIDTag = regexp.MustCompile(`@target-uid\b`)

// columnFunc：列首的 func，前一个方法体到这里为止。
var columnFunc = regexp.MustCompile(`^func\b`)

const uidRule = "rpc-uid-from-ctx"

// checkUIDFromCtx 跑规则 rpc-uid-from-ctx。goFiles 是 businessGoFiles 的结果（只看 handler/ 下的），
// idlFile 是该服务的 IDL（读 @target-uid 豁免），gateways 是 lint.player_gateways；没配就不跑。
func checkUIDFromCtx(root string, goFiles []string, idlFile string, gateways []string) []Finding {
	if len(gateways) == 0 {
		return nil
	}
	reach := loadPlayerReach(root, gateways)
	if len(reach.all) == 0 && len(reach.methods) == 0 {
		return nil
	}
	exempt := idlTaggedMethods(idlFile, targetUIDTag)
	var out []Finding
	for _, rel := range goFiles {
		if !strings.HasPrefix(rel, "handler"+string(filepath.Separator)) {
			continue
		}
		for _, m := range goMethodsOf(filepath.Join(root, rel)) {
			via := reach.via(m.name)
			if len(via) == 0 || exempt[m.name] || ignored(m.decl, uidRule) || ignored(m.above, uidRule) {
				continue
			}
			if !reqUIDRead.MatchString(m.code) || ctxUIDCall.MatchString(m.code) {
				continue
			}
			msg := fmt.Sprintf("%s 按请求体的 uid 办事（%s），而 %s：玩家网关可达的方法不能信请求体里的 uid，被攻破的网关能拿任意 uid 来调。登录者的 uid 从 ctx 取——只给玩家用的方法 uid, err := kitexx.LoginUID(ctx)（无登录者回 1004）；玩家与后台共用的方法先 kitexx.CheckMemberUID(ctx, req.Uid)（会员 realm 的调用者必须等于目标 uid）。uid 确实是被操作的对象（后台替别人改）时，在 IDL 这个方法的注释上加 // @target-uid",
				m.name, reqUIDRead.FindString(m.code), describeReach(via))
			out = append(out, Finding{filepath.ToSlash(rel), m.line, "rpc-uid-from-ctx", msg})
		}
	}
	return out
}

// describeReach 把「谁调得到」写成一句话：放行了哪个玩家网关，或 allow 含 *。
func describeReach(via []string) string {
	var names []string
	star := false
	for _, v := range via {
		if v == "*" {
			star = true
		} else {
			names = append(names, v)
		}
	}
	switch {
	case star && len(names) == 0:
		return "conf 的 callers 规则 allow 含 *（任何服务都能调，玩家网关也在内）"
	case star:
		return fmt.Sprintf("conf 的 callers 规则放行了玩家网关 %s，还有一条 allow 含 *", strings.Join(names, "、"))
	default:
		return fmt.Sprintf("conf 的 callers 规则放行了玩家网关 %s", strings.Join(names, "、"))
	}
}

// playerReach 是「玩家网关调得到哪些方法」：从每份 conf/*.yaml 的 callers.rules 读出并取并集
// （任一环境放行就算可达）。all 是 methods: ["*"] 的规则里放行的网关（每个方法都可达），methods
// 是按方法名放行的；值是放行它的网关名，allow 里的 "*" 原样记为 "*"。
type playerReach struct {
	all     []string
	methods map[string][]string
}

func loadPlayerReach(root string, gateways []string) playerReach {
	r := playerReach{methods: map[string][]string{}}
	isGateway := map[string]bool{}
	for _, g := range gateways {
		isGateway[strings.TrimSpace(g)] = true
	}
	files, _ := filepath.Glob(filepath.Join(root, "conf", "*.yaml"))
	sort.Strings(files)
	for _, f := range files {
		rules, err := loadCallerRules(f)
		if err != nil {
			continue // 服务自己的配置检查会报
		}
		for _, rule := range rules {
			var via []string
			for _, a := range rule.Allow {
				a = strings.TrimSpace(a)
				if a == "*" || isGateway[a] {
					via = append(via, a)
				}
			}
			if len(via) == 0 {
				continue
			}
			for _, m := range rule.Methods {
				m = strings.TrimSpace(m)
				if m == "*" {
					r.all = append(r.all, via...)
				} else {
					r.methods[m] = append(r.methods[m], via...)
				}
			}
		}
	}
	return r
}

// via 回玩家网关调到 method 的途径（网关名或 "*"），去重排序；空表示调不到。
func (r playerReach) via(method string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range append(append([]string{}, r.all...), r.methods[method]...) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// goMethod 是 handler 文件里切出来的一个 RPC 方法。
type goMethod struct {
	name  string
	line  int    // 方法行，1 起
	decl  string // 方法行本身（lint-ignore 可以写在行尾）
	above string // 方法行的上一行（lint-ignore 也可以写在这里）
	code  string // 方法体里的代码，去掉了注释
}

// goMethodsOf 按 rpcMethod 正则把 Go 文件切成 RPC 方法：方法体从方法行到下一个列首 func 或文件尾。
// 读不到文件回 nil。
func goMethodsOf(path string) []goMethod {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var out []goMethod
	var cur *goMethod
	var body []string
	flush := func() {
		if cur != nil {
			cur.code = strings.Join(body, "\n")
			out = append(out, *cur)
		}
		cur, body = nil, nil
	}
	for i, line := range lines {
		if columnFunc.MatchString(line) {
			flush()
			if m := rpcMethod.FindStringSubmatch(line); m != nil {
				above := ""
				if i > 0 {
					above = lines[i-1]
				}
				cur = &goMethod{name: m[1], line: i + 1, decl: line, above: above}
			}
		}
		if cur != nil {
			body = append(body, codeOf(line))
		}
	}
	flush()
	return out
}

// codeOf 去掉一行里的注释（整行注释变空，行尾 // 之后截掉）；和 checkGoFile 一样粗，够用。
func codeOf(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "//") {
		return ""
	}
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// idlTaggedMethods 回 IDL 文件 service 块里、上方注释块含 tag 的方法名；文件不存在回空表。
func idlTaggedMethods(path string, tag *regexp.Regexp) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	out := map[string]bool{}
	in := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "service ") && strings.HasSuffix(t, "{"):
			in = true
		case in && strings.HasPrefix(t, "}"):
			in = false
		case in && !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "#"):
			if m := idlMethodRE.FindStringSubmatch(line); m != nil && commentBlockHas(lines, i, tag) {
				out[m[1]] = true
			}
		}
	}
	return out
}
