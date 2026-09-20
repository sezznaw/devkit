// Package installer wires registry, render and manifest together: it is the
// engine behind `devkit ngs` (install) and `devkit update`.
package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sezznaw/devkit/internal/manifest"
	"github.com/sezznaw/devkit/internal/project"
	"github.com/sezznaw/devkit/internal/registry"
	"github.com/sezznaw/devkit/internal/render"
)

type Installer struct {
	Root     string
	Source   registry.Source
	Manifest *manifest.Manifest
	// Force overwrites user-modified or foreign files (keeping .bak copies).
	Force bool
	// SkipHooks disables post_install / post_update hooks.
	SkipHooks bool
	// Log receives progress lines.
	Log func(format string, args ...any)
	// Output receives hook output; defaults to stdout/stderr.
	Output io.Writer

	// LastNotes is the changelog between the previously installed version
	// and the one the most recent Update moved to.
	LastNotes []registry.ChangeEntry

	// LastSkipped lists the files the most recent Install/Update left alone
	// because they were modified locally (a .new copy was written for each).
	LastSkipped []string
}

func (in *Installer) runHooks(cmds []string) error {
	if in.Output != nil {
		return project.RunHooksTo(in.Root, cmds, in.Output, in.Output)
	}
	return project.RunHooks(in.Root, cmds)
}

// Options for one component installation or update.
type Options struct {
	Name    string
	Version string            // "" = latest from the index
	Vars    map[string]string // user supplied via --set
}

// Install installs a component and its dependencies (dependencies first).
func (in *Installer) Install(ctx context.Context, opts Options) error {
	idx, err := in.Source.Index(ctx)
	if err != nil {
		return err
	}
	return in.install(ctx, idx, opts, map[string]bool{}, true)
}

func (in *Installer) install(ctx context.Context, idx *registry.Index, opts Options, visiting map[string]bool, explicit bool) error {
	name := opts.Name
	if err := registry.ValidateName(name); err != nil {
		return err
	}
	if visiting[name] {
		return fmt.Errorf("dependency cycle involving %q", name)
	}
	visiting[name] = true
	defer delete(visiting, name)

	old := in.Manifest.Components[name]
	if old != nil {
		if !explicit {
			return nil // dependency already satisfied
		}
		if !in.Force {
			return fmt.Errorf("%s@%s is already installed; use `devkit update %s` or --force to reinstall", name, old.Version, name)
		}
	}

	if opts.Version == "" {
		v, err := latestVersion(idx, name)
		if err != nil {
			return err
		}
		opts.Version = v
	}
	comp, dir, err := in.fetch(ctx, name, opts.Version)
	if err != nil {
		return err
	}
	if err := rejectTracked(comp, opts.Vars); err != nil {
		return err
	}
	// Fail on missing variables before touching dependencies.
	if _, err := ResolveVars(in.Root, comp, opts.Vars); err != nil {
		return err
	}
	for _, dep := range comp.Deps {
		if err := in.install(ctx, idx, Options{Name: dep}, visiting, false); err != nil {
			return fmt.Errorf("dependency %s of %s: %w", dep, name, err)
		}
	}

	in.Log("installing %s@%s", name, comp.Version)
	if err := in.apply(comp, dir, opts.Vars, old, comp.Hooks.PostInstall, "post_install"); err != nil {
		return err
	}
	return nil
}

// Update brings an installed component to opts.Version (or the latest).
// Returns (false, nil) when it was already up to date.
func (in *Installer) Update(ctx context.Context, opts Options) (bool, error) {
	name := opts.Name
	old, ok := in.Manifest.Components[name]
	if !ok {
		return false, fmt.Errorf("component %s is not part of this service", name)
	}
	idx, err := in.Source.Index(ctx)
	if err != nil {
		return false, err
	}
	target := opts.Version
	if target == "" {
		if target, err = latestVersion(idx, name); err != nil {
			return false, err
		}
	}
	comp, dir, err := in.fetch(ctx, name, target)
	if err != nil {
		return false, err
	}
	// Refuse a version choice even when there is nothing to update; ignoring it
	// silently would let someone believe the pin took effect.
	if err := rejectTracked(comp, opts.Vars); err != nil {
		return false, err
	}
	if target == old.Version && !in.Force {
		return false, nil
	}
	for _, dep := range comp.Deps {
		if err := in.install(ctx, idx, Options{Name: dep}, map[string]bool{name: true}, false); err != nil {
			return false, fmt.Errorf("dependency %s of %s: %w", dep, name, err)
		}
	}

	// Reuse what the service was created with; --set overrides it. Tracked
	// variables (versions) are never reused: they come from the template.
	vars := map[string]string{}
	for k, v := range old.Vars {
		if d := comp.VarByName(k); d != nil && d.Track {
			continue
		}
		vars[k] = v
	}
	for k, v := range opts.Vars {
		vars[k] = v
	}

	in.Log("updating %s %s -> %s", name, old.Version, comp.Version)
	from := old.Version
	if err := in.apply(comp, dir, vars, old, comp.Hooks.PostUpdate, "post_update"); err != nil {
		return false, err
	}
	in.LastNotes = comp.ChangesBetween(from, comp.Version)
	return true, nil
}

