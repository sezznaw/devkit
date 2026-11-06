package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConf(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, "conf", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestConfYAML: a key written twice in one block (the 2026-11-05 case: two
// `allow` in one callers rule) and a broken indentation are reported with the
// line yaml.v3 names; variables are expanded first, as at start-up, so
// placeholders never make a finding; only conf/*.yaml is read.
func TestConfYAML(t *testing.T) {
	root := writeConf(t, map[string]string{
		"dev.yaml": `service:
  name: ser-x
callers:
  rules:
    - methods: [Ping]
      allow: [ser-x]
    - methods: [List]
      allow: [ser-api]
      allow: [ser-admin-api]
`,
		"local.yaml": `mysql:
  password: ${MYSQL_PASSWORD:-tenant_a}
  user: ${MYSQL_USER}
  addr: "${MYSQL_HOST:-127.0.0.1}:3306"
`,
		"uat.yaml":  "service:\n  name: ser-x\n   port: 8100\n",
		"prod.yaml": "service:\n  name: ser-x\n",
		"notes.txt": "allow: [a]\nallow: [b]\n",
	})
	got := checkConfYAML(root)
	byFile := map[string]Finding{}
	for _, f := range got {
		if f.Rule != confYAMLRule {
			t.Fatalf("rule %q", f.Rule)
		}
		byFile[f.File] = f
	}
	if len(got) != 2 {
		t.Fatalf("want findings for dev.yaml and uat.yaml only, got %v", got)
	}
	dup, ok := byFile["conf/dev.yaml"]
	if !ok || dup.Line != 9 || !strings.Contains(dup.Message, `"allow" already defined`) {
		t.Fatalf("duplicate key: %+v", dup)
	}
	if bad, ok := byFile["conf/uat.yaml"]; !ok || bad.Line == 0 {
		t.Fatalf("indentation: %+v", bad)
	}
}

func TestConfYAMLExpandsLikeTheService(t *testing.T) {
	t.Setenv("CONF_YAML_TEST_LIST", "[a, b]")
	// a value that only becomes YAML once expanded parses; one that breaks once expanded is reported
	ok := writeConf(t, map[string]string{"dev.yaml": "allow: ${CONF_YAML_TEST_LIST}\n"})
	if got := checkConfYAML(ok); len(got) != 0 {
		t.Fatalf("%v", got)
	}
	t.Setenv("CONF_YAML_TEST_BROKEN", "[a, b")
	bad := writeConf(t, map[string]string{"dev.yaml": "allow: ${CONF_YAML_TEST_BROKEN}\n"})
	if got := checkConfYAML(bad); len(got) != 1 {
		t.Fatalf("%v", got)
	}
}

func TestConfYAMLWithoutConf(t *testing.T) {
	if got := checkConfYAML(t.TempDir()); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}
