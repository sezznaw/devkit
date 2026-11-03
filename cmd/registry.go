package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/registry"
	"github.com/sezznaw/devkit/internal/workspace"
)

var registryFlags struct {
	idl string
	key string
}

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Sign or verify the IDL repository's ownership registries (idl/devkit.yaml, idl/redis-keys.txt)",
	Long: `The ownership registries in the IDL repository are grants: which service owns
which Redis key prefix, table model or event topic. They are signed by the one
person who grants ownership (devkit registry sign, private key in
~/.config/devkit/registry.key) and every service's lint verifies the signature
with the public key compiled into devkit (rule registry-signed). A colleague
who edits them without a signature turns every make lint red until the change
is signed, so a self-granted read of another service's keys is caught at push,
not when NOPERM shows up on dev.

  devkit registry keygen   one-time: write the private key, print the public key to compile in
  devkit registry sign     after editing the registries: rewrite idl/registry.sig
  devkit registry verify   what lint does; exit 1 with the reason when the signature does not match

--idl defaults to <project>/idl when run inside the project directory, else to the
current directory.`,
}

func registryIDLDir() (string, error) {
	if registryFlags.idl != "" {
		return registryFlags.idl, nil
	}
	if dir := workspace.Find("."); dir != "" {
		return filepath.Join(dir, "idl"), nil
	}
	if _, err := os.Stat(registry.RulesFile); err == nil {
		return ".", nil
	}
	return "", fmt.Errorf("pass --idl <dir>: not inside the project directory and no %s here", registry.RulesFile)
}

var registryKeygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Create the signing key (refuses to overwrite) and print the public key",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := registryFlags.key
		if path == "" {
			p, err := registry.DefaultKeyPath()
			if err != nil {
				return err
			}
			path = p
		}
		pub, err := registry.Keygen(path)
		if err != nil {
			return err
		}
		fmt.Printf("private key written to %s (mode 0600; back it up, it is the only copy)\n", path)
		fmt.Printf("public key (put it in internal/registry/pubkey.go and release devkit):\n%s\n", pub)
		return nil
	},
}

var registrySignCmd = &cobra.Command{
	Use:   "sign",
	Short: "Sign the current registries into idl/registry.sig",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := registryIDLDir()
		if err != nil {
			return err
		}
		path := registryFlags.key
		if path == "" {
			p, err := registry.DefaultKeyPath()
			if err != nil {
				return err
			}
			path = p
		}
		files, err := registry.Sign(dir, path)
		if err != nil {
			return err
		}
		fmt.Printf("signed %s -> %s\n", strings.Join(files, ", "), filepath.Join(dir, registry.SigFile))
		return nil
	},
}

var registryVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check idl/registry.sig against the registries with the compiled-in public key",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := registryIDLDir()
		if err != nil {
			return err
		}
		if err := registry.Verify(dir, registry.TeamPublicKey()); err != nil {
			fmt.Fprintf(os.Stderr, "registry: %v\n", err)
			os.Exit(1)
		}
		files, _ := registry.Files(dir)
		fmt.Printf("registry: %s signed and unchanged\n", strings.Join(files, ", "))
		return nil
	},
}

func init() {
	registryCmd.PersistentFlags().StringVar(&registryFlags.idl, "idl", "", "IDL directory (default: <project>/idl, or .)")
	registryCmd.PersistentFlags().StringVar(&registryFlags.key, "key", "", "private key file (default: ~/.config/devkit/registry.key)")
	registryCmd.AddCommand(registryKeygenCmd, registrySignCmd, registryVerifyCmd)
	rootCmd.AddCommand(registryCmd)
}
