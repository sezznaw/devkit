package report

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// git runs one git command in dir and returns its trimmed output.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gitLines runs git and splits the output into non-empty lines, keeping
// leading spaces (git status --porcelain starts a line with one).
func gitLines(dir string, args ...string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// isRepo says whether dir is the top of a git work tree.
func isRepo(dir string) bool {
	st, err := os.Stat(dir + "/.git")
	return err == nil && (st.IsDir() || st.Mode().IsRegular())
}

// emptyTree is git's well-known hash of the empty tree, the base of a
// repository's first commit.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
