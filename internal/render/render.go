// Package render copies a component's files/ tree into a project.
//
// Rules:
//   - Paths may contain Go template expressions, e.g. files/internal/{{.Service}}/server.go.
//   - Files ending in ".tmpl" are rendered with text/template and the suffix is dropped.
//   - Every other file is copied verbatim.
package render

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/sezznaw/devkit/internal/manifest"
)

const tmplSuffix = ".tmpl"

// Plan is the list of files a component would write, computed before touching disk.
type Plan struct {
	Files []PlannedFile
}

type PlannedFile struct {
	Rel     string // project-relative, slash separated
	Content []byte
	Mode    fs.FileMode
}

// Build renders every file under srcDir/files with vars and returns the plan.
func Build(srcDir string, vars map[string]string) (*Plan, error) {
	return BuildFrom(filepath.Join(srcDir, "files"), vars)
}

// BuildFrom renders an arbitrary directory tree. Components may ship extra
// trees next to files/ (for example idl/) that devkit writes elsewhere than
// the project.
func BuildFrom(filesDir string, vars map[string]string) (*Plan, error) {
	if _, err := os.Stat(filesDir); err != nil {
		return nil, err
	}
	plan := &Plan{}
	err := filepath.WalkDir(filesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relSrc, err := filepath.Rel(filesDir, path)
		if err != nil {
			return err
		}
		relSrc = filepath.ToSlash(relSrc)

		rel, err := renderString("path:"+relSrc, relSrc, vars)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, tmplSuffix) {
			rel = strings.TrimSuffix(rel, tmplSuffix)
			rendered, err := renderString(relSrc, string(content), vars)
			if err != nil {
				return err
			}
			content = []byte(rendered)
		}
		if strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
			return fmt.Errorf("rendered path escapes project: %s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		plan.Files = append(plan.Files, PlannedFile{Rel: rel, Content: content, Mode: info.Mode().Perm()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Rel < plan.Files[j].Rel })
	return plan, nil
}

// Apply writes the plan into root and returns the manifest file map (rel -> hash).
func (p *Plan) Apply(root string) (map[string]string, error) {
	hashes := make(map[string]string, len(p.Files))
	for _, f := range p.Files {
		abs := filepath.Join(root, filepath.FromSlash(f.Rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(abs, f.Content, f.Mode|0o600); err != nil {
			return nil, err
		}
		h, err := manifest.HashFile(abs)
		if err != nil {
			return nil, err
		}
		hashes[f.Rel] = h
	}
	return hashes, nil
}

// funcs are helpers available inside templates, mostly for deriving Go
// identifiers from a user-supplied name such as "order-item".
var funcs = template.FuncMap{
	"upper":  strings.ToUpper,
	"lower":  strings.ToLower,
	"pascal": Pascal,
	"title":  Pascal,
	"camel": func(s string) string {
		p := Pascal(s)
		if p == "" {
			return ""
		}
		return strings.ToLower(p[:1]) + p[1:]
	},
	"snake": func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "-", "_") },
	"kebab": func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "_", "-") },
}

// Pascal converts "order-item", "order_item" or "order item" to "OrderItem".
func Pascal(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		switch {
		case r == '-' || r == '_' || r == ' ' || r == '.':
			up = true
		case up:
			b.WriteString(strings.ToUpper(string(r)))
			up = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func renderString(name, text string, vars map[string]string) (string, error) {
	t, err := template.New(name).Funcs(funcs).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("template %s: %w", name, err)
	}
	return buf.String(), nil
}

// Strings renders each string (e.g. hook commands) as a template.
func Strings(in []string, vars map[string]string) ([]string, error) {
	out := make([]string, 0, len(in))
	for i, s := range in {
		r, err := renderString(fmt.Sprintf("string[%d]", i), s, vars)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
