package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIDLDeps(t *testing.T) {
	base := t.TempDir()
	idl := filepath.Join(base, "idl")
	run(t, base, "init", "-q", idl)
	os.MkdirAll(filepath.Join(idl, "svc"), 0o755)
	os.WriteFile(filepath.Join(idl, "svc", "svc.thrift"), []byte("namespace go a\n"), 0o644)
	run(t, idl, "add", "-A")
	run(t, idl, "commit", "-q", "-m", "one")
	head := run(t, idl, "rev-parse", "HEAD")
	svc := filepath.Join(base, "svc")
	os.MkdirAll(svc, 0o755)

	// No idl.lock: idl-lock reports, this rule stays quiet.
	if fs := checkIDLDeps(svc, idl); len(fs) != 0 {
		t.Fatalf("no lock: %v", fs)
	}
	os.WriteFile(filepath.Join(svc, IDLLockFile), []byte(head+"\n"), 0o644)
	if fs := checkIDLDeps(svc, idl); len(fs) != 1 || !strings.Contains(fs[0].Message, "missing") {
		t.Fatalf("no idl.deps: %v", fs)
	}
	good := "# Written by make gen\nidl " + head + "\nself svc 1\ndep other 3\n"
	os.WriteFile(filepath.Join(svc, IDLDepsFile), []byte(good), 0o644)
	if fs := checkIDLDeps(svc, idl); len(fs) != 0 {
		t.Fatalf("current: %v", fs)
	}
	// The lock moved on (make gen ran only half way, or idl.deps was not committed with it).
	os.WriteFile(filepath.Join(svc, IDLLockFile), []byte("0000000000000000000000000000000000000001\n"), 0o644)
	fs := checkIDLDeps(svc, idl)
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "idl.lock names 0000000") || !strings.Contains(fs[0].Message, "run make gen") {
		t.Fatalf("stale: %v", fs)
	}
	os.WriteFile(filepath.Join(svc, IDLLockFile), []byte(head+"\n"), 0o644)
	// Edited by hand.
	os.WriteFile(filepath.Join(svc, IDLDepsFile), []byte("idl "+head+"\nself svc 1\nwhatever\n"), 0o644)
	if fs := checkIDLDeps(svc, idl); len(fs) != 1 || fs[0].Line != 3 || !strings.Contains(fs[0].Message, "cannot read") {
		t.Fatalf("hand-edited: %v", fs)
	}
	os.WriteFile(filepath.Join(svc, IDLDepsFile), []byte("# only a placeholder\n"), 0o644)
	if fs := checkIDLDeps(svc, idl); len(fs) != 1 || !strings.Contains(fs[0].Message, "no idl or self line") {
		t.Fatalf("placeholder: %v", fs)
	}
	// Not a checkout (a plain copy of the IDL): nothing to compare with.
	plain := filepath.Join(base, "plain")
	os.MkdirAll(plain, 0o755)
	if fs := checkIDLDeps(svc, plain); len(fs) != 0 {
		t.Fatalf("plain copy: %v", fs)
	}
}
