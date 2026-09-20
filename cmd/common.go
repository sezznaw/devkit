package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sezznaw/devkit/internal/config"
	"github.com/sezznaw/devkit/internal/installer"
	"github.com/sezznaw/devkit/internal/manifest"
	"github.com/sezznaw/devkit/internal/project"
	"github.com/sezznaw/devkit/internal/registry"
	"github.com/sezznaw/devkit/internal/ui"
)

// newInstaller locates the service the current directory belongs to and loads
// config, manifest and registry source for it.
func newInstaller(force, skipHooks bool) (*installer.Installer, error) {
	root, err := project.FindRoot(".")
	if err != nil {
		return nil, err
	}
	return newInstallerAt(root, force, skipHooks)
}

// newInstallerAt builds an installer for the service rooted at root.
func newInstallerAt(root string, force, skipHooks bool) (*installer.Installer, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	src, err := registry.Open(cfg)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Load(root)
	if err != nil {
		return nil, err
	}
	return &installer.Installer{
		Root:      root,
		Source:    src,
		Manifest:  m,
		Force:     force,
		SkipHooks: skipHooks,
		Log:       logf,
		// Hook output (go get, go mod tidy, ...) is secondary: dim it.
		Output: ui.LineWriter(os.Stdout),
	}, nil
}

func parseSet(kvs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --set %q, expected key=value", kv)
		}
		out[k] = v
	}
	return out, nil
}

func logf(format string, args ...any) {
	fmt.Println(ui.Stdout.Line(fmt.Sprintf(format, args...)))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func expandPath(p string) (string, error) {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return filepath.Abs(p)
}
