package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/config"
	"github.com/sezznaw/devkit/internal/installer"
	"github.com/sezznaw/devkit/internal/manifest"
	"github.com/sezznaw/devkit/internal/project"
	"github.com/sezznaw/devkit/internal/registry"
	"github.com/sezznaw/devkit/internal/ui"
	"github.com/sezznaw/devkit/internal/workspace"
)

var updateFlags struct {
	version   string
	set       []string
	force     bool
	skipHooks bool
	check     bool
	yes       bool
}

var updateCmd = &cobra.Command{
	Use:   "update [component...]",
	Short: "Update the devkit-managed files (Makefile, CI, Dockerfile, main.go): one service, or every service of the project",
	Long: `Update re-renders the managed files at the newest template version using the
variables recorded when the service was created.

Where you run it decides the scope:
  inside a service            only that service
  in the project directory    every service below it (directories created by devkit)

Files you have not touched are overwritten. Files you modified are left in
place and the incoming version is written next to them with a .new suffix so
you can merge by hand; pass --force to overwrite them instead (a .bak copy is
kept). Files the new version no longer produces are deleted unless modified.
Files you own (go.mod, idl.mk, conf/, handler/) are never touched.`,
	Example: `  devkit update --check      # what would change, for one service or all of them
  devkit update
  devkit update --force --set CI=gitlab`,
	RunE: func(cmd *cobra.Command, args []string) error {
		roots, single, err := updateScope()
		if err != nil {
			return err
		}
		if updateFlags.check {
			return printStatus(cmd, roots, single)
		}
		vars, err := parseSet(updateFlags.set)
		if err != nil {
			return err
		}
		if updateFlags.version != "" && len(args) != 1 {
			return fmt.Errorf("--version can only be used with exactly one component")
		}
		if updateFlags.force && !single {
			if err := confirmForce(roots); err != nil {
				return err
			}
		}

		var failed, merged []string
		var notes []registry.ChangeEntry
		updated, current := 0, 0
		for _, root := range roots {
			name := filepath.Base(root)
			if !single {
				fmt.Println(ui.Stdout.Heading("== " + name))
			}
			in, err := newInstallerAt(root, updateFlags.force, updateFlags.skipHooks)
			if err == nil {
				var changed bool
				changed, err = updateOne(cmd, in, args, vars)
				if changed {
					updated++
				} else if err == nil {
					current++
				}
				for _, f := range in.LastSkipped {
					merged = append(merged, filepath.Join(name, f))
				}
				notes = mergeNotes(notes, in.LastNotes)
			}
			if err != nil {
				if single {
					return err
				}
				// One broken service must not stop the others.
				fmt.Printf("   %s %v\n", ui.Stdout.Failure("error:"), err)
				failed = append(failed, name)
			}
		}
		// Whatever the scope, the project's common/ and go.work follow the team.
		syncAfterUpdate(cmd, roots, single)
		if single {
			printNotes(notes)
			return nil
		}

		st := ui.Stdout
		summary := fmt.Sprintf("%d service(s): %s, %d already up to date", len(roots), st.Green(fmt.Sprintf("%d updated", updated)), current)
		if len(failed) > 0 {
			summary += ", " + st.Failure(fmt.Sprintf("%d failed (%s)", len(failed), strings.Join(failed, ", ")))
		}
		fmt.Println("\n" + st.Bold(summary))
		if len(merged) > 0 {
			fmt.Println(st.Attention("modified locally, not overwritten") + "; merge the .new copy by hand or rerun with --force:")
			for _, f := range merged {
				fmt.Printf("  %s\n", st.Yellow(f))
			}
		}
		printNotes(notes)
		if len(failed) > 0 {
			return fmt.Errorf("%d service(s) could not be updated", len(failed))
		}
		return nil
	},
}

// mergeNotes adds entries not seen yet (several services usually move through
// the same versions; the notes are printed once).
func mergeNotes(have, more []registry.ChangeEntry) []registry.ChangeEntry {
	for _, e := range more {
		dup := false
		for _, h := range have {
			if h.Version == e.Version {
				dup = true
				break
			}
		}
		if !dup {
			have = append(have, e)
		}
	}
	sort.SliceStable(have, func(i, j int) bool { return registry.CompareVersions(have[i].Version, have[j].Version) < 0 })
	return have
}

