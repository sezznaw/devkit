---
name: devkit-finish
description: 任务收尾——改完代码后跑 devkit report 拿到事实（改了哪些仓库和文件、IDL 新增了什么、lint/test 结果、部署后检查有没有加），补上部署后检查，按模板写汇报并提交推送。做完任何开发任务、准备提交或推送、要向同事汇报时使用。Finish a task on a devkit project - run devkit report, add post-deploy check, write the hand-over, commit and push.
---

# 任务收尾

目的：每个任务结束时交出一份**事实正确、格式一样**的汇报。事实（改了什么、测了什么）由 `devkit report` 从 git 和构建里取，你只写机器不知道的部分。

## 步骤

1. **先把活做完**：改动的每个服务 `make gen && make lint && make test` 通过。没过就先修，不要带着失败收尾。

2. **部署后检查（AGENTS.md「给服务加一条部署后检查」）**。这次任务如果新增了对外的**读**能力——RPC 的查询方法、网关的读接口、后台的 `.view` 接口——在同一次提交里给它加一条只读检查：
   - 网关：扩 `app/verify.go` 里 `login-and-read` 那条，登录后多调一下新接口（公开接口直接 `gw.Post`）。
   - RPC 服务：`app/verify.go` 的 `Checks(cfg, rt)` 加一项 `verifyx.Check{Name, Description, Run}`，用自己的客户端调新方法，参数无害（按 `VERIFY_USERNAME` 查、或分页取 1 条）；查不到数据返回 nil，返回 error 才算失败。
   - 写操作（下注、扣款、改状态）**不加**，除非对测试账号自己的数据幂等；拿不准就不加，在汇报里写原因。
   - 一个服务的检查十条以内、总共几秒跑完；本机 `make verify` 跑一遍。

3. **拿事实**。在项目目录（有 `devkit.yaml` 的那一层）运行：
   ```bash
   devkit report --since 24h        # 默认：最近 24 小时我的提交 + 未提交改动，并跑 make lint / make test
   devkit report --since 3d         # 任务做了几天
   devkit report --since <提交号>    # 从某个提交之后
   ```
   它输出 Markdown；`--json` 给程序读。看三处：
   - **「⚠ 要先处理」**有内容就先处理：改了框架文件（归 devkit update 管，复原它）、kit-common 检出被改（`git -C kit-common checkout .`）、新加了 `@public` 或删了接口但没问过人（去问）。
   - **「验证」**表里有 ✗ 就回去修，修完重跑。
   - 范围不对（漏了仓库或多了别人的提交）就换 `--since`。

4. **填空**。把输出里每个 `（填写：…）` 换成一两句话：做了什么功能、给谁用；`app/verify.go` 加了哪条或为什么没加；本机怎么验证过的（例如 `make run` 后在 `/docs` 调了新接口、测试工具登录后调了它）；还要人做的事（运维加变量、前端改页面）；没做的事。不要删机器填好的部分，不要改表格里的数字。

5. **提交与推送**（AGENTS.md「提交与收尾」）。顺序：**idl 先推**，再推服务；网关最后。服务的提交信息第一行 `feat: <一句话> (idl <IDL 提交号>)`，空一行，正文放汇报里「做了什么」「接口与错误码」「验证」三段（表格可以去掉，留结论）。推送时 pre-push 钩子再跑一遍 lint 和测试。
   ```bash
   git add -A && git commit -F <(printf 'feat: UpdateAvatar (idl abc1234)\n\n<汇报正文>') && git push
   ```

6. **交出去**。把填好的完整 Markdown 原样给同事（聊天里直接贴，或按团队习惯放到文档里）。最后一行写清推送后发布要多久、在哪看结果（项目的 AGENTS.md 写了）。

## 不要做的

- 不要凭记忆写"改了哪些文件""测试都过了"——这些从 `devkit report` 来。
- 不要改 report 里机器填的数字和文件名。
- 不要在 `--since` 范围里"为了好看"去掉警告：警告是给人看的。
