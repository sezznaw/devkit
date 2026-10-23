package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 测试用的 handler：每个方法注释里写了预期。
const uidHandlerSrc = `package handler

import "context"

// GetProfile 按请求体 uid 查资料，玩家网关可达 → 报
func (s *Impl) GetProfile(ctx context.Context, req *api.GetProfileReq) (*api.GetProfileResp, error) {
	if req.Uid <= 0 {
		return nil, errcode.Err(errcode.MemberNotFound)
	}
	return s.repo.GetMember(ctx, req.Uid)
}

// GetWallet 用 kitexx.LoginUID → 不报（也不能替上面的 GetProfile 挡）
func (s *Impl) GetWallet(ctx context.Context, req *api.GetWalletReq) (*api.GetWalletResp, error) {
	uid, err := kitexx.LoginUID(ctx)
	if err != nil {
		return nil, err
	}
	_ = req.Uid
	return s.repo.GetWallet(ctx, uid)
}

// UpdateAvatar 用 kitexx.CheckMemberUID → 不报
func (s *Impl) UpdateAvatar(ctx context.Context, req *api.UpdateAvatarReq) (*api.UpdateAvatarResp, error) {
	if err := kitexx.CheckMemberUID(ctx, req.Uid); err != nil {
		return nil, err
	}
	return s.repo.UpdateAvatar(ctx, req.Uid, req.AvatarKey)
}

// AdminSetPassword 只放行后台网关 → 不报（prod 用 methods: ["*"] 放行 ser-api 时才报）
func (s *Impl) AdminSetPassword(ctx context.Context, req *api.AdminSetPasswordReq) (*api.AdminSetPasswordResp, error) {
	return nil, s.repo.SetPassword(ctx, req.GetUid(), req.Hash)
}

// TouchLogin allow 含 * → 报
func (s *Impl) TouchLogin(ctx context.Context, req *api.TouchLoginReq) (*api.TouchLoginResp, error) {
	return nil, s.repo.TouchLogin(ctx, req.MemberUid)
}

// CreateAgentMember IDL 注释有 @target-uid → 不报
func (s *Impl) CreateAgentMember(ctx context.Context, req *api.CreateAgentMemberReq) (*api.CreateAgentMemberResp, error) {
	return nil, s.repo.Create(ctx, req.Uid)
}

// ListVenues 不读 uid → 不报；注释里提到 req.Uid 和 kitexx.LoginUID( 都不算
func (s *Impl) ListVenues(ctx context.Context, req *api.ListVenuesReq) (*api.ListVenuesResp, error) {
	// 这里不用 req.Uid
	return s.repo.ListVenues(ctx, req.UidList) // 也不用 kitexx.LoginUID(
}

// Reveal 方法行上一行 lint-ignore → 不报
//devkit:lint-ignore rpc-uid-from-ctx
func (s *Impl) Reveal(ctx context.Context, req *api.RevealReq) (*api.RevealResp, error) {
	return s.repo.Reveal(ctx, req.UID)
}

// Split 方法体到下一个列首 func 为止：helper 里的 kitexx.UID 不能替它挡 → 报
func (s *Impl) Split(ctx context.Context, req *api.SplitReq) (*api.SplitResp, error) {
	return s.repo.Get(ctx, req.Uid)
}

func (s *Impl) helper(ctx context.Context) int64 {
	uid, _ := kitexx.UID(ctx)
	return uid
}
`

const uidIDLSrc = `namespace go svc

struct GetProfileReq { 1: i64 uid }

service Svc {
    // 资料
    GetProfileResp GetProfile(1: GetProfileReq req)
    // 后台替代理建会员。uid 是被建的会员
    // @target-uid
    CreateAgentMemberResp CreateAgentMember(1: CreateAgentMemberReq req)
    // 解锁
    RevealResp Reveal(1: RevealReq req)
}
`

const uidDevConf = `service:
  name: svc
callers:
  default: deny
  rules:
    - methods: [GetProfile, GetWallet, UpdateAvatar, ListVenues, Reveal, Split]
      allow: [ser-api]
    - methods: [AdminSetPassword]
      allow: [ser-admin-api]
    - methods: [TouchLogin]
      allow: ["*"]
    - methods: [CreateAgentMember]
      allow: [ser-admin, ser-api]
    - methods: [Ping]
      allow: [svc]
`

// uidFixture 写好临时服务目录：handler/x.go、conf/dev.yaml、idl/svc/svc.thrift，回 root 和 IDL 路径。
func uidFixture(t *testing.T) (root, idl string) {
	t.Helper()
	root = t.TempDir()
	uidWrite(t, filepath.Join(root, "handler", "x.go"), uidHandlerSrc)
	uidWrite(t, filepath.Join(root, "handler", "x_test.go"), "package handler_test\n")
	uidWrite(t, filepath.Join(root, "conf", "dev.yaml"), uidDevConf)
	idl = filepath.Join(root, "idl", "svc", "svc.thrift")
	uidWrite(t, idl, uidIDLSrc)
	return root, idl
}

func uidWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// uidRun 跑规则，回「方法名 → Finding」（方法名从消息开头取）。
func uidRun(t *testing.T, root, idl string, gateways []string) map[string]Finding {
	t.Helper()
	goFiles, err := businessGoFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Finding{}
	for _, f := range checkUIDFromCtx(root, goFiles, idl, gateways) {
		if f.Rule != "rpc-uid-from-ctx" || f.File != "handler/x.go" {
			t.Fatalf("unexpected finding: %+v", f)
		}
		name := strings.Fields(f.Message)[0]
		if f.Line != uidLineOf(t, name) {
			t.Errorf("%s reported at line %d, the method line is %d", name, f.Line, uidLineOf(t, name))
		}
		got[name] = f
	}
	return got
}

