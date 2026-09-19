// Package buildinfo holds values injected at build time via -ldflags.
package buildinfo

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
	// Repo is the GitHub "owner/repo" that publishes devkit releases.
	Repo = "sezznaw/devkit"
)