// Remove deletes a component's unmodified files and drops it from the manifest.
func (in *Installer) Remove(name string) error {
	old, ok := in.Manifest.Components[name]
	if !ok {
		return fmt.Errorf("%s is not installed", name)
	}
	if deps := in.Manifest.Dependents(name); len(deps) > 0 && !in.Force {
		return fmt.Errorf("%s is required by %s; remove those first or use --force", name, strings.Join(deps, ", "))
	}
	in.Log("removing %s@%s", name, old.Version)
	_, res, err := in.applyPlan(name, &render.Plan{}, old)
	if err != nil {
		return err
	}
	in.report(res)
	delete(in.Manifest.Components, name)
	return in.Manifest.Save()
}

// Latest returns the newest published version of a component.
func (in *Installer) Latest(ctx context.Context, name string) (*registry.Component, error) {
	idx, err := in.Source.Index(ctx)
	if err != nil {
		return nil, err
	}
	v, err := latestVersion(idx, name)
	if err != nil {
		return nil, err
	}
	comp, _, err := in.fetch(ctx, name, v)
	return comp, err
}

// Outdated lists installed components whose registry version differs.
type Outdated struct {
	Name, Installed, Latest string
}

func (in *Installer) Outdated(ctx context.Context) ([]Outdated, error) {
	idx, err := in.Source.Index(ctx)
	if err != nil {
		return nil, err
	}
	var out []Outdated
	for _, name := range in.Manifest.Names() {
		c := in.Manifest.Components[name]
		entry, ok := idx.Components[name]
		if !ok {
			out = append(out, Outdated{Name: name, Installed: c.Version, Latest: "(removed from registry)"})
			continue
		}
		if entry.Version != c.Version {
			out = append(out, Outdated{Name: name, Installed: c.Version, Latest: entry.Version})
		}
	}
	return out, nil
}

// apply renders, reconciles files, saves the manifest and runs hooks.
func (in *Installer) apply(comp *registry.Component, dir string, userVars map[string]string, old *manifest.Installed, hooks []string, hookName string) error {
	vars, err := ResolveVars(in.Root, comp, userVars)
	if err != nil {
		return err
	}
	plan, err := render.Build(dir, vars)
	if err != nil {
		return err
	}
	plan, once := splitOnce(comp, plan)
	hashes, res, err := in.applyPlan(comp.Name, plan, old)
	if err != nil {
		return err
	}
	in.LastSkipped = res.Skipped
	in.report(res)
	if err := in.writeOnce(once); err != nil {
		return err
	}

	in.Manifest.Components[comp.Name] = &manifest.Installed{
		Version:     comp.Version,
		InstalledAt: time.Now().UTC(),
		Vars:        userVarsOnly(comp, vars),
		Deps:        comp.Deps,
		Files:       hashes,
	}
	if err := in.Manifest.Save(); err != nil {
		return err
	}

	if !in.SkipHooks && len(hooks) > 0 {
		cmds, err := render.Strings(hooks, vars)
		if err != nil {
			return err
		}
		in.Log("running %s hooks", hookName)
		if err := in.runHooks(cmds); err != nil {
			return err
		}
	}
	if len(res.Skipped) > 0 {
		in.Log("%d file(s) were modified locally and not updated. Merge the %s copies by hand, or rerun with --force to overwrite (backups are kept as %s).",
			len(res.Skipped), NewSuffix, BackupSuffix)
	}
	return nil
}

// RunHooks executes a component's hooks of the given kind ("post_install" or
// "post_update") with the variables stored in the manifest. Used by callers
// that install with SkipHooks and need to run them later.
func (in *Installer) RunHooks(ctx context.Context, name, kind string) error {
	inst, ok := in.Manifest.Components[name]
	if !ok {
		return fmt.Errorf("%s is not installed", name)
	}
	comp, _, err := in.fetch(ctx, name, inst.Version)
	if err != nil {
		return err
	}
	hooks := comp.Hooks.PostInstall
	if kind == "post_update" {
		hooks = comp.Hooks.PostUpdate
	}
	if len(hooks) == 0 {
		return nil
	}
	vars, err := ResolveVars(in.Root, comp, inst.Vars)
	if err != nil {
		return err
	}
	cmds, err := render.Strings(hooks, vars)
	if err != nil {
		return err
	}
	in.Log("running %s hooks", kind)
	return in.runHooks(cmds)
}

