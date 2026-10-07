package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAIPointers(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteAIPointers(dir)
	if err != nil || len(written) != len(AIPointerFiles) {
		t.Fatalf("%d written, %v", len(written), err)
	}
	for _, f := range AIPointerFiles {
		b, err := os.ReadFile(filepath.Join(dir, f.Path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "idl/AGENTS.md") && f.Path != "CLAUDE.md" {
			t.Errorf("%s does not point at the source", f.Path)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); string(b) != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md imports AGENTS.md: %q", b)
	}
	again, err := WriteAIPointers(dir)
	if err != nil || len(again) != 0 {
		t.Errorf("second run rewrites %v", again)
	}
	if !strings.Contains(AIToolList(), "Cursor") {
		t.Error("tool list")
	}
}
