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
	// ReportService is the one service allowed to open the report database
	// (report.enabled in conf); "" switches the rule off.
	ReportService string
	// VendorService is the one service allowed to call third parties; ""
	// switches the vendor-only rule off.
	VendorService string
	// MoneyWords extend the built-in list of field names that must not be
	// floats (a betting project adds odds, stake, payout).
	MoneyWords []string
	// ErrorsDir is the directory of error-code Thrift files, relative to
	// IDLDir (rules error-code, error-dup, error-range, error-comment);
	// ErrorRanges maps a file name to its allowed ranges ("1000-1999,5000-5999").
	ErrorsDir   string
	ErrorRanges map[string]string
	// ErrorsFile is the business-code table, relative to IDLDir; "" switches
	// the error-code rule off.
	ErrorsFile string
	// Disable lists rule names to skip.
	Disable []string
	// HandlerMethods is the most RPC methods one handler file may hold
	// (rule handler-split); 0 means the default, 8.
	HandlerMethods int
	// ProjectDir is the project directory holding the kit-common/ checkout;
	// "" skips the common-checkout rule (set it for one service of a run).
	ProjectDir string
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
	gc := goCheck{isVendor: service == o.VendorService, vendorRule: o.VendorService != "", isAPI: isAPIService(m), moneyGo: moneyGo}
	for _, f := range goFiles {
		fs, err := checkGoFile(o.Root, f, gc)
		if err != nil {
			return nil, err
		}
		out = append(out, fs...)
	}
	out = append(out, checkFrameworkFiles(o.Root, m)...)
	out = append(out, checkCommonVersion(o.Root, m)...)
	out = append(out, checkCommonCheckout(o.ProjectDir)...)
	out = append(out, checkTests(o.Root)...)
	if !isAPIService(m) {
		out = append(out, checkHandlerSplit(o.Root, o.HandlerMethods)...)
	}
	if o.ReportService != "" && service != o.ReportService {
		out = append(out, checkReportOnly(o.Root, o.ReportService)...)
	}
	if o.IDLDir != "" {
		// The service's IDL directory: one file for an RPC service, one file
		// per domain for a gateway (plus a docs-only file).
		files, _ := filepath.Glob(filepath.Join(o.IDLDir, service, "*.thrift"))
		sort.Strings(files)
		for _, idl := range files {
			fs, err := checkIDL(idl, isAPIService(m), moneyName)
			if err != nil {
				return nil, err
			}
			out = append(out, fs...)
		}
		if o.ErrorsDir != "" {
			out = append(out, checkErrorDir(o.Root, goFiles, filepath.Join(o.IDLDir, o.ErrorsDir), o.ErrorRanges)...)
		} else if o.ErrorsFile != "" {
			out = append(out, checkErrorCodes(o.Root, goFiles, filepath.Join(o.IDLDir, o.ErrorsFile))...)
		}
		if !isAPIService(m) {
			out = append(out, checkCallers(o.Root, filepath.Join(o.IDLDir, service, service+".thrift"))...)
		}
		out = append(out, checkIDLLock(o.Root, o.IDLDir, service)...)
		out = append(out, checkIDLDeps(o.Root, o.IDLDir)...)
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
	{regexp.MustCompile(`\bserver\.WithLimit\(|\bserver\.WithLimitReporter\(|\blimit\.Option\{`), "no-direct-middleware", "do not install a Kitex limiter yourself: the limits: section in conf (max_in_flight, max_qps, max_connections) refuses with code 5003 and counts rpc_server_rejected_total"},
	{regexp.MustCompile(`\bfmt\.Print(ln|f)?\(|\blog\.(Print|Printf|Println|Fatal|Fatalf|Fatalln|Panic|Panicf)\(`), "use-zlog", "log through zlog (zlog.Ctx(ctx).Info(...)), which carries the trace_id; not fmt / log"},
	// The schema lives in the project's lib/model/mysql and the owning service applies it with rt.AutoMigrate.
	{regexp.MustCompile(`(?i)\bCREATE\s+TABLE\b|\bALTER\s+TABLE\b|\bDROP\s+TABLE\b`), "schema-in-lib", "no DDL in a service: the table is a struct in the project's lib/model/mysql (column and type on every field) and app.Setup calls rt.AutoMigrate(model.X{}) for the tables this service owns"},
	{regexp.MustCompile(`([^t]|[^r]t)\.AutoMigrate\(|\.Migrator\(\)`), "schema-in-lib", "migrate through rt.AutoMigrate(model.X{}, ...) (Redis lock, explicit types, logged differences, off in uat/prod), not GORM's AutoMigrate / Migrator directly"},
	{regexp.MustCompile(`^func \(\w+ ?\*?\w+\) TableName\(\) string`), "schema-in-lib", "a table struct belongs in the project's lib/model/mysql (one file per table, TableName there); the service imports it instead of defining its own"},
}

