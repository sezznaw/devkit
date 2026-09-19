package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/config"
	"github.com/sezznaw/devkit/internal/deps"
	"github.com/sezznaw/devkit/internal/installer"
	"github.com/sezznaw/devkit/internal/manifest"
	"github.com/sezznaw/devkit/internal/project"
	"github.com/sezznaw/devkit/internal/registry"
	"github.com/sezznaw/devkit/internal/render"
	"github.com/sezznaw/devkit/internal/ui"
	"github.com/sezznaw/devkit/internal/workspace"
)

// The registry component that produces a whole service project.
const serviceComponent = "kitex-service"

var ngsFlags struct {
	workspace  string
	module     string
	component  string
	version    string
	set        []string
	skipIdl    bool
	skipCommon bool
	noGit      bool
	skipHooks  bool
}

var ngsCmd = &cobra.Command{
	Use:     "ngs <service-name>",
	Aliases: []string{"new-service"},
	Short:   "Create a new Kitex gRPC service in the current project directory (ngs = new gRPC service)",
	Long: `ngs sets up everything a new service needs:

  1. clones (or updates) the project's IDL repository into ./idl
  2. clones the common library into ./common (for reading and local changes)
  3. generates ./<service> from the kitex-service component
  4. writes the initial Thrift IDL into ./idl/<service>/
  5. runs the code generator and go mod tidy
  6. makes the first git commit

Missing tools (Go, kitex, thriftgo) are installed automatically first; see
'devkit doctor'.

Run it in your project directory. The project's settings live in devkit.yaml
there (module_prefix, idl_repo, common_repo); on the first run in a new
directory ngs creates that file for you to fill in.`,
	Example: `  devkit ngs order
  devkit ngs order-item --module github.com/sezznaw/order-item
  devkit ngs order --set Port=9000 --skip-common`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNgs(cmd.Context(), args[0])
	},
}

