# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`devkit` is a Go CLI (cobra) whose job is one user flow: `curl install.sh | sh`,
`cd` into the project directory, `devkit ngs <service>` produces a complete
runnable Kitex (Thrift) service with all tools installed. It needs zero
configuration; `devkit.yaml` in the project directory is optional. `devkit update` later upgrades the scaffolding. Templates
live in a separate git repo, `devkit-registry` (sibling checkout at
`../devkit-registry`), fetched from GitHub; shared runtime code is the Go
module `github.com/sezznaw/devkit-common` (GitHub repo `sezznaw/devkit-common`,
sibling checkout at `../common`; the local directory name differs from the
repo name).

The visible command surface is deliberately tiny: `ngs`, `update`,
`self-update`, `doctor`, `version` (`config` and `completion` are hidden).
The earlier package-manager commands (`init/add/list/remove`, `workspace`)
were removed on 2026-09-19 at the user's request; do not reintroduce commands
without being asked. Everything is GitHub-based
(releases, registry, IDL/common clones); the original self-hosted-git design
was dropped on 2026-09-19.

The GitHub owner `sezznaw` is baked into go.mod, Makefile, goreleaser,
install.sh, buildinfo and every import. It is the real owner, not a
placeholder. `scripts/set-org.sh <owner>` exists only for someone forking the
project; never change the owner piecemeal.

State as of 2026-09-19: public on GitHub, releases `v0.1.0` and `v0.1.1`
published, and the whole chain (curl installer, `ngs` against the live
registry, `go get` of devkit-common through proxy.golang.org, `self-update`)
was verified against the real GitHub. `v0.1.0` shipped with the git-identity
bug below; `v0.1.1` is the first good release.

## Commands

```sh
make build                 # -> bin/devkit, version info injected via ldflags
make test                  # go test ./...
make lint                  # go vet ./...
go test ./internal/installer/ -run TestUpdate   # single package / test
gofmt -l .                 # CI-equivalent formatting check (no golangci-lint yet)
```

End-to-end against the local registry without touching your real
`~/.devkit` or GitHub:

```sh
export HOME=$(mktemp -d)                           # isolates config + cache
export DEVKIT_REGISTRY_DIR=$PWD/../devkit-registry # local source, ignores tags
mkdir /tmp/proj && cd /tmp/proj
devkit ngs order            # first run writes devkit.yaml and stops; fill it in, rerun
cd order && devkit update --check
```

Release is CI-only: pushing a `vX.Y.Z` tag runs goreleaser via
`.github/workflows/release.yml`, which creates a GitHub Release. The asset
names in `.goreleaser.yaml` (`devkit_<os>_<arch>.tar.gz` + `checksums.txt`)
are a contract shared by three places:
- `install.sh` without a token uses no API at all: it resolves the tag from
  the `releases/latest` redirect and downloads `releases/download/<tag>/<asset>`
  (the anonymous API allows only 60 requests/hour per IP). With `GITHUB_TOKEN`
  (private repos) it reads the release JSON and finds asset ids with awk.
- `self-update` always uses the releases API (asset `url` with
  `Accept: application/octet-stream`).

Release rules learned the hard way:
- The release workflow does not run tests. Wait for the `ci` workflow to be
  green on the commit before pushing a tag; `v0.1.0` was tagged on a red commit.
- Never move or re-push a release tag. Fix forward with the next patch version.
- Test `install.sh` against the live release after publishing (scratch
  `HOME` and `INSTALL_DIR`); fake-server tests did not catch the real API's
  JSON shape. The `releases/latest` redirect can lag a few seconds behind a
  new release.

## Architecture

Data flows in one direction: `cmd/` → `internal/installer` → (`registry`,
`render`, `manifest`, `project`). Commands do flag parsing only; `update`
builds an `installer.Installer` through `cmd/common.go:newInstaller`, `ngs`
builds its own for the fresh directory. `Installer.Remove` has no command any
more but stays as the empty-plan case of the same engine.

