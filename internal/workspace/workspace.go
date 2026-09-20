// Package workspace manages the developer workspace used by `devkit ngs`:
// a directory holding the shared idl/ and common/ checkouts next to the
// services being developed.
package workspace

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoURL turns a config value into something git can clone.
//   - "owner/repo"               -> https://<host>/owner/repo.git (host defaults to github.com)
//   - "https://..." / "git@..."  -> unchanged
//   - "/abs/path" / "./rel"      -> unchanged (local repository, used in tests)
func RepoURL(host, repo string) string {
	host = strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(host, "/"), "https://"), "http://")
	if host == "" {
		host = "github.com"
	}
	switch {
	case strings.Contains(repo, "://"), strings.HasPrefix(repo, "git@"):
		return repo
	case strings.HasPrefix(repo, "/"), strings.HasPrefix(repo, "."):
		return repo
	default:
		return fmt.Sprintf("https://%s/%s.git", host, strings.Trim(repo, "/"))
	}
}

// IsLocal reports whether url points at a local path rather than a remote.
func IsLocal(url string) bool {
	return strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".")
}

// EnsureRepo clones url into dir if dir does not exist, otherwise fast-forwards it.
// A failed pull is reported through log but is not fatal: the checkout is still usable.
//
// token is only ever sent to tokenHost (the GitHub host devkit is configured
// for). Repositories on any other server, such as a company GitLab, are cloned
// with the user's own git credentials and never see the token.
func EnsureRepo(ctx context.Context, dir, url, token, tokenHost string, log func(string, ...any)) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		if !hasUpstream(ctx, dir) {
			log("using local %s (no remote configured yet)", dir)
			return nil
		}
		log("updating %s", dir)
		if out, err := git(ctx, dir, "", "pull", "--ff-only", "--quiet"); err != nil {
			log("  warning: git pull failed (%v): %s", err, strings.TrimSpace(out))
		}
		return nil
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s exists but is not a git checkout", dir)
	}
	log("cloning %s -> %s", url, dir)
	out, err := git(ctx, filepath.Dir(dir), "", "clone", "--quiet", url, dir)
	if err == nil {
		return nil
	}
	if token == "" || !TokenAllowedFor(url, tokenHost) {
		return fmt.Errorf("git clone %s: %w\n%s", url, err, strings.TrimSpace(out))
	}
	// Retry with the devkit token; the header is passed on the command line
	// only and never written to .git/config. GitHub accepts a token as the
	// password of HTTP basic auth with user x-access-token.
	log("  retrying with the configured GitHub token")
	auth := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	out, err = git(ctx, filepath.Dir(dir), auth, "clone", "--quiet", url, dir)
	if err != nil {
		return fmt.Errorf("git clone %s: %w\n%s", url, err, strings.TrimSpace(out))
	}
	return nil
}

// TokenAllowedFor reports whether the GitHub token may be sent to url: only
// over https and only to tokenHost itself.
func TokenAllowedFor(url, tokenHost string) bool {
	tokenHost = strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(tokenHost, "/"), "https://"), "http://")
	if tokenHost == "" {
		tokenHost = "github.com"
	}
	return strings.HasPrefix(url, "https://"+tokenHost+"/")
}

// EnsureLocalRepo makes dir a git repository without any remote. It is what
// ngs uses for idl/ when the project has no idl_repo yet: work starts locally
// and the directory is pushed to a git server later.
func EnsureLocalRepo(ctx context.Context, dir string, log func(string, ...any)) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		log("using local %s", dir)
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if out, err := git(ctx, dir, "", "init", "--quiet", "-b", "main"); err != nil {
		return fmt.Errorf("git init %s: %w\n%s", dir, err, strings.TrimSpace(out))
	}
	log("created local git repository %s (set idl_repo in devkit.yaml and push it when you have a server)", dir)
	return nil
}

// hasUpstream reports whether the checkout has any remote to pull from.
func hasUpstream(ctx context.Context, dir string) bool {
	out, err := git(ctx, dir, "", "remote")
	return err == nil && strings.TrimSpace(out) != ""
}

// DefaultModulePrefix derives a module prefix from the project directory name
// for projects that have not chosen one: ~/Work/indie-game -> "indie-game".
// Using the directory rather than the bare service name keeps a service called
// "log" or "time" from colliding with the Go standard library.
func DefaultModulePrefix(projectDir string) string {
	base := filepath.Base(projectDir)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune('-')
		}
	}
	p := strings.Trim(b.String(), "-._")
	if p == "" {
		return "app"
	}
	return p
}

func git(ctx context.Context, dir, extraHeader string, args ...string) (string, error) {
	full := []string{}
	if extraHeader != "" {
		full = append(full, "-c", "http.extraHeader="+extraHeader)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// InitRepo runs git init + an initial commit in dir. Errors are returned but
// callers usually treat them as warnings.
func InitRepo(ctx context.Context, dir, message string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	commit := []string{"commit", "--quiet", "-m", message}
	if !hasIdentity(ctx, dir) {
		// A fresh machine (or a CI runner) has no user.name / user.email yet and
		// git refuses to commit. Use a neutral identity for this one scaffold
		// commit instead of failing; the developer's later commits use their own.
		commit = append([]string{"-c", "user.name=devkit", "-c", "user.email=devkit@users.noreply.github.com"}, commit...)
	}
	steps := [][]string{
		{"init", "--quiet", "-b", "main"},
		{"add", "-A"},
		commit,
	}
	for _, s := range steps {
		if out, err := git(ctx, dir, "", s...); err != nil {
			return fmt.Errorf("git %s: %w\n%s", strings.Join(s, " "), err, strings.TrimSpace(out))
		}
	}
	return nil
}

// hasIdentity reports whether git knows who the committer is.
func hasIdentity(ctx context.Context, dir string) bool {
	name, _ := git(ctx, dir, "", "config", "user.name")
	email, _ := git(ctx, dir, "", "config", "user.email")
	return strings.TrimSpace(name) != "" && strings.TrimSpace(email) != ""
}

// EnsureGoPrivate makes sure `go env GOPRIVATE` covers pattern (e.g.
// "github.com/sezznaw") so that `go get` of private modules bypasses the
// public proxy and checksum database. Only needed for private repositories.
// Returns true if it changed anything.
func EnsureGoPrivate(ctx context.Context, pattern string) (bool, error) {
	host := pattern
	if host == "" {
		return false, nil
	}
	out, err := exec.CommandContext(ctx, "go", "env", "GOPRIVATE").Output()
	if err != nil {
		return false, errors.New("go toolchain not found in PATH")
	}
	current := strings.TrimSpace(string(out))
	for _, p := range strings.Split(current, ",") {
		p = strings.TrimSpace(p)
		if p == host {
			return false, nil
		}
		if suffix, ok := strings.CutPrefix(p, "*."); ok && strings.HasSuffix(host, "."+suffix) {
			return false, nil
		}
	}
	next := host
	if current != "" {
		next = current + "," + host
	}
	if err := exec.CommandContext(ctx, "go", "env", "-w", "GOPRIVATE="+next).Run(); err != nil {
		return false, fmt.Errorf("go env -w GOPRIVATE: %w", err)
	}
	return true, nil
}

// OrgPattern reduces a module prefix to the GOPRIVATE pattern for its
// organisation: "github.com/sezznaw/x" -> "github.com/sezznaw". A bare host
// is returned unchanged.
func OrgPattern(modulePrefix string) string {
	parts := strings.SplitN(strings.Trim(modulePrefix, "/"), "/", 3)
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}
