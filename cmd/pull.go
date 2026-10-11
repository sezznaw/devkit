package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/config"
	"github.com/sezznaw/devkit/internal/gitlab"
	"github.com/sezznaw/devkit/internal/hosts"
	"github.com/sezznaw/devkit/internal/project"
	"github.com/sezznaw/devkit/internal/ui"
	"github.com/sezznaw/devkit/internal/workspace"
)

var pullFlags struct {
	gitlab  string
	group   string
	dir     string
	noTools bool
}

var pullCmd = &cobra.Command{
	Use:   "pull <project>",
	Short: "Clone (or update) every repository of a project you can see, and set the directory up",
	Long: `pull brings a whole project onto this machine in one go: every repository
of the project's GitLab group (subgroups included) that your token can see
is cloned flat into ./<project>/<repo>/ (idl, infra, the services, the front
ends, lib), then the directory is set up the way devkit ngs would: the
project's devkit.yaml, go.work, the kit-common/ checkout, the pre-push hook
and the read-only lock in every service, and the generators (make tools).

The first time, say which GitLab and group the project is (--gitlab,
--group; the group defaults to the project name); devkit remembers them.
Run it again any time: new repositories are cloned, the others fast-forward
(a repository with local changes is left alone and listed).

  devkit auth https://gitlab.example.com
  devkit pull sportsbook                      # ./sportsbook/...
  devkit pull sportsbook --group sportsbook --gitlab https://gitlab.example.com`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		ctx := cmd.Context()
		store, err := hosts.Load()
		if err != nil {
			return err
		}
		saved, known := store.Projects[name]
		host := pullFlags.gitlab
		if host == "" && known {
			host = saved.Host
		}
		if host == "" && len(store.Hosts) == 1 {
			for h := range store.Hosts {
				host = h
			}
		}
		if host == "" {
			return fmt.Errorf("which GitLab? run devkit auth <url> first, or pass --gitlab (saved: %s)", strings.Join(hostNames(store), ", "))
		}
		host, err = hosts.Normalize(host)
		if err != nil {
			return err
		}
		h, ok := store.Hosts[host]
		if !ok {
			return fmt.Errorf("no token saved for %s: devkit auth %s", host, host)
		}
		group := pullFlags.group
		if group == "" && known {
			group = saved.Group
		}
		if group == "" {
			group = name
		}
		dir := pullFlags.dir
		if dir == "" {
			cwd, _ := os.Getwd()
			if filepath.Base(cwd) == name {
				dir = cwd
			} else {
				dir = filepath.Join(cwd, name)
			}
		}
		dir, _ = filepath.Abs(dir)

		gl := gitlab.New(host, h.Token)
		projects, err := gl.GroupProjects(ctx, group)
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			return fmt.Errorf("no repository under group %q on %s that this token can see", group, host)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		fmt.Println(ui.Stdout.Heading(fmt.Sprintf("== %s: %d repositories under %s/%s -> %s", name, len(projects), host, group, dir)))
		var cloned, updated, skipped, dirty []string
		sort.Slice(projects, func(i, j int) bool { return projects[i].Path < projects[j].Path })
		for _, p := range projects {
			if p.Archived {
				skipped = append(skipped, p.Path+" (archived)")
				continue
			}
			if p.EmptyRepo {
				skipped = append(skipped, p.Path+" (empty repository)")
				continue
			}
			target := filepath.Join(dir, p.Path)
			switch state := repoState(target); state {
			case "missing":
				if out, err := run(ctx, dir, "git", "clone", "--quiet", p.HTTPURL, target); err != nil {
					return fmt.Errorf("clone %s: %v\n%s", p.PathWithNamespace, err, out)
				}
				cloned = append(cloned, p.Path)
			case "clean":
				if out, err := run(ctx, target, "git", "pull", "--ff-only", "--quiet"); err != nil {
					dirty = append(dirty, p.Path+" (pull failed: "+strings.TrimSpace(out)+")")
				} else {
					updated = append(updated, p.Path)
				}
			case "dirty":
				dirty = append(dirty, p.Path+" (local changes, left alone)")
			default:
				skipped = append(skipped, p.Path+" (exists, not a git checkout)")
			}
		}
		report := func(label string, items []string) {
			if len(items) > 0 {
				fmt.Printf("  %s %s\n", label, strings.Join(items, ", "))
			}
		}
		report("cloned:", cloned)
		report("updated:", updated)
		report(ui.Stdout.Attention("left alone:"), dirty)
		report("skipped:", skipped)

		store.Projects[name] = hosts.Project{Host: host, Group: group, Dir: dir}
		if err := store.Save(); err != nil {
			return err
		}

		// The project directory: devkit.yaml from the IDL repository's shared
		// settings, go.work, kit-common/, hooks and locks in every service.
		logf := func(format string, args ...any) { fmt.Printf("  "+format+"\n", args...) }
		if err := ensureProjectConfig(dir, host, projects); err != nil {
			logf("warning: %v", err)
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		services, _ := project.Services(dir)
		if len(services) > 0 {
			if team, ok := teamFor(cmd, services[0]); ok {
				syncProject(ctx, cfg, dir, team, false, logf)
			} else {
				logf("warning: could not read the team's template versions from the registry; go.work and kit-common/ were not set up (run devkit update in a service later)")
			}
			for _, root := range services {
				installHooksQuietly(root, func(string, ...any) {})
				lockQuietly(root, func(string, ...any) {})
			}
			lockCommonQuietly(dir, func(string, ...any) {})
			logf("%d service(s): pre-push hook installed, framework files locked", len(services))
			if w, r, err := workspace.RefreshAISkills(dir); err != nil {
				logf("warning: skills: %v", err)
			} else if len(w)+len(r) > 0 {
				logf("AI skills refreshed from %s (%d written, %d removed)", workspace.AISkillsSource, len(w), len(r))
			}
			if !pullFlags.noTools {
				logf("make tools (the generators, once for the whole project) ...")
				if out, err := run(ctx, services[0], "make", "tools"); err != nil {
					logf("warning: make tools failed: %v\n%s", err, out)
				}
			}
		}
		fmt.Printf("\n%s %s is at %s\n", ui.Stdout.Success("✓"), name, dir)
		fmt.Printf("  next: cd %s && devkit ai <your tool>   (writes the file your AI tool reads first; it points at %s)\n", dir, workspace.AIRulesSource)
		fmt.Printf("        then read %s yourself once: it is the whole workflow\n", filepath.Join(dir, workspace.AIRulesSource))
		return nil
	},
}

