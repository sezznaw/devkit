package lint

import "testing"

// methodRE 要认出返回类型带包名或是容器的方法：以前 `common.Empty X(...)`、`list<Item> X(...)` 整行被跳过，
// 这些方法的注释、标题、@limit、request_id 检查都没跑。
func TestMethodRE(t *testing.T) {
	cases := []struct {
		line, ret, name, req, ann string
	}{
		{`    PingResp Ping(1: PingReq req)`, "PingResp", "Ping", "PingReq", ""},
		{`    common.Empty Logout(1: LogoutReq req) (api.post="/v1/auth/logout")`, "common.Empty", "Logout", "LogoutReq", `(api.post="/v1/auth/logout")`},
		{`    list<Item> ListItems(1: ListItemsReq req)`, "list<Item>", "ListItems", "ListItemsReq", ""},
		{`    map<string, i64> Counts(1: common.PageReq req)   // devkit:lint-ignore request-id`, "map<string, i64>", "Counts", "common.PageReq", ""},
	}
	for _, c := range cases {
		m := methodRE.FindStringSubmatch(c.line)
		if m == nil {
			t.Fatalf("no match: %s", c.line)
		}
		if m[1] != c.ret || m[2] != c.name || m[3] != c.req || m[4] != c.ann {
			t.Errorf("%s: got ret=%q name=%q req=%q ann=%q", c.line, m[1], m[2], m[3], m[4])
		}
	}
	for _, bad := range []string{`struct LogoutReq {`, `    1: string request_id`} {
		if m := methodRE.FindStringSubmatch(bad); m != nil {
			t.Errorf("should not match: %s -> %v", bad, m)
		}
	}
}

// 返回 common.Empty 的写方法以前漏查 request_id，现在要报。
func TestCheckIDLSeesEmptyReturns(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/auth.thrift"
	uidWrite(t, p, `namespace go auth
struct LogoutReq { 1: string token }
service AuthService {
    // 退出登录
    common.Empty Logout(1: LogoutReq req) (api.post="/v1/auth/logout")
}
`)
	_, moneyName := moneyPattern(nil)
	fs, err := checkIDL(p, true, moneyName)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range fs {
		if f.Rule == "request-id" && f.Line == 5 {
			found = true
		}
	}
	if !found {
		t.Fatalf("Logout returning common.Empty without request_id must be reported: %+v", fs)
	}
}
