package lint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCheckGoFile(t *testing.T) {
	dir := t.TempDir()
	src := `package x
import "net/http"
var c = &http.Client{}          // caught
var r = redis.NewClient(nil)    //devkit:lint-ignore no-direct-middleware
func f() { fmt.Println("x"); zlog.Info("ok") }
type Bet struct { Amount float64; Odds float64 }
var p = rt.Provider("pay")
// fmt.Println in a comment is fine
`
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644)
	mg, mn := moneyPattern([]string{"odds"})
	fs, err := checkGoFile(dir, "a.go", goCheck{isVendor: false, vendorRule: true, moneyGo: mg})
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, f := range fs {
		rules = append(rules, f.Rule)
	}
	got := strings.Join(rules, ",")
	for _, want := range []string{"no-direct-middleware", "use-zlog", "no-float-money", "vendor-only"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Count(got, "no-direct-middleware") != 1 {
		t.Errorf("the ignored redis line must not count: %s", got)
	}
	if strings.Count(got, "no-float-money") != 1 {
		t.Errorf("one finding per line: %s", got)
	}
	_ = mn
	if fs2, _ := checkGoFile(dir, "a.go", goCheck{isVendor: true, vendorRule: true, moneyGo: mg}); strings.Contains(joinRules(fs2), "vendor-only") {
		t.Error("the vendor service may call providers")
	}
}

func joinRules(fs []Finding) string {
	var r []string
	for _, f := range fs {
		r = append(r, f.Rule)
	}
	return strings.Join(r, ",")
}

func TestCheckIDL(t *testing.T) {
	dir := t.TempDir()
	idl := `namespace go x
struct GetProfileReq {
    1: i64 uid
}
struct UpdateNameReq {
    1: string request_id
    2: string name
}
struct TransferReq {
    1: i64 uid
    2: double amount
    3: i64 fee
    4: common.Money stake
    5: list<common.Money> payouts
    6: string currency_note
    7: i64 total
    8: double odds
    9: string fee_rate
    10: common.Decimal fx_rate
    11: i64 duration_ms
    12: string feedback
}
struct Resp {
    1: string result
}
service S {
    // 取资料。
    Resp MemberGetProfile(1: GetProfileReq req) (api.post="/v1/member/getProfile")
    // 改名。
    Resp MemberUpdateName(1: UpdateNameReq req) (api.post="/v1/member/updateName")
    Resp WalletTransfer(1: TransferReq req) (api.post="/v1/wallet/transfer")
    // 自检。
    Resp EgressCheck(1: GetProfileReq req)
}
`
	p := filepath.Join(dir, "s.thrift")
	os.WriteFile(p, []byte(idl), 0o644)
	_, mn := moneyPattern(nil)
	fs, err := checkIDL(p, true, mn)
	if err != nil {
		t.Fatal(err)
	}
	got := joinRules(fs)
	if strings.Count(got, "request-id") != 1 || !strings.Contains(fs[len(fs)-1].Message+got, "WalletTransfer") {
		t.Errorf("only WalletTransfer lacks request_id: %v", fs)
	}
	if strings.Count(got, "method-comment") != 1 {
		t.Errorf("only WalletTransfer lacks a comment: %v", fs)
	}
	if strings.Count(got, "no-float-money") != 2 {
		t.Errorf("amount double and odds double: %v", fs)
	}
	if strings.Count(got, "rate-type") != 1 || !strings.Contains(joinMessages(fs), "TransferReq.fee_rate") {
		t.Errorf("only fee_rate string is the wrong rate type: %v", fs)
	}
	if strings.Count(got, "money-type") != 1 || !strings.Contains(joinMessages(fs), "TransferReq.fee is") {
		t.Errorf("only fee i64 is the wrong money type: %v", fs)
	}
}

func TestCheckErrorCodes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "h.go"), []byte("package h\nconst CodeMemberNotFound = 2001\nconst CodeNew = 2009\nvar e = kerrors.NewBizStatusError(5001, \"x\")\nvar f = kerrors.NewBizStatusError(5009, \"y\") //devkit:lint-ignore error-code\n"), 0o644)
	md := filepath.Join(dir, "errors.md")
	os.WriteFile(md, []byte("| code | 含义 |\n|---|---|\n| 2001 | 会员不存在 |\n| 5001 | 下游 |\n"), 0o644)
	fs := checkErrorCodes(dir, []string{"h.go"}, md)
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "2009") {
		t.Errorf("%v", fs)
	}
}

func joinMessages(fs []Finding) string {
	out := ""
	for _, f := range fs {
		out += f.Message + "\n"
	}
	return out
}

