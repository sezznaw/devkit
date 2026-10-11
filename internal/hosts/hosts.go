// Package hosts keeps the GitLab servers a developer logged in to (devkit
// login) and the projects pulled from them (devkit pull), in
// ~/.devkit/hosts.yaml (mode 600). The token is also written to ~/.netrc,
// which git (clone, push) and go (private modules) read, so one login
// serves all three.
package hosts

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sezznaw/devkit/internal/config"
)

const File = "hosts.yaml"

type Host struct {
	Token    string `yaml:"token"`
	Username string `yaml:"username,omitempty"`
}

type Project struct {
	Host  string `yaml:"host"`  // https://gitlab.example.com
	Group string `yaml:"group"` // sportsbook
	Dir   string `yaml:"dir"`   // where it was pulled last
}

type Store struct {
	Hosts    map[string]Host    `yaml:"hosts"`    // keyed by https://host
	Projects map[string]Project `yaml:"projects"` // keyed by project name
}

func path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, File), nil
}

func Load() (*Store, error) {
	s := &Store{Hosts: map[string]Host{}, Projects: map[string]Project{}}
	p, err := path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if s.Hosts == nil {
		s.Hosts = map[string]Host{}
	}
	if s.Projects == nil {
		s.Projects = map[string]Project{}
	}
	return s, nil
}

func (s *Store) Save() error {
	p, err := path()
	if err != nil {
		return err
	}
	b, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// Normalize turns "gitlab.example.com", "https://gitlab.example.com/" or
// "https://gitlab.example.com/sportsbook" into "https://gitlab.example.com".
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("not a GitLab address: %q", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

// Hostname is the bare host of a normalized address.
func Hostname(host string) string {
	u, _ := url.Parse(host)
	return u.Host
}

// WriteNetrc puts (or replaces) the machine entry for host in ~/.netrc, the
// file git and go read credentials from. The login name GitLab expects for a
// personal access token is "oauth2".
func WriteNetrc(hostname, token string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(home, ".netrc")
	var lines []string
	if f, err := os.Open(p); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		f.Close()
	}
	var kept []string
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) >= 2 && fields[0] == "machine" && fields[1] == hostname {
			continue // replaced below
		}
		kept = append(kept, l)
	}
	kept = append(kept, fmt.Sprintf("machine %s login oauth2 password %s", hostname, token))
	return p, os.WriteFile(p, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
}
