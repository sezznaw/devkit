// Package hooks installs the git hooks of a service: a pre-push that runs the
// same `make lint && make test` the CI pipeline runs, so a push that would
// fail there is refused on the developer's machine. The hook is a small
// shell script with a marker line, so it can be refreshed or removed; a
// hook somebody wrote by hand is left alone unless forced.
package hooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Marker identifies a hook written by devkit (and its version).
const Marker = "# devkit hooks v1"

// PrePush is the pre-push hook.
const PrePush = `#!/bin/sh
` + Marker + `
# Refuses a push that would fail CI: the pipeline's test stage runs the same
# make lint && make test. Skip once with: git push --no-verify (CI still
# checks). Remove with: devkit hooks --remove.
cd "$(git rev-parse --show-toplevel)" || exit 1
[ -f Makefile ] || exit 0
echo ">> devkit pre-push: make lint && make test"
make lint && make test
`

// ErrNoGit: the directory is not a git repository.
var ErrNoGit = errors.New("not a git repository")

// ErrForeignHook: a pre-push hook not written by devkit is there.
var ErrForeignHook = errors.New("a pre-push hook written by hand is already there (devkit hooks --force replaces it)")

// Install writes the pre-push hook of the repository at dir. It returns
// "installed", "updated" or "current".
func Install(dir string, force bool) (string, error) {
	hooksDir, err := hooksDir(dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(hooksDir, "pre-push")
	state := "installed"
	if b, err := os.ReadFile(path); err == nil {
		switch {
		case string(b) == PrePush:
			return "current", nil
		case strings.Contains(string(b), "# devkit hooks "):
			state = "updated"
		case !force:
			return "", ErrForeignHook
		default:
			state = "replaced"
		}
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(PrePush), 0o755); err != nil {
		return "", err
	}
	return state, nil
}

// Remove deletes a pre-push hook written by devkit; a foreign one stays.
func Remove(dir string) (bool, error) {
	hooksDir, err := hooksDir(dir)
	if err != nil {
		return false, err
	}
	path := filepath.Join(hooksDir, "pre-push")
	b, err := os.ReadFile(path)
	if err != nil {
		return false, nil
	}
	if !strings.Contains(string(b), "# devkit hooks ") {
		return false, ErrForeignHook
	}
	return true, os.Remove(path)
}

// Installed reports whether the repository has devkit's current hook.
func Installed(dir string) bool {
	hooksDir, err := hooksDir(dir)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(hooksDir, "pre-push"))
	return err == nil && string(b) == PrePush
}

// hooksDir is <repo>/.git/hooks, also for a worktree whose .git is a file
// pointing at the real git directory.
func hooksDir(dir string) (string, error) {
	g := filepath.Join(dir, ".git")
	st, err := os.Stat(g)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNoGit, dir)
	}
	if st.IsDir() {
		return filepath.Join(g, "hooks"), nil
	}
	b, err := os.ReadFile(g)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(b))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", fmt.Errorf("%w: %s", ErrNoGit, dir)
	}
	gd := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(dir, gd)
	}
	// a worktree's gitdir is <main>/.git/worktrees/<name>; hooks live in <main>/.git/hooks
	if i := strings.Index(gd, string(filepath.Separator)+"worktrees"+string(filepath.Separator)); i > 0 {
		gd = gd[:i]
	}
	return filepath.Join(gd, "hooks"), nil
}