// splitOnce separates files the component marks as write-once from the tracked plan.
func splitOnce(comp *registry.Component, plan *render.Plan) (tracked *render.Plan, once []render.PlannedFile) {
	tracked = &render.Plan{}
	for _, f := range plan.Files {
		if comp.IsOnce(f.Rel) {
			once = append(once, f)
		} else {
			tracked.Files = append(tracked.Files, f)
		}
	}
	return tracked, once
}

// writeOnce creates write-once files that do not exist yet and leaves existing ones alone.
func (in *Installer) writeOnce(files []render.PlannedFile) error {
	for _, f := range files {
		abs := in.abs(f.Rel)
		if _, err := os.Stat(abs); err == nil {
			// The developer's file. Saying so on every update is noise, and
			// across a whole project it buries the lines that matter.
			continue
		}
		if err := writeFile(abs, f.Content, f.Mode); err != nil {
			return err
		}
		in.Log("  + %s  (created once, not managed by devkit)", f.Rel)
	}
	return nil
}

func (in *Installer) fetch(ctx context.Context, name, version string) (*registry.Component, string, error) {
	dir, err := in.Source.Fetch(ctx, name, version)
	if err != nil {
		return nil, "", err
	}
	comp, err := registry.LoadComponent(dir)
	if err != nil {
		return nil, "", err
	}
	return comp, dir, nil
}

func latestVersion(idx *registry.Index, name string) (string, error) {
	entry, ok := idx.Components[name]
	if !ok {
		return "", fmt.Errorf("component %q not found in registry (known: %s)", name, strings.Join(indexNames(idx), ", "))
	}
	return entry.Version, nil
}

// ResolveVars merges built-in variables, component defaults and user input,
// and checks that every required variable has a value.
func ResolveVars(root string, comp *registry.Component, user map[string]string) (map[string]string, error) {
	vars := project.BuiltinVars(root)
	vars["ComponentName"] = comp.Name
	vars["ComponentVersion"] = comp.Version
	var missing []string
	for _, v := range comp.Vars {
		if val, ok := user[v.Name]; ok {
			vars[v.Name] = val
			continue
		}
		if v.Default != "" {
			vars[v.Name] = v.Default
			continue
		}
		if v.Required {
			missing = append(missing, v.Name)
			continue
		}
		vars[v.Name] = ""
	}
	// Anything else the user passed overrides built-ins, e.g. --set Module=...
	// when the project has no go.mod yet.
	for k, v := range user {
		if !declared(comp, k) {
			vars[k] = v
		}
	}
	if len(missing) > 0 {
		var lines []string
		for _, v := range comp.Vars {
			for _, m := range missing {
				if v.Name == m {
					lines = append(lines, fmt.Sprintf("  --set %s=<value>   %s", v.Name, v.Description))
				}
			}
		}
		return nil, fmt.Errorf("component %s requires variables:\n%s", comp.Name, strings.Join(lines, "\n"))
	}
	return vars, nil
}

func declared(comp *registry.Component, name string) bool {
	for _, v := range comp.Vars {
		if v.Name == name {
			return true
		}
	}
	return false
}

// rejectTracked refuses an attempt to choose a tracked variable. Versions are
// decided by the template for the whole team; a per-service choice is exactly
// the drift this rule exists to prevent.
func rejectTracked(comp *registry.Component, vars map[string]string) error {
	var names []string
	for k := range vars {
		if d := comp.VarByName(k); d != nil && d.Track {
			names = append(names, k)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return fmt.Errorf("%s cannot be set: versions are fixed by the %s template (currently %s) so that every service uses the same ones; remove it from --set and from `vars:` in devkit.yaml",
		strings.Join(names, ", "), comp.Name, trackedSummary(comp))
}

func trackedSummary(comp *registry.Component) string {
	var parts []string
	for _, v := range comp.Vars {
		if v.Track && strings.HasSuffix(v.Name, "Version") {
			parts = append(parts, v.Name+"="+v.Default)
		}
	}
	return strings.Join(parts, ", ")
}

func userVarsOnly(comp *registry.Component, vars map[string]string) map[string]string {
	out := map[string]string{}
	for _, v := range comp.Vars {
		out[v.Name] = vars[v.Name]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func indexNames(idx *registry.Index) []string {
	names := make([]string, 0, len(idx.Components))
	for n := range idx.Components {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
