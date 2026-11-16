<div align="center">

# Axon

**Go CLI 只写一次，同时交付给人、脚本和 AI Agent。**

[![CI](https://github.com/jieraltjp/Axon/actions/workflows/ci.yml/badge.svg)](https://github.com/jieraltjp/Axon/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/jieraltjp/axon.svg)](https://pkg.go.dev/github.com/jieraltjp/axon)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

> [!WARNING]
> **Axon 目前是早期原型。** 核心功能已经可用并有测试覆盖，但在 v1.0 之前 API 还会调整。需要 Go 1.25+。
> 非常欢迎反馈：[提交 issue](https://github.com/jieraltjp/Axon/issues)。

---

## 为什么

你的 CLI 的用户不再只有一种：

- **有 shell 的 Agent**（Claude Code、Codex……）：配上一个说明用法的 Skill 效果最好
- **没有 shell 的 Agent**（Claude Desktop、聊天应用、IDE 助手）：只能通过 **MCP** 使用你的工具
- **脚本和程序**：需要 **JSON 输出、结构化错误和稳定的退出码**
- **人**：仍然需要帮助信息、表格，以及危险操作前的确认

要同时支持它们，通常意味着在 CLI 之外再写 MCP Server、Skill 文件、JSON 模式和错误格式，并且一直保持同步。

**用 Axon，每个命令只需写一次类型化的 Go 函数，所有接口都从这一份定义而来。** [notes 示例](examples/notes)总共 236 行，包含 MCP Server、Skill、`schema` 命令、JSON 模式和确认流程。用普通 Cobra 写的同一个 CLI 有 261 行，而这些一样都没有。

> **实测结论。** 在我们的[对比测试](bench/RESULTS.md)中，当前的模型（Sonnet 和 Haiku）使用普通 Cobra CLI 的效果和 Axon CLI 相差不大；在 Claude Code 中，MCP、Skill + CLI、只有 CLI 三种方式的成本也差不多。Axon 不会让 Agent 变聪明，它的价值在于：CLI 只写一次，就能覆盖所有类型的用户。

## 快速上手

```bash
go get github.com/jieraltjp/axon
```

```go
package main

import "github.com/jieraltjp/axon"

type GreetInput struct {
	Name  string `arg:"" help:"Who to greet"`
	Shout bool   `help:"Use capitals"`
}

type Greeting struct {
	Message string `json:"message"`
}

func main() {
	app := axon.New("hello", axon.Description("Says hello"))
	axon.Command(app, "greet", "Greet someone",
		func(ctx *axon.Context, in GreetInput) (Greeting, error) {
			msg := "Hello, " + in.Name
			if in.Shout {
				msg = "HELLO, " + in.Name + "!"
			}
			return Greeting{Message: msg}, nil
		})
	app.Run()
}
```

这一份定义可以得到：

```console
$ hello greet Axon                  # 终端里：可读的输出
message:  Hello, Axon

$ hello greet Axon | cat            # 管道里：自动输出 JSON
{"message":"Hello, Axon"}

$ hello greet                       # 结构化错误，稳定的退出码
{"error":{"kind":"usage","code":"MISSING_ARGUMENT","message":"missing required <name>","hint":"run 'hello greet --help'","retryable":false}}

$ hello schema greet                # Agent 读取的输入 / 输出 JSON Schema
$ hello skills export ./skills      # Agent Skill（SKILL.md）
$ hello mcp serve                   # 同一批命令作为 MCP 工具
```

## 接入 Agent

```bash
go install .                                # agent install 需要已安装的程序
hello agent install claude-desktop          # 写入 Claude Desktop 的 MCP 配置
hello agent install cursor                  # 写入 ~/.cursor/mcp.json
hello agent install codex                   # 写入 ~/.codex/config.toml
hello agent install claude-code             # 安装 Skill 到 ~/.claude/skills
hello agent list                            # 查看安装状态
hello agent uninstall cursor
```

`agent install` 只修改自己的那一项；修改前会把原文件备份为 `.bak`；遇到无法解析的配置文件时拒绝修改；支持 `--dry-run` 预览。危险命令默认不会通过 MCP 暴露，除非加上 `--allow-destructive`。

## 已经有 Cobra CLI 了？

不需要重写。先把已有的 CLI 整个挂载进来，再逐个改写命令：

```go
app := axon.New("mycli")
axon.MountRoot(app, legacyRootCmd)                  // 整个已有 CLI，原样保留
axon.Command(app, "status", "Show status", status)  // 新写或改写的命令是类型化的
app.Run()
```

挂载后可以直接替换原来的 CLI：命令保留自己的 flag（包括根命令的持久 flag）、初始化钩子和根命令本身的动作，并出现在帮助信息中，错误使用 Axon 的错误格式。只想挂载部分命令时用 `axon.Mount(app, cmds...)`。用 `axon.Command` 改写之后，它们才会加入 `schema`、Skill 和 MCP，因为这些需要知道命令的类型。

[`examples/yq`](examples/yq) 用这种方式把真实的 yq CLI 迁移到了 Axon，并加了两个类型化命令 `query` 和 `set`，Agent 可以通过 MCP 使用。

## 试一试示例

```bash
git clone https://github.com/jieraltjp/Axon.git && cd Axon
go run ./examples/notes list              # 表格，给人看
go run ./examples/notes list | cat        # JSON，给 Agent 和脚本
go run ./examples/notes list --fields id,title
go run ./examples/notes delete 1          # 先询问确认；Agent 调用时不带 --yes 会以退出码 4 拒绝
go run ./examples/notes delete 1 --dry-run
go run ./examples/notes schema search
```

## 参考

### 输入：struct 标签

| 标签 | 含义 |
| --- | --- |
| `arg:""` | 位置参数，按字段顺序；没有默认值时为必填 |
| `default:"10"` | 默认值 |
| `required:""` | 必须提供该 flag |
| `enum:"a,b,c"` | 允许的取值 |
| `env:"VAR"` | 未提供时从环境变量读取 |
| `help:"..."` | 描述，用于帮助、schema、Skill 和 MCP |
| `short:"n"` | 单字母别名 |
| `name:"x"` | 覆盖 flag 名（默认是字段名的 kebab-case） |
| `axon:"-"` | 忽略该字段 |

支持的类型：`string`、`bool`、`int`、`int64`、`float64`、`time.Duration`、`[]string`、`[]int`。切片类型的 flag 可以重复传入，也可以用逗号分隔。输出可以是任何能编码为 JSON 的值。

### 命令选项

| 选项 | 作用 |
| --- | --- |
| `axon.Write()` | 标记为会修改状态 |
| `axon.Destructive()` | 需要确认：终端里交互询问，否则必须带 `--yes`；默认不通过 MCP 暴露 |
| `axon.SupportsDryRun()` | 添加 `--dry-run`；在处理函数中检查 `ctx.DryRun()` |
| `axon.Example(...)` | 示例，用于帮助、schema 和 Skill |
| `axon.Aliases(...)` | 别名 |
| `axon.Hidden()` | 在所有地方隐藏 |
| `axon.Exclude(axon.ProjectionMCP, axon.ProjectionSkill)` | 不出现在 MCP 或 Skill 中 |
| `axon.Group(app, "tag", "...")` | 为 `tag add` 这样的命令组添加描述 |

### 输出与错误

| Flag | 作用 |
| --- | --- |
| `-o text\|json\|ndjson` | 输出格式；默认终端里为 text，其他情况为 JSON（也可用 `AXON_OUTPUT`） |
| `--fields a,b` | 只保留这些字段 |
| `--envelope` | 包装为 `{"ok":true,"data":...}` 或 `{"ok":false,"error":...}` |

返回 `axon.NotFound(...)`、`axon.Usage(...)`、`axon.PermissionDenied(...)`、`axon.Unavailable(...)` 或 `axon.Errorf(...)` 可以控制错误类型、错误码和提示。其他错误都会作为一般错误处理。

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 错误 |
| 2 | 用法错误 |
| 3 | 不存在 |
| 4 | 需要确认 |
| 5 | 权限不足 |
| 6 | 临时失败，可重试 |

### 内置命令

| 命令 | 用途 |
| --- | --- |
| `schema [command]` | 机器可读的描述（`axon/v0`） |
| `skills export [dir]` | 生成 Agent Skill；命令很多时自动拆分为索引和参考文件 |
| `mcp serve [--allow-destructive]` | stdio 上的 MCP Server（支持协议版本 2024-11-05 到 2025-11-25，不增加额外依赖） |
| `agent install\|uninstall\|list` | 接入 Claude Code、Claude Desktop、Cursor 或 Codex |
| `completion` | Shell 补全（来自 Cobra） |

需要更底层的控制时，`app.Cobra()` 会返回底层的 `*cobra.Command`。

## 对比

|  | Cobra | ophis | incur | FastMCP `generate-cli` | **Axon** |
| --- | :-: | :-: | :-: | :-: | :-: |
| 语言 | Go | Go | TypeScript | Python | **Go** |
| 类型化命令（从类型推导 Schema） | ✗ | ✗ | ✓ | 不适用 | **✓** |
| 自动 JSON / 结构化错误 | ✗ | ✗ | ✓ | 部分 | **✓** |
| `schema` 自省 | ✗ | 部分 | ✓ | ✗ | **✓** |
| 生成 Skill | ✗ | ✗ | ✓ | ✓ | **✓** |
| MCP Server | ✗ | ✓ | ✓ | 仅客户端 | **✓** |
| 风险分级与确认 | ✗ | ✗ | 部分 | ✗ | **✓** |
| 基于 Cobra | — | ✓ | ✗ | ✗ | **✓** |

## 路线图

- [x] **v0.1**：类型化命令、自动 JSON、结构化错误、`schema`、`--fields`、风险分级、`skills export`、`mcp serve`、`agent install`
- [ ] **v0.2**：~~挂载已有的 Cobra 命令~~（已完成）、~~Codex~~（已完成）、流式输出、中间件、配置文件、`axontest` 测试包
- [ ] **v0.3**：MCP over HTTP、TOON 输出、下一步提示、策略、敏感字段
- [ ] **v1.0**：契约稳定、文档站

## 参与贡献

见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 许可证

[Apache-2.0](LICENSE)
