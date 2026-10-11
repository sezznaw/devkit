package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/sezznaw/devkit/internal/config"
	"github.com/sezznaw/devkit/internal/hosts"
	"github.com/sezznaw/devkit/internal/project"
	"github.com/sezznaw/devkit/internal/takeover"
	"github.com/sezznaw/devkit/internal/workspace"
)

var takeoverFlags struct {
	tag, ssh string
	port     int
	force    bool
}

var takeoverCmd = &cobra.Command{
	Use:   "takeover [service]",
	Short: "Take a service of the development environment over for the requests that carry your tag [takeover]",
	Long: `takeover makes the service you start on this machine (in your IDE, with a
breakpoint) the instance of that service for requests that carry the header
X-Dev-Tag: <your tag>, while everything else keeps going to the environment's
own instances. Stop the local service, or this tool, and traffic is back.

It opens one ssh connection to the environment's node, tunnels every cluster
address the local service needs (Nacos, MySQL, Redis, Kafka, the other
services ...), keeps <service>/.devkit/takeover.json fresh (the service
reads it at start and runs against the environment instead of your local
stack), and registers the local instance in Nacos while its port listens.

  devkit takeover ser-member          in the project or the service directory
  devkit takeover list                who takes what over right now

The ssh target comes from the project's devkit.yaml (takeover_ssh), the tag
from devkit auth's GitLab user name; ~/.devkit/takeover.yaml (ssh:, tag:)
overrides both. Your public key must be in the node's takeover account.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		service, dir, port, err := locateService(args)
		if err != nil {
			return err
		}
		if takeoverFlags.port != 0 {
			port = takeoverFlags.port
		}
		userCfg := loadTakeoverUser()
		tag := first(takeoverFlags.tag, userCfg.Tag, gitlabUsername(), osUser())
		tag = strings.ToLower(strings.TrimSpace(tag))
		target := first(takeoverFlags.ssh, userCfg.SSH, projectTakeoverSSH())
		if target == "" {
			return fmt.Errorf("no ssh target: set takeover_ssh in the IDL repository's devkit.yaml `project:` block (devkit pull copies it), or ssh: in ~/.devkit/takeover.yaml, or pass --ssh")
		}
		fmt.Printf("[takeover] service=%s tag=%s port=%d ssh=%s dir=%s\n", service, tag, port, target, dir)
		return takeover.Run(context.Background(), takeover.Options{Service: service, ServiceDir: dir, LocalPort: port, Tag: tag, SSH: target, Force: takeoverFlags.force, Out: os.Stdout})
	},
}

var takeoverListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the takeovers of the environment: service, tag, owner, host, address, since",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		userCfg := loadTakeoverUser()
		target := first(takeoverFlags.ssh, userCfg.SSH, projectTakeoverSSH())
		if target == "" {
			return fmt.Errorf("no ssh target (takeover_ssh in devkit.yaml, ~/.devkit/takeover.yaml, or --ssh)")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		env, err := takeover.FetchEnv(ctx, target, "")
		if err != nil {
			return err
		}
		layout, err := takeover.Plan(env, takeover.PortTaken)
		if err != nil {
			return err
		}
		if layout.NacosAddr == "" {
			return fmt.Errorf("the environment has no Nacos")
		}
		var nacosFwd []takeover.Forward
		for _, f := range layout.Forwards {
			if strings.HasPrefix(f.What, "nacos ") {
				nacosFwd = append(nacosFwd, f)
			}
		}
		t, taken, err := takeover.Open(ctx, target, "list-"+osUser(), nacosFwd, takeover.Reverse{NodeIP: env.NodeIP, RemotePort: takeover.RemotePort("list", osUser(), 0), LocalPort: 1})
		if err != nil || taken {
			return fmt.Errorf("tunnel: %v", err)
		}
		defer t.Close()
		n, err := takeover.NewNacos(layout.NacosAddr, env.EnvVars["NACOS_NAMESPACE"], env.EnvVars["NACOS_USERNAME"], env.EnvVars["NACOS_PASSWORD"])
		if err != nil {
			return err
		}
		defer n.Close()
		var names []string
		for _, s := range env.Services {
			names = append(names, s.Name)
		}
		list, err := n.Takeovers(names)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no takeover right now")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "SERVICE\tTAG\tOWNER\tHOST\tADDRESS\tSINCE")
		for _, x := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", x.Service, x.Tag, x.Owner, x.Host, x.Addr, x.Since)
		}
		return w.Flush()
	},
}

var takeoverEnvFlags struct {
	kubeconfig, namespace string
	infra, secrets        []string
}

var takeoverEnvCmd = &cobra.Command{
	Use:    "env",
	Short:  "Node side: describe the environment for the tool (run by sshd's ForceCommand, not by hand)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// The tool runs `ssh takeover@node <service>`; sshd hands that word
		// over in SSH_ORIGINAL_COMMAND.
		service := strings.TrimSpace(os.Getenv("SSH_ORIGINAL_COMMAND"))
		if strings.ContainsAny(service, " ;|&$`") {
			service = ""
		}
		env, err := takeover.Collect(takeover.CollectOptions{Kubeconfig: takeoverEnvFlags.kubeconfig, Namespace: takeoverEnvFlags.namespace, Infra: takeoverEnvFlags.infra, Secrets: takeoverEnvFlags.secrets, Service: service})
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(env)
	},
}

