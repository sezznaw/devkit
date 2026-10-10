package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/hooks"
	"github.com/sezznaw/devkit/internal/ui"
)

var hooksFlags struct {
	remove bool
	force  bool
}

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Install the git pre-push hook that runs make lint && make test before a push",
	Long: `hooks installs a pre-push hook in each service's git repository: before a
push runs, make lint && make test run, the same as the CI pipeline's test
stage; a push that would fail there is refused here. Run it inside one
service or in the project directory for every service. devkit ngs and
devkit update install it too; the hook refreshes itself on the next update.

  devkit hooks            install (or refresh)
  devkit hooks --remove   remove devkit's hook
  git push --no-verify    skip it once (CI still checks)

A pre-push hook written by hand is left alone unless --force is given.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		roots, single, err := updateScope()
		if err != nil {
			return err
		}
		failed := 0
		for _, root := range roots {
			name := filepath.Base(root)
			if hooksFlags.remove {
				ok, err := hooks.Remove(root)
				switch {
				case err != nil:
					fmt.Printf("%s %s: %v\n", ui.Stdout.Failure("✗"), name, err)
					failed++
				case ok:
					fmt.Printf("%s %s: pre-push hook removed\n", ui.Stdout.Success("✓"), name)
				default:
					fmt.Printf("  %s: no devkit hook\n", name)
				}
				continue
			}
			state, err := hooks.Install(root, hooksFlags.force)
			switch {
			case errors.Is(err, hooks.ErrNoGit):
				fmt.Printf("  %s: not a git repository, skipped\n", name)
			case err != nil:
				fmt.Printf("%s %s: %v\n", ui.Stdout.Failure("✗"), name, err)
				failed++
			default:
				fmt.Printf("%s %s: pre-push hook %s (make lint && make test before every push)\n", ui.Stdout.Success("✓"), name, state)
			}
		}
		_ = single
		if failed > 0 {
			return fmt.Errorf("%d repository(ies) not done", failed)
		}
		return nil
	},
}

func init() {
	hooksCmd.Flags().BoolVar(&hooksFlags.remove, "remove", false, "remove the hook devkit installed")
	hooksCmd.Flags().BoolVar(&hooksFlags.force, "force", false, "replace a pre-push hook written by hand")
	rootCmd.AddCommand(hooksCmd)
}

// installHooksQuietly is what ngs and update do after their own work: the
// hook arrives with the service and follows devkit's version.
func installHooksQuietly(root string, logf func(string, ...any)) {
	state, err := hooks.Install(root, false)
	switch {
	case errors.Is(err, hooks.ErrNoGit):
	case err != nil:
		logf("pre-push hook not installed: %v", err)
	case state != "current":
		logf("pre-push hook %s: make lint && make test run before every push (devkit hooks --remove to drop it)", state)
	}
}
