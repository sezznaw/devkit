---
name: devkit-gateway-api
description: 给 Hertz 网关加一个 HTTP 接口或改一个接口——在对应领域的 thrift 文件加方法和路由、推 idl、make gen、把 hz 生成的空函数换成 hertzx.Bind/OK/Fail、调 RPC、写领域包测试、扩部署后检查。用户要"加一个 /v1/... 接口""给后台加一个接口""前端要一个新接口"时使用。Add or change an HTTP endpoint on a devkit Hertz gateway.
---

# 给网关加一个 HTTP 接口（例：网关加 /v1/member/updateAvatar）

先读过项目的 `idl/AGENTS.md`（项目长什么样、框架替你做了什么、哪些事要先问人、本项目的样板 handler 在哪）。网关的 IDL 是一个目录，**一个领域一个文件、一个 handler 包**：`idl/<网关>/member.thrift`（`namespace go member`、`service MemberService`、路径 `/v1/member/...`）对应 `<网关>/handler/member/`。接口属于哪个领域，就改哪个文件、填哪个包；别的领域不碰。领域不存在就先按 `devkit-gateway-domain` 建。

## 1. 改 IDL：`idl/<网关>/<领域>.thrift`

- 请求结构体每个字段带 `(api.body="xxx")`，校验规则写 `api.vd`（框架校验不过回 1001）。
- **返回类型就是数据结构体**（`Profile GetProfile(1: GetProfileReq req)`），没有数据返回 `common.Empty`。IDL 里不写 code/msg，壳由框架在出口加（lint 规则 no-envelope）。
- 文件头 `include "../common/common.thrift"` 是领域文件唯一允许的 include（lint 规则 domain-include）。
- 方法注解 `(api.post="/v1/<领域>/<动作>")`，路径里的领域必须等于文件名（lint 规则 domain-path）。POST 还是 GET：带用户态或写一律 POST；拿不准问人。
- 方法名不带领域前缀（`GetProfile`，不是 `MemberGetProfile`）。
- 方法注释**第一句是接口标题**（显示在 `/docs` 左栏）：不超过 12 个字的名词短语，不带标点、错误码和括号说明，同一组接口用同一个前缀加「·」或序号（「验证码·查询」「找回密码 1 核对账号」；不用「：」，apidoc 把它当标题分隔符），lint 规则 method-title；规则、错误码、说明从第二句起写。注释里的标记：`// @public` 免登录（**要先问人**）；后台接口 `// @perm <领域>.<动作>`（view / manage）；登录、注册、密码类 `// @limit 30/m`。
- 列表接口：请求 `1: common.PageReq page`，响应 `items` + `common.PageInfo page`。

## 2. 推送 IDL

```bash
cd idl && git add -A && git commit -m "feat(api): member/updateAvatar" && git push && git rev-parse --short HEAD
```

## 3. 生成：`cd ../<网关> && make gen`

hz 在 `handler/<领域>/<领域>_service.go` 补一个空函数，`cmd/<网关>/openapi.yaml` 同时更新。空函数用了 `c.BindAndValidate` 和 `c.JSON(consts.StatusOK, resp)`，两样都不合规矩（lint bind / envelope-write）；有时还缺 `consts` 的 import 而编译报 `undefined: consts`，这是正常的，不要去修它，直接按第 4 步换掉整个函数体。

## 4. 填函数体（照抄本项目已有的网关 handler，AGENTS.md 指出样板）

```go
var req member.UpdateAvatarReq                        // hertz_gen/<领域> 的类型
if !hertzx.Bind(c, &req) {                            // 校验规则在 IDL 的 api.vd，不过框架回 1001
    return
}
uid := kitexx.MustUID(ctx)                            // 需要当前用户时；登录态由框架校验，请求体不带 uid
r, err := deps.Member.UpdateAvatar(ctx, &ser_member.UpdateAvatarReq{RequestId: req.RequestID, Uid: uid, Avatar: req.Avatar}) // kitex_gen 的类型；把请求的 ctx 原样传下去
if err != nil {
    hertzx.Fail(ctx, c, err)                          // BizStatusError 的 code 原样透传（含过载 5003）；其他错误记日志回 5001
    return
}
hertzx.OK(c, &common.Empty{})                         // 有数据就 hertzx.OK(c, profileOf(r.Profile)) 这样转成 hertz_gen 的结构体
```

写接口把 `req.RequestID` 原样传给 RPC 的 `RequestId`；一个接口要对同一个 RPC 写方法调多次时，各用 `kitexx.SubRequestID(ctx, "<业务名>")`。不要新生成 id（lint 规则 request-id-source，客户端中间件也会拒绝）。

- 网关自己判出的错 `hertzx.FailCode(c, errcode.BadParam, "...")`，码用 `kitex_gen/errcode` 常量。
- **不要写 `c.JSON`**（lint 规则 envelope-write）。`make gen` 自动加的 `hertz_gen/common` import 不要删。
- 分页转发：领域包里的 `paging.go` 把 HTTP 的 page 转给 RPC（`hertz_gen/common` 和 `kitex_gen/common` 是两个 Go 类型），照已有领域的写。
- 领域内的辅助函数放同一个包的另一个文件；跨领域共用的放 `internal/shared/`。
- 网关不 import `lib/model`、不读 Redis 业务键，一切数据经 RPC（lint 规则 model-owner、redis-key-owner）；网关自己确实要存的东西（钉住的角色、空闲计时这类）用登记给网关的前缀。

## 5. 第一次调某个 RPC 服务

`idl.mk` 的 `IDLS` 加它的 thrift；`deps/deps.go` 声明客户端；`app/app.go` 的 `Setup` 里 `kitexx.ClientOptions` 创建。这两处是领域之间唯一共用的文件，各加一行。

## 6. 测试：`handler/<领域>/<领域>_test.go`

包名 `<领域>_test`，照抄已有的。需要登录态的用例，用一个注入 `kitexx.WithIdentity(ctx, authx.Identity{Realm, UID})` 的中间件代替 RequireLogin（已有测试里有）。`make test`。

## 7. 本机看一眼

`make run` 后打开 `http://127.0.0.1:<端口>/docs`（端口在 `conf/local.yaml`）：新接口在页面上、能试调、响应是 `{code, msg, data}` 的壳。

## 8. 部署后检查与收尾

读接口：扩 `app/verify.go` 里 `login-and-read`，登录后多调一下新接口；写接口一般不加。然后按 `devkit-finish`：`devkit report`、填汇报、提交 `feat: ... (idl <提交号>)`、推送。

## 后台网关（realm admin）的接口多一步

注释里 `// @perm <领域>.<动作>`（view / manage）；权限点不用登记，框架从 IDL 生成权限点目录，角色页立刻能勾。`.view` 接口顺手扩一条部署后检查。前端怎么配合（生成类型、菜单的权限点）看项目的 AGENTS.md。
