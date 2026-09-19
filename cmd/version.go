package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/buildinfo"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print devkit version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("devkit %s\n", buildinfo.Version)
		fmt.Printf("  commit: %s\n", buildinfo.Commit)
		fmt.Printf("  built:  %s\n", buildinfo.Date)
		fmt.Printf("  go:     %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
