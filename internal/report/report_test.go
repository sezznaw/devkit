package report

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func sh(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
	for _, a := range args {
		if strings.HasPrefix(a, "--date=") {
			cmd.Env = append(cmd.Env, "GIT_COMMITTER_DATE="+strings.TrimPrefix(a, "--date="))
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func TestCollectSeesCommitsUncommittedAndUntracked(t *testing.T) {
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, "devkit.yaml"), []byte("idl_repo: https://gitlab.example.com/grp/backend/idl.git\n"), 0o644)
	idl := filepath.Join(proj, "idl")
	os.MkdirAll(filepath.Join(idl, "errors"), 0o755)
	sh(t, idl, "git", "init", "-q", "-b", "main")
	sh(t, idl, "git", "config", "user.email", "t@x")
	sh(t, idl, "git", "config", "user.name", "t")
	os.WriteFile(filepath.Join(idl, "a.thrift"), []byte("service A {\n}\n"), 0o644)
	sh(t, idl, "git", "add", "-A")
	old := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	sh(t, idl, "git", "commit", "-qm", "init", "--date="+old)
	// one commit in range, one uncommitted edit, one untracked file
	os.WriteFile(filepath.Join(idl, "a.thrift"), []byte("service A {\n    // ping\n    void Ping()\n}\n"), 0o644)
	sh(t, idl, "git", "commit", "-qam", "feat: Ping")
	os.WriteFile(filepath.Join(idl, "errors", "m.thrift"), []byte("// 不存在\nconst i32 NotFound = 2001\n"), 0o644)
	os.WriteFile(filepath.Join(idl, "a.thrift"), []byte("service A {\n    // ping\n    void Ping()\n    // pong\n    void Pong()\n}\n"), 0o644)

	r, err := Collect(proj, Options{Since: "1h", Fetch: false, Run: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Repos) != 1 || r.Repos[0].Name != "idl" || r.Repos[0].Kind != "idl" {
		t.Fatalf("repos: %+v", r.Repos)
	}
	rp := r.Repos[0]
	if len(rp.Commits) != 1 || rp.Commits[0].Subject != "feat: Ping" || rp.Commits[0].Pushed {
		t.Errorf("commits: %+v", rp.Commits)
	}
	var paths []string
	for _, f := range rp.Files {
		paths = append(paths, f.Path+":"+f.Status+":"+map[bool]string{true: "u", false: "c"}[f.Uncommitted])
	}
	got := strings.Join(paths, " ")
	if got != "a.thrift:M:u errors/m.thrift:A:u" {
		t.Errorf("files: %s", got)
	}
	if rp.Uncommitted != 2 || rp.Unpushed != 1 {
		t.Errorf("counts: %+v", rp)
	}
	if r.IDL == nil || len(r.IDL.Methods) != 2 || len(r.IDL.Codes) != 1 || r.IDL.Codes[0].Message != "不存在" {
		t.Errorf("idl: %+v", r.IDL)
	}
	if rp.WebURL != "https://gitlab.example.com/grp/backend/idl" {
		t.Errorf("web url: %s", rp.WebURL)
	}
	md := r.Markdown()
	if !strings.Contains(md, "`errors/m.thrift` 新（未提交）") || !strings.Contains(md, "新错误码 `2001 NotFound`：不存在") {
		t.Errorf("markdown:\n%s", md)
	}
}

func TestCleanSubject(t *testing.T) {
	// "（rpc 8160）" after a shell ate the first byte of the closing paren.
	got := cleanSubject("ser-game \u7684 dev \u90e8\u7f72\uff08rpc \u00bc\u0089")
	if strings.ContainsRune(got, '\u0089') || !strings.HasSuffix(got, "\u00bc\uFFFD") {
		t.Errorf("control character kept: %q", got)
	}
	if got := cleanSubject("plain\tsubject \u6587\u5b57"); got != "plain\tsubject \u6587\u5b57" {
		t.Errorf("clean subject changed: %q", got)
	}
	if got := cleanSubject("bad \xff byte"); !strings.Contains(got, "\uFFFD") || !utf8.ValidString(got) {
		t.Errorf("invalid utf-8 kept: %q", got)
	}
}
