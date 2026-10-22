package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckCallers(t *testing.T) {
	root := t.TempDir()
	idl := filepath.Join(root, "svc.thrift")
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(idl, `struct GetReq { 1: i64 uid }
service Svc {
    // a comment ( with a paren
    common.Empty Ping(1: common.Empty req)
    GetResp Get(1: GetReq req)
    list<i64> Scan(1: ScanReq req) (api.post="/x")
}
`)
	write(filepath.Join(root, "conf", "dev.yaml"), "service:\n  name: svc\ncallers:\n  default: deny\n  rules:\n    - methods: [Ping, Get]\n      allow: [\"*\"]\n")
	write(filepath.Join(root, "conf", "prod.yaml"), "callers:\n  rules:\n    - methods: [\"*\"]\n      allow: [ser-a]\n")
	write(filepath.Join(root, "conf", "uat.yaml"), "log:\n  level: info\n")
	got := checkCallers(root, idl)
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if got[0].File != "conf/dev.yaml" || got[0].Line != 3 || !strings.Contains(got[0].Message, "for Scan:") {
		t.Fatalf("dev: %v", got[0])
	}
	if got[1].File != "conf/uat.yaml" || !strings.Contains(got[1].Message, "Ping, Get, Scan") {
		t.Fatalf("uat: %v", got[1])
	}
}
