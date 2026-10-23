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

// 归属规则（2026-10-23）：一个服务只碰自己的东西。四条规则各管一种「别人的东西」：
//
//	redis-key-owner  别的服务的 Redis 业务键前缀（登记表 idl/redis-keys.txt）和键构造包
//	redis-pubsub     go-redis 的 Pub/Sub（跨服务通知走 Kafka 事件）
//	event-owner      Publish / PublishTx 的主题必须是本服务领域的 events.<领域>（callbacks.* 只有厂商服务）
//	model-owner      lib 里别的服务拥有的表模型（idl/devkit.yaml lint.model_owners）
//
// 背景：阶段 4 起每个服务的 Valkey / Kafka / MySQL 账号只能碰自己登记的键、主题、库，读别人的键在 dev 上
// 直接 NOPERM（2026-10-23 cold 的 GetAgentIndex、blue 的 game:venue:* 频道都是这么出问题的）。这些规则把
// 「运行时才会撞墙」提前到推送前，并且告诉写代码的人该怎么改：要别人的数据调它的接口或订阅它的事件。

const (
	redisKeyRule   = "redis-key-owner"
	redisPubSubRul = "redis-pubsub"
	eventOwnerRule = "event-owner"
	modelOwnerRule = "model-owner"
)

// strLit 匹配一行里的双引号字符串字面量（含转义）；反引号的原始字符串按整行处理。
var strLit = regexp.MustCompile(`"((?:[^"\\\n]|\\.)*)"|` + "`([^`\n]*)`")

// literalsOf 回这一行代码里的字符串字面量内容（注释已由 codeOf 去掉）。
func literalsOf(code string) []string {
	var out []string
	for _, m := range strLit.FindAllStringSubmatch(code, -1) {
		if m[1] != "" {
			out = append(out, m[1])
		} else if m[2] != "" {
			out = append(out, m[2])
		}
	}
	return out
}

// ---------------------------------------------------------------- redis-key-owner

// keyOwner 是登记表里的一行：前缀（归一成以冒号结尾）、所有者和其它允许访问的服务。
type keyOwner struct {
	prefix  string
	owner   string
	allowed map[string]bool
	line    int
}

// pkgOwner 是 `pkg <lib 包路径后缀> <所有者> [允许 ...]`：lib 里给某个服务构造键的包，别的服务不该 import。
type pkgOwner struct {
	suffix  string
	owner   string
	allowed map[string]bool
	line    int
}

// keyRegistry 是解析好的 idl/redis-keys.txt。
type keyRegistry struct {
	rel  string // 报告用的文件名（相对 idl 目录）
	keys []keyOwner
	pkgs []pkgOwner
	errs []Finding
}

// loadKeyRegistry 读登记表。格式：`#` 注释；`<前缀> <所有者> [其它允许的服务 ...]`；`pkg <后缀> <所有者> [允许 ...]`。
// 文件不存在时回空表并报一条（配了 lint.redis_keys 就应该有这个文件）。
func loadKeyRegistry(path, rel string) keyRegistry {
	reg := keyRegistry{rel: rel}
	f, err := os.Open(path)
	if err != nil {
		reg.errs = append(reg.errs, Finding{rel, 0, redisKeyRule, fmt.Sprintf("读不到 Redis 键登记表 %s（idl/devkit.yaml 的 lint.redis_keys 指向它）：%v", rel, err)})
		return reg
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "pkg" {
			if len(fields) < 3 {
				reg.errs = append(reg.errs, Finding{rel, n, redisKeyRule, "pkg 行要写 `pkg <lib 包路径后缀> <所有者服务> [其它允许的服务 ...]`"})
				continue
			}
			reg.pkgs = append(reg.pkgs, pkgOwner{suffix: strings.Trim(fields[1], "/"), owner: fields[2], allowed: setOf(fields[2:]), line: n})
			continue
		}
		if len(fields) < 2 {
			reg.errs = append(reg.errs, Finding{rel, n, redisKeyRule, "每行要写 `<键前缀> <所有者服务> [其它允许的服务 ...]`，前缀以冒号结尾"})
			continue
		}
		p := fields[0]
		if !strings.HasSuffix(p, ":") {
			p += ":"
		}
		reg.keys = append(reg.keys, keyOwner{prefix: p, owner: fields[1], allowed: setOf(fields[1:]), line: n})
	}
	// 最长前缀优先：admin:login:role: 归网关，admin:login: 归 ser-admin。
	sort.SliceStable(reg.keys, func(i, j int) bool { return len(reg.keys[i].prefix) > len(reg.keys[j].prefix) })
	return reg
}

