// Package report gathers the facts of a piece of work across the
// repositories of a project: which repositories and files changed, what the
// IDL gained, whether lint and tests pass, whether the post-deploy check was
// touched. The facts come from git and the build, never from memory, so the
// hand-over a developer (or their AI tool) writes starts from the truth.
package report

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sezznaw/devkit/internal/manifest"
	"github.com/sezznaw/devkit/internal/workspace"
)

// Options select the range and how much to run.
type Options struct {
	// Since is a duration (24h, 3d), a date (2026-10-11) or a git ref. A
	// duration or date selects the current user's commits in that window
	// (AllAuthors lifts the author filter); a ref means ref..HEAD.
	Since string
	// AllAuthors includes other people's commits in a time window.
	AllAuthors bool
	// Run executes make lint and make test in every changed service.
	Run bool
	// Fetch runs git fetch first so "pushed" is judged against the server.
	Fetch bool
	// Log receives progress lines (stderr); nil discards them.
	Log func(format string, args ...any)
}

// Report is the result; Markdown renders it for people, the JSON tags for tools.
type Report struct {
	Project  string      `json:"project"`
	Date     string      `json:"date"`
	Author   string      `json:"author"`
	Since    string      `json:"since"`
	Repos    []*Repo     `json:"repos"`
	IDL      *IDLChanges `json:"idl,omitempty"`
	Checks   []Check     `json:"checks,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
}

// Repo is one changed repository.
type Repo struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"` // idl, infra, rpc, gateway, frontend, lib, framework, other
	Branch string `json:"branch"`
	// Base is the commit the file list is relative to.
	Base    string   `json:"base"`
	Commits []Commit `json:"commits"`
	Files   []File   `json:"files"`
	// Framework lists changed files that devkit owns (from the manifest):
	// a colleague should not have touched them.
	Framework []string `json:"framework,omitempty"`
	// Upgraded says .devkit/manifest.json changed too, i.e. this was a
	// devkit update, and Framework changes are expected.
	Upgraded      bool   `json:"upgraded,omitempty"`
	VerifyChanged bool   `json:"verify_changed,omitempty"`
	Uncommitted   int    `json:"uncommitted"`
	Unpushed      int    `json:"unpushed"`
	WebURL        string `json:"web_url,omitempty"`
	path          string
}

// Commit is one commit in the range.
type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Pushed  bool   `json:"pushed"`
}

// File is one changed path. Status is git's letter (A M D R).
type File struct {
	Status      string `json:"status"`
	Path        string `json:"path"`
	Uncommitted bool   `json:"uncommitted,omitempty"`
	// Untracked: git does not know the file yet (new, never added).
	Untracked bool `json:"untracked,omitempty"`
}

