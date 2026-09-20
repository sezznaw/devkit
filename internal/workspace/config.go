package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the per-workspace settings file. It lets one machine work on
// several projects (different IDL repositories, module prefixes) while the
// global ~/.devkit/config.yaml keeps only GitHub host and token.
const ConfigFile = "devkit.yaml"

// DefaultCommonRepo is the shared library cloned to common/ for reading. It is
// fixed for everyone using this framework. The `common_repo` key is still
// parsed (forks of the whole framework need it) but is deliberately not
// offered in the devkit.yaml template or the docs: it only chooses what is
// cloned for reading, while the library a service really depends on is the
// CommonModule in the template's go.mod, so offering it misled people into
// thinking it swaps the library.
const DefaultCommonRepo = "sezznaw/devkit-common"

// Config is <workspace>/devkit.yaml.
type Config struct {
	ModulePrefix string `yaml:"module_prefix,omitempty"`
	// IdlRepo is "owner/repo" (or a git URL / local path) of the Thrift IDL repository.
	IdlRepo string `yaml:"idl_repo,omitempty"`
	// CommonRepo overrides DefaultCommonRepo. Undocumented on purpose, see there.
	CommonRepo string `yaml:"common_repo,omitempty"`
	// GoPrivate marks the organisation's modules as private: ngs then adds
	// <module_prefix org> to GOPRIVATE so `go get` bypasses proxy.golang.org.
	GoPrivate bool `yaml:"go_private,omitempty"`
	// Component overrides the registry component `ngs` scaffolds from.
	Component string `yaml:"component,omitempty"`
	// Vars are default template variables for `ngs` (e.g. NacosAddr).
	Vars map[string]string `yaml:"vars,omitempty"`
}

// Find walks up from dir looking for devkit.yaml and returns the directory
// containing it, or "" if none is found.
func Find(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ConfigFile)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// LoadConfig reads <dir>/devkit.yaml. A missing file yields an empty config.
func LoadConfig(dir string) (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, ConfigFile), err)
	}
	return cfg, nil
}

// SaveConfig writes <dir>/devkit.yaml.
func SaveConfig(dir string, cfg *Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	header := "# devkit workspace settings. Commit this file if the workspace is a repository.\n"
	return os.WriteFile(filepath.Join(dir, ConfigFile), append([]byte(header), data...), 0o644)
}

// WriteTemplate creates <dir>/devkit.yaml as a commented file for the user to
// fill in by hand. Known values (from the global config) are pre-filled.
func WriteTemplate(dir string, known *Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	q := func(s string) string { return fmt.Sprintf("%q", s) }
	content := strings.Join([]string{
		"# devkit project settings / devkit 项目设置",
		"#",
		"# This file belongs to the project directory, next to your services. Every",
		"# value is optional: `devkit ngs <service>` works with all of them empty.",
		"# 本文件放在项目目录下，与各个服务同级。每一项都可以不填：",
		"# 全部留空时 `devkit ngs <服务名>` 也能正常工作。",
		"",
		"# ---------------------------------------------------------------------------",
		"# module_prefix",
		"#   The Go module path of a new service is <module_prefix>/<service>. Set it",
		"#   to where the code will be hosted, for example",
		"#       gitlab.yourcompany.com/shop   ->  gitlab.yourcompany.com/shop/order",
		"#   Empty: the name of this directory is used (shop/order).",
		"#   It becomes part of every import line, so choose it before creating many",
		"#   services. Changing it later means a search-and-replace in each service.",
		"#",
		"#   新服务的 Go module 路径是 <module_prefix>/<服务名>。应填代码将来托管的位置，例如",
		"#       gitlab.yourcompany.com/shop   ->  gitlab.yourcompany.com/shop/order",
		"#   留空：使用本目录的名字（shop/order）。",
		"#   它会出现在每一行 import 里，最好在建很多服务之前定下来；之后再改，",
		"#   需要在每个服务里做一次全局替换。",
		"# ---------------------------------------------------------------------------",
		"module_prefix: " + q(known.ModulePrefix),
		"",
		"# ---------------------------------------------------------------------------",
		"# idl_repo",
		"#   The git repository holding the Thrift IDLs of ALL services of this project.",
		"#   It is cloned to ./idl; every service generates code from it, which is how",
		"#   one service can call another. Accepts:",
		"#       owner/repo                                   (on GitHub)",
		"#       git@gitlab.yourcompany.com:shop/idl.git      (any full git URL, SSH)",
		"#       https://gitlab.yourcompany.com/shop/idl.git  (any full git URL, HTTPS)",
		"#   Empty: a local git repository ./idl is created and nothing is cloned.",
		"#   You can work like that and push ./idl to a server later; setting this",
		"#   value afterwards costs nothing.",
		"#",
		"#   存放本项目【所有服务】Thrift IDL 的 git 仓库。它会被 clone 到 ./idl，",
		"#   每个服务都从这里生成代码，服务之间能够互相调用靠的就是它。可以填：",
		"#       owner/repo                                   （GitHub 上的仓库）",
		"#       git@gitlab.yourcompany.com:shop/idl.git      （任意完整 git 地址，SSH）",
		"#       https://gitlab.yourcompany.com/shop/idl.git  （任意完整 git 地址，HTTPS）",
		"#   留空：在本地创建 git 仓库 ./idl，不 clone 任何东西。可以先这样开发，",
		"#   以后再把 ./idl 推到服务器；之后补填这一项没有任何代价。",
		"# ---------------------------------------------------------------------------",
		"idl_repo: " + q(known.IdlRepo),
		"",
		"# ---------------------------------------------------------------------------",
		"# go_private  (default false / 默认 false)",
		"#   Set to true when your Go modules are private. `devkit ngs` then adds the",
		"#   organisation part of module_prefix to GOPRIVATE, so `go get` fetches them",
		"#   with your git credentials instead of the public Go proxy.",
		"#   当你的 Go module 是私有仓库时设为 true。`devkit ngs` 会把 module_prefix 的",
		"#   组织部分加入 GOPRIVATE，`go get` 就会用你的 git 凭据拉取，而不走公共代理。",
		"# ---------------------------------------------------------------------------",
		"# go_private: true",
		"",
		"# ---------------------------------------------------------------------------",
		"# vars",
		"#   Default values for the variables of the service template, applied to every",
		"#   service created here (a --set flag on the command line still wins).",
		"#   服务模板变量的默认值，对在本目录下创建的每个服务生效（命令行的 --set 优先）。",
		"#",
		"#     Port       listen port written to conf/*.yaml          default \"8888\"",
		"#                写入 conf/*.yaml 的监听端口",
		"#     NacosAddr  Nacos address written to conf/dev.yaml      default 127.0.0.1:8848",
		"#                写入 conf/dev.yaml 的 Nacos 地址",
		"#     CI         which pipeline file to generate: github, gitlab, both, none",
		"#                default: decided from module_prefix / idl_repo",
		"#                生成哪种 CI 配置；默认根据 module_prefix / idl_repo 的域名判断",
		"# ---------------------------------------------------------------------------",
		"# vars:",
		"#   Port: \"8888\"",
		"#   NacosAddr: 127.0.0.1:8848",
		"#   CI: gitlab",
		"",
	}, "\n")
	return os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0o644)
}