// takeoverUser is ~/.devkit/takeover.yaml.
type takeoverUser struct {
	SSH string `yaml:"ssh"`
	Tag string `yaml:"tag"`
}

func loadTakeoverUser() takeoverUser {
	var u takeoverUser
	dir, err := config.Dir()
	if err != nil {
		return u
	}
	b, err := os.ReadFile(filepath.Join(dir, "takeover.yaml"))
	if err == nil {
		_ = yaml.Unmarshal(b, &u)
	}
	return u
}

func projectTakeoverSSH() string {
	dir := workspace.Find(".")
	if dir == "" {
		return ""
	}
	cfg, err := workspace.LoadConfig(dir)
	if err != nil {
		return ""
	}
	return cfg.TakeoverSSH
}

func gitlabUsername() string {
	store, err := hosts.Load()
	if err != nil {
		return ""
	}
	for _, h := range store.Hosts {
		if h.Username != "" {
			return h.Username
		}
	}
	return ""
}

func osUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}

func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// locateService finds the service: the argument, else the service directory
// we are in. Its name and port come from conf/local.yaml.
func locateService(args []string) (name, dir string, port int, err error) {
	if len(args) == 1 {
		name = args[0]
		if ws := workspace.Find("."); ws != "" {
			dir = filepath.Join(ws, name)
		} else {
			dir = name
		}
	} else {
		root, err := project.FindRoot(".")
		if err != nil {
			return "", "", 0, fmt.Errorf("say which service: devkit takeover <service> (or run it inside a service directory)")
		}
		dir, name = root, filepath.Base(root)
	}
	if _, err := os.Stat(filepath.Join(dir, "conf")); err != nil {
		return "", "", 0, fmt.Errorf("%s has no conf/ directory; is %q a service of this project?", dir, name)
	}
	var local struct {
		Service struct {
			Name string `yaml:"name"`
			Addr string `yaml:"addr"`
		} `yaml:"service"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "conf", "local.yaml")); err == nil {
		_ = yaml.Unmarshal(b, &local)
	}
	if local.Service.Name != "" {
		name = local.Service.Name
	}
	port, err = takeover.PortOf(local.Service.Addr)
	if err != nil {
		return "", "", 0, fmt.Errorf("%v (conf/local.yaml of %s); pass --port", err, name)
	}
	return name, dir, port, nil
}

func init() {
	takeoverCmd.PersistentFlags().StringVar(&takeoverFlags.tag, "tag", "", "your tag (default: devkit auth's GitLab user name)")
	takeoverCmd.PersistentFlags().StringVar(&takeoverFlags.ssh, "ssh", "", "ssh target of the node, takeover@host or an alias of ~/.ssh/config (default: devkit.yaml takeover_ssh)")
	takeoverCmd.Flags().IntVar(&takeoverFlags.port, "port", 0, "the local service's port (default: conf/local.yaml service.addr)")
	takeoverCmd.Flags().BoolVar(&takeoverFlags.force, "force", false, "take over although someone else holds the same tag")
	takeoverEnvCmd.Flags().StringVar(&takeoverEnvFlags.kubeconfig, "kubeconfig", "", "read-only kubeconfig")
	takeoverEnvCmd.Flags().StringVar(&takeoverEnvFlags.namespace, "namespace", "", "the services' namespace")
	takeoverEnvCmd.Flags().StringSliceVar(&takeoverEnvFlags.infra, "infra", nil, "infrastructure namespaces")
	takeoverEnvCmd.Flags().StringSliceVar(&takeoverEnvFlags.secrets, "secret", nil, "namespace/name of the secrets whose keys become environment variables")
	takeoverCmd.AddCommand(takeoverListCmd, takeoverEnvCmd)
	rootCmd.AddCommand(takeoverCmd)
}
