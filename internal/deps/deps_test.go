package deps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoVersionParsing(t *testing.T) {
	m := goVersionRe.FindStringSubmatch("go version go1.27.1 darwin/arm64")
	if m == nil || m[1] != "1" || m[2] != "27" {
		t.Fatalf("parse failed: %v", m)
	}
}

func TestPrependPathIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", "/usr/bin")
	prependPath(dir)
	prependPath(dir)
	if got := os.Getenv("PATH"); got != dir+string(os.PathListSeparator)+"/usr/bin" {
		t.Fatalf("PATH = %q", got)
	}
	prependPath(filepath.Join(dir, "missing"))
	if strings.Contains(os.Getenv("PATH"), "missing") {
		t.Fatal("non-existent dirs must not be added")
	}
}

func TestCheckReportsGit(t *testing.T) {
	st := Check(context.Background(), Want{})
	if st[0].Name != "git" || (st[0].Missing && st[0].Hint == "") {
		t.Fatalf("git status: %+v", st[0])
	}
}

func TestSameVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v0.16.3", "v0.16.3", true}, {"0.4.5", "v0.4.5", true}, {" v0.4.5\n", "0.4.5", true}, {"v0.16.2", "v0.16.3", false}, {"", "v1", false}} {
		if got := SameVersion(c.a, c.b); got != c.want {
			t.Errorf("SameVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
