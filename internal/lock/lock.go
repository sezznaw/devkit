// Package lock makes the files a colleague must not edit read-only, so the
// IDE says so the moment they try: the framework's files (what devkit wrote
// and replaces on update, listed in .devkit/manifest.json) and generated
// code (kitex_gen, hertz_gen, files with a "Code generated ... DO NOT EDIT"
// header, the OpenAPI document, idl.lock). It is a reminder, not a wall:
// the lint rules, the pre-push hook and CI still decide. Only files change
// mode, never directories, so git and the generators can still add files;
// make gen unlocks before generating and locks again afterwards.
package lock

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sezznaw/devkit/internal/manifest"
)

// Generated directories: everything inside is locked.
var genDirs = []string{"kitex_gen", "hertz_gen", ".idl"}

// Generated single files.
// Generated single files. .devkit/manifest.json is devkit's own record of
// what it installed; a hand edit there blinds the framework-file lint rule.
var genFiles = []string{"idl.lock", ".hz", ".devkit/manifest.json"}

// Never looked into.
var skipDirs = map[string]bool{".git": true, "bin": true, ".go": true, "node_modules": true, "vendor": true, ".devkit": true, ".idea": true, ".vscode": true}

// Files lists the project-relative paths devkit locks in root.
func Files(root string) ([]string, error) {
	set := map[string]struct{}{}
	add := func(rel string) {
		rel = filepath.ToSlash(rel)
		if st, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err == nil && st.Mode().IsRegular() {
			set[rel] = struct{}{}
		}
	}
	if m, err := manifest.Load(root); err == nil {
		for _, c := range m.Components {
			for rel := range c.Files {
				add(rel)
			}
		}
	}
	for _, f := range genFiles {
		add(f)
	}
	if docs, err := filepath.Glob(filepath.Join(root, "cmd", "*", "openapi.yaml")); err == nil {
		for _, d := range docs {
			if rel, err := filepath.Rel(root, d); err == nil {
				add(rel)
			}
		}
	}
	for _, d := range genDirs {
		dir := filepath.Join(root, d)
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err == nil && e.Type().IsRegular() {
				if rel, err := filepath.Rel(root, p); err == nil {
					add(rel)
				}
			}
			return nil
		})
	}
	// Generated Go files anywhere else (hz's router/*.go, for one), found by
	// the header every Go generator writes. middleware.go says "Code
	// generated" without "DO NOT EDIT": it is the service's, and stays writable.
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			name := e.Name()
			if p != root && (skipDirs[name] || contains(genDirs, name)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".go") {
			return nil
		}
		if generatedHeader(p) {
			if rel, err := filepath.Rel(root, p); err == nil {
				add(rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set))
	for rel := range set {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out, nil
}

// Lock removes write permission from every file Files lists; it returns how
// many files changed mode.
func Lock(root string) (int, error) {
	return apply(root, func(m os.FileMode) os.FileMode { return m &^ 0o222 })
}

// Unlock gives the owner write permission back; it returns how many files
// changed mode.
func Unlock(root string) (int, error) {
	return apply(root, func(m os.FileMode) os.FileMode { return m | 0o200 })
}

func apply(root string, f func(os.FileMode) os.FileMode) (int, error) {
	files, err := Files(root)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, rel := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		mode := st.Mode().Perm()
		want := f(mode)
		if want == mode {
			continue
		}
		if err := os.Chmod(p, want); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func generatedHeader(path string) bool {
	fh, err := os.Open(path)
	if err != nil {
		return false
	}
	defer fh.Close()
	head := make([]byte, 1024)
	n, _ := io.ReadFull(fh, head)
	head = head[:n]
	return bytes.Contains(head, []byte("Code generated")) && bytes.Contains(head, []byte("DO NOT EDIT"))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// CommonDir is the project's checkout of the common library (<project>/kit-common).
const CommonDir = "kit-common"

// LockCommon makes every file of the project's kit-common/ checkout
// read-only (git still updates it: it replaces files rather than writing
// into them). It returns how many files changed mode; 0 when there is no
// checkout.
func LockCommon(projectDir string) (int, error) {
	return applyDir(filepath.Join(projectDir, CommonDir), func(m os.FileMode) os.FileMode { return m &^ 0o222 })
}

// UnlockCommon gives the owner write permission back on the checkout.
func UnlockCommon(projectDir string) (int, error) {
	return applyDir(filepath.Join(projectDir, CommonDir), func(m os.FileMode) os.FileMode { return m | 0o200 })
}

func applyDir(dir string, f func(os.FileMode) os.FileMode) (int, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return 0, nil
	}
	n := 0
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if e.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() {
			return nil
		}
		st, err := os.Lstat(p)
		if err != nil {
			return nil
		}
		mode := st.Mode().Perm()
		if want := f(mode); want != mode {
			if err := os.Chmod(p, want); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
