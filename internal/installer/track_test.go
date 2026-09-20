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

// versioned is a registry that, like the real one, can still serve old
// versions. The legacy-manifest logic needs the defaults of the version a
// service was created with.
type versioned struct {
	t      *testing.T
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

func TestTrackedDefaultsFollowTheTemplateOthersStayFrozen(t *testing.T) {
	src := &versioned{t: t, dir: t.TempDir()}
	src.publish("1.0.0", vars("v0.16", "8888"), mk)
	root := newProject(t)
	if err := inst(t, root, src).Install(context.Background(), Options{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "Makefile"); got != "KITEX=v0.16 PORT=8888" {
		t.Fatalf("install rendered %q", got)
	}
	m, _ := manifest.Load(root)
	if e := m.Components["svc"].Explicit; e == nil || len(e) != 0 {
		t.Fatalf("nothing was chosen explicitly; Explicit must be an empty, non-nil list, got %#v", e)
	}

	// The template raises both defaults.
	src.publish("2.0.0", vars("v0.17", "9999"), mk)
	if _, err := inst(t, root, src).Update(context.Background(), Options{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "Makefile"); got != "KITEX=v0.17 PORT=8888" {
		t.Fatalf("after update %q; want the tracked KitexVersion to follow (v0.17) and Port to stay 8888", got)
	}
}

func TestExplicitChoiceOfATrackedVariableIsKeptUntilReleased(t *testing.T) {
	src := &versioned{t: t, dir: t.TempDir()}
	src.publish("1.0.0", vars("v0.16", "8888"), mk)
	root := newProject(t)
	if err := inst(t, root, src).Install(context.Background(), Options{Name: "svc", Vars: map[string]string{"KitexVersion": "v0.15-pinned"}}); err != nil {
		t.Fatal(err)
	}
	src.publish("2.0.0", vars("v0.17", "8888"), mk)
	if _, err := inst(t, root, src).Update(context.Background(), Options{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "Makefile"); got != "KITEX=v0.15-pinned PORT=8888" {
		t.Fatalf("a pinned version must survive the update, got %q", got)
	}
	// `--set KitexVersion=` releases the pin.
	in := inst(t, root, src)
	in.Force = true // same version again
	if _, err := in.Update(context.Background(), Options{Name: "svc", Vars: map[string]string{"KitexVersion": ""}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "Makefile"); got != "KITEX=v0.17 PORT=8888" {
		t.Fatalf("after releasing the pin got %q", got)
	}
	m, _ := manifest.Load(root)
	for _, k := range m.Components["svc"].Explicit {
		if k == "KitexVersion" {
			t.Error("KitexVersion must no longer be listed as explicit")
		}
	}
}

// A manifest written by an older devkit stores every value and has no
// "explicit" list. Which values were chosen is recovered from the defaults of
// the version the service was created with.
func TestLegacyManifestIsUnderstood(t *testing.T) {
	src := &versioned{t: t, dir: t.TempDir()}
	src.publish("1.0.0", vars("v0.16", "8888"), mk)
	src.publish("2.0.0", vars("v0.17", "8888"), mk)

	for _, c := range []struct{ name, storedKitex, want string }{
		{"default merely applied", "v0.16", "KITEX=v0.17 PORT=7000"},
		{"developer had pinned it", "v0.14-mine", "KITEX=v0.14-mine PORT=7000"},
	} {
		root := newProject(t)
		m, _ := manifest.Load(root)
		m.Components["svc"] = &manifest.Installed{Version: "1.0.0", Files: map[string]string{},
			Vars: map[string]string{"KitexVersion": c.storedKitex, "Port": "7000"}} // Explicit is nil: legacy
		m.Save()
		if _, err := inst(t, root, src).Update(context.Background(), Options{Name: "svc"}); err != nil {
			t.Fatal(err)
		}
		if got := read(t, root, "Makefile"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		after, _ := manifest.Load(root)
		if after.Components["svc"].Explicit == nil {
			t.Errorf("%s: the manifest must be upgraded to carry an explicit list", c.name)
		}
	}

	// If the old version cannot be fetched, do not guess: keep everything.
	root := newProject(t)
	m, _ := manifest.Load(root)
	m.Components["svc"] = &manifest.Installed{Version: "0.9.0-gone", Files: map[string]string{},
		Vars: map[string]string{"KitexVersion": "v0.16", "Port": "8888"}}
	m.Save()
	if _, err := inst(t, root, src).Update(context.Background(), Options{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "Makefile"); got != "KITEX=v0.16 PORT=8888" {
		t.Errorf("unknown old version: got %q, want the stored values kept", got)
	}
}
