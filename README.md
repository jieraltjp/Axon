<div align="center">

# Axon

**Write a Go CLI once. Ship it to humans, scripts, and AI agents.**

[![CI](https://github.com/jieraltjp/Axon/actions/workflows/ci.yml/badge.svg)](https://github.com/jieraltjp/Axon/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/jieraltjp/axon.svg)](https://pkg.go.dev/github.com/jieraltjp/axon)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

> [!WARNING]
> **Axon is an early prototype.** The core works and is tested, but the API will change before v1.0. Requires Go 1.25+.
> Feedback is very welcome: [open an issue](https://github.com/jieraltjp/Axon/issues).

---

## Why

Your CLI no longer has one kind of user. It also has:

- **Agents with a shell** (Claude Code, Codex, ...), which work best with a Skill that tells them how to use the tool
- **Agents without a shell** (Claude Desktop, chat apps, IDE assistants), which can only reach your tool through **MCP**
- **Scripts and programs**, which need **JSON output, structured errors and stable exit codes**
- **Humans**, who still want help text, tables and a prompt before anything destructive

Supporting all of them usually means writing an MCP server, a Skill file, a JSON mode and an error format next to your CLI, and then keeping them all in sync.

**With Axon, you write each command once as a typed Go function and every interface comes from that.** The [notes example](examples/notes) is 236 lines including its MCP server, Skill, `schema` command, JSON mode and confirmation flow. The same CLI written with plain Cobra is 261 lines and has none of these.

> **What we measured.** In our [benchmarks](bench/RESULTS.md), current models (Sonnet and Haiku) used a plain Cobra CLI about as well as an Axon one, and MCP, Skill + CLI and bare CLI cost about the same in Claude Code. Axon does not make agents smarter. It means you build the CLI once and reach every kind of user.

## Quick start

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

That single definition gives you:

```console
$ hello greet Axon                  # on a terminal: readable output
message:  Hello, Axon

$ hello greet Axon | cat            # piped: JSON, automatically
{"message":"Hello, Axon"}

$ hello greet                       # errors are structured, exit codes are stable
{"error":{"kind":"usage","code":"MISSING_ARGUMENT","message":"missing required <name>","hint":"run 'hello greet --help'","retryable":false}}

$ hello schema greet                # what agents read: input and output JSON Schema
$ hello skills export ./skills      # an Agent Skill (SKILL.md)
$ hello mcp serve                   # the same commands as MCP tools
```

## Connect to agents

```bash
go install .                                # agent install needs an installed binary
hello agent install claude-desktop          # MCP server in Claude Desktop's config
hello agent install cursor                  # MCP server in ~/.cursor/mcp.json
hello agent install codex                   # MCP server in ~/.codex/config.toml
hello agent install claude-code             # Skill in ~/.claude/skills
hello agent list                            # where it is installed
hello agent uninstall cursor
```

`agent install` changes only its own entry, keeps a `.bak` copy of any file it edits, refuses to touch a config it cannot parse, and supports `--dry-run`. Destructive commands are not exposed over MCP unless you pass `--allow-destructive`.

## Already have a Cobra CLI?

You don't have to rewrite it. Mount your existing commands and convert them one at a time:

```go
app := axon.New("mycli")
axon.Mount(app, legacyRootCmd.Commands()...)        // existing *cobra.Command values, unchanged
axon.Command(app, "status", "Show status", status)  // new or rewritten commands are typed
app.Run()
```

Mounted commands keep their behavior and appear in help; their errors use Axon's error format. They join `schema`, skills and MCP once you rewrite them with `axon.Command`, because Axon needs their types for that.

## Try the example

```bash
git clone https://github.com/jieraltjp/Axon.git && cd Axon
go run ./examples/notes list              # a table, for humans
go run ./examples/notes list | cat        # JSON, for agents and scripts
go run ./examples/notes list --fields id,title
go run ./examples/notes delete 1          # asks first; agents get exit code 4 unless --yes
go run ./examples/notes delete 1 --dry-run
go run ./examples/notes schema search
```

## Reference

### Inputs: struct tags

| Tag | Meaning |
| --- | --- |
| `arg:""` | positional argument, in field order; required unless it has a default |
| `default:"10"` | default value |
| `required:""` | the flag must be given |
| `enum:"a,b,c"` | allowed values |
| `env:"VAR"` | read from the environment when not given |
| `help:"..."` | description for help, schema, skills and MCP |
| `short:"n"` | one-letter alias |
| `name:"x"` | override the flag name (default: the field name in kebab-case) |
| `axon:"-"` | ignore the field |

Supported types: `string`, `bool`, `int`, `int64`, `float64`, `time.Duration`, `[]string`, `[]int`. Slice flags can be repeated or comma-separated. Outputs can be any value that encodes to JSON.

### Command options

| Option | Effect |
| --- | --- |
| `axon.Write()` | marks the command as changing state |
| `axon.Destructive()` | requires confirmation: a prompt on a terminal, otherwise `--yes`; hidden from MCP by default |
| `axon.SupportsDryRun()` | adds `--dry-run`; the handler checks `ctx.DryRun()` |
| `axon.Example(...)` | examples for help, schema and skills |
| `axon.Aliases(...)` | alternative names |
| `axon.Hidden()` | hides the command everywhere |
| `axon.Exclude(axon.ProjectionMCP, axon.ProjectionSkill)` | keeps it out of MCP or skills |
| `axon.Group(app, "tag", "...")` | describes a command group such as `tag add` |

### Output and errors

| Flag | Effect |
| --- | --- |
| `-o text\|json\|ndjson` | output format; default is text on a terminal and JSON otherwise (also `AXON_OUTPUT`) |
| `--fields a,b` | keep only these fields |
| `--envelope` | wrap output as `{"ok":true,"data":...}` or `{"ok":false,"error":...}` |

Return `axon.NotFound(...)`, `axon.Usage(...)`, `axon.PermissionDenied(...)`, `axon.Unavailable(...)` or `axon.Errorf(...)` to control the error kind, code and hint. Any other error becomes a general error.

| Exit code | Meaning |
| --- | --- |
| 0 | success |
| 1 | error |
| 2 | usage error |
| 3 | not found |
| 4 | confirmation required |
| 5 | permission denied |
| 6 | temporary failure; retry |

### Built-in commands

| Command | Purpose |
| --- | --- |
| `schema [command]` | machine-readable manifest (`axon/v0`) |
| `skills export [dir]` | write an Agent Skill; large CLIs are split into an index plus reference files |
| `mcp serve [--allow-destructive]` | MCP server on stdio (protocol versions 2024-11-05 to 2025-11-25; no extra dependencies) |
| `agent install\|uninstall\|list` | register with Claude Code, Claude Desktop, Cursor or Codex |
| `completion` | shell completions (from Cobra) |

Need lower-level control? `app.Cobra()` returns the underlying `*cobra.Command`.

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
| Builds on Cobra | — | ✓ | ✗ | ✗ | **✓** |

## Roadmap

- [x] **v0.1**: typed commands, auto JSON, structured errors, `schema`, `--fields`, risk levels, `skills export`, `mcp serve`, `agent install`
- [ ] **v0.2**: ~~mounting existing Cobra commands~~ (done), ~~Codex~~ (done), streaming output, middleware, config files, an `axontest` package
- [ ] **v0.3**: MCP over HTTP, TOON output, next-step hints, policies, secret fields
- [ ] **v1.0**: stable contracts, docs site

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache-2.0](LICENSE)
