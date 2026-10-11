package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/report"
	"github.com/sezznaw/devkit/internal/workspace"
)

var reportFlags struct {
	since      string
	allAuthors bool
	noRun      bool
	noFetch    bool
	jsonOut    bool
}

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Gather the facts of a piece of work across the project's repositories, as a hand-over",
	Long: `report looks at every git repository in the project directory and writes
down what actually changed: repositories, commits and whether they are
pushed, files (flagging framework files a service should not touch), the
methods, structs and error codes the IDL gained, lint and test results,
and whether app/verify.go (the post-deploy check) was touched. The output
is Markdown with blanks marked （填写：…） for what only the author knows:
what the feature is for, how it was tried out, what remains for others.

The facts come from git and the build, not from memory. Run it before the
final commit; the Markdown is the hand-over, its gist goes into the
commit message (idl/AGENTS.md 第 6 节 and the sportsbook-finish skill).

  devkit report                      my commits of the last 24h + uncommitted changes
  devkit report --since 3d           a longer task
  devkit report --since <ref>        everything after a commit or tag
  devkit report --no-run             skip make lint / make test
  devkit report --json               for tools`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := workspace.Find(".")
		if dir == "" {
			return fmt.Errorf("not inside a devkit project (no devkit.yaml above %s)", mustGetwd())
		}
		r, err := report.Collect(dir, report.Options{
			Since:      reportFlags.since,
			AllAuthors: reportFlags.allAuthors,
			Run:        !reportFlags.noRun,
			Fetch:      !reportFlags.noFetch,
			Log: func(format string, a ...any) {
				fmt.Fprintf(os.Stderr, "… "+format+"\n", a...)
			},
		})
		if err != nil {
			return err
		}
		if reportFlags.jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(r)
		}
		fmt.Print(r.Markdown())
		return nil
	},
}

func mustGetwd() string {
	d, _ := os.Getwd()
	return d
}

func init() {
	reportCmd.Flags().StringVar(&reportFlags.since, "since", "24h", "range: a duration (24h, 3d, 1w), a date (2026-10-11) or a git ref")
	reportCmd.Flags().BoolVar(&reportFlags.allAuthors, "all-authors", false, "in a time window, include other people's commits too")
	reportCmd.Flags().BoolVar(&reportFlags.noRun, "no-run", false, "do not run make lint / make test")
	reportCmd.Flags().BoolVar(&reportFlags.noFetch, "no-fetch", false, "do not git fetch first (offline)")
	reportCmd.Flags().BoolVar(&reportFlags.jsonOut, "json", false, "print JSON instead of Markdown")
	rootCmd.AddCommand(reportCmd)
}
