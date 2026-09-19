// Package config loads the user-level configuration from ~/.devkit/config.yaml.
//
// Every field can be overridden by an environment variable, which is convenient
// on CI machines where writing a config file is inconvenient:
//
//	DEVKIT_GITHUB_HOST      github.com (default) or a GitHub Enterprise host
//	DEVKIT_GITHUB_TOKEN     token with repo read access; optional for public repositories
//	DEVKIT_REGISTRY_REPO    "owner/repo" of the component registry
//	DEVKIT_REGISTRY_DIR     a local checkout of the registry; when set, GitHub is not used
//	DEVKIT_REPO             "owner/repo" publishing devkit releases (for self-update)
//	DEVKIT_WORKSPACE_DIR    fallback workspace directory for `devkit ngs`
//	DEVKIT_MODULE_PREFIX    fallback module prefix, e.g. github.com/sezznaw
//	DEVKIT_IDL_REPO         fallback "owner/repo" (or local path) of the Thrift IDL repository
//	DEVKIT_COMMON_REPO      fallback "owner/repo" of the shared common library
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DirName  = ".devkit"
	FileName = "config.yaml"
)

type Config struct {
	GitHubHost   string `yaml:"github_host,omitempty"`
	GitHubToken  string `yaml:"github_token,omitempty"`
	RegistryRepo string `yaml:"registry_repo"`
	// RegistryDir points at a local registry checkout for development. Optional.
	RegistryDir string `yaml:"registry_dir,omitempty"`
	// DevkitRepo overrides the repository devkit self-updates from. Optional.
	DevkitRepo string `yaml:"devkit_repo,omitempty"`

	// Fallback workspace settings used by `devkit ngs` when the workspace's
	// devkit.yaml does not set them.
	WorkspaceDir string `yaml:"workspace_dir,omitempty"`
	ModulePrefix string `yaml:"module_prefix,omitempty"`
	IdlRepo      string `yaml:"idl_repo,omitempty"`
	CommonRepo   string `yaml:"common_repo,omitempty"`
}

// Keys lists the settable config keys in display order.
var Keys = []string{"github_host", "github_token", "registry_repo", "registry_dir", "devkit_repo", "workspace_dir", "module_prefix", "idl_repo", "common_repo"}

// Dir returns ~/.devkit, creating it if needed.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// CacheDir returns ~/.devkit/cache, creating it if needed.
func CacheDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	cache := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	return cache, nil
}

// Path returns the full path of the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Load reads the config file (missing file is not an error) and applies env overrides.
func Load() (*Config, error) {
	cfg := &Config{}
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read config %s: %w", p, err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", p, err)
		}
	}
	env := map[string]*string{
		"DEVKIT_GITHUB_HOST":   &cfg.GitHubHost,
		"DEVKIT_GITHUB_TOKEN":  &cfg.GitHubToken,
		"DEVKIT_REGISTRY_REPO": &cfg.RegistryRepo,
		"DEVKIT_REGISTRY_DIR":  &cfg.RegistryDir,
		"DEVKIT_REPO":          &cfg.DevkitRepo,
		"DEVKIT_WORKSPACE_DIR": &cfg.WorkspaceDir,
		"DEVKIT_MODULE_PREFIX": &cfg.ModulePrefix,
		"DEVKIT_IDL_REPO":      &cfg.IdlRepo,
		"DEVKIT_COMMON_REPO":   &cfg.CommonRepo,
	}
	for name, field := range env {
		if v := os.Getenv(name); v != "" {
			*field = v
		}
	}
	return cfg, nil
}

// Save writes the config file with owner-only permissions (it may contain a token).
func Save(cfg *Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// Set assigns a key by its config-file name.
func (c *Config) Set(key, val string) error {
	switch key {
	case "github_host":
		c.GitHubHost = val
	case "github_token":
		c.GitHubToken = val
	case "registry_repo":
		c.RegistryRepo = val
	case "registry_dir":
		c.RegistryDir = val
	case "devkit_repo":
		c.DevkitRepo = val
	case "workspace_dir":
		c.WorkspaceDir = val
	case "module_prefix":
		c.ModulePrefix = val
	case "idl_repo":
		c.IdlRepo = val
	case "common_repo":
		c.CommonRepo = val
	default:
		return fmt.Errorf("unknown key %q (valid: %v)", key, Keys)
	}
	return nil
}

// Get returns a key's value by its config-file name.
func (c *Config) Get(key string) string {
	switch key {
	case "github_host":
		return c.GitHubHost
	case "github_token":
		return c.GitHubToken
	case "registry_repo":
		return c.RegistryRepo
	case "registry_dir":
		return c.RegistryDir
	case "devkit_repo":
		return c.DevkitRepo
	case "workspace_dir":
		return c.WorkspaceDir
	case "module_prefix":
		return c.ModulePrefix
	case "idl_repo":
		return c.IdlRepo
	case "common_repo":
		return c.CommonRepo
	}
	return ""
}

// Validate makes sure the fields needed to reach the registry on GitHub are present.
func (c *Config) Validate() error {
	if c.RegistryRepo == "" {
		return errors.New("config incomplete: registry_repo is not set; run `devkit config set registry_repo owner/devkit-registry`")
	}
	return nil
}
