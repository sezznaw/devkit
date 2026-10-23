---
name: devkit-rpc-method
description: 给 Kitex RPC 服务加一个方法或改一个方法——改 idl 的 thrift、加错误码、make gen、在 handler/<主题>.go 实现、写 handler_test、补部署后检查、提交。用户要在某个 RPC 服务里"加接口/加方法/加 RPC/改返回字段"时使用。Add or change an RPC method on a devkit Kitex service.
---

# 给 RPC 服务加一个方法（例：member 服务加 UpdateAvatar）

先读过项目的 `idl/AGENTS.md`（项目长什么样、框架替你做了什么、哪些事要先问人；本项目的样板文件也写在那里）。下面是步骤，各项目一样。顺序**先 IDL，后服务**：服务的 `idl.lock` 记着代码按哪个 IDL 提交生成，`make lint` 会检查 IDL 已提交并推送。

## 1. 改 IDL：`idl/<服务>/<服务>.thrift`

- 加请求、响应结构体和方法。写操作的请求第一个字段 `1: string request_id`。
- **响应只放数据，不放 code / msg**（业务失败在 handler `return nil, errcode.Err(...)`，壳由网关包；lint 规则 no-envelope）。
- 方法上一行写中文注释。
- 列表接口：请求 `1: common.PageReq page`，响应 `items` 加 `common.PageInfo page`（lint 规则 paging）。
- 金额用 `common.Money`，不用 double / i64 分（AGENTS.md「框架替你做了什么」）。

## 2. 新错误码：`idl/<errors_dir>/<领域>.thrift`（`errors_dir` 是 `idl/devkit.yaml` 里 `lint.errors_dir`，通常 `errors`）

两行——上一行中文说明（前端和文档直接显示它），下一行 `const i32 <大驼峰名> = <码>`。号段看 `idl/devkit.yaml` 的 `lint.error_ranges`（目录里的 README 通常也写了）；一个含义一个码，不复用。**编号要和人确认**（AGENTS.md「必须停下来问人的事」）。lint 规则 error-code / error-dup / error-range / error-comment。

## 3. 推送 IDL

```bash
cd idl && git add -A && git commit -m "feat(member): UpdateAvatar" && git push && git rev-parse --short HEAD
```
记下提交号，第 7 步要用。

## 4. 实现：`cd ../<服务> && make gen`

- 方法写在 `handler/<主题>.go`。主题是名词（`profile.go`、`credential.go`、`status.go`）：同一张表或同一件事的方法放一个文件，新主题新建文件；该主题的事件消费者 `OnXxxEvent`、延时任务 `OnXxxExpire` 也放这个文件。`handler.go` 只放结构体、`New`、`Ping`（lint 规则 handler-split）；一个文件最多 8 个 RPC 方法。
- kitex 不生成空函数：忘了实现，编译报 `missing method Xxx`，照着报错补。
- 数据访问写在 `repo/`：一张表一个文件，方法挂在 `*Repo` 上，每条查询带 `ctx`。
- 业务错误：`return nil, errcode.Err(errcode.BadCredentials)`（消息就是 errors 文件里那行注释）；要补细节 `errcode.Errf(errcode.MemberNotFound, "uid 7")`。**不要手写消息字符串**（lint 规则 error-msg），不要写数字码。
- 分页：`page, size, offset := pagex.Normalize(req.Page)`（调用方没传 page 块时 `req.Page` 是 nil，Normalize 按第 1 页、默认条数处理）。
- 需要锁、缓存、延时任务、事件：用 `rt.X`（AGENTS.md「框架替你做了什么」的表），不要自己造。
- 玩家网关调得到的方法（conf 的 callers 规则放行了 `lint.player_gateways` 里的网关）不要按 `req.Uid` 办事——网关被攻破就能拿任意 uid 来调。登录者随上下文传到了这里：只给玩家用的方法 `uid, err := kitexx.LoginUID(ctx)`（无登录者回 1004）；玩家与后台共用的方法先 `kitexx.CheckMemberUID(ctx, req.Uid)`（会员调用者必须等于目标 uid）。uid 确实是被操作的对象（后台替别人改）时，在 IDL 方法注释加 `// @target-uid`（lint 规则 rpc-uid-from-ctx）。
- 调别的服务的写方法（请求带 `request_id`）：`request_id` 传 `req.RequestId` 或 `kitexx.RequestID(ctx)`；一次请求里对同一个方法调多次，各用 `kitexx.SubRequestID(ctx, "fee")` 这样按业务命名的 id。**不要新生成 UUID / 时间戳**——上游重试时下游会再执行一次（lint 规则 request-id-source；客户端中间件也会拒绝这种调用）。消费者、延时任务、job 里先 `ctx = kitexx.WithRequestID(ctx, "<动作>:"+业务键)`（每次重跑相同），再传 `kitexx.RequestID(ctx)`。

## 5. 测试：`handler/<主题>_test.go`

**包名 `handler_test`**（测试要 `app.Setup` 搭整条链，而 `app` 又 import `handler`，写成 `package handler` 会 import cycle）。照抄已有的 `handler_test.go`：`testx.New` 建 Runtime，调 handler 方法，断言返回和错误码。

```bash
make test && go vet ./...
```

## 6. 部署后检查

新方法是**读**操作：扩 `app/verify.go` 里现有的一条检查多调一下它（参数无害，查不到返回 nil）；没有合适的再新开一条。写操作一般不加，汇报里说明。细则见 `devkit-finish`。

## 7. 收尾

按 `devkit-finish`：`devkit report`，填汇报，提交信息 `feat: UpdateAvatar (idl <提交号>)`，推送。推送后 CI 发布；本项目怎么看发布结果、谁会收到通知，见 AGENTS.md。

## 常见错

| 现象 | 原因 / 做法 |
|---|---|
| `make lint` 说 idl.lock 落后 / IDL 未推送 | 第 3 步没做完：先推 idl，再 `make gen` |
| `missing method Xxx` | 第 4 步没实现这个方法 |
| import cycle | 测试包名写成了 `handler`，改 `handler_test` |
| lint error-code / error-msg | 代码里写了数字码或手写消息，改 `errcode.X` / `errcode.Err` |
| lint rpc-uid-from-ctx | 玩家网关可达的方法按 `req.Uid` 办事：改 `kitexx.LoginUID(ctx)` / `kitexx.CheckMemberUID(ctx, req.Uid)`；uid 是被操作对象时 IDL 方法注释加 `// @target-uid` |
| 想加 code/msg 到响应 | 不加：壳由网关出口加（AGENTS.md「框架替你做了什么」、idl README「响应壳」） |
