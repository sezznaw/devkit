package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindAndLoadConfig(t *testing.T) {
	ws := t.TempDir()
	if Find(ws) != "" {
		t.Fatal("no config expected yet")
	}
	if err := SaveConfig(ws, &Config{ModulePrefix: "x.com/a", IdlRepo: "a/idl", Vars: map[string]string{"NacosAddr": "n:1"}}); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(ws, "svc", "cmd")
	os.MkdirAll(nested, 0o755)
	if got := Find(nested); got != ws {
		t.Fatalf("Find = %q, want %q", got, ws)
	}
	cfg, err := LoadConfig(ws)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModulePrefix != "x.com/a" || cfg.IdlRepo != "a/idl" || cfg.Vars["NacosAddr"] != "n:1" {
		t.Errorf("loaded %+v", cfg)
	}
	empty, err := LoadConfig(t.TempDir())
	if err != nil || empty.ModulePrefix != "" {
		t.Errorf("missing file must give empty config, got %+v %v", empty, err)
	}
}

func TestWriteTemplateIsLoadableAndPrefilled(t *testing.T) {
	dir := t.TempDir()
	if err := WriteTemplate(dir, &Config{IdlRepo: "sezznaw/shop-idl", CommonRepo: "someone/fork"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("template must be valid YAML: %v", err)
	}
	if cfg.ModulePrefix != "" || cfg.IdlRepo != "sezznaw/shop-idl" || cfg.GoPrivate {
		t.Errorf("unexpected template values: %+v", cfg)
	}
	// The common library is fixed; the template must not offer it as a setting.
	raw, _ := os.ReadFile(filepath.Join(dir, ConfigFile))
	if strings.Contains(string(raw), "common_repo") || cfg.CommonRepo != "" {
		t.Errorf("template must not mention common_repo:\n%s", raw)
	}
}