// Check is the outcome of one command in one repository.
type Check struct {
	Repo   string `json:"repo"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Took   string `json:"took,omitempty"`
}

// Collect builds the report for the project directory dir.
func Collect(dir string, opt Options) (*Report, error) {
	if opt.Log == nil {
		opt.Log = func(string, ...any) {}
	}
	if opt.Since == "" {
		opt.Since = "24h"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	r := &Report{
		Project: filepath.Base(dir),
		Date:    time.Now().Format("2006-01-02 15:04"),
		Since:   opt.Since,
	}
	webBase := ""
	if cfg, err := workspace.LoadConfig(dir); err == nil {
		if host, path := workspace.ParseRepo(cfg.IdlRepo, ""); host != "" && path != "" {
			webBase = "https://" + host + "/" + strings.TrimSuffix(path, "/idl")
		}
	}
	var repos []*Repo
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if !isRepo(p) {
			continue
		}
		repos = append(repos, &Repo{Name: e.Name(), Kind: kindOf(p), path: p})
	}
	if len(repos) == 0 {
		return nil, fmt.Errorf("no git repositories under %s", dir)
	}
	if r.Author == "" {
		if out, err := git(repos[0].path, "config", "user.email"); err == nil {
			r.Author = out
		}
	}
	if opt.Fetch {
		opt.Log("fetching %d repositories", len(repos))
		var wg sync.WaitGroup
		for _, rp := range repos {
			wg.Add(1)
			go func(rp *Repo) {
				defer wg.Done()
				if _, err := git(rp.path, "fetch", "-q", "origin"); err != nil {
					opt.Log("%s: fetch failed (offline?), judging pushed state from the last fetch", rp.Name)
				}
			}(rp)
		}
		wg.Wait()
	}
	var changed []*Repo
	for _, rp := range repos {
		if err := inspect(rp, r.Author, opt); err != nil {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s: %v", rp.Name, err))
			continue
		}
		if len(rp.Commits) == 0 && len(rp.Files) == 0 {
			continue
		}
		if webBase != "" {
			rp.WebURL = webBase + "/" + rp.Name
		}
		changed = append(changed, rp)
		if rp.Kind == "framework" {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s/ 是框架检出，有本地改动或提交；lint 规则 common-checkout 会报，`git -C %s checkout .` 复原", rp.Name, rp.Name))
		}
		if len(rp.Framework) > 0 && !rp.Upgraded {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s 改了框架文件 %s：这些由 devkit update 覆盖，不归你改（AGENTS.md 4.3）", rp.Name, strings.Join(rp.Framework, ", ")))
		}
	}
	r.Repos = changed
	for _, rp := range changed {
		if rp.Kind == "idl" {
			r.IDL = idlChanges(rp)
			for _, f := range r.IDL.Flags {
				if f.Name == "@public" {
					r.Warnings = append(r.Warnings, fmt.Sprintf("idl/%s 新增 @public（免登录接口）：要人确认过（AGENTS.md 第 5 节）", f.File))
				}
			}
			if n := len(r.IDL.Removed); n > 6 {
				r.Warnings = append(r.Warnings, fmt.Sprintf("idl 删掉了 %d 个方法 / 结构体 / 错误码（清单在「接口与错误码」一节）：删接口、删字段要先问人", n))
			} else {
				for _, d := range r.IDL.Removed {
					r.Warnings = append(r.Warnings, fmt.Sprintf("idl/%s 删掉了 %s %s：删接口、删字段要先问人", d.File, d.Kind, d.Name))
				}
			}
		}
	}
	if opt.Run {
		for _, rp := range changed {
			if rp.Kind != "rpc" && rp.Kind != "gateway" && rp.Kind != "lib" {
				continue
			}
			for _, name := range []string{"lint", "test"} {
				opt.Log("%s: make %s", rp.Name, name)
				r.Checks = append(r.Checks, runMake(rp, name))
			}
		}
	}
	return r, nil
}

// kindOf classifies a repository by what is in it.
func kindOf(p string) string {
	switch filepath.Base(p) {
	case "idl":
		return "idl"
	case "infra":
		return "infra"
	case "lib":
		return "lib"
	case "kit-common":
		return "framework"
	}
	if m, err := manifest.Load(p); err == nil {
		for name := range m.Components {
			switch {
			case strings.Contains(name, "hertz"):
				return "gateway"
			case strings.Contains(name, "kitex"):
				return "rpc"
			}
		}
	}
	if _, err := os.Stat(filepath.Join(p, "package.json")); err == nil {
		return "frontend"
	}
	return "other"
}

var reDuration = regexp.MustCompile(`^(\d+)([hdw])$`)
var reDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// inspect fills one repository's commits and files for the range.
func inspect(rp *Repo, author string, opt Options) error {
	if out, err := git(rp.path, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		rp.Branch = out
	}
	var logArgs []string
	base := ""
	switch {
	case reDuration.MatchString(opt.Since):
		m := reDuration.FindStringSubmatch(opt.Since)
		n, _ := strconv.Atoi(m[1])
		unit := map[string]string{"h": "hours", "d": "days", "w": "weeks"}[m[2]]
		logArgs = []string{"--since=" + fmt.Sprintf("%d %s ago", n, unit)}
	case reDate.MatchString(opt.Since):
		logArgs = []string{"--since=" + opt.Since}
	default:
		ref, err := git(rp.path, "rev-parse", "--verify", "--quiet", opt.Since+"^{commit}")
		if err != nil {
			// A ref that exists only in some repositories (a tag, a branch):
			// elsewhere it means "nothing".
			return nil
		}
		base = ref
		logArgs = []string{base + "..HEAD"}
	}
	if base == "" && !opt.AllAuthors && author != "" {
		logArgs = append(logArgs, "--author="+author)
	}
	var lines []string
	if rp.Kind == "framework" {
		// A checkout of the framework is never worked in: its history is
		// someone else's. Only local edits matter (lint common-checkout).
		base = "HEAD"
	} else {
		var err error
		lines, err = gitLines(rp.path, append([]string{"log", "--format=%H%x00%h%x00%s"}, logArgs...)...)
		if err != nil {
			return fmt.Errorf("git log: %v", err)
		}
	}
	remoteBranch := ""
	if rp.Branch != "" && rp.Branch != "HEAD" {
		if _, err := git(rp.path, "rev-parse", "--verify", "--quiet", "origin/"+rp.Branch); err == nil {
			remoteBranch = "origin/" + rp.Branch
		}
	}
	oldest := ""
	for _, l := range lines {
		parts := strings.SplitN(l, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		c := Commit{Hash: parts[1], Subject: parts[2]}
		if remoteBranch != "" {
			_, err := git(rp.path, "merge-base", "--is-ancestor", parts[0], remoteBranch)
			c.Pushed = err == nil
		}
		if !c.Pushed {
			rp.Unpushed++
		}
		rp.Commits = append(rp.Commits, c)
		oldest = parts[0]
	}
	if base == "" {
		if oldest != "" {
			if p, err := git(rp.path, "rev-parse", "--verify", "--quiet", oldest+"^"); err == nil {
				base = p
			} else {
				base = emptyTree
			}
		} else {
			base = "HEAD"
		}
	}
	rp.Base = base
	// Files: base → working tree (so uncommitted edits count), plus untracked.
	uncommitted := map[string]bool{}
	if st, err := gitLines(rp.path, "status", "--porcelain", "--untracked-files=all"); err == nil {
		for _, l := range st {
			if len(l) > 3 {
				p := strings.TrimSpace(l[3:])
				if i := strings.Index(p, " -> "); i >= 0 {
					p = p[i+4:]
				}
				uncommitted[p] = true
			}
		}
	}
	rp.Uncommitted = len(uncommitted)
	seen := map[string]bool{}
	if ns, err := gitLines(rp.path, "diff", "--name-status", "-M", base); err == nil {
		for _, l := range ns {
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			p := f[len(f)-1]
			seen[p] = true
			rp.Files = append(rp.Files, File{Status: f[0][:1], Path: p, Uncommitted: uncommitted[p]})
		}
	}
	for p := range uncommitted {
		if !seen[p] {
			rp.Files = append(rp.Files, File{Status: "A", Path: p, Uncommitted: true, Untracked: true})
		}
	}
	sort.Slice(rp.Files, func(i, j int) bool { return rp.Files[i].Path < rp.Files[j].Path })
	// Ownership: files the manifest lists are devkit's.
	if m, err := manifest.Load(rp.path); err == nil {
		owned := map[string]bool{}
		for _, c := range m.Components {
			for rel := range c.Files {
				owned[rel] = true
			}
		}
		for _, f := range rp.Files {
			switch {
			case f.Path == ".devkit/manifest.json":
				rp.Upgraded = true
			case f.Path == "app/verify.go":
				rp.VerifyChanged = true
			case owned[f.Path] && f.Path != "idl.lock":
				rp.Framework = append(rp.Framework, f.Path)
			}
		}
	}
	return nil
}

// idlChanges parses the thrift diff of the IDL repository.
func idlChanges(rp *Repo) *IDLChanges {
	diff, err := git(rp.path, "diff", "-U0", rp.Base, "--", "*.thrift")
	if err != nil {
		return &IDLChanges{}
	}
	// Untracked new thrift files are not in the diff: show them whole.
	for _, f := range rp.Files {
		if f.Untracked && strings.HasSuffix(f.Path, ".thrift") {
			if b, err := os.ReadFile(filepath.Join(rp.path, f.Path)); err == nil {
				var sb strings.Builder
				sb.WriteString("+++ b/" + f.Path + "\n")
				for _, l := range strings.Split(string(b), "\n") {
					sb.WriteString("+" + l + "\n")
				}
				diff += "\n" + sb.String()
			}
		}
	}
	return parseThriftDiff(diff, func(file, method string) string {
		return serviceOfMethod(filepath.Join(rp.path, file), method)
	})
}

// runMake runs `make <target>` in the repository and keeps the tail of the
// output when it fails.
func runMake(rp *Repo, target string) Check {
	start := time.Now()
	cmd := exec.Command("make", target)
	cmd.Dir = rp.path
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	c := Check{Repo: rp.Name, Name: "make " + target, OK: err == nil, Took: time.Since(start).Round(time.Second).String()}
	out := strings.TrimSpace(buf.String())
	if err != nil {
		lines := strings.Split(out, "\n")
		if len(lines) > 20 {
			lines = lines[len(lines)-20:]
		}
		c.Detail = strings.Join(lines, "\n")
	} else if target == "test" {
		n := 0
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "ok ") {
				n++
			}
		}
		if n > 0 {
			c.Detail = fmt.Sprintf("%d 个包通过", n)
		}
	}
	return c
}
