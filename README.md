# devkit

**English** | [简体中文](README.zh-CN.md)

`devkit` creates a complete, runnable [Kitex](https://www.cloudwego.io/docs/kitex/)
(Thrift) service with one command: project layout, Nacos registration and
discovery, logging, configuration, code generation, CI and Dockerfile. It also
installs the tools the service needs and keeps the scaffolding up to date.

## Get started

**Joining an existing project** is two commands: `devkit auth https://gitlab.example.com`
(paste a personal access token with api, read_repository, write_repository), then
`devkit pull <project>` in the directory you keep code in. Every repository you have
access to lands in `./<project>/`, the directory is set up (go.work, the shared
library checkout, hooks, locks, generators), and `devkit ai <tool>` points your AI
tool at the project's AGENTS.md. Starting a new project is below.


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
in a local git repository `./idl`. The common library is always
`sezznaw/devkit-common`; a copy is cloned to `./kit-common` for reading.

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
cd order && make run      # conf/local.yaml: your machine and the Nacos on it (the file has the docker command)
```

Every further service in the same project is just `devkit ngs <name>`.

**The HTTP front of those services is `devkit nas <name>`** (new API service):

```sh
devkit nas gateway
cd gateway && make run
curl 'http://127.0.0.1:8080/ping?message=hi'
```

It is the same kind of project on CloudWeGo Hertz. The HTTP API is defined in
the project's Thrift IDL with Hertz annotations (`api.get="/user/:id"`);
`make gen` generates the routes with `hz` and, for the RPC services listed in
`idl.mk`, their clients with `kitex`. Configuration, Nacos, logging (one
`trace_id` from the HTTP request to the last RPC service) and the graceful stop
are those of an RPC service; `hz` is installed for you like the other tools.

**3. Optional: project settings**

The first run writes a commented `devkit.yaml` next to your services. Edit it
when you know where the code will live; another project is simply another
directory with its own `devkit.yaml`.

```yaml
# ~/work/shop/devkit.yaml  (every value is optional)
module_prefix: "gitlab.yourcompany.com/shop"            # "order" becomes gitlab.yourcompany.com/shop/order
idl_repo: "git@gitlab.yourcompany.com:shop/idl.git"     # or owner/repo on GitHub
```

| Setting | When empty | Set it to |
|---------|-----------|-----------|
| `module_prefix` | `<directory name>`, e.g. `shop/order` | where the services' code will be hosted. Decide before creating many services: it is part of every import path |
| `idl_repo` | a local `idl/` git repository is created, nothing is cloned | the project's shared IDL repository: `owner/repo` on GitHub or any full git URL (company GitLab, SSH or HTTPS). Costs nothing to set later |

**Starting before you have a git server.** Leave `idl_repo` empty and work
locally. Later, create the IDL project on your server and run, inside `idl/`:
`git add -A && git commit -m "add idl" && git remote add origin <url> && git push -u origin main`,
then put `<url>` into `idl_repo` so teammates get the same IDLs. The shared
`idl/` repository is what lets every service generate clients for the others.

## What you get

```
~/work/shop/
  devkit.yaml               optional project settings
  go.work                   makes the Go tools and your IDE use ./kit-common and your own ./lib (this machine only)
  idl/                      clone of the IDL repository; order/order.thrift was added for you
  kit-common/                the shared library at the team's version; "go to definition" lands here; read-only
  lib/                      optional: your own shared pure-Go module, listed in go.work, released by tag
  order/
    cmd/order/main.go       framework entry point; replaced by devkit update, never edit it
    app/app.go              yours: the service's Config, its dependencies and shutdown cleanup
    handler/handler.go      yours: the service struct, New and Ping; RPC methods go in handler/<topic>.go
    handler/codes.go        yours: constants the service alone uses (business codes come from the IDL's errors/)
    repo/repo.go            yours: database access, one file per table
    app/jobs.go             yours: scheduled jobs (name, cron, timeout, function)
    app/verify.go           yours: post-deploy checks run by --verify
    conf/local.yaml         your own machine (no APP_ENV): registers in the Nacos on it; every setting explained
    conf/dev.yaml           the shared development server; addresses and secrets from ${ENV_VARS}
    conf/uat.yaml           user acceptance test
    conf/prod.yaml          production
    conf/README.md          reference of all settings, kept up to date by devkit
    kitex_gen/              generated code, git-ignored, read-only
    idl.lock                which IDL commit the code was generated from; written by make gen
    idl.deps                the IDL generation of this service and of each service it calls; written by make gen, waited for at start
    .devkit/manifest.json   devkit's record of the files it manages
    idl.mk                  which IDLs this service generates code for (yours to edit)
    Makefile                tools / gen / build / run / test / docker
    .gitlab-ci.yml            and/or .github/workflows/ci.yml: clone the IDL repo,
                              regenerate, build, test (see "CI" below)
    Dockerfile
```

**The common library is part of your project.** `ngs` and `devkit update`
keep `kit-common/` checked out at the team's released version and maintain a
`go.work` next to it that lists `kit-common/` and every service. The Go tools and
the IDE then resolve `github.com/sezznaw/devkit-common` to that directory, so
"go to definition" opens code inside your project instead of the read-only
module cache. Because `kit-common/` holds exactly the release your services pin,
what you build locally is what CI builds; `go.work` exists only on your
machine and neither CI nor a Docker build ever sees it. If your IDE has only a
single service open and does not pick it up, open the project directory
instead.

The directory is called `kit-common/` because a project has a `common` of its
own; that name is yours. A project created with an earlier devkit has the
checkout in `common/`: the next `devkit update` (or `ngs` / `nas`) renames it
to `kit-common/`, with everything in it, and rewrites `go.work`. A `common/`
that is anything else is never touched.

Your own shared Go code goes in a `lib/` module in the project directory
(`lib/go.mod`): pure functions every service may import (odds conversion,
settlement rules), released by git tag and required by version in each
service's `go.mod`. When `lib/go.mod` exists, `ngs`, `nas` and `update` list
`./lib` in `go.work` as well, so services build against the directory and
"go to definition" opens it there; after creating `lib/` run `devkit update`
once, a line added by hand would not survive the next rewrite. As with
everything in `go.work`, this is for your machine: CI builds a service with the
`lib` version its `go.mod` requires. A `lib` hosted on a private GitLab needs
`go env -w GOPRIVATE=<gitlab host>` and a `~/.netrc` line with a read token on
every laptop; the CI templates write their own netrc from the job token. (A
legacy `common/` module is still listed when it exists; new projects use `lib/`.)

Do not edit `kit-common/`: a change there affects builds on your machine only.
devkit never discards such a change, but `update` and `update --check` call it
out. Changes to the library go into its own repository and reach everyone
through a release. `GOWORK=off go build ./...` builds the way CI does.

**Framework files and your files.** The framework is maintained centrally, so
nothing specific to one service lives in a framework file. `main.go`, the
Makefile, the CI files, the Dockerfile and `conf/README.md` are replaced by
`devkit update`; editing one of them blocks its future updates. Everything
else is yours and never rewritten. To give the service its own configuration
section or a dependency such as Redis, edit `app/app.go`: add a field to
`Config`, create the client in `Setup`, register `kitexx.OnShutdown` there and
pass it to `handler.New(...)`.

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

### AI tools

The project's development guide for AI coding tools is one file,
`idl/AGENTS.md`, in the IDL repository every developer has. Each tool reads
a differently named file on start, so `devkit ai <tool>` writes that pointer
in the project directory for the tool you use (`devkit ai` lists them:
claude, cursor, copilot, gemini, windsurf, junie, kiro, trae, cline, roo,
augment, continue, lingma, aider, qwen, iflow, warp, or `agents` for any
tool that reads AGENTS.md); `--all` writes every one, `--clean` removes what
devkit wrote. Nothing is written unasked, so the directory holds only what
is in use. Start the AI in the project directory, not inside a service, so
it sees the IDL, the services and the infrastructure together. A tool
without a pointer reads the guide when told to.

The guide holds the project's rules; the step-by-step procedures of the
recurring tasks are **skills** (the open Agent Skills format: a directory
with a SKILL.md, frontmatter `name` and `description`, then the steps).
devkit ships the framework's: `devkit-rpc-method`, `devkit-gateway-api`,
`devkit-gateway-domain` and `devkit-finish`, the same in every project and
versioned with the framework they describe. A project adds its own under
`idl/skills/<name>/SKILL.md` (one with a built-in's name replaces it).
`devkit ai <tool>` copies both to `.agents/skills/` (Codex, Copilot,
Cursor, Gemini CLI, Windsurf) and `.claude/skills/` (Claude Code), where
the tools load a skill when its task comes up; `devkit pull` and
`devkit update` refresh the copies, `devkit ai --clean` removes them. The
built-in skills come from the devkit binary: when it is older than the
team's `DEVKIT_VERSION`, those two commands leave the copies alone and say
to run `devkit self-update` first. Edit the sources, never the
copies.

`devkit report` is the finishing step: it collects the facts of a piece of
work from git and the build across every repository of the project
(repositories, commits and pushed state, files with framework files
flagged, what the IDL gained in methods, structs and error codes, `make
lint` / `make test` results, whether `app/verify.go` was touched) and
prints a Markdown hand-over with blanks for what only the author knows.
`--since 24h|3d|<ref>` sets the range, `--no-run` skips lint and tests,
`--json` is for tools.

### Taking a dev service over from your machine

`devkit takeover <service>` lets the service you start in your IDE, with a
breakpoint, be the instance of that service for requests that carry
`X-Dev-Tag: <your tag>`, while every other request keeps going to the
environment's own instances. The tool opens one ssh connection to the
environment's node (the `takeover` account the infrastructure repository
sets up: forced command, TCP forwarding, nothing else), tunnels every
cluster address (service ports + 1000, infrastructure + 10000), keeps
`<service>/.devkit/takeover.json` fresh and registers the local instance in
the Nacos group `DEV_TAKEOVER` while its port listens. The service, started
with `APP_ENV` unset or `local` (an IDE run configuration, or `make run`) and after the tool, finds the marker, acknowledges it in `.devkit/takeover.json.ack` (the tool registers the local instance only then; otherwise it says why not) and runs in takeover mode: the
environment's profile, tunnelled addresses, no registration, no consumers,
no outbox relay, no delayed tasks, no metrics port. Routing lives in
kit-common (`devroute`): on in `APP_ENV=dev`, off in uat and prod, where
nothing of it is on the request path. The ssh target comes from the IDL
repository's devkit.yaml (`takeover_ssh`); `~/.devkit/takeover.yaml` can
override it and the tag per project (`projects: {<dir name>: {ssh, tag}}`).
`devkit tunnel` opens the same connection without taking anything over, for
a database client or the Nacos console on this machine.

### Lint: the team's rules, enforced

`devkit lint` (what `make lint` runs, locally and in CI) checks a service
against the team's conventions, whoever wrote the code. The generic rules
are the same in every project: no middleware opened by hand (`rt.DB`,
`rt.Redis`, `rt.Kafka`, `rt.S3`, `rt.Provider` instead), money fields as
`common.Money` and rates as `common.Decimal`, never a float (`money-type`,
`rate-type`, `no-float-money`), a file field as `<name>_key` in a request
and `<name>_url` in a response, never a path, never a URL sent by a
client and never the file's content in a request (`file-field`: the
database stores the key, the service builds the URL), one upload method
per service, `GetUploadURL(kind, content_type)`, with a kind per file
rather than a method per field, present whenever requests carry file keys
(`upload-method`), no file through the gateway (`upload-through-gateway`:
no `c.FormFile` / multipart and no `S3.Put` in a gateway handler; the
client PUTs to the presigned link), `request_id` on write methods, and passed on from the request being
handled (`request-id-source`: `req.RequestId`, `kitexx.RequestID(ctx)` or
`kitexx.SubRequestID(ctx, part)`, never a new UUID or timestamp), a comment
on every IDL method, a short title as the first sentence of a gateway method's
comment (`method-title`: at most 12 characters, no punctuation; it is the
/docs sidebar entry, details go after it), `// @limit` on login, registration, password and code
endpoints (`limit`), events inside a transaction through `PublishTx`
(`outbox`), no `time.AfterFunc` for later work (`delay`: use `rt.Delay`), a
Redis lock through `redisx.WithLock` rather than a hand-written `SetNX`
(`lock`), no `sync.Mutex` in handler or repo (`mutex`: one replica only),
`hertzx.Bind` rather than `c.BindAndValidate` (`bind`), at least one test in
every handler package (`tests`), no `code`/`msg` inside any response struct
(`no-envelope`: a method returns its data struct; the gateway handler ends
with `hertzx.OK` / `hertzx.Fail`, which add the `{code, msg, data}` envelope
once, and RPC failures are BizStatusErrors) and no raw `c.JSON` in a gateway
handler (`envelope-write`), one file per gateway domain whose name,
namespace and route segment agree and which includes only common
(`domain-file`, `domain-path`, `domain-include`), paging as `common.PageReq` / `common.PageInfo`, never loose
page / page_size / total fields (`paging`), business errors as
`errcode.Err(code)` with the IDL's message, never a hand-written string
(`error-msg`), an RPC handler split by
topic (`handler-split`: handler.go holds struct, New and Ping; at most
`lint.handler_methods` RPC methods per file, default 8), framework files
untouched, the common library at the
team's version in go.mod (`common-version`) and the project's `kit-common/`
checkout clean (`common-checkout`: go.work makes local builds use it), zlog
for logging, `idl.lock` current, and the schema where the project keeps it
(`schema-in-lib`: no DDL strings, no `db.AutoMigrate`, no `TableName()`
structs in a service; the table is a struct in the project's lib and
`rt.AutoMigrate` applies it). The project's own rules live in the IDL repository's `devkit.yaml`
(so the laptop and CI read one file): `lint.vendor_service` (third-party
calls only there), `lint.money_words` (extra money field names),
`lint.errors_dir` + `lint.error_ranges` (business codes are declared once, in
Thrift files per domain, with a comment each, inside their domain's range,
and used as the generated `errcode` constants: `error-code`, `error-dup`,
`error-range`, `error-comment`),
`lint.report_service` (the only service that may open the report database:
`report-only`), `lint.handler_methods`, `lint.disable`.
Every finding says what to do instead; exit status 1 stops CI. A line opts
out of one rule with `//devkit:lint-ignore <rule>`. `idl-lock` closes the
"push the IDL first" trap of separate repositories: `make gen` writes
`idl.lock`, CI builds against that IDL commit, and lint fails before a push
when the lock lags the checkout or the IDL commit is not on the server. `idl-deps` keeps `idl.deps`
in step with it: `make gen` writes both, and the IDL generations in `idl.deps` are what a service
waits for at start (the dependency gate in kit-common) so that releases need no order. `--idl` points at the
IDL checkout when the service is not inside a project directory (CI).

### Ports

Every service gets its port from the project's port table, `idl/ports.yaml`,
which `devkit ngs` / `devkit nas` write: the first API service is 8080 (the
gateway), every other service owns a block of ten from 8100 on (8100-8109,
8110-8119, ...): `+0` is the RPC or HTTP port, `+1` the callback listener,
`+2`..`+9` are reserved. A service uses the same port in every environment,
and the deployment's values follow it. The table lives in the IDL repository
because every developer has that checkout; commit it together with the new
service's IDL. `--set Port=...` still wins. On a laptop, 9000-9999 are
port-forwards to the shared dev environment (port + 1000) and 10000+ are
operations tools, so nothing a service gets collides with them.

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
| `devkit ngs <service>` | create an RPC service (Kitex) in the current project directory |
| `devkit nas <service>` | create an API service (HTTP, Hertz) in the current project directory |
| `devkit update` | upgrade the devkit-managed files (Makefile, CI, Dockerfile, `main.go`, `conf/README*.md`) to the latest template. Inside a service it updates that service; in the project directory it updates every service. It also refreshes the skill copies `devkit ai` installed. `--check` only shows versions and local changes |
| `devkit self-update` | upgrade devkit itself. `--check` only reports |
| `devkit lint` | check the service against the team's rules (what `make lint` runs); see above |
| `devkit hooks` | install the git pre-push hook that runs `make lint && make test` before every push (what CI's test stage runs; `--remove` to drop it, `git push --no-verify` to skip once). `ngs` and `update` install it too |
| `devkit lock` | make the framework's files and generated code read-only, so the editor says so (`ngs`, `update` and `make gen` do it; `--unlock` undoes it, `--list` shows the files, `-q` is what the Makefile passes). The project's `kit-common/` checkout is locked too. A reminder: lint, the hook and CI still decide |
| `devkit ai [tool...]` | write the pointer file your AI coding tool reads on start (CLAUDE.md, .cursorrules, ...) so it finds `idl/AGENTS.md`, and copy the skills (devkit's built-in four plus the project's `idl/skills/`) to `.agents/skills/` and `.claude/skills/`. No argument lists the tools; `--all` writes every pointer, `--clean` removes what devkit wrote |
| `devkit auth <gitlab-url>` | save a GitLab personal access token (asked interactively) for this machine: devkit's API calls, git over HTTPS and go's private modules all use it (~/.devkit/hosts.yaml, ~/.netrc, GOPRIVATE) |
| `devkit takeover <service>` | take a service of the development environment over from this machine for the requests that carry `X-Dev-Tag: <your tag>`: one ssh tunnel to the environment's node, the local instance registered in Nacos while it listens, a marker file the service reads to run against the environment (tunnelled addresses, no registration, no background workers). Everything else keeps going to the environment's own instances; stop and traffic is back. `takeover list` shows who takes what. Needs kit-common's devroute in the services (on in `APP_ENV=dev` only) and the `takeover` account on the node (infra) |
| `devkit tunnel` | reach the development environment's middleware from this machine: the same ssh connection as takeover with the local forwards only (MySQL 13306, Redis 16379, Nacos 18848, Kafka 19093 ... infrastructure port + 10000, services + 1000), the deployments' credentials printed once to the terminal, nothing written. Run in the project directory; Ctrl+C closes it |
| `devkit report` | the facts of a piece of work across the project's repositories, as a Markdown hand-over with blanks for what only the author knows: repositories, commits, pushed state, files (framework files flagged), IDL methods / structs / error codes, lint and test results, post-deploy check touched or not. `--since 24h\|3d\|<ref>`, `--no-run`, `--json` |
| `devkit pull <project>` | clone every repository of the project's GitLab group you can see into ./<project>/, flat, and set the directory up (devkit.yaml, go.work, kit-common/, hooks, locks, make tools); run again to update. First time: `--gitlab`, `--group` when they differ from the saved/only token and the project name |
| `devkit doctor` | show the state of git, Go, kitex, thriftgo and hz against the team's versions. `--fix` installs what is missing and replaces a generator of another version |
| `devkit version` | version info for bug reports |

**Colours.** On a terminal devkit marks where to look: green for success,
red for failure, yellow for what needs your attention (skipped files, newer
versions, things to do by hand), bold for headings, dim for secondary detail
such as `go mod tidy` output. Redirected output, CI and `DEVKIT_PLAIN=1` get
plain text; `NO_COLOR=1` removes only the colours; `CLICOLOR_FORCE=1` keeps
them when piping, e.g. into `less -R`.

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

Services are the subdirectories created by devkit; `idl/` and `kit-common/` are
never touched. A service that fails does not stop the others. With `--force`
in the project directory devkit first lists the modified files it is about to
overwrite and asks for confirmation (`--yes` skips the question).

### Learning what an update brought

Your config files are never rewritten, so an update cannot add a new setting
to them. Instead it tells you. After updating, devkit prints what changed in
each version you moved through and what you may want to do by hand:

```
what changed:
  0.3.0
    - graceful stop: keeps serving after leaving Nacos, configurable drain time, ...

your own files are never rewritten; you may want to:
  * [0.3.0] Optional, in each conf/<env>.yaml (defaults are 3s and 15s) ...
      shutdown:
        deregister_wait: 3s
        drain_timeout: 15s
```

`conf/README.md` in every service is managed by devkit and always lists every
setting the service's version supports, with defaults.

### One version for the whole team

Every service uses the same Go, Kitex, thriftgo and common library version.
They are decided in one place, the service template, and devkit enforces them:

- A new service gets the team's versions. They cannot be chosen per service or
  per project: `--set KitexVersion=...` and `vars:` in `devkit.yaml` are
  refused for them.
- `devkit update` brings an existing service in line: the generator versions
  in the Makefile, the Go image in the Dockerfile and CI, and the Kitex and
  common versions in `go.mod`.
- `devkit update --check` shows the Kitex version each service really builds
  with, in yellow next to the team's when they differ.
- The code generators are one installation per machine. `ngs` and
  `devkit doctor --fix` replace a `kitex` or `thriftgo` of another version,
  and `make gen` refuses to run with one, so everyone generates the same code.

Values that also live in your own files, such as `Port`, stay as created.

### What `update` does to your files

Files you own (`go.mod`, `idl.mk`, `app/*`, `conf/*.yaml`, `handler/*`, `repo/*`, `deps/*`, `README.md`) are created once
and never touched again; `idl.lock` is written by `make gen`, never by `update`. For the managed files:

| Your file | `devkit update` | with `--force` |
|-----------|-----------------|----------------|
| unchanged since it was generated | replaced by the new version | same |
| edited by you | left alone; new version written next to it as `<file>.new` | replaced, your version kept as `<file>.bak` |
| deleted by you | restored | same |
| no longer part of the template | deleted if unchanged, kept if edited | edited one kept as `.bak` |

### Files you cannot edit by accident

Open the Makefile, `cmd/<service>/main.go`, anything under `kitex_gen/`,
`hertz_gen/` or `.idl/`, hz's generated `router/` files (not `middleware.go`),
`.hz`, `.devkit/manifest.json`, the OpenAPI document or `idl.lock` in an
editor and it says **read-only**, exactly like a library in the Go module
cache. That is `devkit lock`: the files devkit wrote (and replaces on
`update`) plus everything a generator writes lose their write bit. Your own
code (`app/`, `handler/`, `repo/`, `conf/*.yaml`, `deps/`, `go.mod`) stays
writable. `ngs`, `update` and `make gen` lock when they finish; `make gen`
unlocks first so the generators can overwrite. It is a reminder, not a wall:
`devkit lint`'s framework-file rule, the pre-push hook and CI still decide.
The project's `kit-common/` checkout is locked too: it is the library every
local build compiles against (go.work), so a change there belongs in the
library's own repository. `devkit lock --unlock` makes everything writable
again, `--list` prints the set. Git does not store the bit, so a fresh clone is locked by its first
`make gen`.

## Troubleshooting

**`"handler" cannot be used as a service name`**
Some names cannot become a compiling Go service: Go keywords and built-ins
(`map`, `type`, `string`, `error`, `new`, ...), `main`, `init`, `internal`,
`vendor`, `handler` (clashes inside Kitex's generated code), and `idl` /
`kit-common`, which are directories of the project, and `common`, which is
left to the project's own shared code. Pick another name, for example
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
and `hertz-service` templates; format in [docs/registry.md](docs/registry.md)),
`devkit-common` (the shared Go library imported by every service, checked out
in every project as `kit-common/`), plus one IDL repository per project.

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
| `module_prefix`, `idl_repo`, `workspace_dir` | `DEVKIT_MODULE_PREFIX`, ... | machine-wide fallbacks for values a `devkit.yaml` leaves empty |

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
