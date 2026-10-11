package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/takeover"
)

var tunnelAll bool

var tunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Reach the development environment's middleware from this machine: MySQL, Redis, Nacos, Kafka ... on local ports [takeover]",
	Long: `tunnel opens the same ssh connection devkit takeover uses, with the local
forwards only: every Service of the environment's infrastructure and every
business service becomes a local port (infrastructure port + 10000, service
port + 1000), and the credentials the deployments use are printed once, to
the terminal. Connect your database client to 127.0.0.1:<port>. Nothing is
registered and no file is written; Ctrl+C closes everything.

Run it in the project directory: the node comes from that project's
devkit.yaml (takeover_ssh), so each project reaches its own environment.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		userCfg := loadTakeoverUser()
		target := first(takeoverFlags.ssh, userCfg.SSH, projectTakeoverSSH())
		if target == "" {
			return fmt.Errorf("no ssh target: run this in a project directory whose devkit.yaml has takeover_ssh (devkit pull writes it from the IDL repository), or set it in ~/.devkit/takeover.yaml, or pass --ssh")
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		env, err := takeover.FetchEnv(ctx, target, "")
		if err != nil {
			return err
		}
		takeover.EndStale(target, "tunnel-"+osUser())
		layout, err := takeover.Plan(env, takeover.PortTaken)
		if err != nil {
			return err
		}
		t, _, err := takeover.Open(ctx, target, "tunnel-"+osUser(), layout.Forwards, takeover.Reverse{})
		if err != nil {
			return err
		}
		defer t.Close()
		printTunnel(layout, env, tunnelAll)
		fmt.Println("\ntunnel is up; Ctrl+C closes it")
		select {
		case <-ctx.Done():
		case err := <-t.Done():
			return fmt.Errorf("tunnel: the ssh connection ended: %v", err)
		}
		fmt.Println("closed")
		return nil
	},
}

// printTunnel lists the forwards a developer connects a client to (the ones
// with a ready-made command) and the business services; the rest of the
// cluster's ports (metrics, internal RPC, probes) are tunnelled too but only
// counted, --all shows them. Then the credentials.
func printTunnel(l *takeover.Layout, env *takeover.Env, all bool) {
	services := map[string]bool{}
	for _, s := range env.Services {
		services[s.Name] = true
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "WHAT\tLOCAL\tCLUSTER\tHOW")
	// A headless twin of a Service ("starrocks-fe-search" next to
	// "starrocks-fe-service", same port) is the same thing twice.
	clusterIP := map[string]bool{}
	for _, f := range l.Forwards {
		if strings.HasPrefix(f.Target, "10.43.") || !strings.HasPrefix(f.Target, "10.42.") {
			clusterIP[stem(f.What)] = true
		}
	}
	hidden := 0
	for _, f := range l.Forwards {
		if f.Target == "" {
			continue
		}
		how := howTo(f)
		twin := strings.HasPrefix(f.Target, "10.42.") && clusterIP[stem(f.What)] && !strings.Contains(f.What, "mysql") && !strings.Contains(f.What, "valkey") && !strings.Contains(f.What, "redpanda")
		if !all && (how == "" || twin) && !services[f.What] {
			hidden++
			continue
		}
		fmt.Fprintf(w, "%s\t127.0.0.1:%d\t%s\t%s\n", f.What, f.LocalPort, f.Target, how)
	}
	w.Flush()
	if hidden > 0 {
		fmt.Printf("(+%d more cluster ports forwarded but not listed: internal RPC, metrics, probes; --all shows them)\n", hidden)
	}
	keys := make([]string, 0, len(l.EnvVars))
	for k := range l.EnvVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("\ncredentials and settings the deployments use (shown once, not written anywhere):")
	for _, k := range keys {
		fmt.Printf("  %s=%s\n", k, l.EnvVars[k])
	}
}

// howTo guesses the client command from the cluster port (and, for Kafka,
// the name: Alertmanager also listens on 9093).
func howTo(f takeover.Forward) string {
	port := ""
	if i := strings.LastIndex(f.Target, ":"); i >= 0 {
		port = f.Target[i+1:]
	}
	p := strconv.Itoa(f.LocalPort)
	name := strings.ToLower(f.What)
	if (port == "9093" || port == "9092") && !strings.Contains(name, "redpanda") && !strings.Contains(name, "kafka") {
		return ""
	}
	switch port {
	case "3306", "9030":
		return "mysql -h127.0.0.1 -P" + p + " -u<user> -p"
	case "6379":
		return "redis-cli -p " + p + " -a <password>"
	case "8848":
		return "http://127.0.0.1:" + p + "/nacos  (console; the SDK uses this port and +1000)"
	case "9093", "9092":
		return "rpk --brokers 127.0.0.1:" + p
	case "8333":
		return "S3 endpoint http://127.0.0.1:" + p
	case "9000":
		return "Centrifugo HTTP API http://127.0.0.1:" + p
	case "8000":
		return "Centrifugo WebSocket ws://127.0.0.1:" + p + "/connection/websocket"
	case "4317":
		return "OTLP gRPC 127.0.0.1:" + p
	}
	return ""
}

func init() {
	tunnelCmd.Flags().StringVar(&takeoverFlags.ssh, "ssh", "", "ssh target of the node (default: devkit.yaml takeover_ssh)")
	tunnelCmd.Flags().BoolVar(&tunnelAll, "all", false, "list every forwarded port, not only the ones a client connects to")
	rootCmd.AddCommand(tunnelCmd)
}

// stem is a forward's name without its last word and the port: the key two
// twins share ("starrocks-fe-search 9030" and "starrocks-fe-service 9030"
// -> "starrocks-fe 9030").
func stem(what string) string {
	name, port, _ := strings.Cut(what, " ")
	if i := strings.LastIndex(name, "-"); i > 0 {
		name = name[:i]
	}
	return name + " " + port
}
