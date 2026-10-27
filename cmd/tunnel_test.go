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
			"ser-admin": {"NACOS_ADDR": "nacos.sportsbook-dev-infra:8848", "NACOS_USERNAME": "ser-admin", "MYSQL_PLATFORM_PASSWORD": "admin-pw"},
			"ser-auth":  {"NACOS_ADDR": "nacos.sportsbook-dev-infra:8848", "NACOS_USERNAME": "ser-auth"},
		},
	}
	l, err := takeover.Plan(env, func(int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() { printTunnel(l, env, "node", false, true) })
	for _, want := range []string{"NACOS_ADDR", "127.0.0.1:18848", "ser-admin\n", "ser-auth\n", "admin-pw"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if n := strings.Count(out, "NACOS_ADDR"); n != 1 {
		t.Errorf("NACOS_ADDR printed %d times, want once:\n%s", n, out)
	}
	if n := strings.Count(out, "NACOS_USERNAME"); n != 2 {
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
