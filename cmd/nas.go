package cmd

import (
	"github.com/spf13/cobra"
)

// apiComponent is the template behind `devkit nas`.
const apiComponent = "hertz-service"

var nasCmd = &cobra.Command{
	Use:     "nas <service-name>",
	Aliases: []string{"new-api-service"},
	Short:   "Create a new API (HTTP) service on Hertz in the current project directory (nas = new API service)",
	Long: `nas is ngs for an API service: the HTTP front of the RPC services of a project.
It sets up everything the new service needs:

  1. clones (or updates) the project's IDL repository into ./idl
  2. clones the common library into ./common (for reading and local changes)
  3. generates ./<service> from the hertz-service component
  4. writes the initial Thrift IDL, with the route annotations of Hertz, into
     ./idl/<service>/
  5. runs the code generators (hz for the routes, kitex for the RPC services
     the API calls) and go mod tidy
  6. makes the first git commit

Missing tools (Go, hz, kitex, thriftgo) are installed automatically first; see
'devkit doctor'.

Run it in your project directory, the one your RPC services are in. No
configuration is required; devkit.yaml there is shared with ngs.`,
	Example: `  devkit nas gateway
  devkit nas admin --set Port=8081`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNew(cmd.Context(), apiService, args[0])
	},
}

func init() {
	addNewServiceFlags(nasCmd, apiService)
	rootCmd.AddCommand(nasCmd)
}
