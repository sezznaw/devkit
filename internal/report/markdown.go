package report

import (
	"fmt"
	"strings"
)

var kindNames = map[string]string{
	"idl": "IDL", "infra": "基础设施", "rpc": "RPC 服务", "gateway": "网关", "frontend": "前端",
	"lib": "共用库", "framework": "框架检出", "other": "其他",
}

// Fill marks a sentence the author (or their AI tool) must write; the
// machine cannot know it. Everything else in the Markdown is measured.
const Fill = "（填写："

// Markdown renders the report for people: facts filled in, judgement
// calls left as marked blanks.
func (r *Report) Markdown() string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	w("# 开发汇报 · %s\n\n", r.Date)
	w("范围：%s；作者 %s；`devkit report --since %s`\n\n", rangeText(r.Since), orDash(r.Author), r.Since)

	w("## 做了什么\n\n")
	w("%s一两句话：什么功能、给谁用、为什么这样做）\n\n", Fill)

	if len(r.Warnings) > 0 {
		w("## ⚠ 要先处理\n\n")
		for _, s := range r.Warnings {
			w("- %s\n", s)
		}
		w("\n")
	}

	w("## 改了哪些仓库\n\n")
	if len(r.Repos) == 0 {
		w("这个范围里没有改动。范围不对就换 `--since`（如 `--since 3d` 或 `--since <提交号>`）。\n\n")
	} else {
		w("| 仓库 | 类型 | 提交 | 已推送 | 未提交文件 | 流水线 |\n|---|---|---|---|---|---|\n")
		for _, rp := range r.Repos {
			pushed := "✓"
			if rp.Unpushed > 0 {
				pushed = fmt.Sprintf("✗ %d 个未推", rp.Unpushed)
			}
			if len(rp.Commits) == 0 {
				pushed = "—"
			}
			link := "—"
			if rp.WebURL != "" {
				link = fmt.Sprintf("[pipelines](%s/-/pipelines?ref=%s)", rp.WebURL, rp.Branch)
			}
			w("| %s | %s | %d | %s | %d | %s |\n", rp.Name, kindNames[rp.Kind], len(rp.Commits), pushed, rp.Uncommitted, link)
		}
		w("\n")
		for _, rp := range r.Repos {
			w("### %s（%s）\n\n", rp.Name, kindNames[rp.Kind])
			for _, c := range rp.Commits {
				mark := "已推"
				if !c.Pushed {
					mark = "未推"
				}
				w("- `%s` %s（%s）\n", c.Hash, c.Subject, mark)
			}
			if len(rp.Files) > 0 {
				w("- 文件：")
				var parts []string
				for _, f := range rp.Files {
					s := fmt.Sprintf("`%s`", f.Path)
					switch f.Status {
					case "A":
						s += " 新"
					case "D":
						s += " 删"
					case "R":
						s += " 改名"
					}
					if f.Uncommitted {
						s += "（未提交）"
					}
					parts = append(parts, s)
				}
				w("%s\n", strings.Join(parts, "、"))
			}
			if rp.Upgraded {
				w("- 这次包含 `devkit update`（.devkit/manifest.json 变了），框架文件的变化是升级带来的。\n")
			}
			w("\n")
		}
	}

	if !r.IDL.Empty() {
		w("## 接口与错误码（来自 idl 的 diff）\n\n")
		for _, m := range r.IDL.Methods {
			if m.Route != "" {
				w("- 新接口 `%s`（%s · %s）\n", m.Route, m.File, m.Name)
			} else {
				w("- 新方法 `%s.%s`（%s）\n", orDash(m.Service), m.Name, m.File)
			}
		}
		for _, s := range r.IDL.Structs {
			w("- 新 %s `%s`（%s）\n", s.Kind, s.Name, s.File)
		}
		for _, c := range r.IDL.Codes {
			w("- 新错误码 `%d %s`：%s（%s）\n", c.Code, c.Name, orDash(c.Message), c.File)
		}
		for _, d := range r.IDL.Removed {
			w("- **删除** %s `%s`（%s）\n", d.Kind, d.Name, d.File)
		}
		for _, f := range r.IDL.Flags {
			w("- 新标记 `%s`（%s）\n", f.Name, f.File)
		}
		w("\n")
	}

	w("## 验证\n\n")
	if len(r.Checks) > 0 {
		w("| 仓库 | 命令 | 结果 | 用时 |\n|---|---|---|---|\n")
		for _, c := range r.Checks {
			res := "✓"
			if !c.OK {
				res = "✗"
			}
			if c.OK && c.Detail != "" {
				res += " " + c.Detail
			}
			w("| %s | `%s` | %s | %s |\n", c.Repo, c.Name, res, c.Took)
		}
		w("\n")
		for _, c := range r.Checks {
			if !c.OK {
				w("`%s` 在 %s 的输出末尾：\n\n```\n%s\n```\n\n", c.Name, c.Repo, c.Detail)
			}
		}
	} else {
		w("lint 和测试没有在这次汇报里跑（`devkit report` 不带 `--no-run` 会跑）。\n\n")
	}
	verifyLines := 0
	for _, rp := range r.Repos {
		if rp.Kind != "rpc" && rp.Kind != "gateway" {
			continue
		}
		verifyLines++
		if rp.VerifyChanged {
			w("- %s 部署后检查：`app/verify.go` 已改。%s加了哪一条、查什么）\n", rp.Name, Fill)
		} else {
			w("- %s 部署后检查：`app/verify.go` 没改。%s为什么没加——写操作 / 已有检查覆盖 / 其他）\n", rp.Name, Fill)
		}
	}
	if verifyLines > 0 {
		w("\n")
	}
	w("- 本机怎么验证过的：%s例如 make run 后在 /docs 调了新接口、测试台登录后调 getProfile）\n\n", Fill)

	w("## 还要人做的事\n\n%s运维加变量、前端改页面、DBA 跑迁移……没有就写“无”）\n\n", Fill)
	w("## 没做的事和原因\n\n%s没有就写“无”）\n", Fill)
	return b.String()
}

func rangeText(since string) string {
	if m := reDuration.FindStringSubmatch(since); m != nil {
		return "最近 " + m[1] + map[string]string{"m": " 分钟", "h": " 小时", "d": " 天", "w": " 周"}[m[2]] + "内我的提交和未提交改动"
	}
	if reDate.MatchString(since) {
		return since + " 以来我的提交和未提交改动"
	}
	return since + " 之后的提交和未提交改动"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
