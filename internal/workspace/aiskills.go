package workspace

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sezznaw/devkit/internal/skills"
)

// AISkillsSource is where a project keeps its own skills: one directory per
// task type with a SKILL.md (frontmatter name/description, then the steps).
// It lives in the IDL repository next to AGENTS.md, so it is versioned and
// pulled with the rules. The framework's skills (devkit-rpc-method,
// devkit-gateway-api, devkit-gateway-domain, devkit-finish) are built into
// devkit and installed alongside; a project skill with the same name as a
// built-in replaces it.
const AISkillsSource = "idl/skills"

// AISkillTargets are the project-level directories the AI tools read skills
// from: .agents/skills is the shared location (Codex, GitHub Copilot, Cursor,
// Gemini CLI, Windsurf/Devin), .claude/skills is Claude Code's. Copies, not
// symlinks: only two of the tools document following symlinks.
var AISkillTargets = []string{".agents/skills", ".claude/skills"}

// skillMarker names the file devkit leaves in every copy so it can tell its
// copies from a skill the person wrote there themselves.
const skillMarker = ".from-devkit"

// legacySkillMarker is what devkit v0.19 wrote; copies carrying it are still devkit's.
const legacySkillMarker = ".from-idl"

// AISkill is one skill to install: its name and where it comes from.
type AISkill struct {
	Name string
	// Source is "devkit" for a built-in, else the project directory
	// (idl/skills/<name>) relative to the project.
	Source string
	files  map[string][]byte
}

// ListAISkills returns the skills to install for the project at dir: the
// built-ins, then the project's own under idl/skills (a project skill with
// a built-in's name wins). Sorted by name.
func ListAISkills(dir string) []AISkill {
	byName := map[string]AISkill{}
	for _, n := range skills.Names() {
		files, _ := skills.Files(n)
		byName[n] = AISkill{Name: n, Source: "devkit", files: files}
	}
	if entries, err := os.ReadDir(filepath.Join(dir, AISkillsSource)); err == nil {
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			src := filepath.Join(dir, AISkillsSource, e.Name())
			if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
				continue
			}
			files, err := readSkillDir(src)
			if err != nil {
				continue
			}
			byName[e.Name()] = AISkill{Name: e.Name(), Source: filepath.ToSlash(filepath.Join(AISkillsSource, e.Name())), files: files}
		}
	}
	out := make([]AISkill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// readSkillDir loads a skill directory (regular files, no dot directories).
func readSkillDir(src string) (map[string][]byte, error) {
	files := map[string][]byte{}
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
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = b
		return nil
	})
	return files, err
}

// SyncAISkills copies the built-in skills and the project's (idl/skills)
// into the target directories under dir (creating them), refreshes copies
// whose content differs and removes copies of skills that no longer exist.
// It returns the target paths written and removed.
func SyncAISkills(dir string) (written, removed []string, err error) {
	list := ListAISkills(dir)
	for _, target := range AISkillTargets {
		w, r, err := syncSkillsTo(dir, list, filepath.Join(dir, target))
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
	list := ListAISkills(dir)
	for _, target := range AISkillTargets {
		t := filepath.Join(dir, target)
		if st, err := os.Stat(t); err != nil || !st.IsDir() {
			continue
		}
		w, r, err := syncSkillsTo(dir, list, t)
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

func syncSkillsTo(dir string, list []AISkill, target string) (written, removed []string, err error) {
	keep := map[string]bool{}
	for _, sk := range list {
		keep[sk.Name] = true
		dst := filepath.Join(target, sk.Name)
		changed, err := writeSkill(sk, dst)
		if err != nil {
			return written, removed, fmt.Errorf("%s: %w", sk.Name, err)
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

// writeSkill mirrors the skill's files into dst and writes the marker. It
// reports whether anything changed.
func writeSkill(sk AISkill, dst string) (bool, error) {
	changed := false
	for rel, b := range sk.files {
		out := filepath.Join(dst, filepath.FromSlash(rel))
		if cur, err := os.ReadFile(out); err == nil && bytes.Equal(cur, b) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return changed, err
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return changed, err
		}
		changed = true
	}
	// Files that left the source leave the copy too.
	_ = filepath.WalkDir(dst, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dst, p)
		rel = filepath.ToSlash(rel)
		if rel != skillMarker && sk.files[rel] == nil {
			changed = true
			_ = os.Remove(p)
		}
		return nil
	})
	marker := filepath.Join(dst, skillMarker)
	from := "devkit (built-in skill)"
	if sk.Source != "devkit" {
		from = sk.Source
	}
	text := "copied by `devkit ai` from " + from + "; edit the source, not this copy\n"
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
			if _, err := os.Stat(filepath.Join(p, legacySkillMarker)); err != nil {
				continue
			}
		}
		if err := os.RemoveAll(p); err != nil {
			return removed, err
		}
		removed = append(removed, p)
	}
	return removed, nil
}
