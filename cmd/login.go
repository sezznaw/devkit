package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sezznaw/devkit/internal/gitlab"
	"github.com/sezznaw/devkit/internal/hosts"
	"github.com/sezznaw/devkit/internal/ui"
)

var loginFlags struct{ tokenStdin bool }

var loginCmd = &cobra.Command{
	Use:   "login <gitlab-url>",
	Short: "Save a GitLab personal access token for devkit pull, git and go",
	Long: `login stores a personal access token for one GitLab server. It is asked for
interactively (never on the command line) and kept in ~/.devkit/hosts.yaml
(mode 600); the same token is written to ~/.netrc, which git (clone, push over
HTTPS) and go (private modules, GOPRIVATE) read, so one login serves all
three. The token needs the scopes api, read_repository, write_repository.

  devkit login https://gitlab.example.com
  devkit login gitlab.example.com --token-stdin < token.txt   (scripts)

Run it again to replace the token.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host, err := hosts.Normalize(args[0])
		if err != nil {
			return err
		}
		var token string
		if loginFlags.tokenStdin {
			b, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			token = strings.TrimSpace(b)
		} else {
			fmt.Printf("Personal access token for %s (scopes api, read_repository, write_repository): ", host)
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("read token: %w", err)
			}
			token = strings.TrimSpace(string(b))
		}
		if token == "" {
			return fmt.Errorf("no token given")
		}
		user, err := gitlab.New(host, token).User(cmd.Context())
		if err != nil {
			return err
		}
		store, err := hosts.Load()
		if err != nil {
			return err
		}
		store.Hosts[host] = hosts.Host{Token: token, Username: user.Username}
		if err := store.Save(); err != nil {
			return err
		}
		netrc, err := hosts.WriteNetrc(hosts.Hostname(host), token)
		if err != nil {
			return err
		}
		goPrivate := addGoPrivate(hosts.Hostname(host))
		fmt.Printf("%s logged in to %s as %s\n", ui.Stdout.Success("✓"), host, user.Username)
		fmt.Printf("  token saved for devkit; %s updated for git and go; GOPRIVATE=%s\n", netrc, goPrivate)
		return nil
	},
}

// addGoPrivate adds host to the persistent GOPRIVATE (go env -w) so go
// fetches this server's modules with the netrc instead of the public proxy.
func addGoPrivate(hostname string) string {
	cur, _ := exec.Command("go", "env", "GOPRIVATE").Output()
	have := strings.TrimSpace(string(cur))
	for _, p := range strings.Split(have, ",") {
		if strings.TrimSpace(p) == hostname {
			return have
		}
	}
	next := hostname
	if have != "" {
		next = have + "," + hostname
	}
	_ = exec.Command("go", "env", "-w", "GOPRIVATE="+next).Run()
	return next
}

func init() {
	loginCmd.Flags().BoolVar(&loginFlags.tokenStdin, "token-stdin", false, "read the token from stdin instead of asking")
	rootCmd.AddCommand(loginCmd)
}
