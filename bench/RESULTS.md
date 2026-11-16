# Benchmark results

How Claude Code uses the same Axon CLI (`examples/notes`) through three interfaces.

- **mcp**: `notes mcp serve`, connected as an MCP server
- **skill**: the generated `SKILL.md` plus the CLI
- **cli**: the CLI alone; the agent is only told that a `notes` command exists

Four tasks (find, add, count, delete), 3 repetitions each, 36 runs in total. Every run gets a fresh project directory and a fresh notes store. Success is checked against the resulting store or the exact answer, not against what the model says it did. Reproduce with `python bench/run.py --repeat 3`.

## 2026-11-16 · Claude Code 2.1.284 · Sonnet · Windows

| Config | Success | Avg input tokens | Avg output tokens | Avg turns | Avg cost (USD) | Avg seconds |
| --- | --- | --- | --- | --- | --- | --- |
| mcp | 12/12 | 53,097 | 210 | 3.8 | 0.0342 | 6.7 |
| skill | 12/12 | 54,090 | 168 | 4.5 | 0.0355 | 6.5 |
| cli | 12/12 | 49,413 | 167 | 3.2 | 0.0365 | 6.4 |

Input tokens include cached tokens.

## What this shows

- **At this size, the interface makes no meaningful difference to cost.** The three configurations are within about 10% of each other. Most input tokens are Claude Code's own context, roughly 15k per turn. Claude Code defers MCP tool definitions by default, so six MCP tools cost about as much as a CLI. The skill configuration takes about one extra turn, because the agent loads the skill first.
- **The bare CLI succeeded 12/12 with no skill and no MCP.** Help text, JSON output and error hints were enough for the agent to work out the commands, sometimes in two turns.
- **Destructive commands:** when the user explicitly approved the deletion, all three configurations passed `--yes` and succeeded.

## Not measured yet

- **A control group.** The same CLI built on plain Cobra (text output, unstructured errors) would show how much the agent-facing defaults matter.
- **Larger CLIs.** Tens or hundreds of commands, where tool definitions and help text start to cost real tokens.
- **Safety without approval.** Whether agents add `--yes` on their own when the user never approved a deletion.
- **Other agents and models.** Codex, Cursor, Gemini CLI, and smaller models.
