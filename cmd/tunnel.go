package cmd

import (
	"context"
	"fmt"
	"net"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/takeover"
	"github.com/sezznaw/devkit/internal/ui"
)

var tunnelAll, tunnelReveal bool

var tunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Reach the development environment's middleware from this machine: MySQL, Redis, Nacos, Kafka ... on local ports [takeover]",
	Long: `tunnel opens the same ssh connection devkit takeover uses, with the local
forwards only: every Service of the environment's infrastructure and every
business service becomes a local port (infrastructure port + 10000, service
port + 1000), and the credentials the deployments use are printed once, to
the terminal, per service where the services differ (each has its own
Nacos account, database user ...). Connect your database client to 127.0.0.1:<port>. Nothing is
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
		// Every backend's credentials; a node whose devkit predates that
		// answers "*" with an error, and then one service's are shown.
		env, err := takeover.FetchEnv(ctx, target, takeover.AllServices)
		if err != nil {
			if env, err = takeover.FetchEnv(ctx, target, ""); err != nil {
				return err
			}
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
		printTunnel(layout, env, target, tunnelAll, tunnelReveal)
		fmt.Printf("%s tunnel is up; Ctrl+C closes it. %s\n", ui.Stdout.Success("✓"), ui.Stdout.Dim("Credentials are shown once and written nowhere."))
		select {
		case <-ctx.Done():
		case err := <-t.Done():
			return fmt.Errorf("tunnel: the ssh connection ended: %v", err)
		}
		fmt.Println(ui.Stdout.Dim("closed"))
		return nil
	},
}

// printTunnel prints one block per middleware a developer connects to: a
// heading with the local address and the cluster name, a ready-made client
// command, then the credentials whose variable names mention that
// middleware: the ones every service shares once, then, per service, the
// ones that differ between services. Business services and the rest of the cluster's ports are
// forwarded too but only counted; --all lists everything and every variable.
func printTunnel(l *takeover.Layout, env *takeover.Env, target string, all, reveal bool) {
	podIP := map[string]bool{}
	for _, ep := range env.Infra {
		for _, p := range ep.Pods {
			podIP[p.IP] = true
		}
	}
	isPod := func(target string) bool {
		h, _, err := net.SplitHostPort(target)
		return err == nil && podIP[h]
	}
	show := func(k, v string) string {
		if reveal || !secretLike(k) {
			return v
		}
		return maskValue(v)
	}
	st := ui.Stdout
	services := map[string]bool{}
	for _, s := range env.Services {
		services[s.Name] = true
	}
	namespace := map[string]string{}
	for _, ep := range env.Infra {
		namespace[ep.Name] = ep.Namespace
	}
	for _, s := range env.Services {
		namespace[s.Name] = s.Namespace
	}
	clusterIP := map[string]bool{}
	for _, f := range l.Forwards {
		if f.Target != "" && !isPod(f.Target) {
			clusterIP[stem(f.What)] = true
		}
	}
	used := map[string]bool{}
	// varying are the variables printed per service: those two services
	// hold different values of, and a service's own account (*_SERVICE_*:
	// MYSQL_SERVICE_PASSWORD ...) even where one service has it. The rest
	// are printed once (shared), also when only some services have them.
	perService := l.ServiceEnvVars
	varying := map[string]bool{}
	shared := map[string]string{}
	for k, v := range l.EnvVars {
		shared[k] = v
	}
	for _, vars := range perService {
		for k, v := range vars {
			if w, ok := shared[k]; (ok && w != v) || strings.Contains(k, "_SERVICE_") {
				varying[k] = true
			}
			shared[k] = v
		}
	}
	for k := range varying {
		delete(shared, k)
	}
	// value is a variable's value as printed, with the account it is the
	// password of when the name tells (service "" for a shared one).
	value := func(k, v, service string) string {
		out := show(k, v)
		tenant := shared["TENANT_CODE"]
		if t, ok := perService[service]["TENANT_CODE"]; ok {
			tenant = t
		}
		if user := mysqlUser(k, service, tenant); user != "" {
			out += "  " + st.Dim("user "+user)
		}
		return out
	}
	serviceNames := make([]string, 0, len(perService))
	for name := range perService {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)
	// printPerService prints, under each service that has any, the varying
	// variables keep selects (and marks them used).
	printPerService := func(keep func(k string) bool) {
		var keys []string
		for k := range varying {
			if !used[k] && keep(k) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, name := range serviceNames {
			first := true
			for _, k := range keys {
				v, ok := perService[name][k]
				if !ok {
					continue
				}
				if first {
					fmt.Printf("  %s\n", st.Bold(name))
					first = false
				}
				fmt.Printf("    %s  %s\n", st.Dim(fmt.Sprintf("%-38s", k)), value(k, v, name))
			}
		}
		for _, k := range keys {
			used[k] = true
		}
	}
	fmt.Printf("%s %s %s\n\n", st.Heading("devkit tunnel"), st.Dim("→"), st.Dim(target))
	hidden := 0
	for _, f := range l.Forwards {
		if f.Target == "" {
			continue
		}
		twin := isPod(f.Target) && clusterIP[stem(f.What)] && !strings.Contains(f.What, "mysql") && !strings.Contains(f.What, "valkey") && !strings.Contains(f.What, "redpanda")
		// Default view: what a person connects a client to (MySQL, Redis,
		// Nacos, Kafka, StarRocks, S3). Tempo, Centrifugo, the services:
		// things services talk to, --all.
		if !all && (!takeover.KnownPort(f) || twin || services[f.What]) {
			hidden++
			continue
		}
		name, port, _ := strings.Cut(f.What, " ")
		cluster := name
		if ns := namespace[name]; ns != "" {
			cluster += "." + ns
		}
		if port != "" {
			cluster += ":" + port
		} else if i := strings.LastIndex(f.Target, ":"); i >= 0 {
			cluster += f.Target[i:]
		}
		fmt.Printf("%s  %s  %s\n", st.Cyan(st.Bold(fmt.Sprintf("%-10s", kindName(name)))), st.Green(st.Bold(fmt.Sprintf("127.0.0.1:%d", f.LocalPort))), st.Dim(cluster))
		if how := howTo(f); how != "" {
			fmt.Printf("  %s\n", how)
		}
		for _, k := range sortedKeys(shared) {
			if !used[k] && relatedVar(k, f.What) {
				used[k] = true
				fmt.Printf("  %s  %s\n", st.Dim(fmt.Sprintf("%-40s", k)), value(k, shared[k], ""))
			}
		}
		printPerService(func(k string) bool { return relatedVar(k, f.What) })
		fmt.Println()
	}
	if perService == nil {
		fmt.Println(st.Dim("the credentials are one service's: the node's devkit predates per-service credentials (rerun the node setup with a newer devkit)"))
	}
	if !reveal {
		fmt.Println(st.Dim("passwords and keys are masked; --reveal prints them (they then sit in your terminal's scrollback)"))
	}
	if all {
		var rest []string
		for _, k := range sortedKeys(shared) {
			if !used[k] {
				rest = append(rest, k)
			}
		}
		restVarying := false
		for k := range varying {
			restVarying = restVarying || !used[k]
		}
		if len(rest) > 0 || restVarying {
			fmt.Printf("%s\n", st.Cyan(st.Bold("other settings")))
			for _, k := range rest {
				fmt.Printf("  %s  %s\n", st.Dim(fmt.Sprintf("%-40s", k)), value(k, shared[k], ""))
			}
			printPerService(func(string) bool { return true })
			fmt.Println()
		}
	}
	if hidden > 0 {
		fmt.Println(st.Dim(fmt.Sprintf("%d more ports forwarded (the %d services at their port + 1000, Tempo, Centrifugo, internal RPC, metrics, probes); --all lists them with their settings", hidden, len(env.Services))))
	}
}

// mysqlUser is the MySQL account a password variable belongs to, "" when
// the name does not tell. MYSQL_SERVICE_PASSWORD is the service's own
// account, <tenant>_<domain>, the domain being the service name without
// "ser-", "-" as "_" (kit-common mysqlx.ServiceDatabase / DomainName; a
// conf that sets mysql.domain names another). MYSQL_<NAME>_PASSWORD is the
// account <name> (platform, tenant_a: the environment's MySQL init).
func mysqlUser(k, service, tenant string) string {
	name, ok := strings.CutPrefix(k, "MYSQL_")
	if !ok {
		return ""
	}
	if name, ok = strings.CutSuffix(name, "_PASSWORD"); !ok || name == "" {
		return ""
	}
	if name == "SERVICE" {
		if service == "" || tenant == "" {
			return ""
		}
		return tenant + "_" + strings.ReplaceAll(strings.TrimPrefix(service, "ser-"), "-", "_")
	}
	return strings.ToLower(name)
}

// kindName is the friendly name of an infrastructure Service.
func kindName(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "mysql"):
		return "MySQL"
	case strings.Contains(n, "valkey"), strings.Contains(n, "redis"):
		return "Valkey"
	case strings.Contains(n, "nacos"):
		return "Nacos"
	case strings.Contains(n, "redpanda"), strings.Contains(n, "kafka"):
		return "Redpanda"
	case strings.Contains(n, "starrocks"):
		return "StarRocks"
	case strings.Contains(n, "seaweed"), strings.Contains(n, "minio"):
		return "S3"
	case strings.Contains(n, "centrifugo"):
		return "Centrifugo"
	case strings.Contains(n, "tempo"):
		return "Tempo"
	}
	return name
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// relatedVar says whether an environment variable belongs to the middleware
// a forward is for, by the words in its name.
func relatedVar(name, what string) bool {
	n := strings.ToUpper(name)
	w := strings.ToUpper(what)
	words := map[string][]string{
		"MYSQL":      {"MYSQL", "TENANT_CODE"},
		"VALKEY":     {"VALKEY", "REDIS"},
		"REDIS":      {"VALKEY", "REDIS"},
		"NACOS":      {"NACOS"},
		"REDPANDA":   {"KAFKA", "REDPANDA"},
		"KAFKA":      {"KAFKA", "REDPANDA"},
		"STARROCKS":  {"STARROCKS"},
		"SEAWEEDFS":  {"S3_", "SEAWEED", "MINIO"},
		"MINIO":      {"S3_", "MINIO"},
		"CENTRIFUGO": {"CENTRIFUGO"},
		"TEMPO":      {"OTEL"},
	}
	for key, list := range words {
		if strings.Contains(w, key) {
			for _, x := range list {
				if strings.Contains(n, x) {
					return true
				}
			}
		}
	}
	return false
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
	tunnelCmd.Flags().BoolVar(&tunnelReveal, "reveal", false, "print passwords and keys instead of masking them")
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

// secretLike says whether an environment variable holds a credential.
func secretLike(k string) bool {
	u := strings.ToUpper(k)
	for _, w := range []string{"PASSWORD", "SECRET", "TOKEN", "KEY", "PASS"} {
		if strings.Contains(u, w) {
			return true
		}
	}
	return false
}

// maskValue keeps the first two characters so two values can be told apart.
func maskValue(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return v[:2] + strings.Repeat("*", 8)
}
