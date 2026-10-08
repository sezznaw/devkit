// Package lint checks a service against the project's own rules: the ones
// idl/AGENTS.md tells an AI, enforced here regardless of who wrote the code.
// It is not a code-quality linter (go vet does that); every rule below is a
// team convention with a reason, and every finding says what to do instead.
package lint

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/sezznaw/devkit/internal/manifest"
)

// Finding is one violation.
type Finding struct {
	File    string
	Line    int
	Rule    string
	Message string
}

func (f Finding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: [%s] %s", f.File, f.Line, f.Rule, f.Message)
	}
	return fmt.Sprintf("%s: [%s] %s", f.File, f.Rule, f.Message)
}

// Options of one run.
type Options struct {
	// Root is the service directory (with .devkit/manifest.json).
	Root string
	// IDLDir is the project's idl checkout (for the service's IDL, the error
	// table and idl.lock); "" disables the IDL rules.
	IDLDir string
	// VendorService is the one service allowed to call third parties; ""
	// switches the vendor-only rule off.
	VendorService string
	// MoneyWords extend the built-in list of field names that must not be
	// floats (a betting project adds odds, stake, payout).
	MoneyWords []string
	// ErrorsFile is the business-code table, relative to IDLDir; "" switches
	// the error-code rule off.
	ErrorsFile string
	// Disable lists rule names to skip.
	Disable []string
}

// Ignore marks a line a rule must skip: `//devkit:lint-ignore <rule>` (Go)
// or `# devkit:lint-ignore <rule>` / `// devkit:lint-ignore <rule>` (Thrift).
var ignoreRE = regexp.MustCompile(`devkit:lint-ignore\s+([\w-]+)`)

func ignored(line, rule string) bool {
	m := ignoreRE.FindStringSubmatch(line)
	return m != nil && (m[1] == rule || m[1] == "all")
}

