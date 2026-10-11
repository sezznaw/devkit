// Package skills holds the skills devkit ships: the step-by-step procedure
// of each recurring task on a devkit project (add an RPC method, add a
// gateway endpoint, add a gateway domain, finish and hand over), in the
// Agent Skills format (SKILL.md with name/description frontmatter). They
// describe the framework's way of doing things, so they live with the
// framework and are versioned with it; a project adds its own under
// idl/skills/ and points at its samples from idl/AGENTS.md.
package skills

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

//go:embed builtin
var builtin embed.FS

// Names lists the built-in skills, sorted.
func Names() []string {
	entries, err := fs.ReadDir(builtin, "builtin")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Files returns the files of one built-in skill, path relative to the
// skill directory → content. ok is false for an unknown name.
func Files(name string) (files map[string][]byte, ok bool) {
	root := "builtin/" + name
	if _, err := fs.Stat(builtin, root); err != nil {
		return nil, false
	}
	files = map[string][]byte{}
	_ = fs.WalkDir(builtin, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(builtin, p)
		if err != nil {
			return nil
		}
		files[strings.TrimPrefix(p, root+"/")] = b
		return nil
	})
	return files, true
}

// Description returns the description line of a built-in skill's
// frontmatter, for listings.
func Description(name string) string {
	files, ok := Files(name)
	if !ok {
		return ""
	}
	for _, line := range strings.Split(string(files["SKILL.md"]), "\n") {
		if strings.HasPrefix(line, "description:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return ""
}
