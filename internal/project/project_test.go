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
