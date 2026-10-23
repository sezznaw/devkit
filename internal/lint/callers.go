package lint

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Every method of an RPC service is named in the callers rules of each of its
// conf profiles (kit-common callers:, "*" covers every method): with
// callers.default deny an unlisted method takes only the service itself, so a
// new method nobody listed would answer 1007 to its first caller after the
// release. A method nobody else calls is listed with allow: [<the service>].

// idlMethodRE: `<return type> Name(` inside a service block.
var idlMethodRE = regexp.MustCompile(`^\s*[\w.<>, ]+\s+(\w+)\s*\(`)

func checkCallers(root, idlFile string) []Finding {
	methods := serviceBlockMethods(idlFile)
	if len(methods) == 0 {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(root, "conf", "*.yaml"))
	sort.Strings(files)
	var out []Finding
	for _, f := range files {
		rel := filepath.ToSlash(filepath.Join("conf", filepath.Base(f)))
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		rules, err := parseCallerRules(b)
		if err != nil {
			continue // the service's own config check reports it
		}
		named := map[string]bool{}
		for _, r := range rules {
			for _, m := range r.Methods {
				named[strings.TrimSpace(m)] = true
			}
		}
		if named["*"] {
			continue
		}
		var missing []string
		for _, m := range methods {
			if !named[m] {
				missing = append(missing, m)
			}
		}
		if len(missing) > 0 {
			out = append(out, Finding{rel, lineOfKey(b, "callers:"), "callers-coverage", fmt.Sprintf("callers.rules names no rule for %s: list who may call each (the services whose code calls it; a method only this service calls gets allow: [<this service>]), the same in every conf profile; with callers.default deny an unlisted method answers 1007 to everybody else", strings.Join(missing, ", "))})
		}
	}
	return out
}

// callerRule 是 conf 里 callers.rules 的一条：哪些方法（"*" 是全部）、谁可以调（服务名，"*" 是任何服务）。
// callers-coverage 和 rpc-uid-from-ctx 两条规则都读它。
type callerRule struct {
	Methods []string `yaml:"methods"`
	Allow   []string `yaml:"allow"`
}

// loadCallerRules 读一份 conf 的 callers.rules。读不到或不是合法 YAML 时返回错误，调用方跳过这一份
// （服务自己的配置检查会报）；没有 callers 段时返回空表。
func loadCallerRules(path string) ([]callerRule, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseCallerRules(b)
}

// parseCallerRules 从一份 conf 的内容里解析 callers.rules。
func parseCallerRules(b []byte) ([]callerRule, error) {
	var conf struct {
		Callers struct {
			Rules []callerRule `yaml:"rules"`
		} `yaml:"callers"`
	}
	if err := yaml.Unmarshal(b, &conf); err != nil {
		return nil, err
	}
	return conf.Callers.Rules, nil
}

// serviceBlockMethods lists the method names of the Thrift file's service
// blocks.
func serviceBlockMethods(path string) []string {
	fh, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	var out []string
	in := false
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(t, "service ") && strings.HasSuffix(t, "{"):
			in = true
		case in && strings.HasPrefix(t, "}"):
			in = false
		case in && !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "#"):
			if m := idlMethodRE.FindStringSubmatch(sc.Text()); m != nil {
				out = append(out, m[1])
			}
		}
	}
	return out
}

// lineOfKey is the 1-based line where a top-level key starts, 0 if absent.
func lineOfKey(b []byte, key string) int {
	for i, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, key) {
			return i + 1
		}
	}
	return 0
}
