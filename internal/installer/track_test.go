package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sezznaw/devkit/internal/manifest"
	"github.com/sezznaw/devkit/internal/registry"
)

// versioned is a registry that can serve several versions of one component.
type versioned struct {
	dir    string
	latest string
}

func (v *versioned) publish(version string, vars []registry.Var, makefile string) {
	d := filepath.Join(v.dir, version)
	os.MkdirAll(filepath.Join(d, "files"), 0o755)
	data, _ := json.Marshal(registry.Component{Name: "svc", Version: version, Vars: vars})
	os.WriteFile(filepath.Join(d, "component.json"), data, 0o644)
	os.WriteFile(filepath.Join(d, "files", "Makefile.tmpl"), []byte(makefile), 0o644)
	v.latest = version
}

func (v *versioned) Index(context.Context) (*registry.Index, error) {
	return &registry.Index{Components: map[string]registry.IndexEntry{"svc": {Version: v.latest}}}, nil
}

func (v *versioned) Fetch(_ context.Context, name, version string) (string, error) {
	d := filepath.Join(v.dir, version)
	if _, err := os.Stat(d); err != nil {
		return "", fmt.Errorf("version %s not available", version)
	}
	return d, nil
}

const mk = "KITEX={{.KitexVersion}} PORT={{.Port}}\n"

func vars(kitex, port string) []registry.Var {
	return []registry.Var{
		{Name: "KitexVersion", Default: kitex, Track: true},
		{Name: "Port", Default: port},
	}
}

func newProject(t *testing.T) string {
	root := t.TempDir()
	if err := manifest.New(root).Save(); err != nil {
		t.Fatal(err)
	}
	return root
}

func inst(t *testing.T, root string, src registry.Source) *Installer {
	m, err := manifest.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return &Installer{Root: root, Source: src, Manifest: m, SkipHooks: true, Log: func(string, ...any) {}}
}

func read(t *testing.T, root, rel string) string {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestVersionsComeFromTheTemplateEverythingElseStaysAsCreated(t *testing.T) {
	src := &versioned{dir: t.TempDir()}
	src.publish("1.0.0", vars("v0.16", "8888"), mk)
	root := newProject(t)
	if err := inst(t, root, src).Install(context.Background(), Options{Name: "svc", Vars: map[string]string{"Port": "9100"}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "Makefile"); got != "KITEX=v0.16 PORT=9100" {
		t.Fatalf("install rendered %q", got)
	}
	// The template raises the version and changes the default port.
	src.publish("2.0.0", vars("v0.17", "7777"), mk)
	if _, err := inst(t, root, src).Update(context.Background(), Options{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "Makefile"); got != "KITEX=v0.17 PORT=9100" {
		t.Fatalf("after update %q; the version must follow the template, the port must stay as created", got)
	}
}

func TestAVersionCannotBeChosenPerService(t *testing.T) {
	src := &versioned{dir: t.TempDir()}
	src.publish("1.0.0", vars("v0.16", "8888"), mk)
	root := newProject(t)
	err := inst(t, root, src).Install(context.Background(), Options{Name: "svc", Vars: map[string]string{"KitexVersion": "v0.15"}})
	if err == nil || !strings.Contains(err.Error(), "KitexVersion cannot be set") || !strings.Contains(err.Error(), "KitexVersion=v0.16") {
		t.Fatalf("install must refuse and name the team version, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "Makefile")); statErr == nil {
		t.Fatal("nothing may be written when the request is refused")
	}
	if err := inst(t, root, src).Install(context.Background(), Options{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := inst(t, root, src).Update(context.Background(), Options{Name: "svc", Vars: map[string]string{"KitexVersion": "v0.15"}}); err == nil {
		t.Fatal("update must refuse as well")
	}
}

// Manifests written by devkit <= 0.1.8 stored the version (and 0.1.8 an
// "explicit" list, possibly naming it as pinned). Both are simply overruled.
func TestOldManifestsWithAStoredOrPinnedVersionAreBroughtInLine(t *testing.T) {
	src := &versioned{dir: t.TempDir()}
	src.publish("1.0.0", vars("v0.16", "8888"), mk)
	src.publish("2.0.0", vars("v0.17", "8888"), mk)
	root := newProject(t)
	raw := `{"schema":1,"components":{"svc":{"version":"1.0.0","installedAt":"2026-09-20T00:00:00Z",
	  "vars":{"KitexVersion":"v0.14-pinned-long-ago","Port":"7000"},"explicit":["KitexVersion","Port"],"files":{}}}}`
	if err := os.WriteFile(manifest.FilePath(root), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := inst(t, root, src).Update(context.Background(), Options{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "Makefile"); got != "KITEX=v0.17 PORT=7000" {
		t.Fatalf("got %q; an old pin must not survive, the port must", got)
	}
	after, _ := os.ReadFile(manifest.FilePath(root))
	if strings.Contains(string(after), "explicit") {
		t.Error("the obsolete explicit list should be gone after rewriting the manifest")
	}
}
