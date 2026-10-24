package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// 免登录契约（2026-10-24）：网关没有登录态时能调的 RPC 方法，在被调服务的 IDL 方法注释上标 `// @anonymous`，
// 运行时的名单仍是该服务 conf 的 identity.anonymous（kit-common identity.require_login）。两条规则把它们串起来：
//
//	anonymous-coverage（RPC 服务）：每份 conf 的 identity.anonymous 必须与 IDL 上标了 @anonymous 的方法集合一致。
//	public-anonymous（网关）：每个 @public 接口（以及 app.Setup 和 h.Use 挂的中间件）沿调用链调到的 deps.X.Method，
//	    被调服务的 IDL 上必须有 @anonymous；否则 enforce 下被调方直接回 1004。
//
// 背景：IDL 是所有服务共用的检出，CI 里网关看不到别的服务的 conf，所以契约必须写在 IDL 上；dev 切 enforce 之前
// 这次核对是手工脚本跑的（2026-10-24），这两条规则把它固化下来。

const (
	anonymousCoverageRule = "anonymous-coverage"
	publicAnonymousRule   = "public-anonymous"
)

var (
	anonymousTag = regexp.MustCompile(`@anonymous\b`)
	publicTag    = regexp.MustCompile(`@public\b`)
	// depsClient 认 deps/deps.go 里的客户端字段：`Member sermemberservice.Client`（可带 var）。
	depsClient = regexp.MustCompile(`^\s*(?:var\s+)?([A-Z]\w*)\s+(\w+)service\.Client\b`)
	// depsCall 认网关代码里对 RPC 客户端的调用：deps.Member.Register(。
	depsCall = regexp.MustCompile(`\bdeps\.([A-Z]\w*)\.([A-Z]\w*)\(`)
	// useMiddleware 认 app 里挂中间件：h.Use(gate.Middleware, accessgate.Middleware(deps.Admin))。
	useMiddleware = regexp.MustCompile(`\.Use\(([^()]*(?:\([^()]*\))?[^()]*)\)`)
	// goFuncDecl 认一个顶层函数 / 方法声明，取名字。
	goFuncDecl = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?(\w+)\s*\(`)
	// thriftService 认 thrift 的 service 声明。
	thriftService = regexp.MustCompile(`^\s*service\s+(\w+)\b`)
	// identCall 认函数体里的调用：可带一个限定词（包名或变量名）。不带限定词的只在同一目录里找，
	// 带限定词的只在目录名等于限定词的包里找（rolepin.Load → internal/rolepin），避免同名函数跨包串线。
	identCall = regexp.MustCompile(`(?:\b(\w+)\.)?\b([A-Za-z_]\w*)\(`)
)

// ---------------------------------------------------------------- anonymous-coverage

// loadAnonymous 读一份 conf 的 identity.anonymous；present 表示写了这个键（空表也算写了）。
func loadAnonymous(path string) (list []string, present bool, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var conf struct {
		Identity struct {
			Anonymous *[]string `yaml:"anonymous"`
		} `yaml:"identity"`
	}
	if err := yaml.Unmarshal(b, &conf); err != nil {
		return nil, false, err
	}
	if conf.Identity.Anonymous == nil {
		return nil, false, nil
	}
	return *conf.Identity.Anonymous, true, nil
}

// checkAnonymousCoverage 跑规则 anonymous-coverage：RPC 服务每份 conf 的 identity.anonymous 与 IDL 上的 @anonymous 一致。
func checkAnonymousCoverage(root, idlFile string) []Finding {
	tagged := idlTaggedMethods(idlFile, anonymousTag)
	files, _ := filepath.Glob(filepath.Join(root, "conf", "*.yaml"))
	sort.Strings(files)
	var out []Finding
	for _, f := range files {
		rel := filepath.ToSlash(filepath.Join("conf", filepath.Base(f)))
		list, present, err := loadAnonymous(f)
		if err != nil {
			continue // 服务自己的配置检查会报
		}
		if !present && len(tagged) == 0 {
			continue
		}
		listed := setOf(list)
		var missing []string
		for m := range tagged {
			if !listed[m] {
				missing = append(missing, m)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			out = append(out, Finding{rel, 0, anonymousCoverageRule,
				fmt.Sprintf("IDL 里标了 // @anonymous 的方法 %s 不在这份 conf 的 identity.anonymous 里：网关没有登录态调它们会被 identity.require_login 拒绝（1004）。四份 conf 都要列上",
					strings.Join(missing, "、"))})
		}
		for _, m := range list {
			m = strings.TrimSpace(m)
			switch {
			case m == "*":
				out = append(out, Finding{rel, 0, anonymousCoverageRule, "identity.anonymous 写了 *：免登录的方法要逐个列出并在 IDL 上标 // @anonymous，否则所有方法都不要求登录者"})
			case !tagged[m]:
				out = append(out, Finding{rel, 0, anonymousCoverageRule,
					fmt.Sprintf("identity.anonymous 里的 %s 在 IDL 方法注释上没有 // @anonymous：它是网关侧 lint（public-anonymous）判断「免登录能不能调」的依据，和名单同一次改——方法确实免登录就在 IDL 注释加上，否则从名单里去掉", m)})
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- public-anonymous

// goFunc 是网关代码里的一个函数：所在文件、起始行、各行代码（去注释）。
type goFunc struct {
	rel   string
	dir   string // 所在目录（Go 包）：不带包名的调用只在同一目录里找
	start int
	lines []string
	raw   []string // 原始行，lint-ignore 要看它
}

// gatewayFuncs 把网关的业务 Go 文件切成函数，按名字归组（同名函数都算，宁可多看）。
func gatewayFuncs(root string, goFiles []string) map[string][]goFunc {
	out := map[string][]goFunc{}
	for _, rel := range goFiles {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		var cur *goFunc
		flush := func() {
			if cur != nil {
				name := goFuncDecl.FindStringSubmatch(cur.raw[0])
				if name != nil {
					out[name[1]] = append(out[name[1]], *cur)
				}
			}
			cur = nil
		}
		for i, line := range lines {
			if goFuncDecl.MatchString(line) {
				flush()
				cur = &goFunc{rel: filepath.ToSlash(rel), dir: filepath.ToSlash(filepath.Dir(rel)), start: i + 1}
			}
			if cur != nil {
				cur.lines = append(cur.lines, codeOf(line))
				cur.raw = append(cur.raw, line)
			}
		}
		flush()
	}
	return out
}

// depsClients 读 deps/deps.go：字段名 → 客户端包名（sermemberservice）。
func depsClients(root string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(root, "deps", "deps.go"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		if m := depsClient.FindStringSubmatch(line); m != nil {
			out[m[1]] = strings.ToLower(m[2] + "service")
		}
	}
	return out
}

// idlServices 扫 idl 目录：thrift service 名（小写）→ 该服务的 IDL 文件。
func idlServices(idlDir string) map[string]string {
	out := map[string]string{}
	dirs, _ := os.ReadDir(idlDir)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		f := filepath.Join(idlDir, d.Name(), d.Name()+".thrift")
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if m := thriftService.FindStringSubmatch(line); m != nil {
				out[strings.ToLower(m[1])] = f
			}
		}
	}
	return out
}

// middlewareRoots 回 app/*.go 里 h.Use(...) 挂上的函数名，加上 Setup（它登记审计等钩子）。
func middlewareRoots(root string) []string {
	var roots []string
	files, _ := filepath.Glob(filepath.Join(root, "app", "*.go"))
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range useMiddleware.FindAllStringSubmatch(codeOnly(string(data)), -1) {
			for _, arg := range strings.Split(m[1], ",") {
				arg = strings.TrimSpace(arg)
				if i := strings.Index(arg, "("); i >= 0 {
					arg = arg[:i]
				}
				if arg != "" {
					roots = append(roots, arg) // 保留 "gate.Middleware" 这样的限定词，按目录名找
				}
			}
		}
	}
	return append(roots, "app.Setup") // Setup 登记审计等钩子，放最后：报告归到更具体的中间件名下
}

// codeOnly 去掉每一行的 // 注释。
func codeOnly(src string) string {
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		lines[i] = codeOf(l)
	}
	return strings.Join(lines, "\n")
}

// rpcCall 是沿调用链找到的一次 deps.X.Method 调用。
type rpcCall struct {
	field, method string
	rel           string
	line          int
	raw           string
}

// candidates 按「限定词 + 名字」找函数实例：没有限定词 → 和 from 同目录的同名函数；有限定词 → 目录名等于
// 限定词的包里的同名函数（deps 是客户端，不算）。
func candidates(funcs map[string][]goFunc, from *goFunc, qual, name string) []goFunc {
	var out []goFunc
	for _, fn := range funcs[name] {
		switch {
		case qual == "" && from != nil && fn.dir == from.dir:
			out = append(out, fn)
		case qual != "" && qual != "deps" && filepath.Base(fn.dir) == qual:
			out = append(out, fn)
		case qual == "" && from == nil:
			out = append(out, fn)
		}
	}
	return out
}

// reachableCalls 从一个函数实例出发沿调用链递归（去重），收集所有 deps.X.Method 调用。
func reachableCalls(fn goFunc, funcs map[string][]goFunc, seen map[string]bool) []rpcCall {
	key := fmt.Sprintf("%s:%d", fn.rel, fn.start)
	if seen[key] {
		return nil
	}
	seen[key] = true
	var out []rpcCall
	for i, code := range fn.lines {
		for _, m := range depsCall.FindAllStringSubmatch(code, -1) {
			out = append(out, rpcCall{field: m[1], method: m[2], rel: fn.rel, line: fn.start + i, raw: fn.raw[i]})
		}
		for _, c := range identCall.FindAllStringSubmatch(code, -1) {
			for _, next := range candidates(funcs, &fn, c[1], c[2]) {
				out = append(out, reachableCalls(next, funcs, seen)...)
			}
		}
	}
	return out
}

// checkPublicAnonymous 跑规则 public-anonymous（只对网关）：@public 接口和中间件调到的 RPC 方法必须在被调服务的 IDL 上标 @anonymous。
func checkPublicAnonymous(root, idlDir, service string, goFiles []string) []Finding {
	domainFiles, _ := filepath.Glob(filepath.Join(idlDir, service, "*.thrift"))
	sort.Strings(domainFiles)
	public := map[string]bool{}
	for _, f := range domainFiles {
		for m := range idlTaggedMethods(f, publicTag) {
			public[m] = true
		}
	}
	funcs := gatewayFuncs(root, goFiles)
	clients := depsClients(root)
	if len(funcs) == 0 || len(clients) == 0 {
		return nil
	}
	services := idlServices(idlDir)
	taggedCache := map[string]map[string]bool{}
	anonymousIn := func(idlFile, method string) bool {
		t, ok := taggedCache[idlFile]
		if !ok {
			t = idlTaggedMethods(idlFile, anonymousTag)
			taggedCache[idlFile] = t
		}
		return t[method]
	}
	type entry struct {
		name string
		kind string // 接口 / 中间件
		fns  []goFunc
	}
	var roots []entry
	publicNames := make([]string, 0, len(public))
	for m := range public {
		publicNames = append(publicNames, m)
	}
	sort.Strings(publicNames)
	for _, m := range publicNames {
		var fns []goFunc
		for _, fn := range funcs[m] {
			if strings.HasPrefix(fn.dir, "handler/") || fn.dir == "handler" {
				fns = append(fns, fn)
			}
		}
		roots = append(roots, entry{m, "接口", fns})
	}
	for _, m := range middlewareRoots(root) {
		qual, name := "", m
		if i := strings.LastIndex(m, "."); i >= 0 {
			qual, name = m[:i], m[i+1:]
		}
		roots = append(roots, entry{m, "中间件", candidates(funcs, nil, qual, name)})
	}
	var out []Finding
	reported := map[string]bool{}
	for _, r := range roots {
		var calls []rpcCall
		seen := map[string]bool{}
		for _, fn := range r.fns {
			calls = append(calls, reachableCalls(fn, funcs, seen)...)
		}
		for _, c := range calls {
			pkg, ok := clients[c.field]
			if !ok {
				continue
			}
			idlFile, ok := services[pkg]
			if !ok || anonymousIn(idlFile, c.method) || ignored(c.raw, publicAnonymousRule) {
				continue
			}
			key := fmt.Sprintf("%s:%d:%s", c.rel, c.line, c.method)
			if reported[key] {
				continue
			}
			reported[key] = true
			callee := filepath.Base(filepath.Dir(idlFile))
			what := fmt.Sprintf("@public 接口 %s", r.name)
			if r.kind == "中间件" {
				what = fmt.Sprintf("%s %s（对免登录的请求也会跑）", r.kind, r.name)
			}
			out = append(out, Finding{c.rel, c.line, publicAnonymousRule,
				fmt.Sprintf("%s 调了 %s.%s，它在 %s 的 IDL 方法注释上没有 // @anonymous：此时网关没有登录态，被调方按 identity.require_login 会回 1004。方法本来就该免登录的，在 %s 的 IDL 方法注释加 // @anonymous 并写进它四份 conf 的 identity.anonymous（lint anonymous-coverage 会核对）；这次调用其实带着刚签出的登录态（kitexx.WithIdentity + WithToken）或只在登录后运行的，在这一行行尾写 //devkit:lint-ignore public-anonymous 并说明",
					what, callee, c.method, callee, callee)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}
