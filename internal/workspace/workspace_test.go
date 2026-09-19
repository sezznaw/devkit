package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRepoURL(t *testing.T) {
	cases := map[string]string{
		"sezznaw/idl":            "https://github.com/sezznaw/idl.git",
		"/sezznaw/idl/":          "/sezznaw/idl/",
		"https://x.com/a/b.git":  "https://x.com/a/b.git",
		"git@github.com:a/b.git": "git@github.com:a/b.git",
		"./local":                "./local",
	}
	for in, want := range cases {
		if got := RepoURL("", in); got != want {
			t.Errorf("RepoURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnsureRepoCloneThenPull(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "src")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644)
	if err := InitRepo(ctx, src, "init"); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	dst := filepath.Join(ws, "idl")
	quiet := func(string, ...any) {}
	if err := EnsureRepo(ctx, dst, src, "", quiet); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Fatal("clone did not produce a.txt")
	}
	// Second call must pull, not fail.
	os.WriteFile(filepath.Join(src, "b.txt"), []byte("b"), 0o644)
	if _, err := git(ctx, src, "", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, src, "", "commit", "-q", "-m", "b"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureRepo(ctx, dst, src, "", quiet); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "b.txt")); err != nil {
		t.Fatal("pull did not bring b.txt")
	}
	// A plain directory that is not a checkout is an error.
	plain := filepath.Join(ws, "plain")
	os.MkdirAll(plain, 0o755)
	if err := EnsureRepo(ctx, plain, src, "", quiet); err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestOrgPattern(t *testing.T) {
	if OrgPattern("github.com/sezznaw") != "github.com/sezznaw" || OrgPattern("github.com/sezznaw/sub/") != "github.com/sezznaw" || OrgPattern("x") != "x" {
		t.Fatal("OrgPattern")
	}
	if got := RepoURL("ghe.corp.com", "a/b"); got != "https://ghe.corp.com/a/b.git" {
		t.Fatalf("enterprise host: %s", got)
	}
}
