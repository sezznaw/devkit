package cmd

import (
	"context"
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
	"github.com/sezznaw/devkit/internal/workspace"
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

// teamValues are the template-decided values the project directory depends on.
type teamValues struct {
	CommonVersion string
	GoVersion     string
}

func teamValuesOf(comp *registry.Component) teamValues {
	var t teamValues
	if v := comp.VarByName("CommonVersion"); v != nil {
		t.CommonVersion = v.Default
	}
	if v := comp.VarByName("GoVersion"); v != nil {
		t.GoVersion = v.Default
	}
	return t
}

// syncProject keeps the two things in the project directory that make the
// common library part of "our code": kit-common/ checked out at the team's
// version, and a go.work that points the Go tools and the IDE at it. Problems
// here are reported, never fatal: the services build without either.
func syncProject(ctx context.Context, cfg *config.Config, project string, team teamValues, skipCommon bool, log func(string, ...any)) *workspace.CommonState {
	var state *workspace.CommonState
	// The AI tools' pointer files: every tool that is opened in the project
	// directory is sent to idl/AGENTS.md, the one development guide.
	if written, err := workspace.WriteAIPointers(project); err != nil {
		log("warning: AI pointer files: %v", err)
	} else if len(written) > 0 {
		log("AI tools: %d pointer files -> %s (%s)", len(written), workspace.AIRulesSource, workspace.AIToolList())
	}
	// The checkout used to be common/, a name the project needs for itself.
	if moved, err := workspace.MigrateCommonDir(ctx, project, commonURL(cfg, project)); err != nil {
		log("warning: %v", err)
	} else if moved {
		log("common/ was the checkout of the shared library: renamed to %s/, with everything in it. common/ is now yours", workspace.CommonDir)
	}
	if !skipCommon && team.CommonVersion != "" {
		st, err := workspace.SyncCommon(ctx, filepath.Join(project, workspace.CommonDir), commonURL(cfg, project), team.CommonVersion, cfg.GitHubToken, cfg.GitHubHost, log)
		if err != nil {
			log("warning: %v", err)
		} else {
			state = &st
			if st.Dirty {
				log("warning: %s/ has local changes, so it was left at %s. Builds on this machine use those changes; CI and everyone else use %s", workspace.CommonDir, st.Version, st.Want)
			}
		}
	}
	if team.GoVersion != "" {
		services, _ := project2Services(project)
		goLine := team.GoVersion
		if strings.Count(goLine, ".") == 1 {
			goLine += ".0"
		}
		changed, err := workspace.SyncGoWork(project, goLine, services)
		switch {
		case err != nil:
			log("warning: %v", err)
		case changed:
			log("go.work updated: \"go to definition\" on the common library opens %s/", workspace.CommonDir)
		}
	}
	return state
}

// commonURL is the address of the shared library's repository for a project.
func commonURL(cfg *config.Config, project string) string {
	repo := workspace.DefaultCommonRepo
	if ws, _ := workspace.LoadConfig(project); ws != nil {
		repo = firstNonEmpty(ws.CommonRepo, cfg.CommonRepo, workspace.DefaultCommonRepo)
	}
	return workspace.RepoURL(cfg.GitHubHost, repo)
}

// project2Services lists the services of a project directory.
func project2Services(dir string) ([]string, error) { return project.Services(dir) }

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
