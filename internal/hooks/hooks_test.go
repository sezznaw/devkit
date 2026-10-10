package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallRemove(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, false); err == nil {
		t.Fatal("not a git repo accepted")
	}
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	if st, err := Install(dir, false); err != nil || st != "installed" {
		t.Fatalf("%s %v", st, err)
	}
	if !Installed(dir) {
		t.Fatal("not installed")
	}
	if st, _ := Install(dir, false); st != "current" {
		t.Fatal(st)
	}
	// an older devkit hook is refreshed
	os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-push"), []byte("#!/bin/sh\n# devkit hooks v0\nmake lint\n"), 0o755)
	if st, _ := Install(dir, false); st != "updated" {
		t.Fatal(st)
	}
	// a foreign hook is kept unless forced
	os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-push"), []byte("#!/bin/sh\necho mine\n"), 0o755)
	if _, err := Install(dir, false); err != ErrForeignHook {
		t.Fatalf("foreign: %v", err)
	}
	if _, err := Remove(dir); err != ErrForeignHook {
		t.Fatal("foreign removed")
	}
	if st, err := Install(dir, true); err != nil || st != "replaced" {
		t.Fatalf("%s %v", st, err)
	}
	if ok, err := Remove(dir); err != nil || !ok {
		t.Fatal(err)
	}
	if Installed(dir) {
		t.Fatal("still installed")
	}
	// a worktree .git file
	wt := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git", "worktrees", "x"), 0o755)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(dir, ".git", "worktrees", "x")+"\n"), 0o644)
	if st, err := Install(wt, false); err != nil || st != "installed" {
		t.Fatalf("worktree: %s %v", st, err)
	}
	if !Installed(dir) {
		t.Fatal("worktree hook not in the main repository")
	}
}
