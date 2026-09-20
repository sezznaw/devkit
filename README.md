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

**2. Create a service**

Go to the directory that holds (or will hold) your project's services and run:

```sh
mkdir -p ~/work/shop && cd ~/work/shop
devkit ngs order
```

No configuration is needed. With nothing set, the service's Go module is
`shop/order` (directory name + service name), the project's shared IDL lives
in a local git repository `./idl`, and the common library comes from
`sezznaw/devkit-common`.

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

**3. Optional: project settings**

The first run writes a commented `devkit.yaml` next to your services. Edit it
when you know where the code will live; another project is simply another
directory with its own `devkit.yaml`.

```yaml
# ~/work/shop/devkit.yaml  (every value is optional)
module_prefix: "gitlab.yourcompany.com/shop"            # "order" becomes gitlab.yourcompany.com/shop/order
idl_repo: "git@gitlab.yourcompany.com:shop/idl.git"     # or owner/repo on GitHub
common_repo: ""                                         # default: sezznaw/devkit-common
```

| Setting | When empty | Set it to |
|---------|-----------|-----------|
| `module_prefix` | `<directory name>`, e.g. `shop/order` | where the services' code will be hosted. Decide before creating many services: it is part of every import path |
| `idl_repo` | a local `idl/` git repository is created, nothing is cloned | the project's shared IDL repository: `owner/repo` on GitHub or any full git URL (company GitLab, SSH or HTTPS). Costs nothing to set later |
| `common_repo` | `sezznaw/devkit-common` | a fork of the common library |

**Starting before you have a git server.** Leave `idl_repo` empty and work
locally. Later, create the IDL project on your server and run, inside `idl/`:
`git add -A && git commit -m "add idl" && git remote add origin <url> && git push -u origin main`,
then put `<url>` into `idl_repo` so teammates get the same IDLs. The shared
`idl/` repository is what lets every service generate clients for the others.

## What you get

```
~/work/shop/
  devkit.yaml               optional project settings
  idl/                      clone of the IDL repository; order/order.thrift was added for you
  common/                   clone of the shared library, for reading
  order/
    cmd/order/main.go       starts Kitex with Nacos, logging and config from common
    handler/handler.go      your RPC implementations
    conf/dev.yaml           local config, Nacos registration disabled
    conf/prod.yaml          production config, secrets from ${ENV_VARS}
    kitex_gen/              generated code, git-ignored
    idl.mk                  which IDLs this service generates code for (yours to edit)
    Makefile                tools / gen / build / run / test / docker
    .gitlab-ci.yml            and/or .github/workflows/ci.yml: clone the IDL repo,
                              regenerate, build, test (see "CI" below)
    Dockerfile
```

Daily loop: edit `../idl/order/order.thrift`, run `make gen`, implement the new
methods in `handler/`, `make run`.

**Calling another service.** Each service generates code only for the IDLs it
needs, listed in its `idl.mk`. To call `user` from `order`, add the IDL and
regenerate; the client is then in `kitex_gen/user/userservice`:

```makefile
# order/idl.mk
IDLS := order/order.thrift user/user.thrift
```
 Open a pull request in the IDL repository
for IDL changes. Generated code is never committed; CI regenerates it.

## CI

The service gets the pipeline that matches where it is hosted, judged from
`module_prefix` (or, failing that, `idl_repo`):

| Host | Generated |
|------|-----------|
| `github.com/...` | `.github/workflows/ci.yml` |
| a GitLab (`gitlab.yourcompany.com/...`) | `.gitlab-ci.yml` |
| not known yet (nothing configured) | both; each platform ignores the other's file |

Once you know, drop the surplus file from inside the service:
`devkit update --force --set CI=gitlab` (or `github`, `both`, `none`). To
choose up front for a whole project, put `vars: {CI: gitlab}` in `devkit.yaml`.

Both pipelines fetch the shared IDL project, run `make tools && make gen`,
then build and test, because generated code is not committed. They look for
the IDL project at the path taken from `idl_repo`; with no `idl_repo` they
assume a project named `idl` next to the service (`<group>/idl` on GitLab,
`<owner>/idl` on GitHub). On GitLab the IDL project must allow the service's
job token: IDL project → Settings → CI/CD → Job token permissions.

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
| `devkit update` | upgrade the devkit-managed files (Makefile, CI, Dockerfile, `main.go`) to the latest template. Inside a service it updates that service; in the project directory it updates every service. `--check` only shows versions and local changes |
| `devkit self-update` | upgrade devkit itself. `--check` only reports |
| `devkit doctor` | show the state of git, Go, kitex, thriftgo. `--fix` installs what is missing |
| `devkit version` | version info for bug reports |

Useful `ngs` flags: `--set Port=9000` (template variable), `--module <path>`
(override the module path), `--skip-common`, `--skip-idl`, `--no-git`.

### Updating all services at once

Run `update` in the project directory instead of inside a service:

```
$ cd ~/work/shop && devkit update --check
SERVICE  COMPONENT      INSTALLED  LATEST  FILES  LOCAL CHANGES
game     kitex-service  0.2.0      0.2.1   6      1 modified, 0 missing
user     kitex-service  0.2.1      0.2.1 (up to date)  6  none

$ devkit update
...
3 service(s): 3 updated, 0 already up to date
modified locally, not overwritten; merge the .new copy by hand or rerun with --force:
  game/Makefile
```

Services are the subdirectories created by devkit; `idl/` and `common/` are
never touched. A service that fails does not stop the others. With `--force`
in the project directory devkit first lists the modified files it is about to
overwrite and asks for confirmation (`--yes` skips the question).

### What `update` does to your files

Files you own (`go.mod`, `idl.mk`, `conf/*`, `handler/*`, `README.md`) are created once
and never touched again. For the managed files:

| Your file | `devkit update` | with `--force` |
|-----------|-----------------|----------------|
| unchanged since it was generated | replaced by the new version | same |
| edited by you | left alone; new version written next to it as `<file>.new` | replaced, your version kept as `<file>.bak` |
| deleted by you | restored | same |
| no longer part of the template | deleted if unchanged, kept if edited | edited one kept as `.bak` |

## Troubleshooting

**`"handler" cannot be used as a service name`**
Some names cannot become a compiling Go service: Go keywords and built-ins
(`map`, `type`, `string`, `error`, `new`, ...), `main`, `init`, `internal`,
`vendor`, `handler` (clashes inside Kitex's generated code), and `idl` /
`common`, which are directories of the project. Pick another name, for example
`handler-svc`.

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
| `module_prefix`, `idl_repo`, `common_repo`, `workspace_dir` | `DEVKIT_MODULE_PREFIX`, ... | machine-wide fallbacks for values a `devkit.yaml` leaves empty |

The GitHub token is only ever sent to the configured GitHub host; IDL
repositories on other servers are cloned with your own git credentials.

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
`self-update` download. Wait for the `ci` workflow to pass on the commit
before tagging; the release workflow does not run tests.

Forking: the GitHub owner `sezznaw` appears in the module path, `install.sh`,
the release configuration and the default registry. `scripts/set-org.sh
<your-github-owner>` rewrites all of them.