// printNotes tells the developer what the update brought, and above all what
// it could not do for them: files they own are never rewritten, so new
// settings only reach them through this text.
func printNotes(notes []registry.ChangeEntry) {
	if len(notes) == 0 {
		return
	}
	st := ui.Stdout
	fmt.Println("\n" + st.Heading("what changed:"))
	for _, e := range notes {
		fmt.Printf("  %s\n", st.Cyan(e.Version))
		for _, c := range e.Changes {
			fmt.Printf("    - %s\n", c)
		}
	}
	first := true
	for _, e := range notes {
		for _, a := range e.Action {
			if first {
				fmt.Println("\n" + st.Attention("your own files are never rewritten; you may want to:"))
				first = false
			}
			lines := strings.Split(a, "\n")
			// "Do this:" followed by lines is a snippet to copy, worth bold.
			// Anything else that wraps is just a long sentence.
			snippet := strings.HasSuffix(strings.TrimSpace(lines[0]), ":")
			for i, line := range lines {
				switch {
				case i == 0:
					fmt.Printf("  %s %s %s\n", st.Yellow("*"), st.Cyan("["+e.Version+"]"), line)
				case snippet:
					fmt.Printf("      %s\n", st.Bold(line))
				default:
					fmt.Printf("      %s\n", line)
				}
			}
		}
	}
	if !first {
		fmt.Println("\n" + st.Dim("every setting is documented in conf/README.md of each service."))
	}
}

// projectOf returns the project directory the services live in.
func projectOf(roots []string, single bool) string {
	if len(roots) == 0 {
		return ""
	}
	if dir := workspace.Find(roots[0]); dir != "" {
		return dir
	}
	return filepath.Dir(roots[0])
}

func teamFor(cmd *cobra.Command, root string) (teamValues, bool) {
	in, err := newInstallerAt(root, false, true)
	if err != nil {
		return teamValues{}, false
	}
	for _, name := range in.Manifest.Names() {
		if comp, err := in.Latest(cmd.Context(), name); err == nil {
			if t := teamValuesOf(comp); t.CommonVersion != "" {
				return t, true
			}
		}
	}
	return teamValues{}, false
}

func syncAfterUpdate(cmd *cobra.Command, roots []string, single bool) {
	project := projectOf(roots, single)
	team, ok := teamFor(cmd, roots[0])
	cfg, err := config.Load()
	if project == "" || !ok || err != nil {
		return
	}
	if _, statErr := os.Stat(filepath.Join(project, "common")); statErr != nil {
		team.CommonVersion = "" // this project opted out of the checkout (--skip-common)
	}
	syncProject(cmd.Context(), cfg, project, team, false, logf)
}

// updateScope returns the services to operate on. single is true when the
// command was started inside one service.
func updateScope() (roots []string, single bool, err error) {
	if root, ferr := project.FindRoot("."); ferr == nil {
		return []string{root}, true, nil
	}
	dir := workspace.Find(".")
	if dir == "" {
		if dir, err = filepath.Abs("."); err != nil {
			return nil, false, err
		}
	}
	roots, err = project.Services(dir)
	if err != nil {
		return nil, false, err
	}
	if len(roots) == 0 {
		return nil, false, fmt.Errorf("no devkit services found: run this inside a service, or in the project directory that contains them (%s has none)", dir)
	}
	return roots, false, nil
}

// updateOne updates the named components of one service, or all of them.
func updateOne(cmd *cobra.Command, in *installer.Installer, names []string, vars map[string]string) (bool, error) {
	if len(names) == 0 {
		names = in.Manifest.Names()
	}
	anyChanged := false
	var skipped []string
	for _, name := range names {
		changed, err := in.Update(cmd.Context(), installer.Options{Name: name, Version: updateFlags.version, Vars: vars})
		if err != nil {
			return anyChanged, err
		}
		skipped = append(skipped, in.LastSkipped...)
		if changed {
			anyChanged = true
		} else {
			fmt.Printf("%s is up to date (%s)\n", name, in.Manifest.Components[name].Version)
		}
	}
	in.LastSkipped = skipped
	return anyChanged, nil
}

// confirmForce protects a project-wide --force: it lists the locally modified
// managed files that are about to be overwritten and asks before doing it.
func confirmForce(roots []string) error {
	var files []string
	for _, root := range roots {
		m, err := manifest.Load(root)
		if err != nil {
			continue
		}
		for _, comp := range m.Names() {
			states, err := m.CheckFiles(root, comp)
			if err != nil {
				continue
			}
			for rel, st := range states {
				if st == manifest.Modified {
					files = append(files, filepath.Join(filepath.Base(root), rel))
				}
			}
		}
	}
	if len(files) == 0 || updateFlags.yes {
		return nil
	}
	fmt.Println(ui.Stdout.Attention(fmt.Sprintf("--force will overwrite %d file(s) you modified", len(files))) + " (each is kept as <file>.bak):")
	for _, f := range files {
		fmt.Printf("  %s\n", ui.Stdout.Yellow(f))
	}
	if !ui.IsTerminal(os.Stdin) {
		return errors.New("refusing to overwrite them without confirmation; pass --yes to proceed")
	}
	fmt.Print("Continue? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		return errors.New("aborted")
	}
	return nil
}

