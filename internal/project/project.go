// Package project locates the current project and reads basic facts about it.
package project

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sezznaw/devkit/internal/manifest"
)

// FindRoot walks up from dir looking for a devkit manifest. It returns the
// directory containing it, or an error if none is found.
func FindRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if manifest.Exists(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside a devkit project (run `devkit init` in the project root)")
		}
		dir = parent
	}
}

// GoModule returns the module path declared in root/go.mod, or "" if there is none.
func GoModule(root string) string {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "module ")), `"`)
		}
	}
	return ""
}

// BuiltinVars are template variables available to every component.
func BuiltinVars(root string) map[string]string {
	return map[string]string{
		"Module":  GoModule(root),
		"Project": filepath.Base(root),
	}
}

// RunHooks executes shell commands in root, streaming their output to stdout/stderr.
func RunHooks(root string, cmds []string) error {
	return RunHooksTo(root, cmds, os.Stdout, os.Stderr)
}

// RunHooksTo is RunHooks with explicit output writers (used by the step UI).
func RunHooksTo(root string, cmds []string, stdout, stderr io.Writer) error {
	for _, c := range cmds {
		fmt.Fprintf(stdout, "$ %s\n", c)
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = root
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.Env = append(os.Environ(), "DEVKIT_PROJECT_ROOT="+root)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("hook %q failed: %w", c, err)
		}
	}
	return nil
}
