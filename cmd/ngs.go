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

// serviceKind is what differs between the commands that create a service.
type serviceKind struct {
	command   string // "ngs"
	component string // the template it generates from
	tools     string // for the first step
	needsHz   bool
}

var (
	rpcService = serviceKind{command: "ngs", component: serviceComponent, tools: "git, go, kitex, thriftgo"}
	apiService = serviceKind{command: "nas", component: apiComponent, tools: "git, go, hz, kitex, thriftgo", needsHz: true}
)

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

Run it in your project directory. No configuration is required. Optional
project settings live in devkit.yaml there (created on the first run):
module_prefix (default: <directory name>) and idl_repo (default: a local idl/
git repository).`,
	Example: `  devkit ngs order
  devkit ngs order-item --module github.com/sezznaw/order-item
  devkit ngs order --set Port=9000 --skip-common`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNew(cmd.Context(), rpcService, args[0])
	},
}

// runNew creates a service of the given kind. ngs and nas are the same steps
// with another template underneath.
func runNew(ctx context.Context, kind serviceKind, name string) error {
	if err := registry.ValidateName(name); err != nil {
		return err
	}
	if err := workspace.ValidateServiceName(name); err != nil {
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
	// Project settings: devkit.yaml wins over the global config. All optional.
	modulePrefix := firstNonEmpty(ws.ModulePrefix, cfg.ModulePrefix, workspace.DefaultModulePrefix(wsDir))
	idlRepo := firstNonEmpty(ws.IdlRepo, cfg.IdlRepo) // empty: local idl/ repository

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
	// The CI templates need the IDL project's path (owner/repo or group/project),
	// not its clone URL, and which platform to generate a pipeline for.
	if _, ok := vars["IdlRepo"]; !ok {
		if _, path := workspace.ParseRepo(idlRepo, cfg.GitHubHost); path != "" {
			vars["IdlRepo"] = path
		}
	}
	if _, ok := vars["CI"]; !ok {
		vars["CI"] = workspace.DetectCI(module, idlRepo, cfg.GitHubHost)
	}
	if _, ok := vars["GoPrivate"]; !ok && ws.GoPrivate {
		vars["GoPrivate"] = workspace.OrgPattern(module)
	}
	component := firstNonEmpty(ngsFlags.component, kind.component)
	if kind.command == rpcService.command {
		component = firstNonEmpty(ngsFlags.component, ws.Component, kind.component) // devkit.yaml "component" is the RPC template's
	}

	es := ui.Stderr
	fmt.Fprintf(os.Stderr, "%s\n  %s %s\n  %s %s\n\n", es.Heading("devkit "+kind.command+" "+name), es.Dim("project"), wsDir, es.Dim("module "), es.Cyan(module))
	r := ui.New(7)
	fail := func(err error) error { r.Failed(err); return err }

	// 1: tools. The component pins the generator versions its Makefile uses.
	var statuses []deps.Status
	var team teamValues
	if err := r.Step("Checking tools ("+kind.tools+")", func(s *ui.Step) error {
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
		team = teamValuesOf(comp)
		want := deps.Want{KitexVersion: resolved["KitexVersion"], ThriftgoVersion: resolved["ThriftgoVersion"]}
		if kind.needsHz {
			if want.HzVersion = resolved["HzVersion"]; want.HzVersion == "" {
				return fmt.Errorf("component %s does not say which hz to use (HzVersion)", component)
			}
		}
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
		if idlRepo == "" {
			return workspace.EnsureLocalRepo(ctx, idlDir, s.Logf)
		}
		return workspace.EnsureRepo(ctx, idlDir, workspace.RepoURL(cfg.GitHubHost, idlRepo), cfg.GitHubToken, cfg.GitHubHost, s.Logf)
	}); err != nil {
		return fail(err)
	}
	if err := r.Step("Preparing common library (team version "+team.CommonVersion+")", func(s *ui.Step) error {
		if ngsFlags.skipCommon {
			s.Log("skipped (--skip-common)")
		} else {
			// go.work is written at the end, once the new service exists.
			syncProject(ctx, cfg, wsDir, teamValues{CommonVersion: team.CommonVersion}, false, s.Logf)
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
			syncProject(ctx, cfg, wsDir, teamValues{GoVersion: team.GoVersion}, true, s.Logf)
			return nil
		}
		if err := workspace.InitRepo(ctx, svcDir, "chore: scaffold "+name+" with devkit "+kind.command); err != nil {
			s.Log("warning: %v", err)
		}
		// The new service joins go.work so the IDE resolves the common library
		// to common/ for it as well.
		syncProject(ctx, cfg, wsDir, teamValues{GoVersion: team.GoVersion}, true, s.Logf)
		return nil
	}); err != nil {
		return fail(err)
	}

	settings := filepath.Join(wsDir, workspace.ConfigFile)
	createdSettings := false
	if _, statErr := os.Stat(settings); os.IsNotExist(statErr) {
		if err := workspace.WriteTemplate(wsDir, &workspace.Config{ModulePrefix: ws.ModulePrefix, IdlRepo: ws.IdlRepo, CommonRepo: ws.CommonRepo}); err == nil {
			createdSettings = true
		}
	}

	r.Done("service %s created at %s", name, svcDir)
	fmt.Println()
	if hint := deps.PathHint(statuses); hint != "" {
		fmt.Println(ui.Stdout.Attention(hint))
		fmt.Println()
	}
	st := ui.Stdout
	fmt.Println(st.Heading("next steps:"))
	fmt.Printf("  %s     %s\n", st.Bold("cd "+svcDir+" && make run"), st.Dim("# conf/local.yaml: your machine, with the Nacos on it (the file says how to start one)"))
	switch {
	case ngsFlags.skipIdl:
	case idlRepo == "":
		fmt.Printf("  the IDL is in %s, a local git repository; when you have a git server,\n", idlDir)
		fmt.Printf("  push it there and put its address into idl_repo in %s\n", settings)
	default:
		fmt.Printf("  commit and push idl/%s/%s.thrift to the IDL repository so others can use it\n", name, name)
	}
	fmt.Printf("  create the remote repository for %s and push the service\n", module)
	if createdSettings {
		fmt.Printf("\n%s\n", st.Dim("project settings (all optional) were written to "+settings))
	}
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
	addNewServiceFlags(ngsCmd, rpcService)
	rootCmd.AddCommand(ngsCmd)
}

// addNewServiceFlags gives ngs and nas the same flags; only one of the two
// commands runs in a process, so they share the variables.
func addNewServiceFlags(cmd *cobra.Command, kind serviceKind) {
	ngsCmd := cmd
	ngsCmd.Flags().StringVar(&ngsFlags.workspace, "workspace", "", "project directory (default: nearest devkit.yaml above, else the current directory)")
	ngsCmd.Flags().StringVar(&ngsFlags.module, "module", "", "Go module path (default: <module_prefix>/<service>)")
	ngsCmd.Flags().StringVar(&ngsFlags.component, "component", "", "registry component to scaffold from (default: "+kind.component+")")
	ngsCmd.Flags().StringVar(&ngsFlags.version, "version", "", "component version (default: latest)")
	ngsCmd.Flags().StringArrayVar(&ngsFlags.set, "set", nil, "extra template variable, key=value (repeatable)")
	ngsCmd.Flags().BoolVar(&ngsFlags.skipIdl, "skip-idl", false, "do not clone the IDL repository or write the initial IDL")
	ngsCmd.Flags().BoolVar(&ngsFlags.skipCommon, "skip-common", false, "do not clone the common library")
	ngsCmd.Flags().BoolVar(&ngsFlags.noGit, "no-git", false, "do not git init / commit the new service")
	ngsCmd.Flags().BoolVar(&ngsFlags.skipHooks, "skip-hooks", false, "do not run code generation / go mod tidy")
}
