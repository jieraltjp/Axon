<div align="center">

# Axon

**给 Agent 时代的 Go CLI 框架。**

写一次命令，同时服务人、AI Agent 和 MCP 客户端。

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

> [!WARNING]
> **Axon 目前是早期原型。** 核心功能已经可用并有测试覆盖，但在 v1.0 之前 API 还会调整。需要 Go 1.25+。
> 欢迎[提出意见](https://github.com/jieraltjp/Axon/issues)。

---

## 为什么

你的 CLI 的用户不再只有一种：

- **有 shell 的 Agent**（Claude Code、Codex……）：配上一个说明用法的 Skill 效果最好
- **没有 shell 的 Agent**（聊天应用、IDE 助手）：只能通过 **MCP** 使用你的工具
- **脚本和程序**：需要 **JSON 输出、结构化错误和稳定的退出码**
- **人**：仍然需要帮助信息、表格，以及危险操作前的确认

要同时支持它们，通常意味着在 CLI 之外再写 MCP Server、Skill 文件、JSON 模式和错误格式，并且一直保持同步。

**用 Axon，每个命令只需写一次类型化的 Go 函数，所有接口都从这一份定义而来。** [notes 示例](examples/notes)总共 236 行，包含 MCP Server、Skill、`schema` 命令、JSON 模式和确认流程。用普通 Cobra 写的同一个 CLI 有 261 行，而这些一样都没有。

> **实测结论。** 在我们的[对比测试](bench/RESULTS.md)中，当前的模型（Sonnet 和 Haiku）使用普通 Cobra CLI 的效果和 Axon CLI 相差不大；在 Claude Code 中，MCP、Skill + CLI、只有 CLI 三种方式的成本也差不多。Axon 不会让 Agent 变聪明，它的价值在于：CLI 只写一次，就能覆盖所有类型的用户。

## 试一试

```bash
git clone https://github.com/jieraltjp/Axon.git && cd Axon
go run ./examples/notes list              # 表格，给人看
go run ./examples/notes list | cat        # JSON，给 Agent 和脚本
go run ./examples/notes delete 1          # 先询问确认；Agent 调用时不带 --yes 会以退出码 4 拒绝
go run ./examples/notes schema search     # Agent 读取的命令描述
go run ./examples/notes skills export     # 生成 skills/notes/SKILL.md
```

## 快速预览

```go
package main

import "github.com/jieraltjp/axon"

type SearchInput struct {
    Query string `arg:"" help:"Text to search for"`
    Limit int    `default:"10" help:"Max results"`
}

type Note struct {
    ID    string `json:"id"`
    Title string `json:"title"`
}

func main() {
    app := axon.New("notes", axon.Description("Personal markdown notes"))

    axon.Command(app, "search", "Full-text search notes",
        func(ctx *axon.Context, in SearchInput) ([]Note, error) {
            return search(in.Query, in.Limit)
        })

    axon.Command(app, "delete", "Delete a note", deleteNote, axon.Destructive())

    app.Run()
}
```

这一份定义可以得到：

```bash
notes search mcp                     # 人：可读的表格
notes search mcp | jq '.[0]'         # Agent / 脚本：自动输出 JSON
notes search mcp --fields id,title   # 裁剪输出
notes delete 42                      # 交互确认；非 TTY 下以退出码 4 结束
notes delete 42 --yes --dry-run      # 显式授权，只预演
notes schema search                  # 机器可读的命令描述
notes skills export ./skills         # 生成 SKILL.md，供 Claude Code、Codex 等使用
notes mcp serve                      # 同一批命令作为 MCP 工具
```

flag、参数、校验和 JSON Schema 都从 Go 类型推导，**不用定义两遍**。

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
| 基于 Cobra（易于迁移） | — | ✓ | ✗ | ✗ | **✓** |

## 路线图

- [ ] **v0.1**：类型化命令、自动 JSON、结构化错误、`schema`、`--fields`、风险分级、`skills export`、`mcp serve`
- [ ] **v0.2**：流式输出、中间件、配置文件、`agent install <claude-code|codex|cursor|...>`、Cobra 迁移、`axontest`
- [ ] **v0.3**：TOON 输出、下一步提示、策略、敏感字段、MCP over HTTP
- [ ] **v1.0**：契约稳定、文档站
- [ ] **之后：Axon Hub**。所有 Axon CLI 都使用同一个 Manifest 协议，一个 `axon` 主程序就能统一发现、搜索、管控和暴露它们。

## 许可证

[Apache-2.0](LICENSE)
