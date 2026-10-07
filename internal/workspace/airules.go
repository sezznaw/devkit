package workspace

import (
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
var AIPointerFiles = []struct {
	Path    string
	Tools   string
	Content string
}{
	{"AGENTS.md", "Codex, Cursor, GitHub Copilot, Gemini CLI, Zed, Amp, Jules, OpenCode, Warp", aiPointer},
	{"CLAUDE.md", "Claude Code", "@AGENTS.md\n"},
	{"GEMINI.md", "Gemini CLI", aiPointer},
	{"QWEN.md", "Qwen Code", aiPointer},
	{"IFLOW.md", "iFlow CLI", aiPointer},
	{"WARP.md", "Warp", aiPointer},
	{".github/copilot-instructions.md", "GitHub Copilot (VS Code, JetBrains)", aiPointer},
	{".cursor/rules/project.mdc", "Cursor", "---\ndescription: 项目开发流程 / project development flow\nalwaysApply: true\n---\n" + aiPointer},
	{".windsurf/rules/project.md", "Windsurf", "---\ntrigger: always_on\n---\n" + aiPointer},
	{".junie/guidelines.md", "JetBrains Junie", aiPointer},
	{".kiro/steering/project.md", "Kiro", "---\ninclusion: always\n---\n" + aiPointer},
	{".trae/rules/project_rules.md", "Trae", aiPointer},
	{".clinerules/project.md", "Cline", aiPointer},
	{".roo/rules/project.md", "Roo Code", aiPointer},
	{".augment/rules/project.md", "Augment", "---\ntype: always\n---\n" + aiPointer},
	{".continue/rules/project.md", "Continue", "---\nalwaysApply: true\n---\n" + aiPointer},
	{".lingma/rules/project.md", "通义灵码 Lingma", aiPointer},
	{".aider.conf.yml", "Aider", "# devkit: Aider reads these files into every chat\nread:\n  - idl/AGENTS.md\n"},
}

// WriteAIPointers writes (or rewrites) the pointer files in dir. It returns
// the paths written. A file with the same content is left untouched, so a
// repeated run is quiet.
func WriteAIPointers(dir string) ([]string, error) {
	var written []string
	for _, f := range AIPointerFiles {
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
