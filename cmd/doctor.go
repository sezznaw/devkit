package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/deps"
	"github.com/sezznaw/devkit/internal/ui"
)

var doctorFlags struct {
	fix             bool
	kitexVersion    string
	thriftgoVersion string
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check (and with --fix install) the tools needed to build services: git, Go, kitex, thriftgo",
	Long: `doctor reports the state of git, Go, kitex and thriftgo. With --fix it installs
what is missing: Go from go.dev into ~/.devkit/go (only when no Go is present),
kitex and thriftgo with go install. Installed tools are put on PATH through
~/.devkit/env; source it from your shell profile once. 'devkit ngs' runs the
same check automatically.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		want := deps.Want{KitexVersion: doctorFlags.kitexVersion, ThriftgoVersion: doctorFlags.thriftgoVersion}
		if !doctorFlags.fix {
			missing := 0
			for _, s := range deps.Check(cmd.Context(), want) {
				state := "ok      " + s.Version
				if s.Missing {
					missing++
					state = "MISSING"
					if s.Hint != "" {
						state += "  " + s.Hint
					}
				}
				fmt.Printf("%-10s %s\n", s.Name, state)
			}
			if missing > 0 {
				fmt.Printf("\n%d tool(s) missing; run `devkit doctor --fix` to install them\n", missing)
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

func init() {
	doctorCmd.Flags().BoolVar(&doctorFlags.fix, "fix", false, "install missing tools")
	doctorCmd.Flags().StringVar(&doctorFlags.kitexVersion, "kitex-version", deps.DefaultKitexVersion, "kitex version to install")
	doctorCmd.Flags().StringVar(&doctorFlags.thriftgoVersion, "thriftgo-version", deps.DefaultThriftgoVersion, "thriftgo version to install")
	rootCmd.AddCommand(doctorCmd)
}