func runNgs(ctx context.Context, name string) error {
	if err := registry.ValidateName(name); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Project directory: --workspace, else the nearest devkit.yaml above the
	// current directory, else the global workspace_dir, else right here.
	wsDir := firstNonEmpty(ngsFlags.workspace, workspace.Find("."), cfg.WorkspaceDir)
	if wsDir == "" {
		if root, err := project.FindRoot("."); err == nil {
			return fmt.Errorf("you are inside the service %s; run ngs from the project directory above it", root)
		}
		wsDir = "."
	}
	if wsDir, err = expandPath(wsDir); err != nil {
		return err
	}
	ws, err := workspace.LoadConfig(wsDir)
	if err != nil {
		return err
	}
	// Project settings: devkit.yaml wins over the global config.
	modulePrefix := firstNonEmpty(ws.ModulePrefix, cfg.ModulePrefix)
	idlRepo := firstNonEmpty(ws.IdlRepo, cfg.IdlRepo)
	commonRepo := firstNonEmpty(ws.CommonRepo, cfg.CommonRepo)

	// First run in a new project directory: hand the user a settings file to fill in.
	if (modulePrefix == "" && ngsFlags.module == "") || (idlRepo == "" && !ngsFlags.skipIdl) {
		file := filepath.Join(wsDir, workspace.ConfigFile)
		if _, statErr := os.Stat(file); os.IsNotExist(statErr) {
			if err := workspace.WriteTemplate(wsDir, &workspace.Config{ModulePrefix: modulePrefix, IdlRepo: idlRepo, CommonRepo: commonRepo}); err != nil {
				return err
			}
			return fmt.Errorf("this directory has no project settings yet.\n\n  I created %s\n  Fill in module_prefix, idl_repo and common_repo, then run `devkit ngs %s` again", file, name)
		}
		var missing []string
		if modulePrefix == "" {
			missing = append(missing, "module_prefix")
		}
		if idlRepo == "" {
			missing = append(missing, "idl_repo")
		}
		return fmt.Errorf("%s is incomplete: set %s, then run `devkit ngs %s` again", file, strings.Join(missing, " and "), name)
	}

	module := ngsFlags.module
	if module == "" {
		module = strings.TrimRight(modulePrefix, "/") + "/" + name
	}
	svcDir := filepath.Join(wsDir, name)
	if _, err := os.Stat(svcDir); err == nil {
		return fmt.Errorf("%s already exists", svcDir)
	}
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return err
	}
	src, err := registry.Open(cfg)
	if err != nil {
		return err
	}
	vars, err := parseSet(ngsFlags.set)
	if err != nil {
		return err
	}
	for k, v := range ws.Vars { // workspace defaults, overridable by --set
		if _, ok := vars[k]; !ok {
			vars[k] = v
		}
	}
	vars["Service"] = name
	vars["Module"] = module
	if _, ok := vars["IdlRepo"]; !ok && idlRepo != "" && !workspace.IsLocal(idlRepo) {
		vars["IdlRepo"] = idlRepo
	}
	if _, ok := vars["GoPrivate"]; !ok && ws.GoPrivate {
		vars["GoPrivate"] = workspace.OrgPattern(module)
	}
	component := firstNonEmpty(ngsFlags.component, ws.Component, serviceComponent)

	fmt.Fprintf(os.Stderr, "devkit ngs %s\n  project %s\n  module  %s\n\n", name, wsDir, module)
	r := ui.New(7)
	fail := func(err error) error { r.Failed(err); return err }

	// 1: tools. The component pins the kitex/thriftgo versions its Makefile uses.
	var statuses []deps.Status
	if err := r.Step("Checking tools (git, go, kitex, thriftgo)", func(s *ui.Step) error {
		idx, err := src.Index(ctx)
		if err != nil {
			return err
		}
		version := ngsFlags.version
		if version == "" {
			entry, ok := idx.Components[component]
			if !ok {
				return fmt.Errorf("component %q not found in registry", component)
			}
			version = entry.Version
		}
		dir, err := src.Fetch(ctx, component, version)
		if err != nil {
			return err
		}
		comp, err := registry.LoadComponent(dir)
		if err != nil {
			return err
		}
		resolved, err := installer.ResolveVars(svcDir, comp, vars)
		if err != nil {
			return err
		}
		want := deps.Want{KitexVersion: resolved["KitexVersion"], ThriftgoVersion: resolved["ThriftgoVersion"]}
		statuses, err = deps.Ensure(ctx, want, s)
		return err
	}); err != nil {
		return fail(err)
	}

	// 2 + 3: shared checkouts.
	idlDir := filepath.Join(wsDir, "idl")
	if err := r.Step("Preparing IDL repository", func(s *ui.Step) error {
		if ngsFlags.skipIdl {
			s.Log("skipped (--skip-idl)")
			return nil
		}
		return workspace.EnsureRepo(ctx, idlDir, workspace.RepoURL(cfg.GitHubHost, idlRepo), cfg.GitHubToken, s.Logf)
	}); err != nil {
		return fail(err)
	}
	if err := r.Step("Preparing common library checkout", func(s *ui.Step) error {
		if ngsFlags.skipCommon || commonRepo == "" {
			s.Log("skipped")
			return nil
		}
		if err := workspace.EnsureRepo(ctx, filepath.Join(wsDir, "common"), workspace.RepoURL(cfg.GitHubHost, commonRepo), cfg.GitHubToken, s.Logf); err != nil {
			s.Log("warning: %v", err)
		}
		if ws.GoPrivate {
			pattern := workspace.OrgPattern(module)
			changed, err := workspace.EnsureGoPrivate(ctx, pattern)
			if err != nil {
				s.Log("warning: %v", err)
			} else if changed {
				s.Log("GOPRIVATE now includes %s", pattern)
			}
		}
		return nil
	}); err != nil {
		return fail(err)
	}

	// 4: the service project.
	var in *installer.Installer
	if err := r.Step("Generating service from "+component, func(s *ui.Step) error {
		if err := os.MkdirAll(svcDir, 0o755); err != nil {
			return err
		}
		m := manifest.New(svcDir)
		if err := m.Save(); err != nil {
			return err
		}
		in = &installer.Installer{Root: svcDir, Source: src, Manifest: m, SkipHooks: true, Log: s.Logf, Output: s.Writer()}
		if err := in.Install(ctx, installer.Options{Name: component, Version: ngsFlags.version, Vars: vars}); err != nil {
			os.RemoveAll(svcDir)
			return err
		}
		return nil
	}); err != nil {
		return fail(err)
	}

	// 5: initial IDL into the shared repository.
	if err := r.Step("Writing initial IDL", func(s *ui.Step) error {
		if ngsFlags.skipIdl {
			s.Log("skipped (--skip-idl)")
			return nil
		}
		return writeIdl(ctx, src, component, in.Manifest.Components[component].Version, idlDir, vars, svcDir, s.Logf)
	}); err != nil {
		return fail(err)
	}

	// 6: hooks (codegen + tidy) now that the IDL exists.
	if err := r.Step("Generating code and resolving dependencies", func(s *ui.Step) error {
		if ngsFlags.skipHooks {
			s.Log("skipped (--skip-hooks)")
			return nil
		}
		in.Log = s.Logf
		in.Output = s.Writer()
		return in.RunHooks(ctx, component, "post_install")
	}); err != nil {
		return fail(err)
	}

	// 7: git.
	if err := r.Step("Creating first commit", func(s *ui.Step) error {
		if ngsFlags.noGit {
			s.Log("skipped (--no-git)")
			return nil
		}
		if err := workspace.InitRepo(ctx, svcDir, "chore: scaffold "+name+" with devkit ngs"); err != nil {
			s.Log("warning: %v", err)
		}
		return nil
	}); err != nil {
		return fail(err)
	}

	r.Done("service %s created at %s", name, svcDir)
	fmt.Println()
	if hint := deps.PathHint(statuses); hint != "" {
		fmt.Println(hint)
		fmt.Println()
	}
	fmt.Println("next steps:")
	fmt.Printf("  cd %s && make run     # starts with conf/dev.yaml (Nacos disabled)\n", svcDir)
	if !ngsFlags.skipIdl {
		fmt.Printf("  open a pull request in the IDL repository for idl/%s/%s.thrift\n", name, name)
	}
	fmt.Printf("  create the GitHub repository %s and push\n", module)
	return nil
}

