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
	// Once lists project-relative glob patterns of files that are written on
	// install only: they are never tracked in the manifest, updated or removed.
	// Use it for files the developer owns afterwards (go.mod, config, handlers).
	Once []string `json:"once,omitempty"`
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
