package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View or edit devkit configuration (~/.devkit/config.yaml)",
	// Maintainer command: install.sh writes a working config, so it is kept
	// out of the help to keep the command list short.
	Hidden: true,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the effective configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		p, _ := config.Path()
		fmt.Printf("%-14s %s\n", "file:", p)
		for _, k := range config.Keys {
			v := cfg.Get(k)
			if k == "github_token" {
				v = mask(v)
			}
			fmt.Printf("%-14s %s\n", k+":", v)
		}
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration key (" + strings.Join(config.Keys, ", ") + ")",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		key, val := args[0], strings.TrimSpace(args[1])
		if strings.HasSuffix(key, "_repo") || key == "module_prefix" {
			val = strings.Trim(val, "/")
		}
		if err := cfg.Set(key, val); err != nil {
			return err
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("%s updated\n", key)
		return nil
	},
}

func mask(s string) string {
	if len(s) <= 6 {
		if s == "" {
			return "(unset)"
		}
		return "***"
	}
	return s[:3] + "***" + s[len(s)-3:]
}

func init() {
	configCmd.AddCommand(configShowCmd, configSetCmd)
	rootCmd.AddCommand(configCmd)
}
