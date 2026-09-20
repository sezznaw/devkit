package workspace

import (
	"net/url"
	"strings"
)

// ParseRepo splits a repository reference into host and project path:
//
//	owner/repo                          -> defaultHost, owner/repo
//	https://host/group/sub/idl.git      -> host, group/sub/idl
//	git@host:group/idl.git              -> host, group/idl
//	ssh://git@host:2222/group/idl.git   -> host, group/idl
//	/local/path, ./rel, ""              -> "", ""
func ParseRepo(ref, defaultHost string) (host, path string) {
	ref = strings.TrimSpace(ref)
	defaultHost = strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(defaultHost, "/"), "https://"), "http://")
	if defaultHost == "" {
		defaultHost = "github.com"
	}
	clean := func(p string) string { return strings.TrimSuffix(strings.Trim(p, "/"), ".git") }
	switch {
	case ref == "", IsLocal(ref):
		return "", ""
	case strings.Contains(ref, "://"):
		u, err := url.Parse(ref)
		if err != nil {
			return "", ""
		}
		return u.Hostname(), clean(u.Path)
	case strings.HasPrefix(ref, "git@") || (strings.Contains(ref, "@") && strings.Contains(ref, ":")):
		rest := ref[strings.Index(ref, "@")+1:]
		h, p, ok := strings.Cut(rest, ":")
		if !ok {
			return "", ""
		}
		return h, clean(p)
	default:
		return defaultHost, clean(ref)
	}
}

// CI kinds understood by the kitex-service template.
const (
	CIGitHub = "github"
	CIGitLab = "gitlab"
	CIBoth   = "both"
)

// DetectCI guesses which CI configuration a new service needs from where its
// code (module path) or, failing that, the project's IDL repository is hosted.
// When nothing is known yet both configurations are generated: each platform
// ignores the other's file, and the surplus one can be dropped later with
// `devkit update --set CI=<kind>`.
func DetectCI(module, idlRepo, githubHost string) string {
	host := ""
	if first, _, _ := strings.Cut(module, "/"); strings.Contains(first, ".") {
		host = first
	}
	if host == "" {
		host, _ = ParseRepo(idlRepo, githubHost)
	}
	gh, _ := ParseRepo("x/y", githubHost)
	switch h := strings.ToLower(host); {
	case h == "":
		return CIBoth
	case h == strings.ToLower(gh) || strings.Contains(h, "github"):
		return CIGitHub
	case strings.Contains(h, "gitlab"):
		return CIGitLab
	default:
		return CIBoth
	}
}
