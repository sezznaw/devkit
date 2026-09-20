// Package registry describes the component registry layout and fetches components from it (GitHub or a local checkout).
//
// Registry repository layout (see docs/registry.md):
//
//	registry.json                       index: latest version of every component
//	components/<name>/component.json    metadata for one component
//	components/<name>/files/...         files copied into the project (".tmpl" files are rendered)
//
// Every published component version is an immutable git tag named "<name>/v<version>".
package registry

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

const (
	IndexFile     = "registry.json"
	ComponentFile = "component.json"
	FilesDir      = "files"
	ComponentsDir = "components"
)

type Index struct {
	Schema     int                   `json:"schema"`
	Components map[string]IndexEntry `json:"components"`
}

type IndexEntry struct {
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Var struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type Hooks struct {
	PostInstall []string `json:"post_install,omitempty"`
	PostUpdate  []string `json:"post_update,omitempty"`
}

type Component struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Deps        []string `json:"deps,omitempty"`
	Vars        []Var    `json:"vars,omitempty"`
	Hooks       Hooks    `json:"hooks,omitempty"`
	// Changelog describes what each version changed, newest first or in any
	// order. `devkit update` prints the entries between the installed and the
	// new version, because files the developer owns (config, handlers) are
	// never rewritten and would otherwise hide new settings from them.
	Changelog []ChangeEntry `json:"changelog,omitempty"`
	// Once lists project-relative glob patterns of files that are written on
	// install only: they are never tracked in the manifest, updated or removed.
	// Use it for files the developer owns afterwards (go.mod, config, handlers).
	Once []string `json:"once,omitempty"`
}

// ChangeEntry is the changelog of one component version.
type ChangeEntry struct {
	Version string `json:"version"`
	// Changes says what is different, for the record.
	Changes []string `json:"changes,omitempty"`
	// Action lists what the developer may want to do by hand, typically in
	// files devkit does not manage.
	Action []string `json:"action,omitempty"`
}

// ChangesBetween returns the entries with from < version <= to, oldest first.
func (c *Component) ChangesBetween(from, to string) []ChangeEntry {
	var out []ChangeEntry
	for _, e := range c.Changelog {
		if CompareVersions(e.Version, from) > 0 && CompareVersions(e.Version, to) <= 0 {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return CompareVersions(out[i].Version, out[j].Version) < 0 })
	return out
}

// CompareVersions compares dotted numeric versions ("0.10.1" > "0.9"). A
// non-numeric part compares as text; a pre-release suffix is ignored.
func CompareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

// IsOnce reports whether rel matches one of the Once patterns.
func (c *Component) IsOnce(rel string) bool {
	for _, pat := range c.Once {
		if ok, _ := path.Match(pat, rel); ok {
			return true
		}
	}
	return false
}

// TagFor returns the git tag under which a component version is published.
func TagFor(name, version string) string {
	return fmt.Sprintf("%s/v%s", name, version)
}

// ComponentPath returns the in-repo directory of a component.
func ComponentPath(name string) string {
	return ComponentsDir + "/" + name
}
