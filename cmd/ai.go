package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/workspace"
)

var aiFlags struct {
	all, clean bool
}

var aiCmd = &cobra.Command{
	Use:   "ai [tool...]",
	Short: "Write the file your AI coding tool reads on start, pointing it at idl/AGENTS.md",
	Long: `The project's development guide for AI tools is one file, idl/AGENTS.md.
Each tool reads a differently named file on start; devkit ai writes that
pointer for the tool(s) you use, in the project directory, so the directory
holds only what is in use.

  devkit ai                 list the tools and which pointers exist here
  devkit ai claude          Claude Code (CLAUDE.md, importing AGENTS.md)
  devkit ai cursor copilot  several at once
  devkit ai --all           every tool
  devkit ai --clean         remove the pointers devkit wrote (edited files are kept)

Skills (the steps of one kind of task, loaded by the tool when that task
comes up) are copied to .agents/skills/ and .claude/skills/ at the same
time: devkit's built-in ones (devkit-rpc-method, devkit-gateway-api,
devkit-gateway-domain, devkit-finish: the framework's way, same in every
project) and the project's own under idl/skills/<name>/SKILL.md (a project
skill with a built-in's name replaces it). devkit pull and devkit update
refresh the copies; the built-in ones come from the devkit binary, so after
a devkit release run devkit self-update first.
Edit the sources, not the copies.

A tool not listed reads the guide when told to: "先读 idl/AGENTS.md".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := workspace.Find(".")
		if dir == "" {
			var err error
			if dir, err = os.Getwd(); err != nil {
				return err
			}
		}
		if aiFlags.clean {
			removed, err := workspace.CleanAIPointers(dir)
			if err != nil {
				return err
			}
			skills, err := workspace.CleanAISkills(dir)
			if err != nil {
				return err
			}
			removed = append(removed, skills...)
			for _, r := range removed {
				fmt.Println("removed", r)
			}
			if len(removed) == 0 {
				fmt.Println("nothing to remove")
			}
			return nil
		}
		if aiFlags.all {
			args = []string{"all"}
		}
		if len(args) == 0 {
			present := workspace.PresentAIPointers(dir)
			fmt.Printf("%-10s %-36s %s\n", "TOOL", "FILE", "FOR")
			for _, f := range workspace.AIPointerFiles {
				mark := ""
				if st, ok := present[f.Key]; ok {
					mark = " [" + st + "]"
				}
				fmt.Printf("%-10s %-36s %s%s\n", f.Key, f.Path, f.Tools, mark)
			}
			fmt.Printf("\n%s is the guide; `devkit ai <tool>` writes the pointer for your tool.\n", workspace.AIRulesSource)
			fmt.Printf("\nskills (copied to %s by `devkit ai <tool>`):\n", strings.Join(workspace.AISkillTargets, " and "))
			for _, sk := range workspace.ListAISkills(dir) {
				fmt.Printf("  %-26s %s\n", sk.Name, sk.Source)
			}
			return nil
		}
		sel, err := workspace.SelectAIPointers(args)
		if err != nil {
			return err
		}
		written, err := workspace.WriteAIPointers(dir, sel)
		if err != nil {
			return err
		}
		skills, removed, err := workspace.SyncAISkills(dir)
		if err != nil {
			return err
		}
		written = append(written, skills...)
		sort.Strings(written)
		for _, w := range written {
			fmt.Println("wrote", w)
		}
		for _, r := range removed {
			fmt.Println("removed", r)
		}
		if len(written)+len(removed) == 0 {
			fmt.Println("already in place")
		}
		return nil
	},
}

func init() {
	aiCmd.Flags().BoolVar(&aiFlags.all, "all", false, "write the pointer files of every tool")
	aiCmd.Flags().BoolVar(&aiFlags.clean, "clean", false, "remove the pointer files devkit wrote")
	rootCmd.AddCommand(aiCmd)
}
