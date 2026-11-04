package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegistryIDLDir: inside the project directory the registries are in
// <project>/idl; inside the IDL repository (whose rules file is also a
// devkit.yaml) they are in the current directory, never <idl>/idl.
func TestRegistryIDLDir(t *testing.T) {
	registryFlags.idl = ""
	project := t.TempDir()
	idl := filepath.Join(project, "idl")
	mustWrite(t, filepath.Join(project, "devkit.yaml"), "module_prefix: x\n")
	mustWrite(t, filepath.Join(idl, "devkit.yaml"), "lint:\n  redis_keys: redis-keys.txt\n")
	mustWrite(t, filepath.Join(idl, "sub", "a.thrift"), "")

	for _, tc := range []struct{ in, want string }{
		{project, idl},
		{idl, idl},
		{filepath.Join(idl, "sub"), idl},
	} {
		t.Chdir(tc.in)
		got, err := registryIDLDir()
		if err != nil {
			t.Fatalf("in %s: %v", tc.in, err)
		}
		if same(t, got) != same(t, tc.want) {
			t.Errorf("in %s: got %s, want %s", tc.in, got, tc.want)
		}
	}

	// the IDL repository checked out on its own (no project directory above)
	alone := t.TempDir()
	mustWrite(t, filepath.Join(alone, "devkit.yaml"), "lint: {}\n")
	t.Chdir(alone)
	got, err := registryIDLDir()
	if err != nil {
		t.Fatal(err)
	}
	if same(t, got) != same(t, alone) {
		t.Errorf("IDL repository alone: got %s, want %s", got, alone)
	}

	// a project directory whose idl/ is not checked out yet: <project>/idl,
	// so sign reports the missing rules file instead of signing the project's
	// own devkit.yaml
	bare := t.TempDir()
	mustWrite(t, filepath.Join(bare, "devkit.yaml"), "module_prefix: x\n")
	t.Chdir(bare)
	got, err = registryIDLDir()
	if err != nil {
		t.Fatal(err)
	}
	if same(t, got) != same(t, filepath.Join(bare, "idl")) {
		t.Errorf("project without idl/: got %s, want %s", got, filepath.Join(bare, "idl"))
	}

	// --idl wins
	registryFlags.idl = "/somewhere"
	defer func() { registryFlags.idl = "" }()
	if got, _ := registryIDLDir(); got != "/somewhere" {
		t.Errorf("--idl ignored: %s", got)
	}
}

// TestRegistryVerifyRefusesDirWithoutRules: verify on a directory that has no
// devkit.yaml is a wrong directory, not "signed and unchanged".
func TestRegistryVerifyRefusesDirWithoutRules(t *testing.T) {
	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		registryFlags.idl = dir
		err := registryVerifyCmd.RunE(registryVerifyCmd, nil)
		registryFlags.idl = ""
		if err == nil || !strings.Contains(err.Error(), "nothing to verify") {
			t.Errorf("verify %s: err = %v, want 'nothing to verify'", dir, err)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// same resolves symlinks (macOS TempDir is under /var -> /private/var).
func same(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	return abs
}
