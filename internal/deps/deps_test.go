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

func TestEnvLineGuardsAgainstDuplicates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := envLine(filepath.Join(home, ".local", "bin"))
	want := `case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) export PATH="$HOME/.local/bin:$PATH" ;; esac` + "\n"
	if got != want {
		t.Fatalf("envLine = %q, want %q", got, want)
	}
	if got := envLine("/opt/tools/bin"); !strings.Contains(got, `export PATH="/opt/tools/bin:$PATH"`) {
		t.Fatalf("envLine outside HOME = %q", got)
	}
}

func TestWriteEnvFileListsDevkitGo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOBIN", filepath.Join(home, "go", "bin"))
	if err := os.MkdirAll(filepath.Join(home, ".devkit", "go", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeEnvFile(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".devkit", "env"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"$HOME/.devkit/go/bin:$PATH"`, `"$HOME/go/bin:$PATH"`} {
		if !strings.Contains(s, want) {
			t.Errorf("env file lacks %s:\n%s", want, s)
		}
	}
	if strings.Count(s, "export PATH=") != 2 {
		t.Errorf("env file should name exactly the two tool dirs:\n%s", s)
	}
}

func TestEnsureProfileAppendsOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("SHELL", "/bin/zsh")
	for i := 0; i < 2; i++ {
		profile, added, err := ensureProfile()
		if err != nil {
			t.Fatal(err)
		}
		if profile != filepath.Join(home, ".zshrc") {
			t.Fatalf("profile = %q", profile)
		}
		if added != (i == 0) {
			t.Fatalf("run %d: added = %v", i, added)
		}
	}
	b, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if strings.Count(string(b), `. "$HOME/.devkit/env"`) != 1 {
		t.Fatalf(".zshrc:\n%s", b)
	}
	// A line the user wrote by hand (the old README's `source ...`) counts too.
	t.Setenv("SHELL", "/bin/bash")
	bashrc := filepath.Join(home, ".bash_profile") // darwin; see profilePath
	if profilePath() != bashrc && profilePath() != filepath.Join(home, ".bashrc") {
		t.Fatalf("bash profile = %q", profilePath())
	}
	if err := os.WriteFile(profilePath(), []byte("source \"$HOME/.devkit/env\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, added, _ := ensureProfile(); added {
		t.Fatal("must not append when the profile already sources the env file")
	}
	// Unknown shell: no file is touched, the hint stays manual.
	t.Setenv("SHELL", "/usr/local/bin/fish")
	if p, added, _ := ensureProfile(); p != "" || added {
		t.Fatalf("fish: profile %q added %v", p, added)
	}
	if hint := PathHint([]Status{{Installed: true}}); !strings.Contains(hint, "echo") {
		t.Fatalf("hint for unknown shell should stay manual: %q", hint)
	}
	t.Setenv("SHELL", "/bin/zsh")
	if hint := PathHint([]Status{{Installed: true}}); !strings.Contains(hint, "~/.zshrc") || strings.Contains(hint, "echo") {
		t.Fatalf("hint when the profile is done: %q", hint)
	}
	if PathHint([]Status{{Installed: false}}) != "" {
		t.Fatal("no hint when nothing was installed")
	}
}