// printStatus shows installed and latest versions and local changes, for one
// service or for every service of the project.
func printStatus(cmd *cobra.Command, roots []string, single bool) error {
	st := ui.Stdout
	header := []ui.Cell{ui.C("COMPONENT", nil), ui.C("INSTALLED", nil), ui.C("LATEST", nil), ui.C("KITEX", nil), ui.C("FILES", nil), ui.C("LOCAL CHANGES", nil)}
	team := map[string]string{} // component -> the Kitex version the newest template prescribes
	if !single {
		header = append([]ui.Cell{ui.C("SERVICE", nil)}, header...)
	}
	rows := [][]ui.Cell{header}
	for _, root := range roots {
		in, err := newInstallerAt(root, false, true)
		if err != nil {
			return err
		}
		latest := map[string]string{}
		outdated, err := in.Outdated(cmd.Context())
		if err != nil {
			return err
		}
		for _, o := range outdated {
			latest[o.Name] = o.Latest
		}
		for _, name := range in.Manifest.Names() {
			c := in.Manifest.Components[name]
			states, err := in.Manifest.CheckFiles(in.Root, name)
			if err != nil {
				return err
			}
			modified, missing := 0, 0
			for _, s := range states {
				switch s {
				case manifest.Modified:
					modified++
				case manifest.Missing:
					missing++
				}
			}
			// Green: nothing to do. Yellow: something for you to look at.
			changes := ui.C("none", st.Dim)
			if modified > 0 || missing > 0 {
				changes = ui.C(fmt.Sprintf("%d modified, %d missing", modified, missing), st.Yellow)
			}
			latestCell := ui.C(c.Version+" (up to date)", st.Green)
			installed := ui.C(c.Version, nil)
			if l := latest[name]; l != "" {
				latestCell = ui.C(l, st.Attention)
				installed = ui.C(c.Version, st.Yellow)
			}
			if _, seen := team[name]; !seen {
				team[name] = ""
				if comp, err := in.Latest(cmd.Context(), name); err == nil {
					if v := comp.VarByName("KitexVersion"); v != nil {
						team[name] = v.Default
					}
				}
			}
			// What the service is actually built with, from its own go.mod.
			kitex := ui.C("-", st.Dim)
			if have := project.RequiredVersion(root, "github.com/cloudwego/kitex"); have != "" {
				switch want := team[name]; {
				case want == "" || have == want:
					kitex = ui.C(have, st.Green)
				default:
					kitex = ui.C(have+" (team: "+want+")", st.Attention)
				}
			}
			row := []ui.Cell{ui.C(name, nil), installed, latestCell, kitex, ui.C(fmt.Sprint(len(c.Files)), nil), changes}
			if !single {
				row = append([]ui.Cell{ui.C(filepath.Base(root), st.Bold)}, row...)
			}
			rows = append(rows, row)
		}
	}
	if len(rows) == 1 {
		fmt.Println("no components installed")
		return nil
	}
	ui.Table(os.Stdout, rows)

	// The common library checkout the IDE navigates into.
	project := projectOf(roots, single)
	if _, err := os.Stat(filepath.Join(project, "common", ".git")); err == nil {
		if team, ok := teamFor(cmd, roots[0]); ok {
			c := workspace.CommonStatus(cmd.Context(), filepath.Join(project, "common"), team.CommonVersion)
			switch {
			case c.Dirty:
				fmt.Printf("\n%s %s, %s\n", st.Bold("common/"), c.Version, st.Attention("has local changes: builds here use them, CI does not (team: "+c.Want+")"))
			case c.Version != c.Want:
				fmt.Printf("\n%s %s\n", st.Bold("common/"), st.Attention(c.Version+" (team: "+c.Want+"); run `devkit update`"))
			default:
				fmt.Printf("\n%s %s\n", st.Bold("common/"), st.Green(c.Version+" (team version)"))
			}
		}
	}
	return nil
}

func init() {
	updateCmd.Flags().StringVar(&updateFlags.version, "version", "", "update to a specific version (single component only)")
	updateCmd.Flags().StringArrayVar(&updateFlags.set, "set", nil, "override a template variable, key=value (repeatable)")
	updateCmd.Flags().BoolVar(&updateFlags.force, "force", false, "overwrite locally modified files (keeps .bak copies)")
	updateCmd.Flags().BoolVar(&updateFlags.yes, "yes", false, "with --force in a project directory: do not ask for confirmation")
	updateCmd.Flags().BoolVar(&updateFlags.skipHooks, "skip-hooks", false, "do not run post_update hooks")
	updateCmd.Flags().BoolVar(&updateFlags.check, "check", false, "show installed and latest versions and local changes, without updating")
	rootCmd.AddCommand(updateCmd)
}
