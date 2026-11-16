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

In 2026, AI agents mostly use tools through **CLIs plus Skills**, with MCP filling the gaps. Your CLI's users now include agents, and agents need a few things humans don't:

- JSON output when stdout is not a TTY, with data on stdout and logs on stderr
- A `schema` command that describes every command, flag, and exit code
- Structured errors and stable exit codes
- Output trimming (`--fields`) to save tokens
- Explicit confirmation before destructive actions
- A Skill file so the agent knows how to use the tool, and optionally an MCP server

Today every CLI author builds all of this by hand. **Axon makes it the default.**

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
