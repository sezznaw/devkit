package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// toolsRoots: one service per template, so a project with RPC services and
// gateways gets both kitex and hz installed.
func TestToolsRootsOnePerTemplate(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, component string) string {
		root := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Join(root, ".devkit"), 0o755); err != nil {
			t.Fatal(err)
		}
		if component != "" {
			if err := os.WriteFile(filepath.Join(root, ".devkit", "manifest.json"), []byte(`{"schema":1,"components":{"`+component+`":{"version":"1.0.0"}}}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	roots := toolsRoots([]string{mk("ser-admin", "kitex-service"), mk("ser-api", "hertz-service"), mk("ser-member", "kitex-service"), mk("odd", "")})
	if len(roots) != 3 || filepath.Base(roots[0]) != "ser-admin" || filepath.Base(roots[1]) != "ser-api" || filepath.Base(roots[2]) != "odd" {
		t.Fatalf("roots: %v", roots)
	}
}
