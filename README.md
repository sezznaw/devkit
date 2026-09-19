# devkit

**English** | [简体中文](README.zh-CN.md)

`devkit` creates a complete, runnable [Kitex](https://www.cloudwego.io/docs/kitex/)
(Thrift) service with one command: project layout, Nacos registration and
discovery, logging, configuration, code generation, CI and Dockerfile. It also
installs the tools the service needs and keeps the scaffolding up to date.

## Get started

**1. Install**

```sh
curl -fsSL https://raw.githubusercontent.com/sezznaw/devkit/main/install.sh | sh
```

**2. Tell devkit about your project**

Go to the directory that will hold your project's services and run `ngs` once.
devkit creates a `devkit.yaml` there for you to fill in:

```sh
mkdir -p ~/work/shop && cd ~/work/shop
devkit ngs order        # first run: creates devkit.yaml and stops
```

```yaml
# ~/work/shop/devkit.yaml
module_prefix: "github.com/sezznaw"    # a service "order" becomes github.com/sezznaw/order
idl_repo: "sezznaw/shop-idl"           # this project's Thrift IDL repository
common_repo: "sezznaw/devkit-common"          # shared library (optional, cloned for reading)
```

These three values are all devkit needs to know, and they are what makes it
work for any project: another project is simply another directory with its own
`devkit.yaml`.

**3. Create the service**

```sh
devkit ngs order
```

```
devkit ngs order
  project /Users/me/work/shop
  module  github.com/sezznaw/order

[1/7] Checking tools (git, go, kitex, thriftgo) ✓ 20.3s
        go 1.27 (installed now)
        kitex v0.16.3 (installed now)
        thriftgo 0.4.5 (installed now)
[2/7] Preparing IDL repository ✓ 1.1s
[3/7] Preparing common library checkout ✓ 0.9s
[4/7] Generating service from kitex-service ✓ 0s
[5/7] Writing initial IDL ✓ 0s
[6/7] Generating code and resolving dependencies ✓ 32.1s
[7/7] Creating first commit ✓ 0.1s

✓ service order created at /Users/me/work/shop/order (54.6s)
```

```sh
cd order && make run      # starts with conf/dev.yaml, no Nacos needed locally
```

Every further service in the same project is just `devkit ngs <name>`.

## What you get

```
~/work/shop/
  devkit.yaml               project settings (the three values above)
  idl/                      clone of the IDL repository; order/order.thrift was added for you
  common/                   clone of the shared library, for reading
  order/
    cmd/order/main.go       starts Kitex with Nacos, logging and config from common
    handler/handler.go      your RPC implementations
    conf/dev.yaml           local config, Nacos registration disabled
    conf/prod.yaml          production config, secrets from ${ENV_VARS}
    kitex_gen/              generated code, git-ignored
    Makefile                tools / gen / build / run / test / docker
    .github/workflows/ci.yml  checks out the IDL repo, regenerates, builds, tests
    Dockerfile
```

Daily loop: edit `../idl/order/order.thrift`, run `make gen`, implement the new
methods in `handler/`, `make run`. Open a pull request in the IDL repository
for IDL changes. Generated code is never committed; CI regenerates it.

## Tools are installed for you

`ngs` checks git, Go, `kitex` and `thriftgo` first. A missing Go is downloaded
from go.dev into `~/.devkit/go`; missing generators are installed with
`go install` at the versions the template pins. git is the only thing devkit
does not install (macOS: `xcode-select --install`).

If something was installed, add it to new shells once:

```sh
echo 'source "$HOME/.devkit/env"' >> ~/.zshrc    # or ~/.bashrc
```

## Commands

| Command | When you need it |
|---------|------------------|
| `devkit ngs <service>` | create a service in the current project directory |
| `devkit update` | inside a service: upgrade the devkit-managed files (Makefile, CI, Dockerfile, `main.go`) to the latest template. `--check` only shows versions and local changes |
| `devkit self-update` | upgrade devkit itself. `--check` only reports |
| `devkit doctor` | show the state of git, Go, kitex, thriftgo. `--fix` installs what is missing |
| `devkit version` | version info for bug reports |

Useful `ngs` flags: `--set Port=9000` (template variable), `--module <path>`
(override the module path), `--skip-common`, `--skip-idl`, `--no-git`.

### What `update` does to your files

Files you own (`go.mod`, `conf/*`, `handler/*`, `README.md`) are created once
and never touched again. For the managed files:

| Your file | `devkit update` | with `--force` |
|-----------|-----------------|----------------|
| unchanged since it was generated | replaced by the new version | same |
| edited by you | left alone; new version written next to it as `<file>.new` | replaced, your version kept as `<file>.bak` |
| deleted by you | restored | same |
| no longer part of the template | deleted if unchanged, kept if edited | edited one kept as `.bak` |

## Troubleshooting

**`this directory has no project settings yet`**
Expected on the first run: fill in the `devkit.yaml` that was created and run
the command again.

**`you are inside the service ...`**
`ngs` was started from within a service. `cd` to the project directory
(the one containing `devkit.yaml`).

**`kitex: command not found` in a new terminal**
Source the env file as shown above, or run `devkit doctor`.

**`go: module github.com/sezznaw/devkit-common: ... 404` during `go mod tidy`**
The repositories are private. Uncomment `go_private: true` in `devkit.yaml`
and make sure git can log in to GitHub (SSH key, `gh auth login`, or a
`~/.netrc` entry with a token).

**`github: GET ...: 404` or `401`**
The registry repository is private or misconfigured. Run
`devkit config set github_token <token>`; see the maintainer section below.

**`API rate limit exceeded`**
Anonymous GitHub API calls are limited to 60 per hour:
`devkit config set github_token <token>`.

## For maintainers

**Repositories.** `devkit` (this CLI), `devkit-registry` (the `kitex-service`
template; format in [docs/registry.md](docs/registry.md)), `common` (shared Go
library imported by every service), plus one IDL repository per project.

**Global configuration** lives in `~/.devkit/config.yaml`, written by
`install.sh`. The hidden command `devkit config show|set` edits it; every key
also has an environment variable.

| Key | Env variable | Purpose |
|-----|--------------|---------|
| `registry_repo` | `DEVKIT_REGISTRY_REPO` | `owner/repo` of the template registry |
| `registry_dir` | `DEVKIT_REGISTRY_DIR` | local registry checkout, for template development |
| `devkit_repo` | `DEVKIT_REPO` | `owner/repo` publishing devkit releases (self-update) |
| `github_token` | `DEVKIT_GITHUB_TOKEN` | needed for private repositories; lifts the API rate limit |
| `github_host` | `DEVKIT_GITHUB_HOST` | GitHub Enterprise host, default `github.com` |
| `module_prefix`, `idl_repo`, `common_repo`, `workspace_dir` | `DEVKIT_MODULE_PREFIX`, ... | fallbacks when a directory has no `devkit.yaml` |

`devkit.yaml` additionally accepts `go_private: true`, `component: <name>`
(scaffold from another registry component) and `vars:` (default template
variables such as `NacosAddr`).

**Installer variables:** `GITHUB_TOKEN` (private repositories), `GITHUB_REPO`,
`REGISTRY_REPO`, `DEVKIT_VERSION`, `INSTALL_DIR`.

**Development and release**

```sh
make build && make test && make lint
export DEVKIT_REGISTRY_DIR=$PWD/../devkit-registry    # use local templates
```

Push a tag `vX.Y.Z`: GitHub Actions runs goreleaser and publishes
`devkit_<os>_<arch>.tar.gz` plus `checksums.txt`, which `install.sh` and
`self-update` download. The sources use the placeholder organisation
`sezznaw`; replace it once with `scripts/set-org.sh <your-github-org>`.