// Run applies every rule and returns the findings, sorted.
func Run(o Options) ([]Finding, error) {
	var out []Finding
	m, err := manifest.Load(o.Root)
	if err != nil {
		return nil, fmt.Errorf("%s is not a service created by devkit: %w", o.Root, err)
	}
	service := serviceName(m)
	goFiles, err := businessGoFiles(o.Root)
	if err != nil {
		return nil, err
	}
	moneyGo, moneyName := moneyPattern(o.MoneyWords)
	gc := goCheck{isVendor: service == o.VendorService, vendorRule: o.VendorService != "", moneyGo: moneyGo}
	for _, f := range goFiles {
		fs, err := checkGoFile(o.Root, f, gc)
		if err != nil {
			return nil, err
		}
		out = append(out, fs...)
	}
	out = append(out, checkFrameworkFiles(o.Root, m)...)
	if o.IDLDir != "" {
		idl := filepath.Join(o.IDLDir, service, service+".thrift")
		if _, err := os.Stat(idl); err == nil {
			fs, err := checkIDL(idl, isAPIService(m), moneyName)
			if err != nil {
				return nil, err
			}
			out = append(out, fs...)
		}
		if o.ErrorsFile != "" {
			out = append(out, checkErrorCodes(o.Root, goFiles, filepath.Join(o.IDLDir, o.ErrorsFile))...)
		}
		out = append(out, checkIDLLock(o.Root, o.IDLDir, service)...)
	}
	if len(o.Disable) > 0 {
		off := map[string]bool{}
		for _, r := range o.Disable {
			off[r] = true
		}
		kept := out[:0]
		for _, f := range out {
			if !off[f.Rule] {
				kept = append(kept, f)
			}
		}
		out = kept
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

func serviceName(m *manifest.Manifest) string {
	for _, c := range m.Components {
		if v := c.Vars["Service"]; v != "" {
			return v
		}
	}
	return ""
}

func isAPIService(m *manifest.Manifest) bool {
	_, ok := m.Components["hertz-service"]
	return ok
}

// generatedDirs are not the colleague's code.
var generatedDirs = map[string]bool{"kitex_gen": true, "hertz_gen": true, "router": true, "bin": true, "output": true, "vendor": true}

// businessGoFiles are the service's own .go files: not generated, not the
// framework's main.go, not tests (tests may fake a client).
func businessGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || generatedDirs[d.Name()]) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if strings.HasPrefix(rel, "cmd"+string(filepath.Separator)) && filepath.Base(p) == "main.go" {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

type pattern struct {
	re      *regexp.Regexp
	rule    string
	message string
}

var goPatterns = []pattern{
	{regexp.MustCompile(`\b(sql|gorm)\.Open\(`), "no-direct-middleware", "do not open the database yourself: mysql.enabled in conf and rt.DB / repo.New(rt)"},
	{regexp.MustCompile(`\bredis\.NewClient\(|\bredis\.NewClusterClient\(`), "no-direct-middleware", "do not create a Redis client: redis.enabled in conf and rt.Redis"},
	{regexp.MustCompile(`\bkgo\.NewClient\(|\bsarama\.New`), "no-direct-middleware", "do not create a Kafka client: kafka.enabled in conf and rt.Kafka.Publish / Subscribe"},
	{regexp.MustCompile(`\bhttp\.Client\{|\bhttp\.Get\(|\bhttp\.Post\(|\bhttp\.PostForm\(|\bresty\.New\(`), "no-direct-middleware", "do not call HTTP APIs yourself: providers.<name> in conf and rt.Provider(name) (ser-vendor only)"},
	{regexp.MustCompile(`\bs3\.NewFromConfig\(|\bs3\.New\(|\bminio\.New\(`), "no-direct-middleware", "do not create an S3 client: s3.enabled in conf and rt.S3"},
	{regexp.MustCompile(`\bgocron\.|\bcron\.New\(|\btime\.NewTicker\(`), "no-direct-middleware", "do not schedule work yourself: a job in app/jobs.go (make jobs / make job NAME=...)"},
	{regexp.MustCompile(`\bfmt\.Print(ln|f)?\(|\blog\.(Print|Printf|Println|Fatal|Fatalf|Fatalln|Panic|Panicf)\(`), "use-zlog", "log through zlog (zlog.Ctx(ctx).Info(...)), which carries the trace_id; not fmt / log"},
}

var vendorOnly = regexp.MustCompile(`\.Provider\(|\bwebhookx\.`)

// baseMoneyWords are field names that are money in any project.
var baseMoneyWords = []string{"amount", "balance", "price", "fee", "total", "credit", "debit", "money", "stake", "payout", "bonus", "turnover", "commission"}

// baseRateWords are field names that are a ratio in any project: odds, a
// fee rate, an FX rate, a percentage. They are common.Decimal (a string),
// never double and never Money.
var baseRateWords = []string{"odds", "rate", "ratio", "percent", "pct", "multiplier"}

var rateName = regexp.MustCompile(`(?i)(` + strings.Join(baseRateWords, "|") + `)`)

// isRateType: common.Decimal / Decimal, or a container of it.
func isRateType(typ string) bool {
	t := strings.TrimSpace(typ)
	if strings.HasSuffix(t, ".Decimal") || t == "Decimal" {
		return true
	}
	if i := strings.Index(t, "<"); i >= 0 && strings.HasSuffix(t, ">") {
		inner := strings.TrimSpace(t[i+1 : len(t)-1])
		if j := strings.LastIndex(inner, ","); j >= 0 {
			inner = strings.TrimSpace(inner[j+1:])
		}
		return isRateType(inner)
	}
	return false
}

// moneyPattern matches a Go field "<money word>... float64" or a Thrift
// field name, built from the base words plus the project's.
func moneyPattern(extra []string) (goField, name *regexp.Regexp) {
	words := append(append([]string{}, baseMoneyWords...), extra...)
	alt := strings.Join(words, "|")
	return regexp.MustCompile(`(?i)\b(` + alt + `)\w*\s+float(32|64)\b`), regexp.MustCompile(`(?i)(` + alt + `)`)
}

// countField: "total" and "count" style names are row counts, not money,
// unless the name says otherwise (total_amount, total_stake).
func countField(name string) bool {
	n := strings.ToLower(name)
	return n == "total" || n == "count" || strings.HasSuffix(n, "_total") || strings.HasSuffix(n, "_count") || strings.HasSuffix(n, "total") && !strings.Contains(n, "_")
}

// isMoneyType: common.Money, Money, or a container of it. A bare i64 named
// like money (amount, balance, stake...) is the mistake the rule exists for.
func isMoneyType(typ string) bool {
	t := strings.TrimSpace(typ)
	if strings.HasSuffix(t, ".Money") || t == "Money" {
		return true
	}
	if i := strings.Index(t, "<"); i >= 0 && strings.HasSuffix(t, ">") {
		inner := strings.TrimSpace(t[i+1 : len(t)-1])
		if j := strings.LastIndex(inner, ","); j >= 0 {
			inner = strings.TrimSpace(inner[j+1:])
		}
		return isMoneyType(inner)
	}
	return false
}

type goCheck struct {
	isVendor   bool
	vendorRule bool
	moneyGo    *regexp.Regexp
}

func checkGoFile(root, rel string, c goCheck) ([]Finding, error) {
	f, err := os.Open(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Finding
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") || t == "" {
			continue
		}
		code := line
		if i := strings.Index(line, "//"); i >= 0 {
			code = line[:i]
		}
		for _, p := range goPatterns {
			if p.re.MatchString(code) && !ignored(line, p.rule) {
				out = append(out, Finding{rel, n, p.rule, p.message})
			}
		}
		if c.moneyGo.MatchString(code) && !ignored(line, "no-float-money") {
			out = append(out, Finding{rel, n, "no-float-money", "money is never a float: int64 in the smallest unit plus a currency code (ask before adding a money field)"})
		}
		if c.vendorRule && !c.isVendor && vendorOnly.MatchString(code) && !ignored(line, "vendor-only") {
			out = append(out, Finding{rel, n, "vendor-only", "third-party calls and callbacks live in the vendor service only (the one that may leave the cluster); move this to ser-vendor and expose an RPC"})
		}
	}
	return out, sc.Err()
}

// checkFrameworkFiles: a managed file that differs from what devkit wrote
// cannot be upgraded any more.
func checkFrameworkFiles(root string, m *manifest.Manifest) []Finding {
	var out []Finding
	for name := range m.Components {
		states, err := m.CheckFiles(root, name)
		if err != nil {
			continue
		}
		for rel, st := range states {
			if st != manifest.Unchanged {
				out = append(out, Finding{rel, 0, "framework-file", fmt.Sprintf("%s: this file belongs to the framework (%s) and is replaced by devkit update; put service code in app/, handler/, repo/ instead, or run devkit update --force to restore it", st, name)})
			}
		}
	}
	return out
}

var (
	structRE = regexp.MustCompile(`^\s*struct\s+(\w+)\s*\{`)
	fieldRE  = regexp.MustCompile(`^\s*\d+\s*:\s*(?:optional\s+|required\s+)?([\w.<>, ]+?)\s+(\w+)`)
	methodRE = regexp.MustCompile(`^\s*(\w+)\s+(\w+)\s*\(\s*\d+\s*:\s*(\w+)\s+\w+\s*\)\s*(\(.*\))?`)
	// A method whose name carries a read verb (anywhere, as a camel-case
	// word: MemberGetProfile, EgressCheck, ListOrders) changes nothing and
	// needs no request_id. Token issuance counts as a read.
	readName = regexp.MustCompile(`(^|[a-z0-9])(Get|List|Query|Find|Search|Count|Check|Ping|Describe|Fetch|Has|Is|Exists|Verify|Preview|Calc|Validate|Health|Stat|Stats|Version|Info|Status|History|Config|Token|Export|Download)([A-Z]|$)`)
)

// checkIDL reads the service's Thrift: money fields as double, write methods
// without request_id, methods without a comment.
func checkIDL(path string, api bool, moneyName *regexp.Regexp) ([]Finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	rel := path
	var out []Finding
	// First pass: struct fields.
	fields := map[string]map[string]bool{} // struct -> field names
	cur := ""
	for i, line := range lines {
		if m := structRE.FindStringSubmatch(line); m != nil {
			cur = m[1]
			fields[cur] = map[string]bool{}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "}") {
			cur = ""
			continue
		}
		if cur == "" {
			continue
		}
		if m := fieldRE.FindStringSubmatch(line); m != nil {
			typ, name := strings.TrimSpace(m[1]), m[2]
			fields[cur][name] = true
			isRate := rateName.MatchString(name)
			isMoney := moneyName.MatchString(name) && !countField(name) && !isRate
			switch {
			case typ == "double" && (isMoney || isRate) && !ignored(line, "no-float-money"):
				out = append(out, Finding{rel, i + 1, "no-float-money", fmt.Sprintf("%s.%s is double: money is common.Money and a rate is common.Decimal (strings; floats cannot hold 0.1)", cur, name)})
			case isMoney && !isMoneyType(typ) && !ignored(line, "money-type"):
				out = append(out, Finding{rel, i + 1, "money-type", fmt.Sprintf("%s.%s is %s: a money field is common.Money (include \"../common/common.thrift\"; a decimal amount string plus a currency code), or list<common.Money>", cur, name, typ)})
			case isRate && !isRateType(typ) && !ignored(line, "rate-type"):
				out = append(out, Finding{rel, i + 1, "rate-type", fmt.Sprintf("%s.%s is %s: odds, fee rates, FX rates and percentages are common.Decimal (include \"../common/common.thrift\"; a decimal string like \"1.85\")", cur, name, typ)})
			}
		}
	}
	// Second pass: methods.
	inService := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "service ") {
			inService = true
			continue
		}
		if inService && strings.HasPrefix(t, "}") {
			inService = false
			continue
		}
		if !inService {
			continue
		}
		m := methodRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		method, req, ann := m[2], m[3], m[4]
		prev := ""
		for j := i - 1; j >= 0; j-- {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			prev = strings.TrimSpace(lines[j])
			break
		}
		if !strings.HasPrefix(prev, "//") && !strings.HasPrefix(prev, "/*") && !strings.HasSuffix(prev, "*/") && !ignored(line, "method-comment") {
			out = append(out, Finding{rel, i + 1, "method-comment", fmt.Sprintf("%s has no comment: the first sentence is the endpoint's title in /docs", method)})
		}
		isGet := strings.Contains(ann, "api.get")
		if readName.MatchString(method) || isGet || ignored(line, "request-id") {
			continue
		}
		if !fields[req]["request_id"] {
			what := "RPC method"
			if api {
				what = "POST endpoint"
			}
			out = append(out, Finding{rel, i + 1, "request-id", fmt.Sprintf("%s %s looks like a write (name does not read like a query) but %s has no request_id field; add `1: string request_id` so the framework makes it idempotent, or mark the line `// devkit:lint-ignore request-id` if it truly changes nothing", what, method, req)})
		}
	}
	return out, nil
}

var bizCodeRE = regexp.MustCompile(`NewBizStatusError\(\s*(\d{3,5})\s*,|\bCode\w*\s*=\s*(\d{3,5})\b`)

// checkErrorCodes: every business code used in Go is a row of idl/errors.md.
func checkErrorCodes(root string, goFiles []string, errorsMD string) []Finding {
	data, err := os.ReadFile(errorsMD)
	if err != nil {
		return nil
	}
	registered := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\|\s*(\d{3,5})\s*\|`).FindAllStringSubmatch(string(data), -1) {
		registered[m[1]] = true
	}
	var out []Finding
	for _, rel := range goFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range bizCodeRE.FindAllStringSubmatch(line, -1) {
				code := m[1]
				if code == "" {
					code = m[2]
				}
				if code == "0" || registered[code] || ignored(line, "error-code") {
					continue
				}
				out = append(out, Finding{rel, i + 1, "error-code", fmt.Sprintf("business code %s is not in idl/errors.md; register it there first (one meaning per code, never reused)", code)})
			}
		}
	}
	return out
}
