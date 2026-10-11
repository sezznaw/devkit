package workspace

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AISkillsSource is where the project keeps its skills: one directory per
// task type with a SKILL.md (frontmatter name/description, then the steps).
// It lives in the IDL repository next to AGENTS.md, so it is versioned and
// pulled with the rules.
const AISkillsSource = "idl/skills"

// AISkillTargets are the project-level directories the AI tools read skills
// from: .agents/skills is the shared location (Codex, GitHub Copilot, Cursor,
// Gemini CLI, Windsurf/Devin), .claude/skills is Claude Code's. Copies, not
// symlinks: only two of the tools document following symlinks.
var AISkillTargets = []string{".agents/skills", ".claude/skills"}

// skillMarker names the file devkit leaves in every copy so it can tell its
// copies from a skill the person wrote there themselves.
const skillMarker = ".from-idl"

// ListAISkills returns the skill names under dir/idl/skills (directories
// with a SKILL.md), sorted.
func ListAISkills(dir string) []string {
	entries, err := os.ReadDir(filepath.Join(dir, AISkillsSource))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			if _, err := os.Stat(filepath.Join(dir, AISkillsSource, e.Name(), "SKILL.md")); err == nil {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// SyncAISkills copies every skill in idl/skills into the target directories
// under dir (creating them), refreshes copies whose content differs and
// removes copies of skills that no longer exist in the source. It returns
// the target paths written and removed. Without idl/skills it does nothing.
func SyncAISkills(dir string) (written, removed []string, err error) {
	names := ListAISkills(dir)
	if len(names) == 0 {
		return nil, nil, nil
	}
	for _, target := range AISkillTargets {
		w, r, err := syncSkillsTo(dir, names, filepath.Join(dir, target))
		if err != nil {
			return written, removed, err
		}
		written = append(written, w...)
		removed = append(removed, r...)
	}
	return written, removed, nil
}

// RefreshAISkills is SyncAISkills limited to target directories that already
// exist: what devkit pull runs after updating idl, so a developer who never
// ran devkit ai does not get the directories.
func RefreshAISkills(dir string) (written, removed []string, err error) {
	names := ListAISkills(dir)
	for _, target := range AISkillTargets {
		t := filepath.Join(dir, target)
		if st, err := os.Stat(t); err != nil || !st.IsDir() {
			continue
		}
		w, r, err := syncSkillsTo(dir, names, t)
		if err != nil {
			return written, removed, err
		}
		written = append(written, w...)
		removed = append(removed, r...)
	}
	return written, removed, nil
}

// CleanAISkills removes devkit's copies (marked) from the targets and the
// directories left empty; a skill the person put there is kept.
func CleanAISkills(dir string) ([]string, error) {
	var removed []string
	for _, target := range AISkillTargets {
		t := filepath.Join(dir, target)
		r, err := removeMarked(t, nil)
		if err != nil {
			return removed, err
		}
		removed = append(removed, r...)
		for d := t; d != dir; d = filepath.Dir(d) {
			if os.Remove(d) != nil {
				break
			}
		}
	}
	return removed, nil
}

func syncSkillsTo(dir string, names []string, target string) (written, removed []string, err error) {
	keep := map[string]bool{}
	for _, n := range names {
		keep[n] = true
		src := filepath.Join(dir, AISkillsSource, n)
		dst := filepath.Join(target, n)
		changed, err := copySkill(src, dst)
		if err != nil {
			return written, removed, fmt.Errorf("%s: %w", n, err)
		}
		if changed {
			rel, _ := filepath.Rel(dir, dst)
			written = append(written, filepath.ToSlash(rel))
		}
	}
	r, err := removeMarked(target, keep)
	for _, p := range r {
		rel, _ := filepath.Rel(dir, p)
		removed = append(removed, filepath.ToSlash(rel))
	}
	return written, removed, err
}

// copySkill mirrors src into dst (files only, regular files, no dot
// directories) and writes the marker. It reports whether anything changed.
func copySkill(src, dst string) (bool, error) {
	changed := false
	want := map[string]bool{}
	err := filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if e.IsDir() {
			if rel != "." && strings.HasPrefix(e.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() {
			return nil
		}
		want[rel] = true
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if cur, err := os.ReadFile(out); err == nil && bytes.Equal(cur, b) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		changed = true
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		return changed, err
	}
	// Files that left the source leave the copy too.
	_ = filepath.WalkDir(dst, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dst, p)
		if rel != skillMarker && !want[rel] {
			changed = true
			_ = os.Remove(p)
		}
		return nil
	})
	marker := filepath.Join(dst, skillMarker)
	text := "copied by `devkit ai` from " + filepath.ToSlash(filepath.Join(AISkillsSource, filepath.Base(src))) + "; edit the source, not this copy\n"
	if cur, err := os.ReadFile(marker); err != nil || string(cur) != text {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return changed, err
		}
		if err := os.WriteFile(marker, []byte(text), 0o644); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// removeMarked deletes the skill directories under target that carry the
// marker and are not in keep. It returns their paths.
func removeMarked(target string, keep map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, nil
	}
	var removed []string
	for _, e := range entries {
		if !e.IsDir() || keep[e.Name()] {
			continue
		}
		p := filepath.Join(target, e.Name())
		if _, err := os.Stat(filepath.Join(p, skillMarker)); err != nil {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return removed, err
		}
		removed = append(removed, p)
	}
	return removed, nil
}
