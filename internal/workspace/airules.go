package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AIRulesSource is the one file with the project's development flow for AI
// tools, kept in the IDL repository (every developer has that checkout).
const AIRulesSource = "idl/AGENTS.md"

// aiPointer is the text of every pointer file: it only sends the tool to
// the source. Content lives in one place; these files never carry rules.
const aiPointer = `先完整阅读 idl/AGENTS.md，再开始任何任务。它是本项目唯一的开发流程说明，所有 AI 工具共用。
Read idl/AGENTS.md in full before starting any task. It is the project's only development guide, shared by every AI tool.
`

// AIPointerFiles are the files the mainstream AI coding tools read
// automatically from the project directory, each pointing at AIRulesSource.
// Written by devkit (ngs / nas / update) and safe to overwrite: they are
// pointers, not content. A tool missing here reads the source when told to.
var AIPointerFiles = []AIPointer{
	{"agents", "AGENTS.md", "Codex, Cursor, GitHub Copilot, Gemini CLI, Zed, Amp, Jules, OpenCode, Warp (any tool that reads AGENTS.md)", aiPointer, nil},
	{"claude", "CLAUDE.md", "Claude Code", "@AGENTS.md\n", []string{"agents"}},
	{"gemini", "GEMINI.md", "Gemini CLI", aiPointer, nil},
	{"qwen", "QWEN.md", "Qwen Code", aiPointer, nil},
	{"iflow", "IFLOW.md", "iFlow CLI", aiPointer, nil},
	{"warp", "WARP.md", "Warp", aiPointer, nil},
	{"copilot", ".github/copilot-instructions.md", "GitHub Copilot (VS Code, JetBrains)", aiPointer, nil},
	{"cursor", ".cursor/rules/project.mdc", "Cursor", "---\ndescription: 项目开发流程 / project development flow\nalwaysApply: true\n---\n" + aiPointer, nil},
	{"windsurf", ".windsurf/rules/project.md", "Windsurf", "---\ntrigger: always_on\n---\n" + aiPointer, nil},
	{"junie", ".junie/guidelines.md", "JetBrains Junie", aiPointer, nil},
	{"kiro", ".kiro/steering/project.md", "Kiro", "---\ninclusion: always\n---\n" + aiPointer, nil},
	{"trae", ".trae/rules/project_rules.md", "Trae", aiPointer, nil},
	{"cline", ".clinerules/project.md", "Cline", aiPointer, nil},
	{"roo", ".roo/rules/project.md", "Roo Code", aiPointer, nil},
	{"augment", ".augment/rules/project.md", "Augment", "---\ntype: always\n---\n" + aiPointer, nil},
	{"continue", ".continue/rules/project.md", "Continue", "---\nalwaysApply: true\n---\n" + aiPointer, nil},
	{"lingma", ".lingma/rules/project.md", "通义灵码 Lingma", aiPointer, nil},
	{"aider", ".aider.conf.yml", "Aider", "# devkit: Aider reads these files into every chat\nread:\n  - idl/AGENTS.md\n", nil},
}

// AIPointer is one tool's pointer file. Needs lists other keys the file
// depends on (CLAUDE.md imports AGENTS.md).
type AIPointer struct {
	Key     string
	Path    string
	Tools   string
	Content string
	Needs   []string
}

// FindAIPointer looks a tool up by key.
func FindAIPointer(key string) (AIPointer, bool) {
	for _, f := range AIPointerFiles {
		if f.Key == key {
			return f, true
		}
	}
	return AIPointer{}, false
}

// SelectAIPointers resolves keys (plus what they need) in list order;
// "all" means every tool.
func SelectAIPointers(keys []string) ([]AIPointer, error) {
	want := map[string]bool{}
	for _, k := range keys {
		if k == "all" {
			for _, f := range AIPointerFiles {
				want[f.Key] = true
			}
			continue
		}
		f, ok := FindAIPointer(k)
		if !ok {
			return nil, fmt.Errorf("unknown AI tool %q; devkit ai lists the known ones", k)
		}
		want[k] = true
		for _, n := range f.Needs {
			want[n] = true
		}
	}
	var out []AIPointer
	for _, f := range AIPointerFiles {
		if want[f.Key] {
			out = append(out, f)
		}
	}
	return out, nil
}

// WriteAIPointers writes (or rewrites) the given pointer files in dir and
// returns the paths written. A file with the same content is left untouched.
func WriteAIPointers(dir string, files []AIPointer) ([]string, error) {
	var written []string
	for _, f := range files {
		p := filepath.Join(dir, f.Path)
		if cur, err := os.ReadFile(p); err == nil && string(cur) == f.Content {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			return written, err
		}
		written = append(written, f.Path)
	}
	return written, nil
}

// AIToolList is the tools covered, for messages and docs.
func AIToolList() string {
	seen := map[string]bool{}
	var out []string
	for _, f := range AIPointerFiles {
		for _, t := range strings.Split(f.Tools, ", ") {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return strings.Join(out, ", ")
}

// PresentAIPointers says which tools' files exist in dir, and whether each
// is devkit's (same content) or the person's own.
func PresentAIPointers(dir string) map[string]string {
	out := map[string]string{}
	for _, f := range AIPointerFiles {
		cur, err := os.ReadFile(filepath.Join(dir, f.Path))
		if err != nil {
			continue
		}
		if string(cur) == f.Content {
			out[f.Key] = "devkit"
		} else {
			out[f.Key] = "yours"
		}
	}
	return out
}

// CleanAIPointers removes the pointer files devkit wrote (content unchanged;
// a file the person edited is kept) and the directories left empty.
func CleanAIPointers(dir string) ([]string, error) {
	var removed []string
	for _, f := range AIPointerFiles {
		p := filepath.Join(dir, f.Path)
		cur, err := os.ReadFile(p)
		if err != nil || string(cur) != f.Content {
			continue
		}
		if err := os.Remove(p); err != nil {
			return removed, err
		}
		removed = append(removed, f.Path)
		for d := filepath.Dir(p); d != dir; d = filepath.Dir(d) {
			if os.Remove(d) != nil { // not empty, or gone
				break
			}
		}
	}
	return removed, nil
}
