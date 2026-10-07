package lint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func TestIDLLock(t *testing.T) {
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	run(t, base, "init", "--bare", "-q", remote)
	idl := filepath.Join(base, "idl")
	run(t, base, "clone", "-q", remote, idl)
	os.MkdirAll(filepath.Join(idl, "svc"), 0o755)
	os.WriteFile(filepath.Join(idl, "svc", "svc.thrift"), []byte("namespace go a\n"), 0o644)
	run(t, idl, "add", "-A")
	run(t, idl, "commit", "-q", "-m", "one")
	run(t, idl, "push", "-q", "-u", "origin", "HEAD:main")
	pushed := run(t, idl, "rev-parse", "HEAD")
	svc := filepath.Join(base, "svc")
	os.MkdirAll(svc, 0o755)

	if fs := checkIDLLock(svc, idl, "svc"); len(fs) != 1 || !strings.Contains(fs[0].Message, "missing") {
		t.Fatalf("no lock: %v", fs)
	}
	os.WriteFile(filepath.Join(svc, IDLLockFile), []byte(pushed+"\n"), 0o644)
	if fs := checkIDLLock(svc, idl, "svc"); len(fs) != 0 {
		t.Fatalf("pushed and matching: %v", fs)
	}
	// A commit elsewhere in the IDL (docs, another service) does not stale this service.
	os.WriteFile(filepath.Join(idl, "README.md"), []byte("x\n"), 0o644)
	run(t, idl, "add", "-A")
	run(t, idl, "commit", "-q", "-m", "docs")
	run(t, idl, "push", "-q", "origin", "HEAD:main")
	if fs := checkIDLLock(svc, idl, "svc"); len(fs) != 0 {
		t.Fatalf("unrelated commit must not stale the lock: %v", fs)
	}
	// A new, unpushed commit to this service's IDL: the lock lags and the commit is not on the server.
	os.WriteFile(filepath.Join(idl, "svc", "svc.thrift"), []byte("namespace go a\nstruct X {}\n"), 0o644)
	run(t, idl, "commit", "-q", "-am", "two")
	fs := checkIDLLock(svc, idl, "svc")
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "run make gen") {
		t.Fatalf("lock lags: %v", fs)
	}
	os.WriteFile(filepath.Join(svc, IDLLockFile), []byte(run(t, idl, "rev-parse", "HEAD")+"\n"), 0o644)
	fs = checkIDLLock(svc, idl, "svc")
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "not on the server") {
		t.Fatalf("unpushed: %v", fs)
	}
	run(t, idl, "push", "-q", "origin", "HEAD:main")
	if fs := checkIDLLock(svc, idl, "svc"); len(fs) != 0 {
		t.Fatalf("after push: %v", fs)
	}
}
