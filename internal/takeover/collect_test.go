package takeover

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeKubectl puts a kubectl on PATH that answers `get -o json <what> ...`
// from the files in dir named after <what> (secret: "secret-<name>").
func fakeKubectl(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := `#!/bin/sh
what=$4
[ "$what" = secret ] && what="secret-$5"
cat "` + dir + `/$what.json"
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCollectAllServices(t *testing.T) {
	dep := func(name, user string) string {
		return `{"metadata":{"name":"` + name + `"},"spec":{"template":{"spec":{"containers":[{"env":[
			{"name":"NACOS_ADDR","value":"nacos.infra:8848"},{"name":"NACOS_NAMESPACE","value":"dev"},{"name":"NACOS_USERNAME","value":"` + user + `"},
			{"name":"NACOS_PASSWORD","valueFrom":{"secretKeyRef":{"name":"nacos-` + user + `","key":"NACOS_PASSWORD"}}}]}]}}}}`
	}
	fakeKubectl(t, map[string]string{
		"nodes":     `{"items":[{"status":{"addresses":[{"type":"InternalIP","address":"10.0.0.1"}]}}]}`,
		"services":  `{"items":[]}`,
		"endpoints": `{"items":[]}`,
		"deployments": `{"items":[` + dep("ser-admin", "ser-admin") + `,` + dep("ser-auth", "ser-auth") + `,
			{"metadata":{"name":"web-admin"},"spec":{"template":{"spec":{"containers":[{"env":[{"name":"X","value":"1"}]}]}}}}]}`,
		"secret-nacos-ser-admin": `{"data":{"NACOS_PASSWORD":"YWRtaW4tcHc="}}`,
		"secret-nacos-ser-auth":  `{"data":{"NACOS_PASSWORD":"YXV0aC1wdw=="}}`,
	})
	o := CollectOptions{Namespaces: []string{"dev"}, Secrets: []string{"dev/nacos-ser-admin", "dev/nacos-ser-auth"}, Service: AllServices}
	env, err := Collect(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(env.ServiceEnvVars) != 2 {
		t.Fatalf("service env vars: %v", env.ServiceEnvVars)
	}
	if got := env.ServiceEnvVars["ser-auth"]; got["NACOS_USERNAME"] != "ser-auth" || got["NACOS_PASSWORD"] != "auth-pw" {
		t.Errorf("ser-auth: %v", got)
	}
	if got := env.ServiceEnvVars["ser-admin"]["NACOS_PASSWORD"]; got != "admin-pw" {
		t.Errorf("ser-admin password: %q", got)
	}
	if env.EnvVars["NACOS_USERNAME"] != "ser-admin" {
		t.Errorf("env vars: %v", env.EnvVars)
	}
	o.Service = "ser-auth"
	if env, err = Collect(o); err != nil || env.ServiceEnvVars != nil || env.EnvVars["NACOS_USERNAME"] != "ser-auth" {
		t.Errorf("one service: %v %v", env, err)
	}
}
