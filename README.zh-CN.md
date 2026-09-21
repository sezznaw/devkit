# devkit

[English](README.md) | **简体中文**

`devkit` 用一条命令创建一个完整可运行的 [Kitex](https://www.cloudwego.io/zh/docs/kitex/)（Thrift）服务：目录结构、Nacos 注册发现、日志、配置、代码生成、CI 和 Dockerfile 一应俱全。它还会自动安装服务所需的工具，并负责后续升级脚手架文件。

## 开始使用

**1. 安装**

```sh
curl -fsSL https://raw.githubusercontent.com/sezznaw/devkit/main/install.sh | sh
```

**2. 创建服务**

进入用来放（或将要放）项目服务的目录，执行：

```sh
mkdir -p ~/work/shop && cd ~/work/shop
devkit ngs order
```

不需要任何配置。什么都不设置时，服务的 Go module 是 `shop/order`（目录名 + 服务名），项目共用的 IDL 放在本地 git 仓库 `./idl` 里。公共库固定是 `sezznaw/devkit-common`，会 clone 一份到 `./common` 供阅读。

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
cd order && make run      # 使用 conf/local.yaml：你的电脑和本机上的 Nacos（文件里有启动 Nacos 的 docker 命令）
```

同一个项目里之后再建服务，只需要 `devkit ngs <名字>`。

**3. 可选：项目设置**

第一次执行会在服务旁边生成一份带注释的 `devkit.yaml`。等你确定代码放在哪里之后再编辑它；另一个项目就是另一个目录，里面放它自己的 `devkit.yaml`。

```yaml
# ~/work/shop/devkit.yaml （每一项都可以不填）
module_prefix: "gitlab.yourcompany.com/shop"            # order 的 module 就是 gitlab.yourcompany.com/shop/order
idl_repo: "git@gitlab.yourcompany.com:shop/idl.git"     # 或 GitHub 上的 owner/repo
```

| 设置项 | 不填时 | 应该填什么 |
|--------|--------|-----------|
| `module_prefix` | 用目录名，如 `shop/order` | 服务代码将来托管的位置。它是每一行 import 的一部分，最好在建很多服务之前定下来 |
| `idl_repo` | 在本地创建 `idl/` git 仓库，不拉取任何东西 | 项目共用的 IDL 仓库：GitHub 上的 `owner/repo`，或任意完整 git 地址（公司 GitLab，SSH 或 HTTPS 均可）。以后再填没有任何代价 |

**还没有 git 服务器时先开工。** `idl_repo` 留空，先在本地开发。之后在服务器上建好 IDL 项目，在 `idl/` 目录里执行：
`git add -A && git commit -m "add idl" && git remote add origin <地址> && git push -u origin main`，
再把 `<地址>` 填进 `idl_repo`，同事就能拿到同一份 IDL。共用的 `idl/` 仓库正是让每个服务都能为其他服务生成客户端代码的基础。

## 生成的内容

```
~/work/shop/
  devkit.yaml               可选的项目设置
  go.work                   让 Go 工具和 IDE 使用 ./common（只存在于本机）
  idl/                      IDL 仓库的 clone；已替你加好 order/order.thrift
  common/                   团队当前版本的共享库；"跳转到定义"会落在这里
  order/
    cmd/order/main.go       框架入口；由 devkit update 替换，不要修改
    app/app.go              归你：本服务的 Config、依赖的创建和退出清理
    handler/handler.go      归你：RPC 的实现
    conf/local.yaml         你自己的电脑（不设置 APP_ENV）：注册到本机的 Nacos；每个配置项都有说明
    conf/dev.yaml           公共开发服务器；地址和密码来自 ${环境变量}
    conf/uat.yaml           验收测试环境
    conf/prod.yaml          生产配置，密钥来自 ${环境变量}
    conf/README.md          全部配置项的参考文档，由 devkit 保持最新
    kitex_gen/              生成的代码，已被 git 忽略
    idl.mk                  本服务要为哪些 IDL 生成代码（归你编辑）
    Makefile                tools / gen / build / run / test / docker
    .gitlab-ci.yml            以及/或者 .github/workflows/ci.yml：clone IDL 仓库、
                              重新生成、编译、测试（见下面的"CI"一节）
    Dockerfile
```

**common 库是你项目的一部分。** `ngs` 和 `devkit update` 会让 `common/` 始终检出团队当前发布的版本，并在旁边维护一个 `go.work`，其中列出 `common/` 和每个服务。Go 工具和 IDE 因此会把 `github.com/sezznaw/devkit-common` 解析到这个目录，"跳转到定义"打开的是你项目里的代码，而不是只读的模块缓存。由于 `common/` 里正好是你的服务锁定的那个发布版本，你本地编出来的和 CI 编出来的完全一致；`go.work` 只存在于你的机器上，CI 和 Docker 构建都看不到它。如果你的 IDE 只打开了单个服务而没有识别到它，请改为打开项目目录。

不要修改 `common/`：那里的改动只影响你本机的编译结果。devkit 不会丢弃这类改动，但 `update` 和 `update --check` 会明确提示。对库的修改应当提交到它自己的仓库，通过发布新版本到达所有人。`GOWORK=off go build ./...` 可以按 CI 的方式编译。

**框架文件和你的文件。** 框架由专人统一维护，所以任何只属于某个服务的东西都不放在框架文件里。`main.go`、Makefile、CI 文件、Dockerfile 和 `conf/README.md` 会被 `devkit update` 替换；一旦改动其中某个文件，它以后就无法再被升级。其余文件都归你所有，永远不会被改写。要给服务加自己的配置节，或者加一个 Redis 这样的依赖，改 `app/app.go`：在 `Config` 里加字段，在 `Setup` 里创建客户端并注册 `kitexx.OnShutdown`，再传给 `handler.New(...)`。

日常流程：改 `../idl/order/order.thrift`，执行 `make gen`，在 `handler/` 里实现新方法，`make run`。

**调用其他服务。** 每个服务只为自己需要的 IDL 生成代码，清单写在它的 `idl.mk` 里。要在 `order` 里调用 `user`，把对方的 IDL 加进去并重新生成，客户端代码就出现在 `kitex_gen/user/userservice`：

```makefile
# order/idl.mk
IDLS := order/order.thrift user/user.thrift
```
IDL 的改动向 IDL 仓库提 PR。生成的代码不提交，CI 会重新生成。

## CI

服务会得到与其托管平台匹配的流水线配置，依据是 `module_prefix`（没有的话看 `idl_repo`）：

| 托管位置 | 生成的文件 |
|----------|-----------|
| `github.com/...` | `.github/workflows/ci.yml` |
| GitLab（`gitlab.yourcompany.com/...`） | `.gitlab-ci.yml` |
| 还不确定（什么都没配置） | 两份都生成；各平台会忽略对方的文件 |

确定之后，在服务目录里去掉多余的那份：`devkit update --force --set CI=gitlab`（也可以是 `github`、`both`、`none`）。想为整个项目预先指定，在 `devkit.yaml` 里写 `vars: {CI: gitlab}`。

因为生成的代码不提交，两种流水线都会先拉取共用的 IDL 项目，执行 `make tools && make gen`，再编译和测试。IDL 项目的路径取自 `idl_repo`；没有配置 `idl_repo` 时，默认认为服务旁边有一个名为 `idl` 的项目（GitLab 上是 `<分组>/idl`，GitHub 上是 `<所有者>/idl`）。在 GitLab 上，IDL 项目需要放行服务项目的 job token：IDL 项目 → Settings → CI/CD → Job token permissions。

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
| `devkit update` | 把 devkit 管理的文件（Makefile、CI、Dockerfile、`main.go`）升级到最新模板。在服务目录内执行只更新该服务；在项目目录下执行则更新全部服务。`--check` 只显示版本和本地改动 |
| `devkit self-update` | 升级 devkit 自身。`--check` 只报告 |
| `devkit doctor` | 对照团队版本查看 git、Go、kitex、thriftgo 的状态。`--fix` 安装缺失的工具，并替换版本不对的生成器 |
| `devkit version` | 报问题时提供版本信息 |

**颜色。** 在终端里 devkit 用颜色标出重点：绿色表示成功，红色表示失败，黄色表示需要你留意的内容（被跳过的文件、有新版本、需要手动处理的事项），加粗是标题，灰色是次要信息（例如 `go mod tidy` 的输出）。输出被重定向、在 CI 里或设置了 `DEVKIT_PLAIN=1` 时是纯文本；`NO_COLOR=1` 只去掉颜色；`CLICOLOR_FORCE=1` 在管道里也保留颜色，比如配合 `less -R`。

常用的 `ngs` 参数：`--set Port=9000`（模板变量）、`--module <路径>`（覆盖 module 路径）、`--skip-common`、`--skip-idl`、`--no-git`。

### 一次更新全部服务

在项目目录下执行 `update`，而不是进到某个服务里：

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

"服务"指由 devkit 创建的那些子目录，`idl/` 和 `common/` 不会被碰。某个服务更新失败不会中断其他服务。在项目目录下使用 `--force` 时，devkit 会先列出即将被覆盖的、你改过的文件并要求确认（`--yes` 跳过询问）。

### 如何知道一次更新带来了什么

你的配置文件永远不会被改写，所以更新没有办法替你往里面加新的配置项，而是改为告诉你。更新结束后，devkit 会列出你经过的每个版本改了什么，以及你可能需要手动做什么：

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

每个服务里的 `conf/README.md` 由 devkit 管理，始终列出该服务当前版本支持的全部配置项及其默认值。

### 全团队统一版本

所有服务使用相同的 Go、Kitex、thriftgo 和 common 库版本。它们只在一个地方决定，也就是服务模板，并由 devkit 强制执行：

- 新建的服务直接使用团队的版本。这些版本不能按服务或按项目单独指定：`--set KitexVersion=...` 以及 `devkit.yaml` 里的 `vars:` 对它们都会被拒绝。
- `devkit update` 会把已有服务拉齐：Makefile 里的生成器版本、Dockerfile 和 CI 里的 Go 镜像、`go.mod` 里的 Kitex 和 common 版本。
- `devkit update --check` 会显示每个服务实际编译用的 Kitex 版本，和团队版本不一致时以黄色标出并列出团队版本。
- 代码生成器每台机器只有一份。`ngs` 和 `devkit doctor --fix` 会替换掉版本不对的 `kitex` 或 `thriftgo`，`make gen` 遇到版本不对的生成器也会拒绝执行，保证所有人生成的代码一致。

同时存在于你自己文件里的值，比如 `Port`，保持创建时的样子。

### `update` 如何处理你的文件

归你所有的文件（`go.mod`、`idl.mk`、`app/*`、`conf/*.yaml`、`handler/*`、`README.md`）只创建一次，之后永远不会被碰。对于 devkit 管理的文件：

| 你的文件 | `devkit update` | 加 `--force` |
|---------|-----------------|--------------|
| 生成后未改动 | 替换为新版本 | 相同 |
| 被你修改过 | 保持不动，新版本写到旁边的 `<文件>.new` | 替换，你的版本保留为 `<文件>.bak` |
| 被你删除 | 恢复 | 相同 |
| 模板里已不再包含 | 未改动则删除，改过则保留 | 改过的保留为 `.bak` |

## 常见问题

**`"handler" cannot be used as a service name`**
有些名字无法生成可编译的 Go 服务：Go 的关键字和内置标识符（`map`、`type`、`string`、`error`、`new` 等）、`main`、`init`、`internal`、`vendor`、`handler`（与 Kitex 生成的代码内部冲突），以及项目目录已占用的 `idl` 和 `common`。换一个名字即可，例如 `handler-svc`。

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
| `module_prefix`、`idl_repo`、`workspace_dir` | `DEVKIT_MODULE_PREFIX` 等 | `devkit.yaml` 里留空的值在本机的兜底值 |

GitHub token 只会发送给配置的 GitHub 地址；其他服务器上的 IDL 仓库用你自己的 git 凭据 clone。

`devkit.yaml` 另外还支持 `go_private: true`、`component: <名称>`（换用 registry 里的其他组件）和 `vars:`（默认模板变量，如 `NacosAddr`）。

**安装脚本变量：** `GITHUB_TOKEN`（私有仓库）、`GITHUB_REPO`、`REGISTRY_REPO`、`DEVKIT_VERSION`、`INSTALL_DIR`。

**开发与发布**

```sh
make build && make test && make lint
export DEVKIT_REGISTRY_DIR=$PWD/../devkit-registry    # 使用本地模板
```

推送 `vX.Y.Z` tag：GitHub Actions 运行 goreleaser，发布 `devkit_<os>_<arch>.tar.gz` 和 `checksums.txt`，`install.sh` 和 `self-update` 都从那里下载。打 tag 之前请等该提交的 `ci` 工作流通过，发布工作流本身不跑测试。

如果要 fork：GitHub 所有者 `sezznaw` 出现在 module 路径、`install.sh`、发布配置和默认 registry 里，`scripts/set-org.sh <你的GitHub用户名或组织名>` 可以一次性全部改掉。