// writeIdl renders the component's idl/ tree into the shared IDL checkout,
// never overwriting files that already exist there.
func writeIdl(ctx context.Context, src registry.Source, component, version, idlDir string, vars map[string]string, svcDir string, logf func(string, ...any)) error {
	compDir, err := src.Fetch(ctx, component, version)
	if err != nil {
		return err
	}
	comp, err := registry.LoadComponent(compDir)
	if err != nil {
		return err
	}
	full, err := installer.ResolveVars(svcDir, comp, vars)
	if err != nil {
		return err
	}
	plan, err := render.BuildFrom(filepath.Join(compDir, "idl"), full)
	if errors.Is(err, os.ErrNotExist) {
		return nil // component ships no IDL
	}
	if err != nil {
		return err
	}
	for _, f := range plan.Files {
		dst := filepath.Join(idlDir, filepath.FromSlash(f.Rel))
		if _, err := os.Stat(dst); err == nil {
			logf("  = idl/%s (exists, left unchanged)", f.Rel)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, f.Content, 0o644); err != nil {
			return err
		}
		logf("  + idl/%s", f.Rel)
	}
	return nil
}

func init() {
	ngsCmd.Flags().StringVar(&ngsFlags.workspace, "workspace", "", "project directory (default: nearest devkit.yaml above, else the current directory)")
	ngsCmd.Flags().StringVar(&ngsFlags.module, "module", "", "Go module path (default: <module_prefix>/<service>)")
	ngsCmd.Flags().StringVar(&ngsFlags.component, "component", "", "registry component to scaffold from (default: "+serviceComponent+")")
	ngsCmd.Flags().StringVar(&ngsFlags.version, "version", "", "component version (default: latest)")
	ngsCmd.Flags().StringArrayVar(&ngsFlags.set, "set", nil, "extra template variable, key=value (repeatable)")
	ngsCmd.Flags().BoolVar(&ngsFlags.skipIdl, "skip-idl", false, "do not clone the IDL repository or write the initial IDL")
	ngsCmd.Flags().BoolVar(&ngsFlags.skipCommon, "skip-common", false, "do not clone the common library")
	ngsCmd.Flags().BoolVar(&ngsFlags.noGit, "no-git", false, "do not git init / commit the new service")
	ngsCmd.Flags().BoolVar(&ngsFlags.skipHooks, "skip-hooks", false, "do not run code generation / go mod tidy")
	rootCmd.AddCommand(ngsCmd)
}
