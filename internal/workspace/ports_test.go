package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllocatePort(t *testing.T) {
	dir := t.TempDir()
	p, assigned, err := AllocatePort(dir, "ser-api", true)
	if err != nil || p != 8080 || !assigned {
		t.Fatalf("first api: %d %v %v", p, assigned, err)
	}
	p, _, _ = AllocatePort(dir, "ser-user", false)
	if p != 8100 {
		t.Fatalf("first rpc: %d", p)
	}
	p, _, _ = AllocatePort(dir, "ser-vendor", false)
	if p != 8110 {
		t.Fatalf("second rpc: %d", p)
	}
	p, assigned, _ = AllocatePort(dir, "ser-user", false)
	if p != 8100 || assigned {
		t.Fatalf("existing: %d %v", p, assigned)
	}
	p, _, _ = AllocatePort(dir, "web-admin-api", true)
	if p != 8120 {
		t.Fatalf("second api takes a block: %d", p)
	}
	data, _ := os.ReadFile(filepath.Join(dir, PortsFile))
	want := "ports:\n  ser-api: 8080\n  ser-user: 8100\n  ser-vendor: 8110\n  web-admin-api: 8120\n"
	if !strings.HasSuffix(string(data), want) {
		t.Errorf("file:\n%s", data)
	}
	// A hand-edited table with comments and gaps is honoured.
	os.WriteFile(filepath.Join(dir, PortsFile), []byte("# x\nports:\n  a: 8100   # taken\n  b: 8120\n"), 0o644)
	p, _, _ = AllocatePort(dir, "c", false)
	if p != 8110 {
		t.Fatalf("gap reused: %d", p)
	}
}
