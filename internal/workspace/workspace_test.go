package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if err := EnsureRepo(ctx, dst, src, "", "", quiet); err != nil {
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
	if out, err := git(ctx, src, "", "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "b"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if err := EnsureRepo(ctx, dst, src, "", "", quiet); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "b.txt")); err != nil {
		t.Fatal("pull did not bring b.txt")
	}
	// A plain directory that is not a checkout is an error.
	plain := filepath.Join(ws, "plain")
	os.MkdirAll(plain, 0o755)
	if err := EnsureRepo(ctx, plain, src, "", "", quiet); err == nil {
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

// A machine without git identity (new laptop, CI runner) must still get the scaffold commit.
func TestInitRepoWithoutGitIdentity(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// Hide every source of identity: global/system config and env.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL"} {
		// Unset, not empty: an empty GIT_AUTHOR_NAME is an explicit (invalid) identity.
		if v, ok := os.LookupEnv(k); ok {
			os.Unsetenv(k)
			t.Cleanup(func() { os.Setenv(k, v) })
		}
	}
	ctx := context.Background()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	if hasIdentity(ctx, dir) {
		t.Skip("could not isolate git identity on this machine")
	}
	if err := InitRepo(ctx, dir, "scaffold"); err != nil {
		t.Fatalf("InitRepo must succeed without a configured identity: %v", err)
	}
	out, err := git(ctx, dir, "", "log", "--format=%an <%ae> %s")
	if err != nil || out == "" {
		t.Fatalf("no commit created: %v %q", err, out)
	}
	if want := "devkit <devkit@users.noreply.github.com> scaffold"; !strings.Contains(out, want) {
		t.Errorf("log = %q, want it to contain %q", out, want)
	}
}

func TestTokenIsOnlySentToItsOwnHost(t *testing.T) {
	cases := []struct {
		url, host string
		want      bool
	}{
		{"https://github.com/sezznaw/idl.git", "", true},
		{"https://github.com/sezznaw/idl.git", "github.com", true},
		{"https://ghe.corp.com/a/b.git", "https://ghe.corp.com/", true},
		{"https://gitlab.corp.com/a/b.git", "", false},
		{"https://github.com.evil.io/a/b.git", "", false},
		{"http://github.com/a/b.git", "", false},
		{"git@github.com:a/b.git", "", false},
		{"/local/path", "", false},
	}
	for _, c := range cases {
		if got := TokenAllowedFor(c.url, c.host); got != c.want {
			t.Errorf("TokenAllowedFor(%q, %q) = %v, want %v", c.url, c.host, got, c.want)
		}
	}
}

func TestEnsureLocalRepoAndNoRemoteIsQuiet(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "idl")
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, f) }
	if err := EnsureLocalRepo(ctx, dir, logf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatal("idl/ must be a git repository")
	}
	if err := EnsureLocalRepo(ctx, dir, logf); err != nil { // idempotent
		t.Fatal(err)
	}
	// A repo without remotes must not produce a pull warning.
	logs = nil
	if err := EnsureRepo(ctx, dir, "git@gitlab.example.com:x/idl.git", "", "", logf); err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if strings.Contains(l, "warning") {
			t.Errorf("unexpected warning for a remote-less repo: %q", l)
		}
	}
}

func TestDefaultModulePrefix(t *testing.T) {
	cases := map[string]string{
		"/Users/me/Work/indie-game": "indie-game",
		"/tmp/My Project":           "my-project",
		"/tmp/---":                  "app",
		"/tmp/shop_v2.1":            "shop_v2.1",
	}
	for in, want := range cases {
		if got := DefaultModulePrefix(in); got != want {
			t.Errorf("DefaultModulePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
