package registry

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
		// An older version is needed (devkit looks at the version a service was
		// created with). A checkout that is a git repository still has it, under
		// the same tag the published registry uses.
		if old, gerr := l.fromTag(ctx, name, version); gerr == nil {
			return old, nil
		}
		return "", fmt.Errorf("local registry: component %q is at version %s in the working tree, not %s (and no git tag %s to take it from)", name, c.Version, version, TagFor(name, version))
	}
	return dir, nil
}

// fromTag extracts components/<name> as of tag <name>/v<version> into a
// temporary directory.
func (l *Local) fromTag(ctx context.Context, name, version string) (string, error) {
	tag := TagFor(name, version)
	archive := exec.CommandContext(ctx, "git", "-C", l.dir, "archive", "--format=tar", tag, ComponentPath(name))
	data, err := archive.Output()
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "devkit-local-"+name+"-*")
	if err != nil {
		return "", err
	}
	untar := exec.CommandContext(ctx, "tar", "-xf", "-", "-C", tmp)
	untar.Stdin = bytes.NewReader(data)
	if out, err := untar.CombinedOutput(); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("%w: %s", err, out)
	}
	dir := filepath.Join(tmp, filepath.FromSlash(ComponentPath(name)))
	if c, err := LoadComponent(dir); err != nil || c.Version != version {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("tag %s does not contain %s@%s", tag, name, version)
	}
	return dir, nil
}