func hostNames(s *hosts.Store) []string {
	var out []string
	for h := range s.Hosts {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func repoState(dir string) string {
	if _, err := os.Stat(dir); err != nil {
		return "missing"
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return "foreign"
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil || strings.TrimSpace(string(out)) != "" {
		return "dirty"
	}
	return "clean"
}

func run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	return string(out), err
}

// ensureProjectConfig writes <dir>/devkit.yaml when it is missing: the
// shared settings from the IDL repository's devkit.yaml `project:` block
// (module_prefix, vars) plus idl_repo pointing at the project's idl
// repository on this GitLab.
func ensureProjectConfig(dir, host string, projects []gitlab.Project) error {
	if _, err := os.Stat(filepath.Join(dir, workspace.ConfigFile)); err == nil {
		return nil
	}
	cfg := &workspace.Config{}
	for _, p := range projects {
		if p.Path == "idl" {
			cfg.IdlRepo = p.HTTPURL
		}
	}
	shared, err := workspace.LoadSharedProject(filepath.Join(dir, "idl"))
	if err != nil {
		return err
	}
	if shared != nil {
		cfg.ModulePrefix = shared.ModulePrefix
		cfg.Vars = shared.Vars
		cfg.GoPrivate = shared.GoPrivate
	}
	if cfg.ModulePrefix == "" {
		cfg.ModulePrefix = filepath.Base(dir)
	}
	if err := workspace.SaveConfig(dir, cfg); err != nil {
		return err
	}
	fmt.Printf("  %s written (module_prefix %s, idl_repo %s)\n", workspace.ConfigFile, cfg.ModulePrefix, cfg.IdlRepo)
	return nil
}

func init() {
	pullCmd.Flags().StringVar(&pullFlags.gitlab, "gitlab", "", "the GitLab the project is on (default: the saved one, or the only one with a token)")
	pullCmd.Flags().StringVar(&pullFlags.group, "group", "", "the GitLab group holding the project's repositories (default: the project name)")
	pullCmd.Flags().StringVar(&pullFlags.dir, "dir", "", "where to put the project (default: ./<project>, or the current directory when it is named so)")
	pullCmd.Flags().BoolVar(&pullFlags.noTools, "no-tools", false, "do not run make tools")
	rootCmd.AddCommand(pullCmd)
}
