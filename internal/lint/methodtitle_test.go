package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMethodTitle: a gateway method's title (the first sentence of its
// comment) is short and has no punctuation; RPC services are not checked.
func TestMethodTitle(t *testing.T) {
	dir := t.TempDir()
	idl := `namespace go auth
struct Req {
    1: string request_id
}
struct Resp {
    1: string ticket
}
service S {
    // 找回密码第二步：发 6 位验证码到绑定的手机 / 邮箱，5 分钟有效，60 秒内只发一次（2015）。票据失效 2016。
    // @public
    // @limit 10/m
    Resp ResetSendCode(1: Req req) (api.post="/v1/auth/resetSendCode")
    // 会员输赢，按场馆汇总。时间规则同数据概览。
    // @perm audit.view
    Resp ListAudit(1: Req req) (api.post="/v1/auth/listAudit")
    // 找回密码 2 发验证码。发 6 位验证码到绑定的手机 / 邮箱，5 分钟有效，60 秒内只发一次（2015）。
    // @public
    // @limit 10/m
    Resp ResetSendCodeShort(1: Req req) (api.post="/v1/auth/resetSendCodeShort")
    // 角色列表。
    // @perm role.view
    Resp ListRoles(1: Req req) (api.post="/v1/auth/listRoles")
    // 验证码·查询。按账号或手机列已发送的验证码。
    Resp ListCodes(1: Req req) (api.post="/v1/auth/listCodes")
    // 一个很长很长很长很长很长很长很长的标题。
    Resp LongIgnored(1: Req req) (api.post="/v1/auth/longIgnored")   // devkit:lint-ignore method-title
}
`
	p := filepath.Join(dir, "auth.thrift")
	os.WriteFile(p, []byte(idl), 0o644)
	_, mn := moneyPattern(nil)
	fs, err := checkIDL(p, true, mn)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, f := range fs {
		if f.Rule == "method-title" {
			titles = append(titles, f.Message)
		}
	}
	if len(titles) != 2 {
		t.Fatalf("want 2 method-title findings (too long, punctuation), got %d: %v", len(titles), titles)
	}
	if !strings.HasPrefix(titles[0], "ResetSendCode:") || !strings.Contains(titles[0], "characters") {
		t.Errorf("ResetSendCode is reported as too long: %s", titles[0])
	}
	if !strings.HasPrefix(titles[1], "ListAudit:") || !strings.Contains(titles[1], "punctuation") {
		t.Errorf("ListAudit is reported for its comma: %s", titles[1])
	}
	// An RPC service has no /docs: not checked.
	fs, err = checkIDL(p, false, mn)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if f.Rule == "method-title" {
			t.Errorf("RPC IDL must not get method-title findings: %s", f.Message)
		}
	}
}
