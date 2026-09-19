package registry

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sezznaw/devkit/internal/github"
)

// GitHub fetches components from a registry repository on GitHub and caches
// every version under <cacheDir>/<name>/<version>. Tags are immutable, so a
// cached version never needs to be re-downloaded.
type GitHub struct {
	client   *github.Client
	repo     string // owner/repo
	cacheDir string
	indexRef string
}

func NewGitHub(host, token, repo, cacheDir string) *GitHub {
	return &GitHub{
		client:   github.New(host, token),
		repo:     repo,
		cacheDir: cacheDir,
		indexRef: "main",
	}
}

func (g *GitHub) Index(ctx context.Context) (*Index, error) {
	data, err := g.client.RawFile(ctx, g.repo, IndexFile, g.indexRef)
	if err != nil {
		return nil, err
	}
	return parseIndex(data)
}

func (g *GitHub) Fetch(ctx context.Context, name, version string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	if version == "" {
		return "", errors.New("github registry: version is required")
	}
	dest := filepath.Join(g.cacheDir, name, version)
	if _, err := os.Stat(filepath.Join(dest, ComponentFile)); err == nil {
		return dest, nil
	}

	tag := TagFor(name, version)
	body, err := g.client.Tarball(ctx, g.repo, tag)
	if err != nil {
		return "", err
	}
	defer body.Close()

	tmp, err := os.MkdirTemp(g.cacheDir, name+"-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	if err := extractComponent(body, tmp, ComponentPath(name)); err != nil {
		return "", fmt.Errorf("extract %s@%s: %w", name, version, err)
	}
	c, err := LoadComponent(tmp)
	if err != nil {
		return "", fmt.Errorf("tag %s: %w", tag, err)
	}
	if c.Name != name || c.Version != version {
		return "", fmt.Errorf("tag %s contains component %s@%s, expected %s@%s", tag, c.Name, c.Version, name, version)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// extractComponent unpacks a GitHub tarball. Entries look like
// "<owner>-<repo>-<sha>/components/<name>/component.json"; the first path
// element and the component prefix are stripped so that dest ends up with
// component.json and files/. Everything outside the component is skipped.
func extractComponent(r io.Reader, dest, prefix string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		parts := strings.SplitN(filepath.ToSlash(hdr.Name), "/", 2)
		if len(parts) < 2 {
			continue
		}
		rel := parts[1]
		if !strings.HasPrefix(rel, prefix+"/") {
			continue
		}
		rel = strings.TrimPrefix(rel, prefix+"/")
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry escapes destination: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
			found = true
		}
	}
	if !found {
		return fmt.Errorf("archive contains no files under %s", prefix)
	}
	return nil
}
