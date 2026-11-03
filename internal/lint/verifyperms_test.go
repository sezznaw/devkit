package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const riskIDL = `namespace go risk

service RiskService {
    // 流水稽核列表
    // @perm risk.turnover.view
    TurnoverAuditList ListTurnoverAudits(1: ListTurnoverAuditsReq req) (api.post="/v1/risk/listTurnoverAudits")

    // 解除稽核
    // @perm risk.turnover.manage
    common.Empty Release(1: ReleaseReq req) (api.post="/v1/risk/release")

    // 版本
    VersionInfo Version(1: common.Empty req) (api.post="/v1/risk/version")
}
`

func verifyPermsFixture(t *testing.T, registry, verifyGo string) (root, idl string) {
	t.Helper()
	dir := t.TempDir()
	root, idl = filepath.Join(dir, "ser-x-api"), filepath.Join(dir, "idl")
	writeFile(t, filepath.Join(idl, "ser-x-api", "risk.thrift"), riskIDL)
	if registry != "" {
		writeFile(t, filepath.Join(idl, "verify-perms.txt"), registry)
	}
	writeFile(t, filepath.Join(root, "app", "verify.go"), verifyGo)
	return root, idl
}

const verifyGoRisk = `package app

func checks() {
	// gw.Post(ctx, "/v1/risk/release") 注释里的不算
	gw.Post(ctx, "/v1/risk/version", nil)
	for _, c := range []struct{ path string }{
		{"/v1/risk/listTurnoverAudits"},
	} {
	}
}
`

// TY-108 之后的复盘：ba1942b 在检查里调了 risk.turnover.view 的接口而种子没有，发布被 403 卡住；登记了才过。
func TestVerifyPermsReportsUngrantedPoint(t *testing.T) {
	root, idl := verifyPermsFixture(t, "# 部署后检查账号\nser-x-api other.view\n", verifyGoRisk)
	got := checkVerifyPerms(root, idl, "ser-x-api", "verify-perms.txt")
	var verifyGo, stale int
	for _, f := range got {
		switch {
		case f.File == "app/verify.go" && f.Line == 7 && strings.Contains(f.Message, "`ser-x-api risk.turnover.view`"):
			verifyGo++
		case f.File == "idl/verify-perms.txt" && f.Line == 2 && strings.Contains(f.Message, "other.view"):
			stale++
		default:
			t.Errorf("unexpected finding: %v", f)
		}
	}
	if verifyGo != 1 || stale != 1 {
		t.Fatalf("want one ungranted point and one stale row, got %v", got)
	}

	root, idl = verifyPermsFixture(t, "ser-x-api risk.turnover.view\nser-y-api whatever.view\n", verifyGoRisk)
	if got := checkVerifyPerms(root, idl, "ser-x-api", "verify-perms.txt"); len(got) != 0 {
		t.Fatalf("granted: want no findings, got %v", got)
	}
}

func TestVerifyPermsRegistryRejectsWritesAndWildcards(t *testing.T) {
	root, idl := verifyPermsFixture(t, "ser-x-api risk.turnover.view\nser-x-api risk.turnover.manage\nser-x-api risk.*\nser-x-api\nser-x-api risk.turnover.view\n", verifyGoRisk)
	got := checkVerifyPerms(root, idl, "ser-x-api", "verify-perms.txt")
	lines := map[int]bool{}
	for _, f := range got {
		if f.File != "idl/verify-perms.txt" {
			t.Errorf("unexpected finding: %v", f)
		}
		lines[f.Line] = true
	}
	for _, n := range []int{2, 3, 4, 5} {
		if !lines[n] {
			t.Errorf("line %d not reported: %v", n, got)
		}
	}
}

func TestVerifyPermsIgnoreAndNoPermGateway(t *testing.T) {
	ignoredGo := strings.Replace(verifyGoRisk, `{"/v1/risk/listTurnoverAudits"},`, `{"/v1/risk/listTurnoverAudits"}, //devkit:lint-ignore verify-perms —— 测试`, 1)
	root, idl := verifyPermsFixture(t, "", ignoredGo)
	writeFile(t, filepath.Join(idl, "verify-perms.txt"), "")
	if got := checkVerifyPerms(root, idl, "ser-x-api", "verify-perms.txt"); len(got) != 0 {
		t.Fatalf("lint-ignore: %v", got)
	}
	// 没有 @perm 的网关（玩家网关）：不读登记表，不报
	if got := checkVerifyPerms(root, idl, "ser-api", "missing.txt"); len(got) != 0 {
		t.Fatalf("gateway without @perm: %v", got)
	}
}
