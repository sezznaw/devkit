package registry

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalServesOlderVersionsFromGitTags(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(version, def string) {
		comp := filepath.Join(dir, "components", "svc")
		os.MkdirAll(filepath.Join(comp, "files"), 0o755)
		os.WriteFile(filepath.Join(comp, "component.json"), []byte(`{"name":"svc","version":"`+version+`","vars":[{"name":"K","default":"`+def+`"}]}`), 0o644)
		os.WriteFile(filepath.Join(comp, "files", "a.txt"), []byte(version), 0o644)
	}
	run("init", "-q", "-b", "main")
	write("1.0.0", "old-default")
	run("add", "-A")
	run("commit", "-q", "-m", "1.0.0")
	run("tag", "svc/v1.0.0")
	write("2.0.0", "new-default")
	run("add", "-A")
	run("commit", "-q", "-m", "2.0.0")

	l := NewLocal(dir)
	cur, err := l.Fetch(context.Background(), "svc", "2.0.0")
	if err != nil || cur != filepath.Join(dir, "components", "svc") {
		t.Fatalf("current version must come from the working tree: %q, %v", cur, err)
	}
	old, err := l.Fetch(context.Background(), "svc", "1.0.0")
	if err != nil {
		t.Fatalf("older version must come from its tag: %v", err)
	}
	c, err := LoadComponent(old)
	if err != nil || c.Version != "1.0.0" || c.Vars[0].Default != "old-default" {
		t.Fatalf("wrong content from tag: %+v, %v", c, err)
	}
	if b, _ := os.ReadFile(filepath.Join(old, "files", "a.txt")); string(b) != "1.0.0" {
		t.Errorf("files of the old version missing: %q", b)
	}
	if _, err := l.Fetch(context.Background(), "svc", "0.5.0"); err == nil || !strings.Contains(err.Error(), "svc/v0.5.0") {
		t.Errorf("an unknown version must name the tag that was looked for, got %v", err)
	}
}
