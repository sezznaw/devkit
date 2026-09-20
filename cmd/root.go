package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/ui"
)

var rootCmd = &cobra.Command{
	Use:   "devkit",
	Short: "devkit creates ready-to-run Kitex services and keeps their scaffolding up to date",
	Long: `devkit sets up a new Kitex (Thrift) service in one command.

  cd <your project directory>
  devkit ngs order

The first run in a new directory creates devkit.yaml for you to fill in
(module prefix, IDL repository, common library). Missing tools (Go, kitex,
thriftgo) are installed automatically.`,
	SilenceUsage:      true,
	SilenceErrors:     true,
	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
}

// Execute runs the root command and prints any error to stderr.
func Execute(ctx context.Context) error {
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, ui.Stderr.Failure("error:"), err)
		return err
	}
	return nil
}
