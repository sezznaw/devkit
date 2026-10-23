package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// idl/devkit.yaml 的 lint 段按键读进 LintRules；没有这个文件就是空规则。
func TestLoadProjectRules(t *testing.T) {
	dir := t.TempDir()
	if r, err := LoadProjectRules(dir); err != nil || len(r.Lint.PlayerGateways) != 0 || r.Lint.VendorService != "" {
		t.Fatalf("missing file: %+v, %v", r, err)
	}
	src := "lint:\n  vendor_service: ser-vendor\n  errors_dir: errors\n  error_ranges:\n    member: 2000-2999\n  handler_methods: 6\n  player_gateways: [ser-api, ser-h5]\n  disable: [tests]\n"
	if err := os.WriteFile(filepath.Join(dir, RulesFile), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := LoadProjectRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Lint.VendorService != "ser-vendor" || r.Lint.ErrorsDir != "errors" || r.Lint.ErrorRanges["member"] != "2000-2999" || r.Lint.HandlerMethods != 6 {
		t.Errorf("rules: %+v", r.Lint)
	}
	if len(r.Lint.PlayerGateways) != 2 || r.Lint.PlayerGateways[0] != "ser-api" || r.Lint.PlayerGateways[1] != "ser-h5" {
		t.Errorf("player_gateways: %v", r.Lint.PlayerGateways)
	}
	if len(r.Lint.Disable) != 1 || r.Lint.Disable[0] != "tests" {
		t.Errorf("disable: %v", r.Lint.Disable)
	}
}
