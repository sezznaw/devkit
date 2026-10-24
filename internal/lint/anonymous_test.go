package lint

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const anonMemberIDL = `namespace go ser_member

service SerMemberService {
    // 注册
    // @anonymous
    RegisterResp Register(1: RegisterReq req)
    // 记登录时间（带登录态调）
    TouchLoginResp TouchLogin(1: TouchLoginReq req)
    // 找回密码第一步
    // @anonymous
    ResetStartResp ResetStart(1: ResetStartReq req)
    // 资料
    GetProfileResp GetProfile(1: GetProfileReq req)
}
`

const anonAdminIDL = `namespace go ser_admin

service SerAdminService {
    // 进站判断
    // @anonymous
    CheckVisitResp CheckVisit(1: CheckVisitReq req)   // devkit:lint-ignore request-id —— 只读
    // 权限点（登录后）
    GetPermissionsResp GetPermissions(1: GetPermissionsReq req)
}
`

func TestAnonymousCoverage(t *testing.T) {
	root := t.TempDir()
	idl := filepath.Join(root, "idl", "ser-member", "ser-member.thrift")
	uidWrite(t, idl, anonMemberIDL)
	uidWrite(t, filepath.Join(root, "conf", "dev.yaml"), "identity:\n  anonymous: [Register, ResetStart]\n")
	uidWrite(t, filepath.Join(root, "conf", "prod.yaml"), "identity:\n  anonymous: [Register, GetProfile]\n")
	uidWrite(t, filepath.Join(root, "conf", "uat.yaml"), "identity:\n  anonymous: [\"*\"]\n")
	uidWrite(t, filepath.Join(root, "conf", "local.yaml"), "service:\n  name: x\n") // 没写 identity 段
	fs := checkAnonymousCoverage(root, idl)
	got := map[string]int{}
	for _, f := range fs {
		if f.Rule != anonymousCoverageRule {
			t.Fatalf("rule: %+v", f)
		}
		got[f.File]++
	}
	// dev 一致；prod 少 ResetStart、多 GetProfile；uat 写了 *（且少两个）；local 没写键但 IDL 有标记 → 少两个
	if got["conf/dev.yaml"] != 0 || got["conf/prod.yaml"] != 2 || got["conf/uat.yaml"] != 2 || got["conf/local.yaml"] != 1 {
		t.Fatalf("findings per conf: %v\n%v", got, fs)
	}
	// IDL 没有任何 @anonymous、conf 也没写键：安静
	root2 := t.TempDir()
	idl2 := filepath.Join(root2, "idl", "ser-x", "ser-x.thrift")
	uidWrite(t, idl2, "service X {\n    // 健康\n    PingResp Ping(1: PingReq req)\n}\n")
	uidWrite(t, filepath.Join(root2, "conf", "dev.yaml"), "service:\n  name: x\n")
	if fs := checkAnonymousCoverage(root2, idl2); len(fs) != 0 {
		t.Fatalf("silent expected: %+v", fs)
	}
}

func TestPublicAnonymous(t *testing.T) {
	root := t.TempDir()
	idlDir := filepath.Join(root, "idl")
	uidWrite(t, filepath.Join(idlDir, "ser-member", "ser-member.thrift"), anonMemberIDL)
	uidWrite(t, filepath.Join(idlDir, "ser-admin", "ser-admin.thrift"), anonAdminIDL)
	uidWrite(t, filepath.Join(idlDir, "ser-api", "auth.thrift"), `namespace go auth
service AuthService {
    // 注册
    // @public
    // @limit 10/m
    Tokens Register(1: RegisterReq req) (api.post="/v1/auth/register")
    // 找回密码 1 核对账号
    // @public
    ResetStartResult ResetStart(1: ResetStartReq req) (api.post="/v1/auth/resetStart")
    // 资料（要登录）
    Profile GetProfile(1: GetProfileReq req) (api.post="/v1/member/getProfile")
}
`)
	uidWrite(t, filepath.Join(root, "deps", "deps.go"), `package deps

var Member sermemberservice.Client
var Admin seradminservice.Client
`)
	uidWrite(t, filepath.Join(root, "handler", "auth", "auth_service.go"), `package auth

func Register(ctx context.Context, c *app.RequestContext) {
	r, err := deps.Member.Register(ctx, &ser_member.RegisterReq{})
	issue(ctx, r.Uid)
}

func issue(ctx context.Context, uid int64) {
	ctx = kitexx.WithToken(kitexx.WithIdentity(ctx, id), token)
	_, _ = deps.Member.TouchLogin(ctx, &ser_member.TouchLoginReq{}) // 带登录态，但没写 ignore → 报（第 10 行）
}

func ResetStart(ctx context.Context, c *app.RequestContext) {
	_, _ = deps.Member.ResetStart(ctx, &ser_member.ResetStartReq{})
	_, _ = deps.Member.TouchLogin(ctx, &ser_member.TouchLoginReq{}) //devkit:lint-ignore public-anonymous —— 带着新令牌调
}

func GetProfile(ctx context.Context, c *app.RequestContext) {
	_, _ = deps.Member.GetProfile(ctx, &ser_member.GetProfileReq{}) // 非 @public，不查
}
`)
	uidWrite(t, filepath.Join(root, "internal", "gate", "gate.go"), `package gate

func Middleware(ctx context.Context, c *app.RequestContext) {
	_, _ = deps.Admin.CheckVisit(ctx, &ser_admin.CheckVisitReq{})
	_, _ = deps.Admin.GetPermissions(ctx, &ser_admin.GetPermissionsReq{}) // 中间件调了非 @anonymous → 报（第 5 行）
}
`)
	uidWrite(t, filepath.Join(root, "app", "app.go"), `package app

func Setup() {
	h.Use(gate.Middleware, accessgate.Middleware(deps.Admin))
}
`)
	uidWrite(t, filepath.Join(root, "handler", "auth", "auth_test.go"), "package auth_test\n")
	goFiles, err := businessGoFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	fs := checkPublicAnonymous(root, idlDir, "ser-api", goFiles)
	var got []string
	for _, f := range fs {
		if f.Rule != publicAnonymousRule {
			t.Fatalf("rule: %+v", f)
		}
		got = append(got, fmt.Sprintf("%s:%d", f.File, f.Line))
		if !strings.Contains(f.Message, "@anonymous") {
			t.Fatalf("message: %s", f.Message)
		}
	}
	want := []string{"handler/auth/auth_service.go:10", "internal/gate/gate.go:5"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("findings %v, want %v\n%+v", got, want, fs)
	}
	if !strings.Contains(fs[1].Message, "中间件 gate.Middleware") || !strings.Contains(fs[0].Message, "@public 接口 Register") {
		t.Fatalf("root naming: %q / %q", fs[0].Message, fs[1].Message)
	}
}
