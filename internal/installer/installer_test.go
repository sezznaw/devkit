package installer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sezznaw/devkit/internal/manifest"
	"github.com/sezznaw/devkit/internal/registry"
)

type fixture struct {
	t        *testing.T
	registry string
	root     string
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, registry: t.TempDir(), root: t.TempDir()}
	os.WriteFile(filepath.Join(f.root, "go.mod"), []byte("module example.com/app\n"), 0o644)
	if err := manifest.New(f.root).Save(); err != nil {
		t.Fatal(err)
	}
	return f
}

// publish writes a component version into the local registry and bumps the index.
func (f *fixture) publish(name, version string, deps []string, vars []registry.Var, files map[string]string, once ...string) {
	f.t.Helper()
	dir := filepath.Join(f.registry, "components", name)
	os.RemoveAll(dir)
	comp := registry.Component{Name: name, Version: version, Deps: deps, Vars: vars, Once: once}
	data, _ := json.Marshal(comp)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "component.json"), data, 0o644)
	for rel, content := range files {
		p := filepath.Join(dir, "files", filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	idxPath := filepath.Join(f.registry, "registry.json")
	idx := registry.Index{Schema: 1, Components: map[string]registry.IndexEntry{}}
	if b, err := os.ReadFile(idxPath); err == nil {
		json.Unmarshal(b, &idx)
	}
	idx.Components[name] = registry.IndexEntry{Version: version}
	b, _ := json.Marshal(idx)
	os.WriteFile(idxPath, b, 0o644)
}

func (f *fixture) installer(force bool) *Installer {
	m, err := manifest.Load(f.root)
	if err != nil {
		f.t.Fatal(err)
	}
	return &Installer{Root: f.root, Source: registry.NewLocal(f.registry), Manifest: m, Force: force, SkipHooks: true, Log: func(string, ...any) {}}
}

func (f *fixture) read(rel string) string {
	b, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(rel)))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func (f *fixture) write(rel, content string) {
	os.WriteFile(filepath.Join(f.root, filepath.FromSlash(rel)), []byte(content), 0o644)
}

func TestInstallWithDependencyAndVars(t *testing.T) {
	f := newFixture(t)
	f.publish("logger", "0.1.0", nil, nil, map[string]string{"pkg/logger/logger.go.tmpl": "package logger // {{.Module}}\n"})
	f.publish("grpc", "0.1.0", []string{"logger"},
		[]registry.Var{{Name: "Service", Required: true}},
		map[string]string{"cmd/{{.Service}}/main.go.tmpl": "package main // {{.Service | title}}\n"})

	in := f.installer(false)
	err := in.Install(context.Background(), Options{Name: "grpc"})
	if err == nil || !strings.Contains(err.Error(), "requires variables") {
		t.Fatalf("expected missing var error, got %v", err)
	}
	if _, ok := in.Manifest.Components["logger"]; ok {
		t.Fatal("dependency must not be installed when the component's vars are invalid")
	}

	if err := in.Install(context.Background(), Options{Name: "grpc", Vars: map[string]string{"Service": "order"}}); err != nil {
		t.Fatal(err)
	}
	if got := f.read("cmd/order/main.go"); got != "package main // Order\n" {
		t.Errorf("rendered main.go = %q", got)
	}
	if got := f.read("pkg/logger/logger.go"); got != "package logger // example.com/app\n" {
		t.Errorf("rendered logger.go = %q", got)
	}
	m, _ := manifest.Load(f.root)
	if m.Components["grpc"].Vars["Service"] != "order" || m.Components["grpc"].Deps[0] != "logger" {
		t.Errorf("manifest entry wrong: %+v", m.Components["grpc"])
	}
	if err := f.installer(false).Install(context.Background(), Options{Name: "grpc"}); err == nil {
		t.Error("second install must fail without --force")
	}
}

