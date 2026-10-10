package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/lock"
	"github.com/sezznaw/devkit/internal/ui"
	"github.com/sezznaw/devkit/internal/workspace"
)

var lockFlags struct {
	unlock bool
	quiet  bool
	list   bool
}

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Make the framework's files and generated code read-only, so the IDE says so",
	Long: `lock removes write permission from the files a service's own code never
touches: what devkit wrote and replaces on update (.devkit/manifest.json),
generated code (kitex_gen/, hertz_gen/, files with a "Code generated ...
DO NOT EDIT" header), the OpenAPI document and idl.lock. Opening one in an
editor then shows "read-only", exactly like a library in the Go module
cache. app/, handler/, repo/, conf/*.yaml, deps/, go.mod stay writable.

It is a reminder, not a wall: devkit lint, the pre-push hook and CI still
decide. make gen unlocks before the generators run and locks afterwards;
devkit ngs and devkit update lock when they finish. Run it inside one
service or in the project directory for every service.

  devkit lock            lock (idempotent)
  devkit lock --unlock   make them writable again
  devkit lock --list     print what would be locked`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		roots, _, err := updateScope()
		if err != nil {
			return err
		}
		failed := 0
		for _, root := range roots {
			name := filepath.Base(root)
			if lockFlags.list {
				files, err := lock.Files(root)
				if err != nil {
					return err
				}
				for _, f := range files {
					fmt.Println(filepath.Join(name, f))
				}
				continue
			}
			var n int
			if lockFlags.unlock {
				n, err = lock.Unlock(root)
			} else {
				n, err = lock.Lock(root)
			}
			if err != nil {
				fmt.Printf("%s %s: %v\n", ui.Stdout.Failure("✗"), name, err)
				failed++
				continue
			}
			if !lockFlags.quiet {
				if lockFlags.unlock {
					fmt.Printf("%s %s: %d file(s) writable again\n", ui.Stdout.Success("✓"), name, n)
				} else {
					fmt.Printf("%s %s: %d file(s) made read-only (framework files and generated code; devkit lock --unlock to undo)\n", ui.Stdout.Success("✓"), name, n)
				}
			}
		}
		// The project's kit-common/ checkout: go.work makes every local build
		// use it, so an edit there is a framework change nobody else has.
		if ws := workspace.Find("."); ws != "" && !lockFlags.list {
			var n int
			if lockFlags.unlock {
				n, err = lock.UnlockCommon(ws)
			} else {
				n, err = lock.LockCommon(ws)
			}
			if err != nil {
				fmt.Printf("%s kit-common/: %v\n", ui.Stdout.Failure("✗"), err)
				failed++
			} else if n > 0 && !lockFlags.quiet {
				fmt.Printf("%s kit-common/: %d file(s) %s\n", ui.Stdout.Success("✓"), n, map[bool]string{true: "writable again", false: "made read-only (the shared library's checkout; change it in its own repository)"}[lockFlags.unlock])
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d service(s) not done", failed)
		}
		return nil
	},
}

func init() {
	lockCmd.Flags().BoolVar(&lockFlags.unlock, "unlock", false, "give write permission back")
	lockCmd.Flags().BoolVarP(&lockFlags.quiet, "quiet", "q", false, "print nothing unless something fails")
	lockCmd.Flags().BoolVar(&lockFlags.list, "list", false, "list the files instead of changing them")
	rootCmd.AddCommand(lockCmd)
}

// lockCommonQuietly locks the project's kit-common/ checkout after it was synced.
func lockCommonQuietly(projectDir string, logf func(string, ...any)) {
	if projectDir == "" {
		return
	}
	n, err := lock.LockCommon(projectDir)
	switch {
	case err != nil:
		logf("kit-common/ not made read-only: %v", err)
	case n > 0:
		logf("kit-common/ (%d files) made read-only: the shared library is changed in its own repository, not here", n)
	}
}

// lockQuietly is what ngs and update do when they finish: the files they
// own and the generated code become read-only in the editor.
func lockQuietly(root string, logf func(string, ...any)) {
	n, err := lock.Lock(root)
	switch {
	case err != nil:
		logf("read-only lock not applied: %v", err)
	case n > 0:
		logf("%d framework and generated file(s) made read-only in the editor (devkit lock --unlock to undo)", n)
	}
}
