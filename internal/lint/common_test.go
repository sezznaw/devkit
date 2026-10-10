package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sezznaw/devkit/internal/manifest"
)

func TestCommonVersion(t *testing.T) {
	root := t.TempDir()
	m := &manifest.Manifest{Components: map[string]*manifest.Installed{"kitex-service": {Vars: map[string]string{"CommonModule": "github.com/x/common", "CommonVersion": "v0.44.0"}}}}
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("module svc\n\ngo 1.26\n\nrequire (\n\tgithub.com/x/common v0.44.0\n\tother.io/y v1.0.0\n)\n")
	if fs := checkCommonVersion(root, m); len(fs) != 0 {
		t.Fatalf("in line: %v", fs)
	}
	write("module svc\n\nrequire github.com/x/common v0.45.0\n")
	fs := checkCommonVersion(root, m)
	if len(fs) != 1 || fs[0].Rule != "common-version" || fs[0].Line != 3 {
		t.Fatalf("moved: %+v", fs)
	}
	write("module svc\n")
	if fs := checkCommonVersion(root, m); len(fs) != 0 {
		t.Fatalf("not required: %v", fs)
	}
}
