package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/installer"
	"github.com/sezznaw/devkit/internal/manifest"
)

var updateFlags struct {
	version   string
	set       []string
	force     bool
	skipHooks bool
	check     bool
}

var updateCmd = &cobra.Command{
	Use:   "update [component...]",
	Short: "Update the devkit-managed files of this service (Makefile, CI, Dockerfile, main.go)",
	Long: `Update re-renders a component at its newest version using the variables
recorded at install time.

Files you have not touched are overwritten. Files you modified are left in
place and the incoming version is written next to them with a .new suffix so
you can merge by hand; pass --force to overwrite them instead (a .bak copy is
kept). Files the new version no longer produces are deleted unless modified.

With no arguments every installed component is updated.`,
	Example: `  devkit update
  devkit update logger
  devkit update logger --version 0.3.0
  devkit update --check`,
	RunE: func(cmd *cobra.Command, args []string) error {
		in, err := newInstaller(updateFlags.force, updateFlags.skipHooks)
		if err != nil {
			return err
		}
		if updateFlags.check {
			return printStatus(cmd, in)
		}
		vars, err := parseSet(updateFlags.set)
		if err != nil {
			return err
		}
		names := args
		if len(names) == 0 {
			names = in.Manifest.Names()
			if len(names) == 0 {
				fmt.Println("no components installed")
				return nil
			}
		}
		if updateFlags.version != "" && len(names) != 1 {
			return fmt.Errorf("--version can only be used with exactly one component")
		}
		upToDate := 0
		for _, name := range names {
			changed, err := in.Update(cmd.Context(), installer.Options{Name: name, Version: updateFlags.version, Vars: vars})
			if err != nil {
				return err
			}
			if !changed {
				upToDate++
				fmt.Printf("%s is up to date (%s)\n", name, in.Manifest.Components[name].Version)
			}
		}
		return nil
	},
}

// printStatus shows every installed component with its version, the latest
// registry version and how many of its files were changed locally.
func printStatus(cmd *cobra.Command, in *installer.Installer) error {
	names := in.Manifest.Names()
	if len(names) == 0 {
		fmt.Println("no components installed")
		return nil
	}
	latest := map[string]string{}
	outdated, err := in.Outdated(cmd.Context())
	if err != nil {
		return err
	}
	for _, o := range outdated {
		latest[o.Name] = o.Latest
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "COMPONENT\tINSTALLED\tLATEST\tFILES\tLOCAL CHANGES")
	for _, name := range names {
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
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", name, c.Version, l, len(c.Files), changes)
	}
	return w.Flush()
}

func init() {
	updateCmd.Flags().StringVar(&updateFlags.version, "version", "", "update to a specific version (single component only)")
	updateCmd.Flags().StringArrayVar(&updateFlags.set, "set", nil, "override a template variable, key=value (repeatable)")
	updateCmd.Flags().BoolVar(&updateFlags.force, "force", false, "overwrite locally modified files (keeps .bak copies)")
	updateCmd.Flags().BoolVar(&updateFlags.skipHooks, "skip-hooks", false, "do not run post_update hooks")
	updateCmd.Flags().BoolVar(&updateFlags.check, "check", false, "show installed and latest versions and local changes, without updating")
	rootCmd.AddCommand(updateCmd)
}
