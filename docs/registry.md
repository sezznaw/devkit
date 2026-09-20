# Component registry format

The registry is a plain git repository on GitHub (`registry_repo` in the
devkit config, `owner/repo`). devkit reads it through the GitHub API (raw
files and tarballs), so no git clone is needed on the user's machine. Public
repositories work without a token; private ones need `github_token`.

A component is a versioned tree of template files. Today there is one,
`kitex-service`, the whole-project template behind `devkit ngs`; `devkit.yaml`
can select another with `component: <name>`.

```
registry.json                         index of latest versions (read from `main`)
components/
  kitex-service/
    component.json                    metadata
    files/                            written into the new service directory
      cmd/{{.Service}}/main.go.tmpl
      Makefile.tmpl
      ...
    idl/                              written into the project's IDL checkout
      {{.Service}}/{{.Service}}.thrift.tmpl
```

## registry.json

```json
{
  "schema": 1,
  "components": {
    "kitex-service": { "version": "0.1.0", "description": "..." }
  }
}
```

`version` is what `devkit ngs` installs and what `devkit update` compares
the installed version against.

## component.json

```json
{
  "name": "kitex-service",
  "version": "0.1.0",
  "description": "...",
  "deps": [],
  "vars": [
    { "name": "Service", "description": "...", "required": true },
    { "name": "Port", "description": "...", "default": "8888" }
  ],
  "hooks": {
    "post_install": ["make gen && go mod tidy"],
    "post_update":  ["go mod tidy"]
  },
  "once": ["go.mod", "conf/*.yaml", "handler/*", "README.md"],
  "changelog": [
    { "version": "0.3.0",
      "changes": ["graceful stop settings"],
      "action":  ["Optional, in conf/<env>.yaml:\nshutdown:\n  drain_timeout: 15s"] }
  ]
}
```

* `vars` are filled by `ngs` (`Service`, `Module`, `IdlRepo` as the IDL
  project's *path*, `CI` as `github`/`gitlab`/`both`, `GoPrivate`), by
  `vars:` in `devkit.yaml`, or by `--set Name=value`. The values used at
  install time are stored in the service's manifest and reused by `update`.
* A var with `"track": true` follows the template: when a new template
  version changes its default, existing services get the new value on
  `devkit update`, unless someone set it explicitly (`--set`, `vars:` in
  `devkit.yaml`), in which case it stays pinned until released with
  `--set Name=`. Use it for library and tool versions. Vars without `track`
  keep the value the service was created with, which is right for anything
  that also appears in files the developer owns (a `Port` that changed in the
  managed Dockerfile would no longer match their `conf/*.yaml`).
* `deps` are other components installed first (latest version).
* Hooks run with `sh -c` in the service directory and may use template variables.
* `changelog` has one entry per version: `changes` says what is different,
  `action` what the developer may want to do by hand (multi-line strings are
  printed as an indented block, so include the exact snippet). `devkit update`
  prints the entries between the installed and the new version. It exists
  because `once` files are never rewritten: a new config setting reaches
  existing services only through an `action`.
* `once` lists glob patterns (matched against the service-relative path) of
  files written on first install only. They are not recorded in the manifest,
  so `update` never touches them. Use it for everything the developer owns
  after scaffolding: go.mod, configs, handlers, README.

## Files and templates

Everything under `files/` is written into the service directory, keeping the
relative path. Two transformations apply:

* Path segments may contain template expressions: `cmd/{{.Service}}/main.go`.
* A file whose name ends in `.tmpl` is rendered with Go `text/template` and the
  suffix is dropped. A template that renders to nothing but whitespace
  produces **no file**: wrap a whole file in `{{- if ... -}} ... {{- end}}` to
  make it exist only for some variable values (this is how `kitex-service`
  ships either `.gitlab-ci.yml`, the GitHub workflow, or both, depending on
  the `CI` variable). Requires devkit 0.1.3 or newer; older versions write an
  empty file instead. Any other file is copied byte for byte, so `{{` in a
  Markdown file is safe. To emit a literal `${{ x }}` (GitHub Actions) from a
  `.tmpl` file write `${{"{{"}} x {{"}}"}}`.

Variables available in templates:

| Variable            | Source                                          |
|---------------------|-------------------------------------------------|
| `.Module`           | go.mod of the service, or `--set Module` (ngs)  |
| `.Project`          | base name of the service directory              |
| `.ComponentName`    | from component.json                             |
| `.ComponentVersion` | from component.json                             |
| `.<Var>`            | each entry of `vars`                            |

Referencing an undefined variable is an error, so typos fail loudly.

| Function | `"order-item"` becomes |
|----------|------------------------|
| `title` / `pascal` | `OrderItem` |
| `camel`  | `orderItem` |
| `snake`  | `order_item` |
| `kebab`  | `order-item` |
| `upper` / `lower` | `ORDER-ITEM` / `order-item` |

## The `idl/` tree

Rendered with the same rules, but written into `<project>/idl/` (the clone of
the project's IDL repository) instead of the service, and never over an
existing file. This is how `kitex-service` seeds `idl/<service>/<service>.thrift`.

## Publishing a version

1. Edit the component and bump `version` in its `component.json`.
2. Update `registry.json` on `main` with the same version.
3. Tag the commit `<name>/v<version>`, e.g. `kitex-service/v0.2.0`, and push the tag.

Tags are immutable: devkit caches every fetched version under
`~/.devkit/cache/<name>/<version>` and never re-downloads it.

## How `devkit update` treats files

| File state on disk | Action |
|--------------------|--------|
| unchanged since install | overwritten with the new version |
| modified by the user | left alone; new content written to `<file>.new` (`--force` overwrites and keeps `<file>.bak`) |
| deleted by the user | restored |
| no longer produced by the component, unchanged | deleted |
| no longer produced, modified | kept, reported |

The manifest records the hash of the *template* output, so `devkit update
--check` keeps reporting a skipped file as modified until it is merged.

## Developing templates locally

Point devkit at a checkout instead of GitHub; it then reads the working tree
and ignores tags:

```sh
export DEVKIT_REGISTRY_DIR=~/src/devkit-registry
mkdir /tmp/proj && cd /tmp/proj
printf 'module_prefix: "github.com/sezznaw"\nidl_repo: "sezznaw/shop-idl"\n' > devkit.yaml
devkit ngs order && cd order && go build ./... && go vet ./...
```

`idl_repo` may also be a local path to a git repository.
