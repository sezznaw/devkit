package takeover

import (
	"strings"
	"testing"
)

func sampleEnv() *Env {
	return &Env{Version: 1, NodeIP: "172.31.41.180",
		Services: []Service{{Name: "ser-auth", Namespace: "sportsbook-dev", ClusterIP: "10.43.128.101", Port: 80, TargetPort: 8120}},
		Infra: []Endpoint{
			{Name: "nacos", Namespace: "sportsbook-dev-infra", ClusterIP: "10.43.243.195", Ports: []int{8848, 9848, 9849}},
			{Name: "mysql", Namespace: "sportsbook-dev-infra", Ports: []int{3306}, Pods: []Pod{{Name: "mysql-0", IP: "10.42.1.29"}}},
			{Name: "redpanda", Namespace: "sportsbook-dev-infra", Ports: []int{9093, 8082}, Pods: []Pod{{Name: "redpanda-0", IP: "10.42.1.38"}}},
			{Name: "seaweedfs", Namespace: "sportsbook-dev-infra", ClusterIP: "10.43.77.254", Ports: []int{8333, 8080}},
			{Name: "redpanda-console", Namespace: "sportsbook-dev-infra", ClusterIP: "10.43.37.191", Ports: []int{8080}},
			{Name: "tempo", Namespace: "monitoring", ClusterIP: "10.43.202.164", Ports: []int{4317, 55680}},
		},
		EnvVars: map[string]string{"NACOS_ADDR": "nacos.sportsbook-dev-infra:8848", "OTEL_EXPORTER_OTLP_ENDPOINT": "tempo.monitoring:4317", "MYSQL_TENANT_A_PASSWORD": "pw", "APP_ENV": "dev"},
	}
}

func TestPlan(t *testing.T) {
	l, err := Plan(sampleEnv(), func(int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if l.NacosAddr != "127.0.0.1:18848" {
		t.Errorf("nacos: %s", l.NacosAddr)
	}
	want := map[string]string{
		"nacos.sportsbook-dev-infra:8848":                                 "127.0.0.1:18848",
		"nacos.sportsbook-dev-infra.svc.cluster.local:9848":               "127.0.0.1:19848",
		"10.43.243.195:8848":                                              "127.0.0.1:18848",
		"mysql.sportsbook-dev-infra:3306":                                 "127.0.0.1:13306",
		"mysql-0.mysql.sportsbook-dev-infra.svc.cluster.local:3306":       "127.0.0.1:13306",
		"10.42.1.29:3306":                                                 "127.0.0.1:13306",
		"redpanda-0.redpanda.sportsbook-dev-infra.svc.cluster.local:9093": "127.0.0.1:19093",
		"ser-auth.sportsbook-dev:80":                                      "127.0.0.1:9120",
		"10.43.128.101:80":                                                "127.0.0.1:9120",
	}
	for k, v := range want {
		if l.Rewrite[k] != v {
			t.Errorf("rewrite[%s] = %q, want %q", k, l.Rewrite[k], v)
		}
	}
	if l.Services["ser-auth"] != "127.0.0.1:9120" {
		t.Errorf("services: %v", l.Services)
	}
	if l.EnvVars["NACOS_ADDR"] != "127.0.0.1:18848" || l.EnvVars["MYSQL_TENANT_A_PASSWORD"] != "pw" {
		t.Errorf("env: %v", l.EnvVars)
	}
	// Two infrastructure services on 8080: the second moves up.
	a, b := l.Rewrite["seaweedfs.sportsbook-dev-infra:8080"], l.Rewrite["redpanda-console.sportsbook-dev-infra:8080"]
	if a == b || a == "" || b == "" {
		t.Errorf("port collision not resolved: %q %q", a, b)
	}
	if l.Rewrite["tempo.monitoring:55680"] == "" || l.Rewrite["tempo.monitoring:4317"] != "127.0.0.1:14317" {
		t.Errorf("high port handling: 55680 -> %q, 4317 -> %q", l.Rewrite["tempo.monitoring:55680"], l.Rewrite["tempo.monitoring:4317"])
	}
	var mysqlTarget string
	for _, f := range l.Forwards {
		if f.LocalPort == 13306 {
			mysqlTarget = f.Target
		}
	}
	if mysqlTarget != "10.42.1.29:3306" {
		t.Errorf("headless mysql forwards to %q, want the pod", mysqlTarget)
	}
}

func TestPlanRefusesWhenNacosGRPCPortTaken(t *testing.T) {
	_, err := Plan(sampleEnv(), func(p int) bool { return p == 19848 })
	if err == nil || !strings.Contains(err.Error(), "1000") {
		t.Errorf("want the nacos port error, got %v", err)
	}
}

func TestRemotePortStableAndInRange(t *testing.T) {
	a, b := RemotePort("ser-member", "alice", 0), RemotePort("ser-member", "alice", 0)
	if a != b || a < 20000 || a >= 30000 {
		t.Errorf("%d %d", a, b)
	}
	if RemotePort("ser-member", "alice", 1) != a+1 {
		t.Error("attempt does not advance")
	}
}

func TestPortOf(t *testing.T) {
	for in, want := range map[string]int{"8100": 8100, ":8100": 8100, "0.0.0.0:8100": 8100} {
		if got, err := PortOf(in); err != nil || got != want {
			t.Errorf("PortOf(%q) = %d %v", in, got, err)
		}
	}
	if _, err := PortOf("x"); err == nil {
		t.Error("garbage accepted")
	}
}

func TestListeningSeesOwnListener(t *testing.T) {
	l, err := newListener()
	if err != nil {
		t.Skip(err)
	}
	defer l.Close()
	if !Listening(l.port()) {
		t.Errorf("own listener on %d not seen", l.port())
	}
	l.Close()
	if Listening(l.port()) {
		t.Errorf("closed listener on %d still seen", l.port())
	}
}
