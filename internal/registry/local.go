package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Local reads components straight from a registry checkout on disk.
// It ignores tags: whatever is in the working tree is what you get, which is
// exactly what a component author wants while iterating.
type Local struct {
	dir string
}

func NewLocal(dir string) *Local { return &Local{dir: dir} }

func (l *Local) Index(ctx context.Context) (*Index, error) {
	data, err := os.ReadFile(filepath.Join(l.dir, IndexFile))
	if err != nil {
		return nil, fmt.Errorf("local registry: %w", err)
	}
	return parseIndex(data)
}

func (l *Local) Fetch(ctx context.Context, name, version string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir := filepath.Join(l.dir, ComponentsDir, name)
	c, err := LoadComponent(dir)
	if err != nil {
		return "", fmt.Errorf("local registry: component %q: %w", name, err)
	}
	if version != "" && c.Version != version {
		return "", fmt.Errorf("local registry: component %q is at version %s in the working tree, not %s", name, c.Version, version)
	}
	return dir, nil
}
