package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commonRepo builds a source repository with tags v0.1.0 and v0.2.0 and an
// unreleased commit on main after them.
func commonRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := filepath.Join(t.TempDir(), "src")
	os.MkdirAll(dir, 0o755)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(s string) { os.WriteFile(filepath.Join(dir, "lib.go"), []byte("package lib // "+s+"\n"), 0o644) }
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/common\n"), 0o644)
	for _, v := range []string{"v0.1.0", "v0.2.0"} {
		write(v)
		run("add", "-A")
		run("commit", "-q", "-m", v)
		run("tag", v)
	}
	write("unreleased work on main")
	run("add", "-A")
	run("commit", "-q", "-m", "wip")
	return dir
}

func TestSyncCommonFollowsTheTeamVersionNotMain(t *testing.T) {
	src := commonRepo(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "common")
	quiet := func(string, ...any) {}

	st, err := SyncCommon(ctx, dir, src, "v0.1.0", "", "", quiet)
	if err != nil || !st.InLine() {
		t.Fatalf("fresh clone: %+v, %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "lib.go")); !strings.Contains(string(b), "v0.1.0") {
		t.Fatalf("content must be the release, not main: %s", b)
	}
	// The team moves on.
	st, err = SyncCommon(ctx, dir, src, "v0.2.0", "", "", quiet)
	if err != nil || st.Version != "v0.2.0" || !st.InLine() {
		t.Fatalf("after the team version changed: %+v, %v", st, err)
	}
	// Someone edits common/: the work is kept and reported.
	os.WriteFile(filepath.Join(dir, "lib.go"), []byte("package lib // my local hack\n"), 0o644)
	st, err = SyncCommon(ctx, dir, src, "v0.1.0", "", "", quiet)
	if err != nil || !st.Dirty || st.InLine() {
		t.Fatalf("a dirty checkout must be reported, not failed: %+v, %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "lib.go")); !strings.Contains(string(b), "my local hack") {
		t.Fatal("local changes must never be discarded")
	}
	if _, err := SyncCommon(ctx, filepath.Join(t.TempDir(), "c2"), src, "v9.9.9", "", "", quiet); err == nil {
		t.Error("a version that does not exist must be an error")
	}
}

func TestSyncGoWork(t *testing.T) {
	project := t.TempDir()
	mod := func(name string) string {
		d := filepath.Join(project, name)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "go.mod"), []byte("module x/"+name+"\n"), 0o644)
		return d
	}
	user, game := mod("user"), mod("game")
	os.MkdirAll(filepath.Join(project, "idl"), 0o755) // not a module, never listed

	if changed, err := SyncGoWork(project, "1.26.0", []string{user, game}); err != nil || !changed {
		t.Fatalf("first write: %v %v", changed, err)
	}
	got, _ := os.ReadFile(filepath.Join(project, "go.work"))
	if strings.Contains(string(got), "./common") || !strings.Contains(string(got), "\t./game\n\t./user\n") || !strings.Contains(string(got), "go 1.26.0") {
		t.Fatalf("without a common checkout only the services are listed, sorted:\n%s", got)
	}
	mod("common")
	if changed, _ := SyncGoWork(project, "1.26.0", []string{user, game}); !changed {
		t.Fatal("common appeared: the file must be rewritten")
	}
	got, _ = os.ReadFile(filepath.Join(project, "go.work"))
	if !strings.Contains(string(got), "\t./common\n\t./game\n\t./user\n") {
		t.Fatalf("common must be listed:\n%s", got)
	}
	if changed, _ := SyncGoWork(project, "1.26.0", []string{user, game}); changed {
		t.Error("nothing changed: the file must not be rewritten")
	}
	os.WriteFile(filepath.Join(project, "go.work"), []byte("go 1.26.0\nuse ./mine\n"), 0o644)
	if _, err := SyncGoWork(project, "1.26.0", []string{user}); err == nil {
		t.Error("a hand-written go.work must not be overwritten")
	}
}