func setOf(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// match 回字面量 lit 命中的最长登记前缀：lit 必须以「前缀 + 冒号」开头（裸词 "member" 是 realm 名、JSON 字段名
// 这类普通字符串，不算键）。
func (r keyRegistry) match(lit string) (keyOwner, bool) {
	for _, k := range r.keys {
		if strings.HasPrefix(lit, k.prefix) {
			return k, true
		}
	}
	return keyOwner{}, false
}

// redisxCall：redisx 的锁 / 缓存 / 限流等 helper 自己给名字加 <种类>:<服务>: 前缀，传给它们的名字不是裸键，
// 和登记表撞了前缀也不是在碰别人的键。
var redisxCall = regexp.MustCompile(`\bredisx\.`)

// importPath 认 import 行里的路径：`import "x"`、import 块里的 `"x"` 或 `alias "x"`。
var importPath = regexp.MustCompile(`^\s*(?:import\s+)?(?:[\w.]+\s+)?"([^"]+)"\s*$`)

// checkRedisKeyOwner 跑规则 redis-key-owner：service 的业务 Go 文件里，字符串字面量以别的服务登记的前缀开头、
// 或 import 了别的服务的键构造包，都报。
func checkRedisKeyOwner(root, service string, goFiles []string, reg keyRegistry) []Finding {
	if len(reg.keys) == 0 && len(reg.pkgs) == 0 {
		return nil
	}
	var out []Finding
	for _, rel := range goFiles {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if ignored(line, redisKeyRule) {
				continue
			}
			code := codeOf(line)
			if m := importPath.FindStringSubmatch(code); m != nil {
				for _, p := range reg.pkgs {
					if (m[1] == p.suffix || strings.HasSuffix(m[1], "/"+p.suffix)) && p.owner != service && !p.allowed[service] {
						out = append(out, Finding{filepath.ToSlash(rel), i + 1, redisKeyRule,
							fmt.Sprintf("import 了 %s：它构造的 Redis 键归 %s（%s 第 %d 行）。别的服务的键不直接读写，要这份数据调 %s 的接口或订阅它的事件；确属共用的键在登记表那一行加上本服务并同步 infra 的 Valkey ACL",
								m[1], p.owner, reg.rel, p.line, p.owner)})
					}
				}
				continue
			}
			if redisxCall.MatchString(code) {
				continue
			}
			for _, lit := range literalsOf(code) {
				k, ok := reg.match(lit)
				if !ok || k.owner == service || k.allowed[service] {
					continue
				}
				out = append(out, Finding{filepath.ToSlash(rel), i + 1, redisKeyRule,
					fmt.Sprintf("Redis 键 %q 的前缀 %s 归 %s（%s 第 %d 行），本服务的 Valkey 账号碰它会 NOPERM。要这份数据调 %s 的接口或订阅它的事件存自己的副本；确属共用的键在登记表那一行加上本服务并同步 infra 的 Valkey ACL；新业务键用本服务自己的前缀并先登记",
						lit, k.prefix, k.owner, reg.rel, k.line, k.owner)})
				break
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- redis-pubsub

// pubsubCall 认 Redis 客户端上的 Pub/Sub 调用；接收者留给 isMessagingReceiver 排除 Kafka / 推送。
var pubsubCall = regexp.MustCompile(`([\w.\[\]()]*)\.(PSubscribe|SSubscribe|Subscribe|Publish)\(`)

// goRedisImport 是 go-redis 的模块路径：文件没 import 它就不会在用 Redis 的 Pub/Sub。
const goRedisImport = "github.com/redis/go-redis"

// isMessagingReceiver：接收者一看就是 Kafka、推送、消息总线的，不是 Redis。
func isMessagingReceiver(recv string) bool {
	r := strings.ToLower(recv)
	for _, w := range []string{"kafka", "push", "centrifugo", "events", "bus", "nats", "broker", "producer", "consumer"} {
		if strings.Contains(r, w) {
			return true
		}
	}
	return false
}

// checkRedisPubSub 跑规则 redis-pubsub：import 了 go-redis 的业务文件里，接收者不是消息组件的 Subscribe / Publish 调用都报。
func checkRedisPubSub(root string, goFiles []string) []Finding {
	var out []Finding
	for _, rel := range goFiles {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || !strings.Contains(string(data), goRedisImport) {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if ignored(line, redisPubSubRul) {
				continue
			}
			code := codeOf(line)
			for _, m := range pubsubCall.FindAllStringSubmatch(code, -1) {
				if isMessagingReceiver(m[1]) {
					continue
				}
				out = append(out, Finding{filepath.ToSlash(rel), i + 1, redisPubSubRul,
					fmt.Sprintf("%s.%s 是 Redis 的 Pub/Sub：不持久、不能回放、没有按服务的授权，跨服务通知一律 Kafka 事件——写方在事务里 rt.Kafka.PublishTx(ctx, tx, \"events.<领域>\", id, \"<领域>.<过去式>\", data)，读方 rt.Kafka.Subscribe。确属本服务内部用途的写 //devkit:lint-ignore redis-pubsub 并说明",
						m[1], m[2])})
				break
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- event-owner

// topicConst 认 `X = "events.foo"` / `X = "callbacks.foo"`（const 块里和单行 const 都是这个形状）。
var topicConst = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*=\s*"((?:events|callbacks)\.[a-z0-9_.-]+)"`)

// publishCall 认 Publish / PublishTx 调用的开头；参数由 callArgs 从括号起切。
var publishCall = regexp.MustCompile(`\.(PublishTx|Publish)\(`)

// topicLike：主题形状的字符串，其它的（推送频道 user:7、普通字符串）不归这条规则管。
var topicLike = regexp.MustCompile(`^(events|callbacks)\.([a-z0-9_.-]+)$`)

// callArgs 从 src[from]（紧跟左括号后的位置）起按顶层逗号切参数，直到配对的右括号；跨行也行。
func callArgs(src string, from int) []string {
	depth := 0
	inStr := byte(0)
	start := from
	var args []string
	for i := from; i < len(src); i++ {
		c := src[i]
		if inStr != 0 {
			if c == '\\' {
				i++
			} else if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '`', '\'':
			inStr = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				args = append(args, strings.TrimSpace(src[start:i]))
				return args
			}
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(src[start:i]))
				start = i + 1
			}
		}
	}
	return args
}

// resolveTopic 把主题参数解成主题名：字面量直接取，标识符（可带包名）查本服务的常量表；解不出回空。
func resolveTopic(arg string, consts map[string]string) string {
	if len(arg) >= 2 && arg[0] == '"' && arg[len(arg)-1] == '"' {
		return arg[1 : len(arg)-1]
	}
	if i := strings.LastIndex(arg, "."); i >= 0 {
		arg = arg[i+1:]
	}
	return consts[arg]
}

// checkEventOwner 跑规则 event-owner：Publish / PublishTx 的主题 events.<x> 必须 x 是本服务领域（服务名去掉 ser-；
// - 与 _ 等价），callbacks.* 只有 vendorService 能发；producers 是 idl/devkit.yaml 的 lint.event_producers（主题 →
// 额外允许的生产者），给过渡期的代发用。
func checkEventOwner(root, service, vendorService string, goFiles []string, producers map[string][]string) []Finding {
	domain := strings.TrimPrefix(service, "ser-")
	okDomain := map[string]bool{domain: true, strings.ReplaceAll(domain, "-", "_"): true, strings.ReplaceAll(domain, "_", "-"): true}
	allowed := func(topic string) bool {
		for _, s := range producers[topic] {
			if strings.TrimSpace(s) == service {
				return true
			}
		}
		return false
	}
	type file struct {
		rel string
		src string
	}
	var files []file
	consts := map[string]string{}
	for _, rel := range goFiles {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		src := string(data)
		files = append(files, file{rel, src})
		for _, m := range topicConst.FindAllStringSubmatch(src, -1) {
			consts[m[1]] = m[2]
		}
	}
	var out []Finding
	for _, f := range files {
		lines := strings.Split(f.src, "\n")
		for _, loc := range publishCall.FindAllStringSubmatchIndex(f.src, -1) {
			lineNo := strings.Count(f.src[:loc[0]], "\n") + 1
			line := lines[lineNo-1]
			if ignored(line, eventOwnerRule) || strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			args := callArgs(f.src, loc[1])
			idx := 1 // Publish(ctx, topic, ...)
			if f.src[loc[2]:loc[3]] == "PublishTx" {
				idx = 2 // PublishTx(ctx, tx, topic, ...)
			}
			if len(args) <= idx {
				continue
			}
			topic := resolveTopic(args[idx], consts)
			m := topicLike.FindStringSubmatch(topic)
			if m == nil {
				continue
			}
			switch m[1] {
			case "events":
				if okDomain[m[2]] || allowed(topic) {
					continue
				}
				out = append(out, Finding{filepath.ToSlash(f.rel), lineNo, eventOwnerRule,
					fmt.Sprintf("往 %s 发事件：这个主题归 ser-%s，本服务只发 events.%s（Kafka ACL 也只给了本服务自己的主题）。要通知的事属于别人的领域时，调它的接口让它自己改、自己发；确属过渡期代发的，在 idl/devkit.yaml 的 lint.event_producers 里给 %s 加上本服务并写明何时收回",
						topic, m[2], domain, topic)})
			case "callbacks":
				if service == vendorService || allowed(topic) {
					continue
				}
				out = append(out, Finding{filepath.ToSlash(f.rel), lineNo, eventOwnerRule,
					fmt.Sprintf("往 %s 发消息：厂商回调留档只由 %s（lint.vendor_service）发；别的服务要的是业务事件，用本服务的 events.%s",
						topic, vendorService, domain)})
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- model-owner

// modelRef 认 lib 表模型的引用：model.X / mysql.X（服务里这两种 import 别名都有）。
var modelRef = regexp.MustCompile(`\b(?:model|mysql)\.([A-Z]\w*)\b`)

// checkModelOwner 跑规则 model-owner：owners 是 idl/devkit.yaml 的 lint.model_owners（服务 → 它拥有的模型），
// exceptions 是 lint.model_owner_exceptions（服务 → 阶段 1 前暂时允许引用的别家模型）。引用别人的模型报一次
// （每个文件每个模型第一处），rt.AutoMigrate 别人的模型不看例外、每处都报。
func checkModelOwner(root, service string, goFiles []string, owners, exceptions map[string][]string) []Finding {
	if len(owners) == 0 {
		return nil
	}
	ownerOf := map[string]string{}
	for svc, models := range owners {
		for _, m := range models {
			ownerOf[strings.TrimSpace(m)] = svc
		}
	}
	exempt := setOf(exceptions[service])
	var out []Finding
	for _, rel := range goFiles {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		migrateDepth := -1 // >=0 表示在 AutoMigrate(...) 的参数里，值是未闭合的括号数
		for i, line := range strings.Split(string(data), "\n") {
			code := codeOf(line)
			inMigrate := migrateDepth >= 0
			if j := strings.Index(code, "AutoMigrate("); j >= 0 {
				inMigrate = true
				migrateDepth = 0
				code2 := code[j+len("AutoMigrate("):]
				migrateDepth += strings.Count(code2, "(") - strings.Count(code2, ")")
				if migrateDepth < 0 {
					migrateDepth = -1
				}
			} else if migrateDepth >= 0 {
				migrateDepth += strings.Count(code, "(") - strings.Count(code, ")")
				if migrateDepth < 0 {
					migrateDepth = -1
				}
			}
			if ignored(line, modelOwnerRule) {
				continue
			}
			for _, m := range modelRef.FindAllStringSubmatch(code, -1) {
				name := m[1]
				owner, known := ownerOf[name]
				if !known || owner == service {
					continue
				}
				if inMigrate {
					out = append(out, Finding{filepath.ToSlash(rel), i + 1, modelOwnerRule,
						fmt.Sprintf("rt.AutoMigrate 了 %s：这张表归 %s（idl/devkit.yaml lint.model_owners），只有它迁移。本服务要自己的表就在 lib 里建新结构体并登记到自己名下", name, owner)})
					continue
				}
				if exempt[name] || seen[name] {
					continue
				}
				seen[name] = true
				out = append(out, Finding{filepath.ToSlash(rel), i + 1, modelOwnerRule,
					fmt.Sprintf("用了 %s 的表模型 %s：每个服务只碰自己的表（分库后别的库连不上）。要这份数据调 %s 的接口，量大或要按它筛选就订阅它的事件、在自己库里存只读副本；阶段 1 前确需过渡的，在 idl/devkit.yaml 的 lint.model_owner_exceptions 里给本服务加上并写明收回时间", owner, name, owner)})
			}
		}
	}
	return out
}
