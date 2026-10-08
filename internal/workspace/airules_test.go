package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIPointersSelectWriteClean(t *testing.T) {
	dir := t.TempDir()
	sel, err := SelectAIPointers([]string{"claude"})
	if err != nil || len(sel) != 2 { // CLAUDE.md needs AGENTS.md
		t.Fatalf("claude selects CLAUDE.md + AGENTS.md: %v %v", sel, err)
	}
	written, err := WriteAIPointers(dir, sel)
	if err != nil || len(written) != 2 {
		t.Fatalf("%v %v", written, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); string(b) != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md")); !strings.Contains(string(b), "idl/AGENTS.md") {
		t.Errorf("AGENTS.md points at the source")
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor")); err == nil {
		t.Error("only the asked tools are written")
	}
	if _, err := SelectAIPointers([]string{"nope"}); err == nil {
		t.Error("unknown tool is an error")
	}
	all, _ := SelectAIPointers([]string{"all"})
	if len(all) != len(AIPointerFiles) {
		t.Errorf("all = %d", len(all))
	}
	WriteAIPointers(dir, all)
	os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("my own notes\n"), 0o644)
	present := PresentAIPointers(dir)
	if present["cursor"] != "devkit" || present["gemini"] != "yours" {
		t.Errorf("present: %v", present)
	}
	removed, err := CleanAIPointers(dir)
	if err != nil || len(removed) != len(AIPointerFiles)-1 {
		t.Fatalf("clean removes devkit's files only: %d %v", len(removed), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "GEMINI.md")); err != nil {
		t.Error("the edited file is kept")
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor")); err == nil {
		t.Error("empty directories are removed")
	}
	if !strings.Contains(AIToolList(), "Cursor") {
		t.Error("tool list")
	}
}
