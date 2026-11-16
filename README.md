<div align="center">

# Axon

**The agent-native CLI framework for Go.**

Write a command once. Ship it to humans, AI agents, and MCP clients.

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

> [!WARNING]
> **Axon is an early prototype.** The core works and is tested, but the API will change before v1.0. Requires Go 1.25+.
> Have an opinion? [Open an issue](https://github.com/jieraltjp/Axon/issues).

---

## Why

Your CLI no longer has one kind of user. It also has:

- **Agents with a shell** (Claude Code, Codex, ...), which work best with a Skill that tells them how to use the tool
- **Agents without a shell** (chat apps, IDE assistants), which can only reach your tool through **MCP**
- **Scripts and programs**, which need **JSON output, structured errors and stable exit codes**
- **Humans**, who still want help text, tables and a prompt before anything destructive

Supporting all of them usually means writing an MCP server, a Skill file, a JSON mode and an error format next to your CLI, and then keeping them all in sync.

**With Axon, you write each command once as a typed Go function and every interface comes from that.** The [notes example](examples/notes) is 236 lines including its MCP server, Skill, `schema` command, JSON mode and confirmation flow. The same CLI written with plain Cobra is 261 lines and has none of these.

> **What we measured.** In our [benchmarks](bench/RESULTS.md), current models (Sonnet and Haiku) used a plain Cobra CLI about as well as an Axon one, and MCP, Skill + CLI and bare CLI cost about the same in Claude Code. Axon does not make agents smarter. It means you build the CLI once and reach every kind of user.

## Try it

```bash
git clone https://github.com/jieraltjp/Axon.git && cd Axon
go run ./examples/notes list              # a table, for humans
go run ./examples/notes list | cat        # JSON, for agents and scripts
go run ./examples/notes delete 1          # asks first; agents get exit code 4 unless --yes
go run ./examples/notes schema search     # what an agent reads
go run ./examples/notes skills export     # writes skills/notes/SKILL.md
```

## A quick look

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

From that one definition you get:

```bash
notes search mcp                     # humans: a readable table
notes search mcp | jq '.[0]'         # agents and scripts: JSON, automatically
notes search mcp --fields id,title   # trimmed output
notes delete 42                      # asks for confirmation, or exits with code 4 when not a TTY
notes delete 42 --yes --dry-run      # explicit consent, preview only
notes schema search                  # machine-readable command description
notes skills export ./skills         # generates SKILL.md for Claude Code, Codex, ...
notes mcp serve                      # the same commands as MCP tools
```

Flags, arguments, validation, and JSON Schema all come from your Go types. **You never define them twice.**

## How it compares

|  | Cobra | ophis | incur | FastMCP `generate-cli` | **Axon** |
| --- | :-: | :-: | :-: | :-: | :-: |
| Language | Go | Go | TypeScript | Python | **Go** |
| Typed commands (schema from types) | ✗ | ✗ | ✓ | n/a | **✓** |
| Auto JSON / structured errors | ✗ | ✗ | ✓ | partial | **✓** |
| `schema` introspection | ✗ | partial | ✓ | ✗ | **✓** |
| Skill generation | ✗ | ✗ | ✓ | ✓ | **✓** |
| MCP server | ✗ | ✓ | ✓ | client only | **✓** |
| Risk levels and confirmation | ✗ | ✗ | partial | ✗ | **✓** |
| Builds on Cobra (easy migration) | — | ✓ | ✗ | ✗ | **✓** |

## Principles

1. **Single source of truth.** Each command is defined once; every interface is derived from that definition.
2. **Agent-ready by default.** You don't configure it; it's already there.
3. **Humans stay first-class.** Help text, completions, and readable output are at least as good as Cobra's.
4. **Machine contracts are versioned.** Output, errors, exit codes, and the manifest are treated as an API.
5. **Safe by default.** Destructive commands require consent, and agents get the more conservative behaviour.

## Roadmap

- [ ] **v0.1**: typed commands, auto JSON, structured errors, `schema`, `--fields`, risk levels, `skills export`, `mcp serve`
- [ ] **v0.2**: streaming, middleware, config files, `agent install <claude-code|codex|cursor|...>`, Cobra migration helpers, `axontest`
- [ ] **v0.3**: TOON output, next-step hints, policies, secret fields, MCP over HTTP
- [ ] **v1.0**: stable contracts, docs site
- [ ] **Later: Axon Hub.** Every Axon CLI speaks the same manifest protocol, so one `axon` binary could discover, search, govern, and serve all of them.

## License

[Apache-2.0](LICENSE)
