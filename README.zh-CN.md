# devkit

[English](README.md) | **简体中文**

`devkit` 用一条命令创建一个完整可运行的 [Kitex](https://www.cloudwego.io/zh/docs/kitex/)（Thrift）服务：目录结构、Nacos 注册发现、日志、配置、代码生成、CI 和 Dockerfile 一应俱全。它还会自动安装服务所需的工具，并负责后续升级脚手架文件。

## 开始使用

**1. 安装**

```sh
curl -fsSL https://raw.githubusercontent.com/sezznaw/devkit/main/install.sh | sh
```

**2. 告诉 devkit 你的项目信息**

进入用来放项目服务的目录，先执行一次 `ngs`。devkit 会在那里生成一份 `devkit.yaml` 让你填写：

```sh
mkdir -p ~/work/shop && cd ~/work/shop
devkit ngs order        # 第一次执行：生成 devkit.yaml 后停止
```

```yaml
# ~/work/shop/devkit.yaml
module_prefix: "github.com/sezznaw"    # 服务 order 的 module 就是 github.com/sezznaw/order
idl_repo: "sezznaw/shop-idl"           # 本项目的 Thrift IDL 仓库
common_repo: "sezznaw/devkit-common"          # 共享库（可选，clone 下来供阅读）
```

devkit 只需要知道这三个值，也正因为如此它可以用于任何项目：另一个项目就是另一个目录，里面放它自己的 `devkit.yaml`。

**3. 创建服务**

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
cd order && make run      # 使用 conf/dev.yaml 启动，本地不需要 Nacos
```

同一个项目里之后再建服务，只需要 `devkit ngs <名字>`。

## 生成的内容

```
~/work/shop/
  devkit.yaml               项目设置（上面那三个值）
  idl/                      IDL 仓库的 clone；已替你加好 order/order.thrift
  common/                   共享库的 clone，供阅读
  order/
    cmd/order/main.go       用 common 启动 Kitex：Nacos、日志、配置
    handler/handler.go      你的 RPC 实现
    conf/dev.yaml           本地配置，关闭 Nacos 注册
    conf/prod.yaml          生产配置，密钥来自 ${环境变量}
    kitex_gen/              生成的代码，已被 git 忽略
    Makefile                tools / gen / build / run / test / docker
    .github/workflows/ci.yml  CI 检出 IDL 仓库、重新生成、编译、测试
    Dockerfile
```

日常流程：改 `../idl/order/order.thrift`，执行 `make gen`，在 `handler/` 里实现新方法，`make run`。IDL 的改动向 IDL 仓库提 PR。生成的代码不提交，CI 会重新生成。

## 工具会自动装好

`ngs` 第一步会检查 git、Go、`kitex` 和 `thriftgo`。没有 Go 就从 go.dev 下载到 `~/.devkit/go`；缺生成器就按模板锁定的版本 `go install`。只有 git 不会代装（macOS 执行 `xcode-select --install`）。

如果有工具是这次装的，把它们加进以后的 shell 只需一次：

```sh
echo 'source "$HOME/.devkit/env"' >> ~/.zshrc    # 或 ~/.bashrc
```

## 命令

| 命令 | 什么时候用 |
|------|-----------|
| `devkit ngs <服务名>` | 在当前项目目录下创建服务 |
| `devkit update` | 在服务目录内执行：把 devkit 管理的文件（Makefile、CI、Dockerfile、`main.go`）升级到最新模板。`--check` 只显示版本和本地改动 |
| `devkit self-update` | 升级 devkit 自身。`--check` 只报告 |
| `devkit doctor` | 查看 git、Go、kitex、thriftgo 的状态。`--fix` 安装缺失的工具 |
| `devkit version` | 报问题时提供版本信息 |

常用的 `ngs` 参数：`--set Port=9000`（模板变量）、`--module <路径>`（覆盖 module 路径）、`--skip-common`、`--skip-idl`、`--no-git`。

### `update` 如何处理你的文件

归你所有的文件（`go.mod`、`conf/*`、`handler/*`、`README.md`）只创建一次，之后永远不会被碰。对于 devkit 管理的文件：

| 你的文件 | `devkit update` | 加 `--force` |
|---------|-----------------|--------------|
| 生成后未改动 | 替换为新版本 | 相同 |
| 被你修改过 | 保持不动，新版本写到旁边的 `<文件>.new` | 替换，你的版本保留为 `<文件>.bak` |
| 被你删除 | 恢复 | 相同 |
| 模板里已不再包含 | 未改动则删除，改过则保留 | 改过的保留为 `.bak` |

## 常见问题

**`this directory has no project settings yet`**
第一次执行时的正常提示：填好生成的 `devkit.yaml`，再执行一次命令。

**`you are inside the service ...`**
你在某个服务目录内部执行了 `ngs`。回到项目目录（含 `devkit.yaml` 的那一层）。

**新开终端里 `kitex: command not found`**
按上面的方法加载 env 文件，或执行 `devkit doctor`。

**`go mod tidy` 时出现 `go: module github.com/sezznaw/devkit-common: ... 404`**
仓库是私有的。取消 `devkit.yaml` 里 `go_private: true` 的注释，并确保 git 能登录 GitHub（SSH key、`gh auth login`，或 `~/.netrc` 里配置 token）。

**`github: GET ...: 404` 或 `401`**
registry 仓库是私有的或配置不对。执行 `devkit config set github_token <token>`，详见下面的维护者部分。

**`API rate limit exceeded`**
匿名访问 GitHub API 每小时限 60 次：`devkit config set github_token <token>`。

## 维护者须知

**仓库。** `devkit`（本 CLI）、`devkit-registry`（`kitex-service` 模板，格式见 [docs/registry.md](docs/registry.md)）、`common`（每个服务都会 import 的共享 Go 库），以及每个项目一个 IDL 仓库。

**全局配置** 在 `~/.devkit/config.yaml`，由 `install.sh` 写入。隐藏命令 `devkit config show|set` 可以编辑它；每个键也都有对应的环境变量。

| 键 | 环境变量 | 作用 |
|----|----------|------|
| `registry_repo` | `DEVKIT_REGISTRY_REPO` | 模板 registry 的 `owner/repo` |
| `registry_dir` | `DEVKIT_REGISTRY_DIR` | 本地 registry 目录，开发模板时使用 |
| `devkit_repo` | `DEVKIT_REPO` | 发布 devkit 的 `owner/repo`（自更新用） |
| `github_token` | `DEVKIT_GITHUB_TOKEN` | 私有仓库需要；同时解除 API 限流 |
| `github_host` | `DEVKIT_GITHUB_HOST` | GitHub Enterprise 地址，默认 `github.com` |
| `module_prefix`、`idl_repo`、`common_repo`、`workspace_dir` | `DEVKIT_MODULE_PREFIX` 等 | 目录里没有 `devkit.yaml` 时的兜底值 |

`devkit.yaml` 另外还支持 `go_private: true`、`component: <名称>`（换用 registry 里的其他组件）和 `vars:`（默认模板变量，如 `NacosAddr`）。

**安装脚本变量：** `GITHUB_TOKEN`（私有仓库）、`GITHUB_REPO`、`REGISTRY_REPO`、`DEVKIT_VERSION`、`INSTALL_DIR`。

**开发与发布**

```sh
make build && make test && make lint
export DEVKIT_REGISTRY_DIR=$PWD/../devkit-registry    # 使用本地模板
```

推送 `vX.Y.Z` tag：GitHub Actions 运行 goreleaser，发布 `devkit_<os>_<arch>.tar.gz` 和 `checksums.txt`，`install.sh` 和 `self-update` 都从那里下载。源码中使用占位组织名 `sezznaw`，用 `scripts/set-org.sh <你的GitHub组织名>` 一次性替换。
