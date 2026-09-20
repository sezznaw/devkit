package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sezznaw/devkit/internal/manifest"
)

func TestServicesFindsOnlyDevkitDirectories(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"user", "game", "idl", "common", ".hidden"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	os.WriteFile(filepath.Join(root, "devkit.yaml"), nil, 0o644)
	for _, svc := range []string{"user", "game", ".hidden"} {
		if err := manifest.New(filepath.Join(root, svc)).Save(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Services(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || filepath.Base(got[0]) != "game" || filepath.Base(got[1]) != "user" {
		t.Fatalf("Services = %v, want [game user] (sorted, no idl/common/hidden)", got)
	}
}

func TestRequiredVersion(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.24\n\nrequire (\n\tgithub.com/cloudwego/kitex v0.16.3\n\tgithub.com/cloudwego/kitexx v9.9.9 // indirect\n)\n\nrequire example.com/single v1.2.3\n"), 0o644)
	if got := RequiredVersion(root, "github.com/cloudwego/kitex"); got != "v0.16.3" {
		t.Errorf("block form: %q", got)
	}
	if got := RequiredVersion(root, "example.com/single"); got != "v1.2.3" {
		t.Errorf("single-line form: %q", got)
	}
	if got := RequiredVersion(root, "example.com/absent"); got != "" {
		t.Errorf("absent module: %q", got)
	}
}
