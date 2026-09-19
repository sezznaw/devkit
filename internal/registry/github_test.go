package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// githubTarball mimics GET /repos/{owner}/{repo}/tarball/{ref}:
// every entry is prefixed with "<owner>-<repo>-<sha>/".
func githubTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: "sezznaw-devkit-registry-abc123/" + name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtractComponentStripsPrefixes(t *testing.T) {
	data := githubTarball(t, map[string]string{
		"components/logger/component.json":                        `{"name":"logger","version":"0.1.0"}`,
		"components/logger/files/pkg/{{.Package}}/logger.go.tmpl": "package x",
		"components/other/component.json":                         `{"name":"other","version":"9.9.9"}`,
	})
	dest := t.TempDir()
	if err := extractComponent(bytes.NewReader(data), dest, "components/logger"); err != nil {
		t.Fatal(err)
	}
	c, err := LoadComponent(dest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "logger" || c.Version != "0.1.0" {
		t.Errorf("unexpected component %+v", c)
	}
	if _, err := os.Stat(filepath.Join(dest, "files", "pkg", "{{.Package}}", "logger.go.tmpl")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "..", "other")); err == nil {
		t.Error("unrelated component must not be extracted")
	}
}

func TestExtractComponentRejectsTraversal(t *testing.T) {
	data := githubTarball(t, map[string]string{"components/logger/../../evil": "x"})
	if err := extractComponent(bytes.NewReader(data), t.TempDir(), "components/logger"); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestExtractComponentEmpty(t *testing.T) {
	data := githubTarball(t, map[string]string{"components/other/component.json": "{}"})
	if err := extractComponent(bytes.NewReader(data), t.TempDir(), "components/logger"); err == nil {
		t.Fatal("expected error for empty archive")
	}
}
