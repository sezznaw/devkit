package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/buildinfo"
	"github.com/sezznaw/devkit/internal/config"
	"github.com/sezznaw/devkit/internal/github"
	"github.com/sezznaw/devkit/internal/selfupdate"
	"github.com/sezznaw/devkit/internal/ui"
)

// selfUpdateTimeout bounds each request of self-update, the archive download
// included.
const selfUpdateTimeout = 10 * time.Minute

var selfUpdateFlags struct {
	version string
	check   bool
	force   bool
}

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Update devkit itself to the latest GitHub release",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		repo := cfg.DevkitRepo
		if repo == "" {
			repo = buildinfo.Repo
		}
		u := &selfupdate.Updater{
			Client:  github.New(cfg.GitHubHost, cfg.GitHubToken).WithTimeout(selfUpdateTimeout),
			Repo:    repo,
			Current: buildinfo.Version,
			Log:     logf,
		}
		target := selfUpdateFlags.version
		if target == "" {
			if target, err = u.Latest(cmd.Context()); err != nil {
				return err
			}
		}
		current := selfupdate.Normalize(buildinfo.Version)
		if selfupdate.Normalize(target) == current && !selfUpdateFlags.force {
			fmt.Printf("devkit %s is up to date\n", buildinfo.Version)
			return nil
		}
		if selfUpdateFlags.check {
			fmt.Printf("devkit %s installed, %s available (run `devkit self-update`)\n", buildinfo.Version, target)
			return nil
		}
		r := ui.New(1)
		var exe string
		err = r.Step("Updating devkit to "+target, func(s *ui.Step) error {
			u.Log = s.Log
			u.Progress = s.Progress
			var err error
			exe, err = u.Apply(cmd.Context(), target)
			return err
		})
		if err != nil {
			r.Failed(err)
			return err
		}
		r.Done("updated %s: %s -> %s", exe, buildinfo.Version, target)
		return nil
	},
}

func init() {
	selfUpdateCmd.Flags().StringVar(&selfUpdateFlags.version, "version", "", "install a specific release tag instead of the latest")
	selfUpdateCmd.Flags().BoolVar(&selfUpdateFlags.check, "check", false, "only report whether a newer release exists")
	selfUpdateCmd.Flags().BoolVar(&selfUpdateFlags.force, "force", false, "reinstall even if already on the target version")
	rootCmd.AddCommand(selfUpdateCmd)
}
