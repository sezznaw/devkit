package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the per-workspace settings file. It lets one machine work on
// several projects (different IDL repositories, module prefixes) while the
// global ~/.devkit/config.yaml keeps only GitHub host and token.
const ConfigFile = "devkit.yaml"

// Config is <workspace>/devkit.yaml.
type Config struct {
	ModulePrefix string `yaml:"module_prefix,omitempty"`
	// IdlRepo is "owner/repo" (or a git URL / local path) of the Thrift IDL repository.
	IdlRepo string `yaml:"idl_repo,omitempty"`
	// CommonRepo is "owner/repo" of the shared library, cloned for reference. Optional.
	CommonRepo string `yaml:"common_repo,omitempty"`
	// GoPrivate marks the organisation's modules as private: ngs then adds
	// <module_prefix org> to GOPRIVATE so `go get` bypasses proxy.golang.org.
	GoPrivate bool `yaml:"go_private,omitempty"`
	// Component overrides the registry component `ngs` scaffolds from.
	Component string `yaml:"component,omitempty"`
	// Vars are default template variables for `ngs` (e.g. NacosAddr).
	Vars map[string]string `yaml:"vars,omitempty"`
}

// Find walks up from dir looking for devkit.yaml and returns the directory
// containing it, or "" if none is found.
func Find(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ConfigFile)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// LoadConfig reads <dir>/devkit.yaml. A missing file yields an empty config.
func LoadConfig(dir string) (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, ConfigFile), err)
	}
	return cfg, nil
}

// SaveConfig writes <dir>/devkit.yaml.
func SaveConfig(dir string, cfg *Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	header := "# devkit workspace settings. Commit this file if the workspace is a repository.\n"
	return os.WriteFile(filepath.Join(dir, ConfigFile), append([]byte(header), data...), 0o644)
}

// WriteTemplate creates <dir>/devkit.yaml as a commented file for the user to
// fill in by hand. Known values (from the global config) are pre-filled.
func WriteTemplate(dir string, known *Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	q := func(s string) string { return fmt.Sprintf("%q", s) }
	content := "# devkit project settings. Fill in the values, then run: devkit ngs <service>\n" +
		"#\n" +
		"# module_prefix  Go module prefix of your services. A service named \"order\" becomes\n" +
		"#                <module_prefix>/order, e.g. github.com/sezznaw/order\n" +
		"# idl_repo       owner/repo of this project's Thrift IDL repository on GitHub\n" +
		"# common_repo    owner/repo of the shared common library (optional, cloned for reading)\n" +
		"\n" +
		"module_prefix: " + q(known.ModulePrefix) + "\n" +
		"idl_repo: " + q(known.IdlRepo) + "\n" +
		"common_repo: " + q(known.CommonRepo) + "\n" +
		"\n" +
		"# Uncomment if these repositories are private (adds the organisation to GOPRIVATE):\n" +
		"# go_private: true\n" +
		"\n" +
		"# Default template variables for new services:\n" +
		"# vars:\n" +
		"#   NacosAddr: 127.0.0.1:8848\n" +
		"#   Port: \"8888\"\n"
	return os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0o644)
}