**registry.Source** (`internal/registry/source.go`) is the abstraction over
"where components come from". Two implementations:
- `GitHub`: reads `registry.json` from `main` via raw.githubusercontent.com,
  and fetches a component version as the repository tarball at git tag
  `<name>/v<version>` (GitHub has no subpath archives, so `extractComponent`
  keeps only `components/<name>/`). Cached forever under
  `~/.devkit/cache/<name>/<version>` because tags are immutable.
- `Local`: reads a checkout directly (`registry_dir` / `DEVKIT_REGISTRY_DIR`).
  Ignores tags and versions; used by component authors and by tests.

`registry.Open(cfg)` picks Local whenever `registry_dir` is set, otherwise
requires `registry_repo`. There is no real GitHub in tests; `internal/github`
is a thin HTTP client (`NewWithBase` lets tests point it at httptest) and the
tarball layout is reproduced in `registry/github_test.go`.

**render** turns `<component>/files/**` into a `Plan` (list of rel path +
content) before anything touches disk. Two rules: path segments may contain
`{{.Var}}`, and files ending in `.tmpl` are executed as `text/template` with
`missingkey=error` (a typo'd variable fails the whole install). Helper funcs
`title/pascal/camel/snake/kebab/upper/lower` are defined in `render.go`; hooks
in `component.json` are also rendered as templates.

**manifest** (`<project>/.devkit/manifest.json`) records per component the
version, the vars given at install time (reused by `update`), its deps, and a
`sha256` per written file. The hash is always of the *template output*, not of
what is on disk. That is what lets `update` / `update --check` classify a file as
Unchanged / Modified / Missing, and why a file skipped during update keeps
showing as modified until the user merges it.

**installer/apply.go:applyPlan** is the heart of the tool and the only place
that writes into a project. It reconciles a new Plan against the previous
`manifest.Installed` entry for the same component:

| on disk                                  | without --force            | with --force            |
|------------------------------------------|----------------------------|-------------------------|
| unchanged / missing, still produced      | overwrite / restore        | same                    |
| user-modified, still produced            | skip, write `<file>.new`   | rename to `.bak`, write |
| exists, not tracked by this component    | error before any write     | `.bak` + write          |
| tracked, no longer produced, unchanged   | delete, prune empty dirs   | same                    |
| tracked, no longer produced, modified    | keep, report               | `.bak`                  |

`Install` (fresh, or `--force` reinstall), `Update` and `Remove` (empty plan)
all go through this one function; keep it that way rather than adding
file-writing code elsewhere. Conflicts are detected in a pre-pass so a refused
install leaves the project untouched. Required-variable validation runs before
dependencies are installed for the same reason.

**selfupdate** replaces the running binary: latest release → find the
platform asset and `checksums.txt` in the release JSON → download via the
asset API URL → verify sha256 → extract → atomic `os.Rename` over the
executable. `Updater.Target`
exists so tests can point it at a temp file instead of the test binary.

**ngs** (`cmd/ngs.go`) is an orchestrator over the same pieces: it prepares
the workspace (`internal/workspace`: git clone/pull of the `idl` and `common`
repos; `GOPRIVATE` only when the workspace sets `go_private`), installs the `kitex-service` component into a fresh
directory with `--set Service/Module` (user vars override built-ins for this
reason), renders the component's `idl/` tree into `<workspace>/idl/` via
`render.BuildFrom`, then runs the deferred `post_install` hooks
(`Installer.RunHooks`) so codegen sees the IDL, and finally `git init`s.
`workspace.InitRepo` falls back to a neutral committer identity when git has
no `user.name`/`user.email` (new laptops, CI runners); without that the
scaffold commit silently fails.

**Project directory.** `<project>/devkit.yaml` (`workspace.Config`) carries
the per-project settings (module_prefix, idl_repo, common_repo, component,
go_private, vars) and is edited by hand; there is no command for it. ngs
resolves the directory as `--workspace` > nearest `devkit.yaml` above cwd >
global `workspace_dir` > cwd, and refuses to run from inside a service. Every
setting has a default: `module_prefix` -> `workspace.DefaultModulePrefix`
(the sanitised directory name, not the bare service name, so a service called
`log` does not shadow the standard library), `idl_repo` -> a local `idl/`
repository made by `workspace.EnsureLocalRepo` (nothing cloned, no pull
warnings while it has no remote), `common_repo` -> `workspace.DefaultCommonRepo`.
After a successful run ngs writes a commented `devkit.yaml`
(`workspace.WriteTemplate`) if none exists, for discoverability and so that
`workspace.Find` works from subdirectories. Precedence of values: flags >
devkit.yaml > global config / env > defaults.

Projects may live on another git server (the owner's services and IDL are on
a company GitLab while devkit itself is on GitHub): `idl_repo` accepts any
full git URL and `module_prefix` any host. `workspace.TokenAllowedFor` makes
sure the GitHub token is only ever sent over https to the configured GitHub
host. `workspace.ValidateServiceName` rejects names measured to break the
build (Go keywords and builtins, `main`, `init`, `internal`, `vendor`,
`handler`, and the project directories `idl`/`common`). Known gap: the
template only ships a GitHub Actions workflow; services hosted on GitLab get
no usable CI file yet. One machine, several
projects, one global config: that is the reason this layer exists.

**deps + ui.** `internal/deps.Ensure` is what makes "curl install, then ngs"
work on a blank machine: it checks git (never installed, only hinted), Go
(downloaded from go.dev into `~/.devkit/go` only when absent or < 1.21; newer
toolchains required by go.mod are auto-fetched by Go itself), and
kitex/thriftgo (`go install` at the versions the component's vars pin). It
prepends the tool dirs to the current process PATH so later hooks find them,
and writes `~/.devkit/env` (rustup-style) for future shells. `internal/ui`
renders numbered steps: spinner + ✓/✗ + duration on a TTY, plain lines when
`CI`/`DEVKIT_PLAIN`/non-TTY; `Step.Writer()` indents subprocess output and
`Step.Progress()` draws download bars. `ngs` is written as seven `r.Step`
calls; `devkit doctor` is the standalone entry.

**Write-once files.** `component.json` `once` globs (`go.mod`, `conf/*`,
`handler/*`, ...) are split off the plan in `installer.apply` before
`applyPlan`: created if absent, never tracked, updated or removed. Anything a
developer edits after scaffolding belongs there; otherwise `go mod tidy` alone
makes `devkit update --check` report the component as modified.

Full ngs e2e without GitHub needs three fakes: a local registry
(`DEVKIT_REGISTRY_DIR`), a bare git repo as `idl_repo` (any local path is
cloned as-is), and a file-based Go module proxy serving
`github.com/sezznaw/devkit-common` (`GOPROXY=file://...,https://proxy.golang.org,direct
GONOSUMDB=github.com/sezznaw GONOPROXY=none`) so the generated service's
`go mod tidy` resolves. `kitex` and `thriftgo` must be on `PATH` for
`make gen`; the generated `main.go` cannot compile without `kitex_gen/`.
A true fresh-machine run (PATH without go, empty HOME) exercises the Go
download and takes several minutes; use `DEVKIT_PLAIN=1` for readable logs.

## Conventions worth knowing

- Config precedence: `~/.devkit/config.yaml` < `DEVKIT_*` env vars. Tests set
  `HOME` to a temp dir instead of mocking config.
- Component names must match `^[a-z0-9][a-z0-9-]*$` (they become directory
  names and tag names); `registry.ValidateName` enforces this at every entry.
- Version strings are stored without a leading `v` in `registry.json` and
  `component.json`; the `v` appears only in git tags.
- The registry format and publishing steps are documented in
  `docs/registry.md`; update it when changing `component.json` fields or
  template behaviour, since component authors read it, not the Go code.
- Commit style: `feat:`/`fix:` prefixes, one commit per delivered step. The
  pre-publication history was squashed into a single initial commit on
  2026-09-19; history from there on is public and must not be rewritten.
- Docs come in pairs: `README.md` and `README.zh-CN.md` have identical
  structure; change both together.
