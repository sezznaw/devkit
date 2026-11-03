package lint

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// verify-perms（项目规则，idl/devkit.yaml 的 lint.verify_perms 指向登记表时才跑，只对网关）：
//
// 后台网关的部署后检查（app/verify.go）用一个只读的检查账号登录后调各个接口；这个账号的角色有哪些权限点由种子决定，
// 种子在运维的仓库里，同事看不到也改不了。检查里新调一个带 `// @perm` 的接口而角色没有这个点，本机和 CI 都过，
// 发布后检查回 HTTP 403 / 1007，ArgoCD 卡住这次发布。这条规则把账号的权限点放进 IDL 仓库的登记表
// （每行 `<网关> <权限点>`，由 IDL 仓库的流水线同步进种子），核对 app/verify.go 里每个接口路径的 @perm 都登记了。
//
// 登记表只许 `.view` 结尾的具体权限点（检查账号只读；不许通配符），并且每个点要是该网关 IDL 里真有的 @perm（拼错、
// 接口删了留下的旧行都报）。

const verifyPermsRule = "verify-perms"

var (
	permTagRE      = regexp.MustCompile(`@perm\s+(\S+)`)
	apiPathRE      = regexp.MustCompile(`api\.(?:post|get|put|delete|patch)\s*=\s*"([^"]+)"`)
	verifyPathRE   = regexp.MustCompile(`"(/v\d+/[A-Za-z0-9_/]+)"`)
	verifyPermLine = regexp.MustCompile(`^(\S+)\s+(\S+)\s*$`)
)

// gatewayPermPaths: path → @perm of every method in the gateway's domain files that declares one.
func gatewayPermPaths(idlDir, service string) map[string]string {
	files, _ := filepath.Glob(filepath.Join(idlDir, service, "*.thrift"))
	sort.Strings(files)
	out := map[string]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		in := false
		for i, line := range lines {
			t := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(t, "service ") && strings.HasSuffix(t, "{"):
				in = true
			case in && strings.HasPrefix(t, "}"):
				in = false
			case in && !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "#"):
				p := apiPathRE.FindStringSubmatch(line)
				if p == nil {
					continue
				}
				for j := i - 1; j >= 0; j-- {
					c := strings.TrimSpace(lines[j])
					if c == "" || !(strings.HasPrefix(c, "//") || strings.HasPrefix(c, "*") || strings.HasPrefix(c, "/*")) {
						break
					}
					if m := permTagRE.FindStringSubmatch(c); m != nil {
						out[p[1]] = m[1]
						break
					}
				}
			}
		}
	}
	return out
}

type verifyGrant struct {
	line int
}

// loadVerifyPerms reads the registry: gateway → permission point → line. Malformed lines are findings.
func loadVerifyPerms(path, rel string) (map[string]map[string]verifyGrant, []Finding) {
	f, err := os.Open(path)
	if err != nil {
		return nil, []Finding{{rel, 0, verifyPermsRule, fmt.Sprintf("lint.verify_perms 指向的登记表读不到（%v）", err)}}
	}
	defer f.Close()
	out := map[string]map[string]verifyGrant{}
	var errs []Finding
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		t := strings.TrimSpace(sc.Text())
		if i := strings.Index(t, "#"); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
		if t == "" {
			continue
		}
		m := verifyPermLine.FindStringSubmatch(t)
		if m == nil {
			errs = append(errs, Finding{rel, n, verifyPermsRule, "每行是 `<网关> <权限点>`，如 `ser-admin-api member.view`"})
			continue
		}
		gw, perm := m[1], m[2]
		if strings.Contains(perm, "*") || !strings.HasSuffix(perm, ".view") {
			errs = append(errs, Finding{rel, n, verifyPermsRule, fmt.Sprintf("%s：检查账号只读，只许登记 `.view` 结尾的具体权限点，不许通配符；要检查写接口不要放进部署后检查", perm)})
			continue
		}
		if out[gw] == nil {
			out[gw] = map[string]verifyGrant{}
		}
		if _, dup := out[gw][perm]; dup {
			errs = append(errs, Finding{rel, n, verifyPermsRule, fmt.Sprintf("%s %s 重复登记", gw, perm)})
			continue
		}
		out[gw][perm] = verifyGrant{line: n}
	}
	return out, errs
}

// checkVerifyPerms runs rule verify-perms for one gateway.
func checkVerifyPerms(root, idlDir, service, registry string) []Finding {
	rel := filepath.ToSlash(filepath.Join("idl", registry))
	paths := gatewayPermPaths(idlDir, service)
	if len(paths) == 0 {
		return nil // 没有 @perm 的网关（玩家网关、代理后台）：检查账号不需要权限点
	}
	grants, out := loadVerifyPerms(filepath.Join(idlDir, registry), rel)
	if grants == nil && len(out) > 0 {
		return out
	}
	// 登记表里只看本网关的行，别的网关的错由它自己的 lint 报
	var mine []Finding
	for _, f := range out {
		mine = append(mine, f)
	}
	declared := map[string]bool{}
	for _, p := range paths {
		declared[p] = true
	}
	for perm, g := range grants[service] {
		if !declared[perm] {
			mine = append(mine, Finding{rel, g.line, verifyPermsRule, fmt.Sprintf("%s 的 IDL 里没有接口声明 @perm %s（拼错了，或接口已删）：改对或删掉这一行", service, perm)})
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "app", "verify.go"))
	if err != nil {
		return mine
	}
	reported := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") || ignored(line, verifyPermsRule) {
			continue // 注释行，或写了 //devkit:lint-ignore verify-perms
		}
		for _, m := range verifyPathRE.FindAllStringSubmatch(line, -1) {
			perm, ok := paths[m[1]]
			if !ok {
				continue
			}
			if _, granted := grants[service][perm]; granted || reported[perm] {
				continue
			}
			reported[perm] = true
			mine = append(mine, Finding{"app/verify.go", i + 1, verifyPermsRule, fmt.Sprintf(
				"部署后检查调 %s（@perm %s），检查账号的角色没有这个权限点，发布后会回 403 / 1007 卡住发布：在 %s 加一行 `%s %s`，先推 IDL（流水线同步进种子），再推本服务",
				m[1], perm, rel, service, perm)})
		}
	}
	return mine
}