var vendorOnly = regexp.MustCompile(`\.Provider\(|\bwebhookx\.`)

// gatewayFileIn: a gateway handler reading an uploaded file out of the
// request (multipart), or writing a body into S3 itself. Files never pass
// through the gateway (1 MiB body limit): the service presigns a PUT link
// (GetUploadURL), the client uploads to S3 directly and submits the key.
var gatewayFileIn = regexp.MustCompile(`\bc\.(FormFile|MultipartForm)\(|\bmultipart\.(NewReader|File)\b|\.S3\.(Put|PutBytes)\(|\.S3\.Public\(\)\.(Put|PutBytes)\(`)

// baseMoneyWords are field names that are money in any project.
var baseMoneyWords = []string{"amount", "balance", "price", "fee", "total", "credit", "debit", "money", "stake", "payout", "bonus", "turnover", "commission"}

// baseRateWords are field names that are a ratio in any project: odds, a
// fee rate, an FX rate, a percentage. They are common.Decimal (a string),
// never double and never Money.
var baseRateWords = []string{"odds", "rate", "ratio", "percent", "pct", "multiplier"}

// Field names are matched as snake-case words, not substrings: "duration_ms"
// is not a ratio and "feedback" is not a fee.
var rateName = regexp.MustCompile(`(?i)(^|_)(` + strings.Join(baseRateWords, "|") + `)(_|$)`)

// fileWords are the words that make a field about a file (an image, a
// document, an export...).
const fileWords = `image|img|file|avatar|logo|icon|photo|picture|pic|banner|cover|thumb|thumbnail|doc|document|attachment|video|audio|media|cert|certificate|license|licence|receipt|voucher|qrcode|qr|kyc|idcard|id_card|passport|selfie|export|upload|download`

// fileWord: a field about a file. Rule file-field: such a field is
// `<name>_key` in a request and `<name>_url` in a response; never `_path`,
// never `_url` in a request, and never the content itself in a request
// (binary, or a `_data` / `_base64` / `_content` / `_bytes` / `_blob`
// string, or a bare `avatar` string): the client uploads to S3 through the
// link GetUploadURL gives it and sends the key.
var fileWord = regexp.MustCompile(`(?i)(^|_)(` + fileWords + `)(_|$)`)

// fileLastWord: the field's last word is a file word (`avatar`,
// `kyc_passport`), so the field is the file itself rather than something
// about it (`file_name`, `document_type`, `export_format`).
var fileLastWord = regexp.MustCompile(`(?i)(^|_)(` + fileWords + `)$`)

// fileContentSuffix: a request field carrying the bytes of a file.
var fileContentSuffix = regexp.MustCompile(`(?i)_(data|base64|content|bytes|blob)$`)

// uploadMethod: a method name that says "upload" without being the one
// upload method a service has (GetUploadURL, kind in the request).
var uploadMethod = regexp.MustCompile(`(?i)upload|presign`)

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
	return regexp.MustCompile(`(?i)\b(` + alt + `)\w*\s+float(32|64)\b`), regexp.MustCompile(`(?i)(^|_)(` + alt + `)(_|$)`)
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
	isAPI      bool
	moneyGo    *regexp.Regexp
}

// directPublish is rt.Kafka.Publish / s.events.Publish: fine on its own,
// wrong inside code that also runs a database transaction (the event
// belongs in it: PublishTx).
var directPublish = regexp.MustCompile(`\b(Kafka|events|Events|kafka|producer|Producer)\.Publish\(`)

var runsTransaction = regexp.MustCompile(`\.Transaction\(|WithTx\(`)

// handRolledLock is SetNX in service code: a lock written by hand misses
// the token, the extension and the release; redisx.WithLock has them.
var handRolledLock = regexp.MustCompile(`\.SetNX\(`)

// localMutex in handler/ or repo/ code: it locks one replica only.
var localMutex = regexp.MustCompile(`\bsync\.(RW)?Mutex\b`)

