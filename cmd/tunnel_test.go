package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sezznaw/devkit/internal/takeover"
)

// TestPrintTunnelPerService: what every service shares is printed once,
// what differs is printed under each service, addresses rewritten.
func TestPrintTunnelPerService(t *testing.T) {
	env := &takeover.Env{Version: 1, NodeIP: "172.31.41.180",
		Infra: []takeover.Endpoint{
			{Name: "nacos", Namespace: "sportsbook-dev-infra", ClusterIP: "10.43.243.195", Ports: []int{8848}},
			{Name: "mysql", Namespace: "sportsbook-dev-infra", Ports: []int{3306}, Pods: []takeover.Pod{{Name: "mysql-0", IP: "10.42.1.29"}}},
		},
		EnvVars: map[string]string{"NACOS_ADDR": "nacos.sportsbook-dev-infra:8848", "NACOS_USERNAME": "ser-admin"},
		ServiceEnvVars: map[string]map[string]string{
			"ser-admin":       {"NACOS_ADDR": "nacos.sportsbook-dev-infra:8848", "NACOS_USERNAME": "ser-admin", "MYSQL_PLATFORM_PASSWORD": "platform-pw", "TENANT_CODE": "tenant_a"},
			"ser-auth":        {"NACOS_ADDR": "nacos.sportsbook-dev-infra:8848", "NACOS_USERNAME": "ser-auth", "TENANT_CODE": "tenant_a"},
			"ser-agent-admin": {"NACOS_ADDR": "nacos.sportsbook-dev-infra:8848", "NACOS_USERNAME": "ser-agent-admin", "MYSQL_PLATFORM_PASSWORD": "platform-pw", "MYSQL_SERVICE_PASSWORD": "agent-pw", "TENANT_CODE": "tenant_a"},
		},
	}
	l, err := takeover.Plan(env, func(int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() { printTunnel(l, env, "node", false, true) })
	for _, want := range []string{"NACOS_ADDR", "127.0.0.1:18848", "ser-admin\n", "ser-auth\n", "platform-pw  user platform", "agent-pw  user tenant_a_agent_admin"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if n := strings.Count(out, "NACOS_ADDR"); n != 1 {
		t.Errorf("NACOS_ADDR printed %d times, want once:\n%s", n, out)
	}
	// the same value in the services that have it: once, not per service
	if n := strings.Count(out, "MYSQL_PLATFORM_PASSWORD"); n != 1 {
		t.Errorf("MYSQL_PLATFORM_PASSWORD printed %d times, want once:\n%s", n, out)
	}
	if n := strings.Count(out, "NACOS_USERNAME"); n != 3 {
		t.Errorf("NACOS_USERNAME printed %d times, want per service:\n%s", n, out)
	}
	if strings.Contains(out, "predates") {
		t.Errorf("old-node note with per-service vars:\n%s", out)
	}
}

func capture(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	f()
	os.Stdout = old
	w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

func TestMysqlUser(t *testing.T) {
	for _, c := range []struct{ k, service, tenant, want string }{
		{"MYSQL_SERVICE_PASSWORD", "ser-finance-admin", "tenant_a", "tenant_a_finance_admin"},
		{"MYSQL_SERVICE_PASSWORD", "", "tenant_a", ""},
		{"MYSQL_SERVICE_PASSWORD", "ser-member", "", ""},
		{"MYSQL_PLATFORM_PASSWORD", "", "", "platform"},
		{"MYSQL_TENANT_A_PASSWORD", "ser-bet", "tenant_a", "tenant_a"},
		{"MYSQL_PASSWORD", "", "", ""},
		{"VALKEY_SERVICE_PASSWORD", "ser-auth", "tenant_a", ""},
	} {
		if got := mysqlUser(c.k, c.service, c.tenant); got != c.want {
			t.Errorf("mysqlUser(%s, %s, %s) = %q, want %q", c.k, c.service, c.tenant, got, c.want)
		}
	}
}
