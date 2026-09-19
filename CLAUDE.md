# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`devkit` is a Go CLI (cobra) whose job is one user flow: `curl install.sh | sh`,
fill three values into `devkit.yaml` in the project directory, then
`devkit ngs <service>` produces a complete runnable Kitex (Thrift) service with
all tools installed. `devkit update` later upgrades the scaffolding. Templates
live in a separate git repo, `devkit-registry` (sibling checkout at
`../devkit-registry`), fetched from GitHub; shared runtime code is the
`../common` Go module.

The visible command surface is deliberately tiny: `ngs`, `update`,
`self-update`, `doctor`, `version` (`config` and `completion` are hidden).
The earlier package-manager commands (`init/add/list/remove`, `workspace`)
were removed on 2026-09-19 at the user's request; do not reintroduce commands
without being asked. Everything is GitHub-based
(releases, registry, IDL/common clones); the original self-hosted-git design
was dropped on 2026-09-19.

The placeholder organisation `sezznaw` is baked into go.mod, Makefile,
goreleaser, install.sh, buildinfo and imports. Replace it only via
`scripts/set-org.sh`, never piecemeal.

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
`.github/workflows/release.yml`, which creates a GitHub Release. `install.sh`
and `self-update` download its assets through the releases API (asset `url`
with `Accept: application/octet-stream`, which also works for private repos),
so the asset names in `.goreleaser.yaml` (`devkit_<os>_<arch>.tar.gz` +
`checksums.txt`) are a contract shared by three places.

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
what is on disk. That is what lets `list`/`update` classify a file as
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

**Project directory.** `<project>/devkit.yaml` (`workspace.Config`) carries
the per-project settings (module_prefix, idl_repo, common_repo, component,
go_private, vars) and is edited by hand; there is no command for it. ngs
resolves the directory as `--workspace` > nearest `devkit.yaml` above cwd >
global `workspace_dir` > cwd, and refuses to run from inside a service. When
the required values are missing and no `devkit.yaml` exists it writes a
commented template (`workspace.WriteTemplate`) and stops with instructions;
when the file exists but is incomplete it names the missing keys. Precedence
of values: flags > devkit.yaml > global config / env. One machine, several
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
- Commit style: `feat:`/`fix:` prefixes; each delivered step so far is one commit.
