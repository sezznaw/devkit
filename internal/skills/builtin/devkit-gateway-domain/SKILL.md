---
name: devkit-gateway-domain
description: 给 Hertz 网关加一个新领域——新建 idl/<网关>/<领域>.thrift（namespace + service + /v1/<领域>/ 路由）、docs.thrift 加分组、make gen 生成 handler/<领域>/ 包、填 handler、声明 RPC 客户端、写测试和部署后检查。用户要加一组新接口而现有领域文件（auth、member、system……）都不合适时使用，例如"加钱包接口""加注单接口"。Add a new domain (thrift file + handler package) to a devkit Hertz gateway.
---

# 给网关加一个领域（例：网关加钱包 wallet）

先读过项目的 `idl/AGENTS.md`。**一个领域 = 一个 thrift 文件 + 一个 namespace + 一个 handler 包**，三者同名、全小写一个词（lint 规则 domain-file；不能用 Go 关键字或 handler 都要 import 的包名，如 time、context、app、deps）。**新领域的名字要先和人确认**（AGENTS.md「必须停下来问人的事」）。

## 1. 新建 `idl/<网关>/<领域>.thrift`

```thrift
include "../common/common.thrift"
namespace go wallet

struct BalanceReq {}
struct Balance { 1: common.Money available  2: common.Money frozen }

service WalletService {
    // 查余额
    Balance GetBalance(1: BalanceReq req) (api.post="/v1/wallet/getBalance")
}
```
- 路径全部 `/v1/<领域>/<动作>`；领域只用自己的结构体，两个领域共用的放 `common/common.thrift`，不 include 别的领域文件。
- 字段、注解、注释、标记的规矩同 `devkit-gateway-api` 第 1 步。

## 2. `idl/<网关>/docs.thrift` 加一行

`// @docs tag wallet: 钱包`（文档页的分组名）。

## 3. 推送 idl，生成

```bash
cd idl && git add -A && git commit -m "feat(api): wallet domain" && git push && git rev-parse --short HEAD
cd ../<网关> && make gen
```
hz 生成 `handler/<领域>/<领域>_service.go`（空函数）、`router/<领域>/`，`router/register.go` 自动挂上。

## 4. 填 handler、声明客户端、测试、检查

- 按 `devkit-gateway-api` 第 4、5 步填函数体、在 `idl.mk` / `deps/deps.go` / `app/app.go` 各加一行声明要调的 RPC 客户端。
- 加 `handler/<领域>/<领域>_test.go`（包名 `<领域>_test`）。
- `app/verify.go` 加一条该领域的只读检查。
- 列表接口要转分页的话加 `handler/<领域>/paging.go`，照已有领域抄。

## 5. 收尾

按 `devkit-finish`。两个人同时做不同领域，可能冲突的只有 `deps/deps.go`、`app/app.go` 各一行和 `docs.thrift` 一行，推送时 rebase 一下就过。

## RPC 服务没有"领域包"

Kitex 要一个结构体实现整个 service，Go 规定一个类型的方法在同一个包里，所以 RPC 服务是**一个包按主题分文件**（`handler.go`、`codes.go`、`<主题>.go`），见 `devkit-rpc-method` 第 4 步。