// inProcessTimer is a timer that dies with the process: a business delay
// ("cancel in 15 minutes") belongs in rt.Delay.Schedule, a repeat in a job.
var inProcessTimer = regexp.MustCompile(`\btime\.(AfterFunc|NewTimer)\(`)

func checkGoFile(root, rel string, c goCheck) ([]Finding, error) {
	whole, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}
	inTx := runsTransaction.Match(whole)
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
		if bizLiteral.MatchString(code) && !ignored(line, "error-msg") {
			out = append(out, Finding{rel, n, "error-msg", "the message of a business error is the comment in idl/errors/*.thrift: return errcode.Err(errcode.<Name>) (or errcode.Errf(code, detail) to append a detail), not kerrors.NewBizStatusError with a hand-written string"})
		}
		if c.isAPI && strings.HasPrefix(rel, "handler/") && (strings.Contains(code, "c.JSON(") || strings.Contains(code, "c.String(") || strings.Contains(code, "c.AbortWithStatusJSON(")) && !ignored(line, "envelope-write") {
			out = append(out, Finding{rel, n, "envelope-write", "answer with hertzx.OK(c, &data) / hertzx.Fail(ctx, c, err) / hertzx.FailCode(c, code, msg): they add the {code, msg, data} envelope once, so every endpoint looks the same (hz's generated c.JSON(consts.StatusOK, resp) is the stub to replace)"})
		}
		if strings.Contains(code, "c.BindAndValidate(") && !ignored(line, "bind") {
			out = append(out, Finding{rel, n, "bind", "use hertzx.Bind(c, &req) (returns false after answering {code:1001, msg:\"<field>: <rule>\"}) instead of c.BindAndValidate: every parameter error must look the same; the IDL's api.vd rules are checked by it"})
		}
		if handRolledLock.MatchString(code) && !ignored(line, "lock") {
			out = append(out, Finding{rel, n, "lock", "a lock written by hand (SetNX) has no owner token, no extension and no safe release: use redisx.WithLock(ctx, rt.Redis, name, ttl, fn) (redisx.Wait(d) to wait a little)"})
		}
		if (strings.HasPrefix(rel, "handler"+string(filepath.Separator)) || strings.HasPrefix(rel, "repo"+string(filepath.Separator))) && localMutex.MatchString(code) && !ignored(line, "mutex") {
			out = append(out, Finding{rel, n, "mutex", "a sync.Mutex locks this replica only; the service runs several, so exclusion across them is redisx.WithLock(ctx, rt.Redis, name, ttl, fn) (a mutex that only guards in-process state: //devkit:lint-ignore mutex)"})
		}
		if v, bad := checkRequestIDSource(rel, code); bad && !ignored(line, "request-id-source") {
			out = append(out, Finding{rel, n, "request-id-source", "request_id set from " + v + ": a write call made while handling a request passes that request's id, req.RequestId / kitexx.RequestID(ctx), or kitexx.SubRequestID(ctx, part) for several calls of one method; a new id (UUID, time, counter) makes a retry run the write again. A job or consumer sets its own repeatable id once with ctx = kitexx.WithRequestID(ctx, \"settle:\"+key) and passes kitexx.RequestID(ctx)"})
		}
		if inProcessTimer.MatchString(code) && !ignored(line, "delay") {
			out = append(out, Finding{rel, n, "delay", "a timer dies with the process: something to run at a time (cancel an unpaid order, close bets at kick-off) is rt.Delay.Schedule(ctx, tx, kind, key, at, payload) with rt.Delay.Handle(kind, fn) in app.Setup; repeating work is a job (--job)"})
		}
		if inTx && directPublish.MatchString(code) && !ignored(line, "outbox") {
			out = append(out, Finding{rel, n, "outbox", "this file runs a database transaction: an event that belongs with the change goes through rt.Kafka.PublishTx(ctx, tx, ...) inside it (the outbox), not Publish; keep Publish only for events without a transaction behind them"})
		}
		if c.moneyGo.MatchString(code) && !ignored(line, "no-float-money") {
			out = append(out, Finding{rel, n, "no-float-money", "money is never a float: common.Money in the IDL (a decimal string plus a currency code) and moneyx.Money in Go (ask before adding a money field)"})
		}
		if c.isAPI && strings.HasPrefix(rel, "handler"+string(filepath.Separator)) && gatewayFileIn.MatchString(code) && !ignored(line, "upload-through-gateway") {
			out = append(out, Finding{rel, n, "upload-through-gateway", "a file never passes through the gateway (the body limit is 1 MiB): the owning RPC service presigns a PUT link (its GetUploadURL method, one per service, kind says which file), the client uploads to S3 directly and submits the key to the write endpoint (`<name>_key`); the gateway only forwards"})
		}
		if c.vendorRule && !c.isVendor && vendorOnly.MatchString(code) && !ignored(line, "vendor-only") {
			out = append(out, Finding{rel, n, "vendor-only", "third-party calls and callbacks live in the vendor service only (the one that may leave the cluster); move this to ser-vendor and expose an RPC"})
		}
	}
	return out, sc.Err()
}

