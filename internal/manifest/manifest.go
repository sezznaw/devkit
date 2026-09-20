// Package manifest tracks which components are installed in a project.
//
// The manifest lives at <project>/.devkit/manifest.json and records, for every
// component, the installed version, the variables it was rendered with, and a
// hash of every file it wrote. The hashes let `devkit update` tell whether the
// user modified a file since installation.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	DirName  = ".devkit"
	FileName = "manifest.json"
	Schema   = 1
)

type Manifest struct {
	Schema     int                   `json:"schema"`
	Components map[string]*Installed `json:"components"`

	path string
}

type Installed struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
	// Vars are the values the files were last rendered with.
	Vars map[string]string `json:"vars,omitempty"`
	Deps []string          `json:"deps,omitempty"`
	// Files maps project-relative path (slash separated) to "sha256:<hex>".
	Files map[string]string `json:"files"`
}

// FilePath returns the manifest location for a project root.
func FilePath(root string) string {
	return filepath.Join(root, DirName, FileName)
}

// Exists reports whether root has a manifest.
func Exists(root string) bool {
	_, err := os.Stat(FilePath(root))
	return err == nil
}

// New creates an empty manifest for root (not yet written).
func New(root string) *Manifest {
	return &Manifest{Schema: Schema, Components: map[string]*Installed{}, path: FilePath(root)}
}

// Load reads the manifest of root. A missing manifest is an error; use Exists/New.
func Load(root string) (*Manifest, error) {
	p := FilePath(root)
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s is not a service created by devkit (no %s); create one with `devkit ngs <service>`", root, filepath.Join(DirName, FileName))
		}
		return nil, err
	}
	m := &Manifest{path: p}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if m.Schema > Schema {
		return nil, fmt.Errorf("%s uses schema %d, this devkit only understands up to %d; please upgrade devkit", p, m.Schema, Schema)
	}
	if m.Components == nil {
		m.Components = map[string]*Installed{}
	}
	return m, nil
}

// Save writes the manifest with stable key ordering.
func (m *Manifest) Save() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, append(data, '\n'), 0o644)
}

// Names returns installed component names, sorted.
func (m *Manifest) Names() []string {
	names := make([]string, 0, len(m.Components))
	for n := range m.Components {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Owner returns which installed component wrote the given project-relative path, if any.
func (m *Manifest) Owner(rel string) (string, bool) {
	for name, c := range m.Components {
		if _, ok := c.Files[rel]; ok {
			return name, true
		}
	}
	return "", false
}

// HashBytes computes the "sha256:<hex>" digest of in-memory content.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Dependents returns installed components that list name as a dependency.
func (m *Manifest) Dependents(name string) []string {
	var out []string
	for other, c := range m.Components {
		for _, d := range c.Deps {
			if d == name {
				out = append(out, other)
			}
		}
	}
	sort.Strings(out)
	return out
}

// HashFile computes the "sha256:<hex>" digest of a file.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// FileState describes how an installed file compares to the manifest.
type FileState int

const (
	Unchanged FileState = iota
	Modified
	Missing
)

func (s FileState) String() string {
	switch s {
	case Modified:
		return "modified"
	case Missing:
		return "missing"
	default:
		return "unchanged"
	}
}

// CheckFiles compares every recorded file of a component with what is on disk.
func (m *Manifest) CheckFiles(root, name string) (map[string]FileState, error) {
	c, ok := m.Components[name]
	if !ok {
		return nil, fmt.Errorf("component %q is not installed", name)
	}
	states := map[string]FileState{}
	for rel, want := range c.Files {
		got, err := HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		switch {
		case errors.Is(err, os.ErrNotExist):
			states[rel] = Missing
		case err != nil:
			return nil, err
		case got != want:
			states[rel] = Modified
		default:
			states[rel] = Unchanged
		}
	}
	return states, nil
}