func TestOutboxRule(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "repo"), 0o755)
	os.WriteFile(filepath.Join(dir, "repo", "wallet.go"), []byte(`package repo
func (r *Repo) Debit() error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		_ = r.events.Publish(ctx, "events.wallet", "1", "wallet.debited", d)
		return r.events.PublishTx(ctx, tx, "events.wallet", "1", "wallet.debited", d)
	})
}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "repo", "notify.go"), []byte(`package repo
func (r *Repo) Notify() error { return r.events.Publish(ctx, "events.member", "1", "member.updated", d) }
`), 0o644)
	_, mn := moneyPattern(nil)
	_ = mn
	fs, err := checkGoFile(dir, "repo/wallet.go", goCheck{moneyGo: regexp.MustCompile(`$^`)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(joinRules(fs), "outbox") != 1 {
		t.Errorf("Publish inside a transaction file: %v", fs)
	}
	fs, _ = checkGoFile(dir, "repo/notify.go", goCheck{moneyGo: regexp.MustCompile(`$^`)})
	if strings.Contains(joinRules(fs), "outbox") {
		t.Errorf("Publish without a transaction is fine: %v", fs)
	}
}

func TestDelayRule(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "handler"), 0o755)
	os.WriteFile(filepath.Join(dir, "handler", "h.go"), []byte(`package handler
func A() {
	time.AfterFunc(15*time.Minute, func() { cancel(orderNo) })
	t := time.NewTimer(time.Second) //devkit:lint-ignore delay
	_ = t
	_, _ = s.rt.Delay.Schedule(ctx, tx, "order.cancel-unpaid", orderNo, time.Now().Add(15*time.Minute), p)
}
`), 0o644)
	fs, err := checkGoFile(dir, "handler/h.go", goCheck{moneyGo: regexp.MustCompile(`$^`)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(joinRules(fs), "delay") != 1 {
		t.Errorf("one AfterFunc (the ignored NewTimer and Schedule are fine): %v", fs)
	}
}

func TestReportOnlyRule(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "conf"), 0o755)
	os.WriteFile(filepath.Join(dir, "conf", "dev.yaml"), []byte("mysql:\n  enabled: true\nreport:\n  source: platform\n  enabled: true\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "conf", "local.yaml"), []byte("report:\n  enabled: false\n"), 0o644)
	fs := checkReportOnly(dir, "ser-report")
	if len(fs) != 1 || fs[0].Rule != "report-only" || fs[0].File != "conf/dev.yaml" {
		t.Fatalf("%v", fs)
	}
}

func TestTestsRule(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "handler", "ser_api"), 0o755)
	os.WriteFile(filepath.Join(dir, "handler", "ser_api", "h.go"), []byte("package ser_api\n"), 0o644)
	if fs := checkTests(dir); len(fs) != 1 || fs[0].Rule != "tests" {
		t.Fatalf("hertz handler package without a test: %v", fs)
	}
	os.WriteFile(filepath.Join(dir, "handler", "ser_api", "h_test.go"), []byte("package ser_api\n"), 0o644)
	if fs := checkTests(dir); len(fs) != 0 {
		t.Fatalf("with a test: %v", fs)
	}
	os.WriteFile(filepath.Join(dir, "handler", "handler.go"), []byte("package handler\n"), 0o644)
	if fs := checkTests(dir); len(fs) != 1 || fs[0].File != "handler" {
		t.Fatalf("kitex handler package without a test: %v", fs)
	}
}

func TestLimitRule(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "ser-api.thrift")
	os.WriteFile(f, []byte(`struct AuthLoginReq { 1: string username }
struct AuthRegisterReq { 1: string request_id }
struct MemberGetProfileReq {}
service S {
    // 登录。
    AuthLoginResp AuthLogin(1: AuthLoginReq req) (api.post="/v1/auth/login")
    // 注册。
    // @limit 10/m
    AuthRegisterResp AuthRegister(1: AuthRegisterReq req) (api.post="/v1/auth/register")
    // 资料。
    MemberGetProfileResp MemberGetProfile(1: MemberGetProfileReq req) (api.post="/v1/member/getProfile")
}
`), 0o644)
	fs, err := checkIDL(f, true, regexp.MustCompile(`$^`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(joinRules(fs), "limit") != 1 {
		t.Errorf("login without @limit, register with, profile not sensitive: %v", fs)
	}
	fs, _ = checkIDL(f, false, regexp.MustCompile(`$^`))
	if strings.Contains(joinRules(fs), "limit") {
		t.Errorf("RPC IDLs have no routes to limit: %v", fs)
	}
}

func TestLockRules(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "handler"), 0o755)
	os.MkdirAll(filepath.Join(dir, "app"), 0o755)
	os.WriteFile(filepath.Join(dir, "handler", "h.go"), []byte(`package handler
type S struct {
	mu sync.Mutex
	ok sync.RWMutex //devkit:lint-ignore mutex
}
func A() {
	ok, _ := rdb.SetNX(ctx, "lock:x", 1, time.Second).Result()
	_ = redisx.WithLock(ctx, rdb, "x", time.Second, fn)
}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "app", "a.go"), []byte(`package app
var mu sync.Mutex
`), 0o644)
	fs, err := checkGoFile(dir, "handler/h.go", goCheck{moneyGo: regexp.MustCompile(`$^`)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(joinRules(fs), "mutex") != 1 || strings.Count(joinRules(fs), "lock") != 1 {
		t.Errorf("one mutex, one lock: %v", fs)
	}
	fs, _ = checkGoFile(dir, "app/a.go", goCheck{moneyGo: regexp.MustCompile(`$^`)})
	if strings.Contains(joinRules(fs), "mutex") {
		t.Errorf("a mutex in app/ is fine: %v", fs)
	}
}

func TestBindRule(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "handler"), 0o755)
	os.WriteFile(filepath.Join(dir, "handler", "h.go"), []byte(`package handler
func A(ctx context.Context, c *app.RequestContext) {
	var req X
	if err := c.BindAndValidate(&req); err != nil { return }
}
func B(ctx context.Context, c *app.RequestContext) {
	var req X
	if !hertzx.Bind(c, &req) { return }
}
`), 0o644)
	fs, err := checkGoFile(dir, "handler/h.go", goCheck{moneyGo: regexp.MustCompile(`$^`)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(joinRules(fs), "bind") != 1 {
		t.Errorf("one BindAndValidate: %v", fs)
	}
}
