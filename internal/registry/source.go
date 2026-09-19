package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/sezznaw/devkit/internal/config"
)

// Source is where components come from: the company GitHub or a local checkout.
type Source interface {
	// Index returns the latest version of every component.
	Index(ctx context.Context) (*Index, error)
	// Fetch makes the given component version available on disk and returns
	// the directory containing component.json and files/.
	Fetch(ctx context.Context, name, version string) (string, error)
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateName rejects component names that could escape directories or break tag names.
func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid component name %q (lowercase letters, digits and dashes only)", name)
	}
	return nil
}

// Open picks a source from the config: a local registry directory when set, else GitHub.
func Open(cfg *config.Config) (Source, error) {
	if cfg.RegistryDir != "" {
		return NewLocal(cfg.RegistryDir), nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cacheDir, err := config.CacheDir()
	if err != nil {
		return nil, err
	}
	return NewGitHub(cfg.GitHubHost, cfg.GitHubToken, cfg.RegistryRepo, cacheDir), nil
}

// LoadComponent parses component.json in dir.
func LoadComponent(dir string) (*Component, error) {
	data, err := os.ReadFile(filepath.Join(dir, ComponentFile))
	if err != nil {
		return nil, err
	}
	var c Component
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, ComponentFile), err)
	}
	if c.Name == "" || c.Version == "" {
		return nil, fmt.Errorf("%s: name and version are required", filepath.Join(dir, ComponentFile))
	}
	return &c, nil
}

func parseIndex(data []byte) (*Index, error) {
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse %s: %w", IndexFile, err)
	}
	if idx.Components == nil {
		idx.Components = map[string]IndexEntry{}
	}
	return &idx, nil
}
