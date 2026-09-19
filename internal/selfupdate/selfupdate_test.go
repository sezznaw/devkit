package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	os.WriteFile(archive, tarGz(t, "devkit", "#!/bin/sh\necho hi\n"), 0o644)
	dest := filepath.Join(dir, "out")
	if err := extractBinary(archive, "devkit", dest); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "#!/bin/sh\necho hi\n" {
		t.Errorf("wrong content: %q", b)
	}
	if err := extractBinary(archive, "missing", dest); err == nil {
		t.Error("expected error for missing entry")
	}
}

func TestNormalize(t *testing.T) {
	if Normalize(" v1.2.0\n") != "1.2.0" || Normalize("1.2.0") != "1.2.0" {
		t.Error("Normalize failed")
	}
}