// uidLineOf 是测试 handler 里方法 name 的方法行（1 起）。
func uidLineOf(t *testing.T, name string) int {
	t.Helper()
	for i, l := range strings.Split(uidHandlerSrc, "\n") {
		if strings.HasPrefix(l, "func (s *Impl) "+name+"(") {
			return i + 1
		}
	}
	t.Fatalf("no method %s in the fixture", name)
	return 0
}

func TestUIDFromCtx(t *testing.T) {
	root, idl := uidFixture(t)
	got := uidRun(t, root, idl, []string{"ser-api"})
	want := []string{"GetProfile", "TouchLogin", "Split"}
	if len(got) != len(want) {
		t.Fatalf("want findings for %v, got %v", want, got)
	}
	for _, name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("%s not reported: %v", name, got)
		}
	}
	// 消息说清谁调得到、读的是哪个字段、该怎么改
	if m := got["GetProfile"].Message; !strings.Contains(m, "放行了玩家网关 ser-api") || !strings.Contains(m, "（req.Uid）") || !strings.Contains(m, "kitexx.LoginUID(ctx)") || !strings.Contains(m, "kitexx.CheckMemberUID(ctx, req.Uid)") || !strings.Contains(m, "@target-uid") {
		t.Errorf("GetProfile message: %s", m)
	}
	if m := got["TouchLogin"].Message; !strings.Contains(m, "allow 含 *") || !strings.Contains(m, "（req.MemberUid）") {
		t.Errorf("TouchLogin message: %s", m)
	}
	// 没配 player_gateways：规则不跑
	if got := uidRun(t, root, idl, nil); len(got) != 0 {
		t.Errorf("no player_gateways configured, still reported: %v", got)
	}
	// 配的网关没出现在任何 allow 里：也不报
	if got := uidRun(t, root, idl, []string{"ser-h5"}); len(got) != 1 || got["TouchLogin"].Rule == "" {
		t.Errorf("only the allow: [\"*\"] method is reachable by an unknown gateway: %v", got)
	}
}

// methods: ["*"] 的规则放行了玩家网关：每个方法都可达，AdminSetPassword（读 req.GetUid()）也报。
func TestUIDFromCtxWildcardMethods(t *testing.T) {
	root, idl := uidFixture(t)
	uidWrite(t, filepath.Join(root, "conf", "prod.yaml"), "callers:\n  rules:\n    - methods: [\"*\"]\n      allow: [ser-api]\n")
	got := uidRun(t, root, idl, []string{"ser-api"})
	for _, name := range []string{"GetProfile", "TouchLogin", "Split", "AdminSetPassword"} {
		if _, ok := got[name]; !ok {
			t.Errorf("%s not reported: %v", name, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("want 4 findings, got %v", got)
	}
	if m := got["AdminSetPassword"].Message; !strings.Contains(m, "（req.GetUid）") {
		t.Errorf("AdminSetPassword message: %s", m)
	}
}

// IDL 读不到时没有豁免可查：CreateAgentMember 也报（conf 放行了 ser-api）。
func TestUIDFromCtxWithoutIDL(t *testing.T) {
	root, idl := uidFixture(t)
	got := uidRun(t, root, filepath.Join(root, "idl", "missing.thrift"), []string{"ser-api"})
	if _, ok := got["CreateAgentMember"]; !ok || len(got) != 4 {
		t.Errorf("without the IDL the @target-uid exemption cannot apply: %v", got)
	}
	_ = idl
}

// 解析失败的 conf 跳过，不影响别的 profile。
func TestUIDFromCtxBadConf(t *testing.T) {
	root, idl := uidFixture(t)
	uidWrite(t, filepath.Join(root, "conf", "broken.yaml"), "callers: [\n")
	if got := uidRun(t, root, idl, []string{"ser-api"}); len(got) != 3 {
		t.Errorf("a broken conf is skipped: %v", got)
	}
}

// 通过 Run 跑：RPC 服务跑这条规则，网关（hertz-service）不跑，没配 PlayerGateways 不跑。
func TestRunUIDFromCtx(t *testing.T) {
	root, _ := uidFixture(t)
	uidWrite(t, filepath.Join(root, ".devkit", "manifest.json"), `{"schema":1,"components":{"kitex-service":{"version":"1.0.0","vars":{"Service":"svc"}}}}`)
	count := func(o Options) int {
		t.Helper()
		fs, err := Run(o)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, f := range fs {
			if f.Rule == "rpc-uid-from-ctx" {
				n++
			}
		}
		return n
	}
	idlDir := filepath.Join(root, "idl")
	if n := count(Options{Root: root, IDLDir: idlDir, PlayerGateways: []string{"ser-api"}}); n != 3 {
		t.Errorf("RPC service with player_gateways: want 3 findings, got %d", n)
	}
	if n := count(Options{Root: root, IDLDir: idlDir}); n != 0 {
		t.Errorf("no player_gateways: want 0, got %d", n)
	}
	if n := count(Options{Root: root, IDLDir: idlDir, PlayerGateways: []string{"ser-api"}, Disable: []string{"rpc-uid-from-ctx"}}); n != 0 {
		t.Errorf("disabled: want 0, got %d", n)
	}
	uidWrite(t, filepath.Join(root, ".devkit", "manifest.json"), `{"schema":1,"components":{"hertz-service":{"version":"1.0.0","vars":{"Service":"svc"}}}}`)
	if n := count(Options{Root: root, IDLDir: idlDir, PlayerGateways: []string{"ser-api"}}); n != 0 {
		t.Errorf("a gateway is not checked: want 0, got %d", n)
	}
}
