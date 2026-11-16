# Case study: moving yq to Axon

[yq](https://github.com/mikefarah/yq) is a popular Cobra CLI. This example turns it into an Axon app without changing a line of yq:

```go
app := axon.New("yq", axon.Description("A portable YAML, JSON, XML, CSV and TOML processor"))
axon.MountRoot(app, yqcmd.New()) // all of yq, unchanged
axon.Command(app, "query", ...)  // typed commands for scripts and agents
axon.Command(app, "set", ..., axon.Write(), axon.SupportsDryRun())
app.Run()
```

`main.go` is 130 lines, and most of it is the two typed commands.

## What stays the same

Everything yq did before still works the same way: `yq '.a' file.yaml`, `cat file | yq '.a'`, `yq -o json -I0`, `yq -i`, `eval`, `eval-all` and `completion`. yq's own `-o` (output format) keeps its meaning; Axon's `-o` applies only to Axon's commands.

## What it gains

| | Before | After |
| --- | --- | --- |
| Machine-readable results | text in whatever format `-o` chose | `yq query` returns `{"results": [...]}` with JSON types preserved |
| Errors | `Error: ...` on stderr, exit code 1 for everything | JSON errors with codes, hints and exit codes: invalid expression (2), missing file (3) |
| Writing files | `yq -i` writes immediately | `yq set` is marked as a write and supports `--dry-run`, which returns the new content without writing |
| Agents with a shell | none | `yq schema`, `yq skills export` (a 1.8 KB SKILL.md) |
| Agents without a shell | none | `yq mcp serve` exposes `query` (read-only) and `set` as MCP tools; `yq agent install claude-desktop` registers it |
| Binary size | 22.09 MB | 22.46 MB (+1.7%) |

```console
$ yq query '.spec.tags[]' deploy.yaml
{"results":["x","y"]}
$ yq query '.spec |' deploy.yaml; echo "exit=$?"
{"error":{"kind":"usage","code":"BAD_EXPRESSION","message":"invalid expression: '|' expects 2 args but there is 1","hint":"see https://mikefarah.gitbook.io/yq/ for the expression syntax","retryable":false}}
exit=2
$ yq set '.spec.replicas = 3' deploy.yaml --dry-run --fields changed,dry_run
{"changed":true,"dry_run":true}
```

## What it found in Axon

The first attempt used `Mount(app, root.Commands()...)`, which the README recommended at the time. It failed in three ways, each typical of Cobra CLIs, and all three are fixed:

1. **A reserved name.** yq has its own `completion` command, so mounting panicked. `Mount` now skips `help` and `completion`, which Axon provides.
2. **Lost root setup.** yq defines its flags and its logging setup on the root, as persistent flags and a `PersistentPreRunE`. The mounted `eval` lost both and crashed with a nil pointer. Mounted commands now keep the persistent flags and hooks they inherited.
3. **A lost default action.** Most of yq's users type `yq '.a' file`, which is the root's own action. `MountRoot` keeps it.

Fixing the second also required a change in Axon itself: `--output`, `--fields` and `--envelope` were persistent flags on the root, so they clashed with yq's `-o`. They are now flags of each Axon command instead, so mounted commands never see them.

## Run it

```bash
cd examples/yq
go run . '.a' some.yaml
go run . query .a some.yaml
go run . mcp serve
```

This example is a separate module so that Axon itself does not depend on yq. It needs Go 1.26, because yq does.