func TestUpdatePreservesLocalEditsAndReconcilesFiles(t *testing.T) {
	f := newFixture(t)
	f.publish("logger", "0.1.0", nil, nil, map[string]string{
		"pkg/logger/logger.go": "v1\n",
		"pkg/logger/old.go":    "old\n",
		"pkg/logger/gone.go":   "gone\n",
	})
	if err := f.installer(false).Install(context.Background(), Options{Name: "logger"}); err != nil {
		t.Fatal(err)
	}
	f.write("pkg/logger/logger.go", "v1 + my edit\n")      // modified, still produced
	f.write("pkg/logger/old.go", "old + my edit\n")        // modified, dropped by 0.2.0
	os.Remove(filepath.Join(f.root, "pkg/logger/gone.go")) // deleted by user, still produced

	f.publish("logger", "0.2.0", nil, nil, map[string]string{
		"pkg/logger/logger.go": "v2\n",
		"pkg/logger/gone.go":   "gone\n",
		"pkg/logger/new.go":    "new\n",
	})
	in := f.installer(false)
	changed, err := in.Update(context.Background(), Options{Name: "logger"})
	if err != nil || !changed {
		t.Fatalf("Update = %v, %v", changed, err)
	}
	if f.read("pkg/logger/logger.go") != "v1 + my edit\n" {
		t.Error("modified file must be preserved")
	}
	if f.read("pkg/logger/logger.go.new") != "v2\n" {
		t.Error(".new copy must contain the incoming version")
	}
	if f.read("pkg/logger/old.go") != "old + my edit\n" {
		t.Error("modified stale file must be kept")
	}
	if f.read("pkg/logger/gone.go") != "gone\n" {
		t.Error("user-deleted file must be restored")
	}
	if f.read("pkg/logger/new.go") != "new\n" {
		t.Error("new file must be written")
	}
	m, _ := manifest.Load(f.root)
	if m.Components["logger"].Version != "0.2.0" {
		t.Errorf("version = %s", m.Components["logger"].Version)
	}
	states, _ := m.CheckFiles(f.root, "logger")
	if states["pkg/logger/logger.go"] != manifest.Modified {
		t.Error("skipped file must still report as modified")
	}
	if _, tracked := m.Components["logger"].Files["pkg/logger/old.go"]; tracked {
		t.Error("stale file must no longer be tracked")
	}

	// Same version again: no-op unless forced.
	if changed, _ := f.installer(false).Update(context.Background(), Options{Name: "logger"}); changed {
		t.Error("expected up to date")
	}
	if _, err := f.installer(true).Update(context.Background(), Options{Name: "logger"}); err != nil {
		t.Fatal(err)
	}
	if f.read("pkg/logger/logger.go") != "v2\n" || f.read("pkg/logger/logger.go.bak") != "v1 + my edit\n" {
		t.Error("--force must overwrite and keep a .bak")
	}
	if f.read("pkg/logger/logger.go.new") != "<missing>" {
		t.Error("stale .new must be removed after force")
	}
}

func TestRemoveRespectsDependentsAndPrunesDirs(t *testing.T) {
	f := newFixture(t)
	f.publish("logger", "0.1.0", nil, nil, map[string]string{"pkg/logger/logger.go": "x\n"})
	f.publish("grpc", "0.1.0", []string{"logger"}, nil, map[string]string{"internal/grpcx/i.go": "y\n"})
	if err := f.installer(false).Install(context.Background(), Options{Name: "grpc"}); err != nil {
		t.Fatal(err)
	}
	if err := f.installer(false).Remove("logger"); err == nil {
		t.Error("remove must refuse while grpc depends on logger")
	}
	if err := f.installer(false).Remove("grpc"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "internal")); !os.IsNotExist(err) {
		t.Error("empty directories must be pruned")
	}
	if err := f.installer(false).Remove("logger"); err != nil {
		t.Fatal(err)
	}
	m, _ := manifest.Load(f.root)
	if len(m.Components) != 0 {
		t.Errorf("manifest not empty: %v", m.Components)
	}
}

func TestOnceFilesAreCreatedButNotManaged(t *testing.T) {
	f := newFixture(t)
	os.Remove(filepath.Join(f.root, "go.mod")) // the fixture seeds one; this test wants the component to create it
	f.publish("svc", "0.1.0", nil, nil, map[string]string{
		"go.mod":        "module a\n",
		"conf/dev.yaml": "v1\n",
		"cmd/main.go":   "v1\n",
	}, "go.mod", "conf/*")
	if err := f.installer(false).Install(context.Background(), Options{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if f.read("go.mod") != "module a\n" || f.read("conf/dev.yaml") != "v1\n" {
		t.Fatal("once files must be created on first install")
	}
	m, _ := manifest.Load(f.root)
	if _, tracked := m.Components["svc"].Files["go.mod"]; tracked {
		t.Error("once files must not be tracked")
	}
	if len(m.Components["svc"].Files) != 1 {
		t.Errorf("tracked files = %v", m.Components["svc"].Files)
	}

	f.write("go.mod", "module a // tidied\n")
	f.write("conf/dev.yaml", "mine\n")
	f.publish("svc", "0.2.0", nil, nil, map[string]string{
		"go.mod":        "module b\n",
		"conf/dev.yaml": "v2\n",
		"cmd/main.go":   "v2\n",
	}, "go.mod", "conf/*")
	if _, err := f.installer(false).Update(context.Background(), Options{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if f.read("go.mod") != "module a // tidied\n" || f.read("conf/dev.yaml") != "mine\n" {
		t.Error("once files must never be overwritten on update")
	}
	if f.read("cmd/main.go") != "v2\n" {
		t.Error("tracked file must be updated")
	}
	if f.read("go.mod.new") != "<missing>" {
		t.Error("no .new for once files")
	}
	if err := f.installer(false).Remove("svc"); err != nil {
		t.Fatal(err)
	}
	if f.read("go.mod") != "module a // tidied\n" {
		t.Error("remove must leave once files alone")
	}
	if f.read("cmd/main.go") != "<missing>" {
		t.Error("remove must delete tracked files")
	}
}
