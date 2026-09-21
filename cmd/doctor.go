package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/config"
	"github.com/sezznaw/devkit/internal/deps"
	"github.com/sezznaw/devkit/internal/installer"
	"github.com/sezznaw/devkit/internal/registry"
	"github.com/sezznaw/devkit/internal/ui"
)

var doctorFlags struct {
	fix bool
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check (and with --fix install) the tools needed to build services: git, Go, kitex, thriftgo, hz",
	Long: `doctor reports the state of git, Go and the code generators: kitex and
thriftgo for RPC services, hz for API services. Their versions are the team's,
taken from the service templates, and a tool of another version counts as a
problem. With --fix it installs or replaces
what is needed: Go from go.dev into ~/.devkit/go (only when no Go is present),
the generators with go install. Installed tools are put on PATH through
~/.devkit/env; source it from your shell profile once. 'devkit ngs' runs the
same check automatically, and so does 'devkit nas'.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		want, source := teamVersions(cmd)
		if !doctorFlags.fix {
			problems := 0
			for _, s := range deps.Check(cmd.Context(), want) {
				state := ui.Stdout.Green("ok     ") + " " + s.Version
				switch {
				case s.Missing:
					problems++
					state = ui.Stdout.Failure("MISSING")
					if s.Hint != "" {
						state += "  " + s.Hint
					}
				case s.Wanted != "":
					problems++
					state = ui.Stdout.Attention("WRONG  ") + " " + s.Version + ui.Stdout.Yellow("  the team uses "+s.Wanted)
				}
				fmt.Printf("%-10s %s\n", s.Name, state)
			}
			versions := "kitex " + want.KitexVersion + ", thriftgo " + want.ThriftgoVersion
			if want.HzVersion != "" {
				versions += ", hz " + want.HzVersion
			}
			fmt.Println(ui.Stdout.Dim("team versions from " + source + ": " + versions))
			if problems > 0 {
				fmt.Printf("\n%s; run `devkit doctor --fix`\n", ui.Stdout.Attention(fmt.Sprintf("%d problem(s)", problems)))
			}
			return nil
		}
		r := ui.New(1)
		var statuses []deps.Status
		err := r.Step("Checking and installing tools", func(s *ui.Step) error {
			var err error
			statuses, err = deps.Ensure(cmd.Context(), want, s)
			return err
		})
		if err != nil {
			r.Failed(err)
			return err
		}
		r.Done("all tools ready")
		if hint := deps.PathHint(statuses); hint != "" {
			fmt.Println(hint)
		}
		return nil
	},
}

// teamVersions reads the tool versions every service uses from the newest
// service template. Offline, the versions known when this devkit was built
// are used and named as such.
func teamVersions(cmd *cobra.Command) (deps.Want, string) {
	fallback := deps.Want{KitexVersion: deps.DefaultKitexVersion, ThriftgoVersion: deps.DefaultThriftgoVersion}
	cfg, err := config.Load()
	if err != nil {
		return fallback, "this devkit build (config unreadable)"
	}
	src, err := registry.Open(cfg)
	if err != nil {
		return fallback, "this devkit build (no registry configured)"
	}
	in := &installer.Installer{Source: src, Log: func(string, ...any) {}}
	comp, err := in.Latest(cmd.Context(), serviceComponent)
	if err != nil {
		return fallback, "this devkit build (registry unreachable)"
	}
	want := fallback
	if v := comp.VarByName("KitexVersion"); v != nil && v.Default != "" {
		want.KitexVersion = v.Default
	}
	if v := comp.VarByName("ThriftgoVersion"); v != nil && v.Default != "" {
		want.ThriftgoVersion = v.Default
	}
	source := "template " + comp.Name + "@" + comp.Version
	// hz comes from the API template. A registry without it (an older one, or
	// a team that has no API services) simply has no hz to check.
	if api, err := in.Latest(cmd.Context(), apiComponent); err == nil {
		if v := api.VarByName("HzVersion"); v != nil && v.Default != "" {
			want.HzVersion = v.Default
			source = "templates " + comp.Name + "@" + comp.Version + " and " + api.Name + "@" + api.Version
		}
	}
	return want, source
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFlags.fix, "fix", false, "install missing tools and replace ones of another version")
	rootCmd.AddCommand(doctorCmd)
}
