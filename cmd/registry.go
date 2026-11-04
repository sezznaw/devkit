package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/sezznaw/devkit/internal/registry"
	"github.com/sezznaw/devkit/internal/workspace"
)

// isRegistryRules tells the IDL repository's devkit.yaml (ownership
// registries under lint:) from a project directory's devkit.yaml (module
// prefix, IDL repository, template variables: never a lint: section).
//
// isRegistryRules 区分 IDL 仓库的 devkit.yaml（登记表在 lint: 下）和项目目录的 devkit.yaml（模块前缀、
// IDL 仓库、模板变量，从不含 lint: 段）。
func isRegistryRules(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var top map[string]any
	if err := yaml.Unmarshal(data, &top); err != nil {
		return false
	}
	_, ok := top["lint"]
	return ok
}

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

// registryIDLDir is the directory holding the registries: --idl when given;
// <project>/idl inside the project directory; the current directory inside the
// IDL repository itself. The IDL repository's rules file is also named
// devkit.yaml, so workspace.Find stops there when run inside it: a directory
// with an idl/ subdirectory is the project, one whose devkit.yaml has the
// lint: section is the IDL repository, anything else is a project whose idl/
// is not checked out yet (sign then reports the missing rules file instead of
// signing the project's own devkit.yaml).
//
// registryIDLDir 是登记表所在目录：给了 --idl 用它；在项目目录里是 <项目>/idl；在 IDL 仓库里就是当前目录。
// IDL 仓库的登记表也叫 devkit.yaml，所以在 IDL 仓库里跑时 workspace.Find 会停在 IDL 仓库本身：
// 有 idl/ 子目录的是项目目录；devkit.yaml 里有 lint: 段的是 IDL 仓库；其余是还没 clone idl/ 的项目目录
// （这时 sign 报登记表不存在，而不是把项目自己的 devkit.yaml 签了）。
func registryIDLDir() (string, error) {
	if registryFlags.idl != "" {
		return registryFlags.idl, nil
	}
	if dir := workspace.Find("."); dir != "" {
		idl := filepath.Join(dir, "idl")
		if st, err := os.Stat(idl); err == nil && st.IsDir() {
			return idl, nil
		}
		if isRegistryRules(filepath.Join(dir, registry.RulesFile)) {
			return dir, nil
		}
		return idl, nil
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
		// Verify passes a directory without a rules file (lint: nothing is
		// registered, nothing to protect); on the command line that is a
		// wrong directory, not a verified registry.
		files, err := registry.Files(dir)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf("%s has no %s: nothing to verify (pass --idl <dir>)", dir, registry.RulesFile)
		}
		if err := registry.Verify(dir, registry.TeamPublicKey()); err != nil {
			fmt.Fprintf(os.Stderr, "registry: %v\n", err)
			os.Exit(1)
		}
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
