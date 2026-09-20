package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

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
				fmt.Printf("== %s\n", name)
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
				fmt.Printf("   error: %v\n", err)
				failed = append(failed, name)
			}
		}
		if single {
			printNotes(notes)
			return nil
		}

		fmt.Printf("\n%d service(s): %d updated, %d already up to date", len(roots), updated, current)
		if len(failed) > 0 {
			fmt.Printf(", %d failed (%s)", len(failed), strings.Join(failed, ", "))
		}
		fmt.Println()
		if len(merged) > 0 {
			fmt.Println("modified locally, not overwritten; merge the .new copy by hand or rerun with --force:")
			for _, f := range merged {
				fmt.Printf("  %s\n", f)
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
	fmt.Println("\nwhat changed:")
	for _, e := range notes {
		fmt.Printf("  %s\n", e.Version)
		for _, c := range e.Changes {
			fmt.Printf("    - %s\n", c)
		}
	}
	first := true
	for _, e := range notes {
		for _, a := range e.Action {
			if first {
				fmt.Println("\nyour own files are never rewritten; you may want to:")
				first = false
			}
			for i, line := range strings.Split(a, "\n") {
				if i == 0 {
					fmt.Printf("  * [%s] %s\n", e.Version, line)
				} else {
					fmt.Printf("      %s\n", line)
				}
			}
		}
	}
	if !first {
		fmt.Println("\nevery setting is documented in conf/README.md of each service.")
	}
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
	fmt.Printf("--force will overwrite %d file(s) you modified (each is kept as <file>.bak):\n", len(files))
	for _, f := range files {
		fmt.Printf("  %s\n", f)
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
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if single {
		fmt.Fprintln(w, "COMPONENT\tINSTALLED\tLATEST\tFILES\tLOCAL CHANGES")
	} else {
		fmt.Fprintln(w, "SERVICE\tCOMPONENT\tINSTALLED\tLATEST\tFILES\tLOCAL CHANGES")
	}
	rows := 0
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
			for _, st := range states {
				switch st {
				case manifest.Modified:
					modified++
				case manifest.Missing:
					missing++
				}
			}
			changes := "none"
			if modified > 0 || missing > 0 {
				changes = fmt.Sprintf("%d modified, %d missing", modified, missing)
			}
			l := latest[name]
			if l == "" {
				l = c.Version + " (up to date)"
			}
			if single {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", name, c.Version, l, len(c.Files), changes)
			} else {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", filepath.Base(root), name, c.Version, l, len(c.Files), changes)
			}
			rows++
		}
	}
	if rows == 0 {
		fmt.Println("no components installed")
		return nil
	}
	return w.Flush()
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