var reportEnabled = regexp.MustCompile(`(?m)^report:\s*\n(?:[ \t]+.*\n)*?[ \t]+enabled:[ \t]*true\b`)

// checkReportOnly: only the report service reads the report database (a
// copy a few seconds behind); any other service with report.enabled is
// about to decide something on stale data.
func checkReportOnly(root, reportService string) []Finding {
	var out []Finding
	files, _ := filepath.Glob(filepath.Join(root, "conf", "*.yaml"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if reportEnabled.Match(b) {
			rel, _ := filepath.Rel(root, f)
			out = append(out, Finding{rel, 0, "report-only", fmt.Sprintf("report.enabled is on: only %s reads the report database (StarRocks, a copy a few seconds behind); other services read MySQL through the service that owns the table, and ask %s for statistics", reportService, reportService)})
		}
	}
	return out
}

// checkTests: a handler package with code but no test at all. One test that
// builds the testx Runtime and calls the handler is the floor.
func checkTests(root string) []Finding {
	var out []Finding
	dirs, _ := filepath.Glob(filepath.Join(root, "handler"))
	sub, _ := filepath.Glob(filepath.Join(root, "handler", "*"))
	for _, d := range sub {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			dirs = append(dirs, d)
		}
	}
	for _, d := range dirs {
		goFiles, _ := filepath.Glob(filepath.Join(d, "*.go"))
		code, tests := 0, 0
		for _, f := range goFiles {
			if strings.HasSuffix(f, "_test.go") {
				tests++
			} else {
				code++
			}
		}
		if code > 0 && tests == 0 {
			rel, _ := filepath.Rel(root, d)
			out = append(out, Finding{rel, 0, "tests", "no test in this handler package: add one that builds testx.New(t, service, ...) and calls a handler (see idl/AGENTS.md 写测试); run it with make test"})
		}
	}
	return out
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
	// fieldFullRE also captures the field id and the optional/required keyword.
	fieldFullRE = regexp.MustCompile(`^\s*(\d+)\s*:\s*(optional\s+|required\s+)?([\w.<>, ]+?)\s+(\w+)`)
	methodRE    = regexp.MustCompile(`^\s*(\w+)\s+(\w+)\s*\(\s*\d+\s*:\s*(\w+)\s+\w+\s*\)\s*(\(.*\))?`)
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
	isCommonFile := filepath.Base(filepath.Dir(path)) == "common"
	totalLines := map[string]int{}   // struct -> line of its `total` field
	listStructs := map[string]bool{} // structs with a `list<...> items` field
	var keyReqs []string             // file `<name>_key` fields in requests: this IDL needs the upload method
	hasUploadKind := false           // enum UploadKind (RPC service: the kinds of file it signs links for)
	if api {
		out = append(out, checkDomainFile(rel, lines)...)
	}
	// First pass: struct fields.
	fields := map[string]map[string]bool{} // struct -> field names
	var shapes []respShape                 // every *Resp struct, in order
	cur := ""
	for i, line := range lines {
		if uploadKindRE.MatchString(line) {
			hasUploadKind = true
		}
		if m := structRE.FindStringSubmatch(line); m != nil {
			cur = m[1]
			fields[cur] = map[string]bool{}
			shapes = append(shapes, respShape{name: cur, line: i + 1})
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
			if !isCommonFile && (name == "page_size" || (name == "page" && typ != "common.PageReq" && typ != "common.PageInfo")) && !ignored(line, "paging") {
				out = append(out, Finding{rel, i + 1, "paging", fmt.Sprintf("%s.%s: paging is `common.PageReq page` in a request and `common.PageInfo page` in a response (include \"../common/common.thrift\"; the service applies the defaults with pagex.Normalize), not loose page / page_size fields", cur, name)})
			}
			if !isCommonFile && name == "total" {
				totalLines[cur] = i + 1 // a list response's total belongs in common.PageInfo; checked once the struct is read
			}
			if !isCommonFile && name == "items" && strings.HasPrefix(typ, "list<") {
				listStructs[cur] = true
			}
			if len(shapes) > 0 && shapes[len(shapes)-1].name == cur {
				fm := fieldFullRE.FindStringSubmatch(line)
				sh := &shapes[len(shapes)-1]
				sh.fields = append(sh.fields, respField{id: fm[1], optional: strings.TrimSpace(fm[2]) == "optional", typ: typ, name: name, line: i + 1, ignored: ignored(line, "no-envelope")})
			}
			if !isCommonFile && fileWord.MatchString(name) && !ignored(line, "file-field") {
				isReq := strings.HasSuffix(cur, "Req")
				plain := strings.TrimSpace(typ) == "string" || strings.TrimSpace(typ) == "binary"
				switch {
				case strings.HasSuffix(name, "_path"):
					out = append(out, Finding{rel, i + 1, "file-field", fmt.Sprintf("%s.%s: a file is `<name>_key` in a request (the key the client got with its upload link) and `<name>_url` in a response (the service builds the URL: rt.S3.Public().URL(key) for a public file, rt.S3.PresignGet for a private one); the database stores the key; nothing is a path for a client to complete", cur, name)})
				case strings.HasSuffix(name, "_url") && isReq:
					out = append(out, Finding{rel, i + 1, "file-field", fmt.Sprintf("%s.%s: a client sends the key of the file it uploaded (`%s_key`), not a URL; the URL is built by the service when it answers", cur, name, strings.TrimSuffix(name, "_url"))})
				case isReq && (strings.TrimSpace(typ) == "binary" || (plain && fileContentSuffix.MatchString(name))):
					out = append(out, Finding{rel, i + 1, "file-field", fmt.Sprintf("%s.%s carries the file's content: a file never travels in a request (the gateway body limit is 1 MiB and nothing should buffer it). The client asks the service's GetUploadURL for a presigned PUT link, uploads to S3 itself, and sends the key as `%s_key`", cur, name, fileContentSuffix.ReplaceAllString(name, ""))})
				case isReq && plain && fileLastWord.MatchString(name):
					out = append(out, Finding{rel, i + 1, "file-field", fmt.Sprintf("%s.%s is a bare %s field: if it is the file, the client sends the key it got from GetUploadURL as `%s_key` (never the content, never a URL); if it is something about the file, say what (`%s_name`, `%s_type`), or mark the line `// devkit:lint-ignore file-field`", cur, name, typ, name, name, name)})
				}
			}
			if !isCommonFile && isReqStruct(cur) && fileWord.MatchString(name) && strings.HasSuffix(name, "_key") {
				keyReqs = append(keyReqs, name)
			}
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
	for name, line := range totalLines {
		if listStructs[name] && !ignored(lines[line-1], "paging") {
			out = append(out, Finding{rel, line, "paging", fmt.Sprintf("%s.total: a list response carries `common.PageInfo page` (page, page_size, total) next to its items, not a loose total", name)})
		}
	}
	// Second pass: methods.
	returns := map[string]bool{} // struct names a method returns
	inService := false
	hasGetUploadURL := false
	serviceLine := 0
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "service ") {
			inService = true
			serviceLine = i + 1
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
		returns[strings.TrimPrefix(m[1], "common.")] = true
		returns[m[1]] = true
		if method == "GetUploadURL" {
			hasGetUploadURL = true
		} else if !isCommonFile && uploadMethod.MatchString(method) && !ignored(line, "upload-method") {
			out = append(out, Finding{rel, i + 1, "upload-method", fmt.Sprintf("%s: a service has one upload method, GetUploadURL(kind, content_type) → upload_url + file_key, and a kind per file (enum UploadKind in an RPC service, a string in a gateway); not one method per file field. Add a kind instead of this method", method)})
		}
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
		// the first sentence of the comment is the /docs sidebar entry: short, no punctuation
		if api && !ignored(line, "method-title") {
			if title, ok := methodTitle(lines, i); ok {
				if n := len([]rune(title)); n > maxTitleRunes {
					out = append(out, Finding{rel, i + 1, "method-title", fmt.Sprintf("%s: its title %q is %d characters, at most %d: the first sentence of the comment is the entry in the /docs sidebar, so keep it a short noun phrase (登录, 角色列表, 找回密码 1 核对账号) and put rules, codes and details after the first 。", method, title, n, maxTitleRunes)})
				} else if titlePunct.MatchString(title) {
					out = append(out, Finding{rel, i + 1, "method-title", fmt.Sprintf("%s: its title %q contains punctuation: the first sentence of the comment is the entry in the /docs sidebar, a short noun phrase without ，、；：（）; put details after the first 。 and prefix a group with · or a number (验证码·查询, 找回密码 1 核对账号), not ：, which apidoc treats as the end of the title", method, title)})
				}
			}
		}
		// a brute-force surface needs its own request-rate limit
		if api && sensitiveMethod.MatchString(method+" "+ann) && !commentBlockHas(lines, i, limitTag) && !ignored(line, "limit") {
			out = append(out, Finding{rel, i + 1, "limit", fmt.Sprintf("%s takes credentials or codes: add `// @limit <count>/m` above it (per client IP; the gateway's guard enforces it), for example `// @limit 30/m`", method)})
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
	if len(keyReqs) > 0 && !hasGetUploadURL && serviceLine > 0 && !ignored(lines[serviceLine-1], "upload-method") {
		what := "the one upload method GetUploadURL(kind, content_type) → upload_url + file_key"
		if !api && !hasUploadKind {
			what += " and an enum UploadKind listing the kinds of file it signs links for"
		}
		out = append(out, Finding{rel, serviceLine, "upload-method", fmt.Sprintf("requests carry file keys (%s) but this service has no GetUploadURL: the client must get each key from %s, and the write method checks the key is the caller's and the object exists (Stat) before storing it", strings.Join(keyReqs, ", "), what)})
	}
	var responses []respShape
	for _, sh := range shapes {
		if returns[sh.name] || strings.HasSuffix(sh.name, "Resp") {
			responses = append(responses, sh)
		}
	}
	out = append(out, checkRespShapes(rel, responses, api)...)
	return out, nil
}

// uploadKindRE: the enum an RPC service declares its upload kinds in.
var uploadKindRE = regexp.MustCompile(`^\s*enum\s+UploadKind\s*\{`)

// isReqStruct: a request struct (the input of a method).
func isReqStruct(name string) bool { return strings.HasSuffix(name, "Req") }

// sensitiveMethod: a method name or path that takes credentials or
// one-time codes, which someone will try to guess.
var sensitiveMethod = regexp.MustCompile(`(?i)login|register|signup|password|passwd|otp|captcha|verifycode|sendcode|sms|resetpwd|forgot`)

var limitTag = regexp.MustCompile(`@limit\s+\S+`)

// maxTitleRunes: a method title is a menu entry, not a sentence.
const maxTitleRunes = 12

// titlePunct: punctuation that means the title is carrying details (or a
// group prefix written with ：, which apidoc cuts the title at).
var titlePunct = regexp.MustCompile(`[，、；：（）,;:()]`)

// methodTitle returns the title apidoc shows for the method on line i: the
// first sentence (up to the first 。 or ". ") of the first comment line
// above it that is not a directive (@public, @perm, ...). ok is false when
// there is no such line (method-comment reports that).
func methodTitle(lines []string, i int) (title string, ok bool) {
	var block []string
	for j := i - 1; j >= 0; j-- {
		t := strings.TrimSpace(lines[j])
		if t == "" || !(strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*")) {
			break
		}
		t = strings.TrimSpace(strings.TrimLeft(t, "/* "))
		block = append([]string{t}, block...)
	}
	for _, t := range block {
		if t == "" || strings.HasPrefix(t, "@") || strings.HasPrefix(t, "devkit:") {
			continue
		}
		for _, sep := range []string{"。", ". "} {
			if k := strings.Index(t, sep); k > 0 {
				t = t[:k]
			}
		}
		return strings.TrimSpace(t), true
	}
	return "", false
}

// commentBlockHas reports whether the comment lines right above line i
// contain a match of re.
func commentBlockHas(lines []string, i int, re *regexp.Regexp) bool {
	for j := i - 1; j >= 0; j-- {
		t := strings.TrimSpace(lines[j])
		if t == "" || !(strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*")) {
			return false
		}
		if re.MatchString(t) {
			return true
		}
	}
	return false
}

// bizLiteral: NewBizStatusError(<anything>, "...") with a string literal as the message.
var bizLiteral = regexp.MustCompile(`NewBizStatusError\([^,]+,\s*"`)

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
