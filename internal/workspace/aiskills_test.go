package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sezznaw/devkit/internal/skills"
)

func TestSyncAISkillsCopiesRefreshesAndCleans(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, "idl", "skills", "demo-task")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: demo-task\n---\nsteps\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "idl", "skills", "README.md"), []byte("not a skill"), 0o644)
	// A skill the person wrote themselves must survive everything.
	own := filepath.Join(dir, ".claude", "skills", "mine")
	os.MkdirAll(own, 0o755)
	os.WriteFile(filepath.Join(own, "SKILL.md"), []byte("mine"), 0o644)

	w, r, err := SyncAISkills(dir)
	if err != nil || len(r) != 0 {
		t.Fatalf("sync: %v %v", w, err)
	}
	builtins := len(skills.Names())
	if len(w) != 2*(builtins+1) {
		t.Fatalf("written %v, want %d built-ins + demo-task in both targets", w, builtins)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "devkit-finish", "SKILL.md")); err != nil {
		t.Error("built-in skill not installed")
	}
	for _, tg := range AISkillTargets {
		b, err := os.ReadFile(filepath.Join(dir, tg, "demo-task", "SKILL.md"))
		if err != nil || string(b) != "---\nname: demo-task\n---\nsteps\n" {
			t.Errorf("%s: %q %v", tg, b, err)
		}
	}
	if w, _, _ := SyncAISkills(dir); len(w) != 0 {
		t.Errorf("second sync wrote %v", w)
	}
	// Source changes propagate; a removed skill's copies go.
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("v2"), 0o644)
	if w, _, _ := RefreshAISkills(dir); len(w) != 2 {
		t.Errorf("refresh wrote %v", w)
	}
	os.RemoveAll(skill)
	os.MkdirAll(filepath.Join(dir, "idl", "skills", "other"), 0o755)
	os.WriteFile(filepath.Join(dir, "idl", "skills", "other", "SKILL.md"), []byte("o"), 0o644)
	_, r, _ = SyncAISkills(dir)
	if len(r) != 2 {
		t.Errorf("removed %v", r)
	}
	removed, err := CleanAISkills(dir)
	if err != nil || len(removed) != 2*(builtins+1) {
		t.Fatalf("clean: %v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(own, "SKILL.md")); err != nil {
		t.Error("own skill removed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents")); err == nil {
		t.Error(".agents left behind though empty")
	}
}
