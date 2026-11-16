# Changelog

All notable changes to this project are documented here. Axon follows [Semantic Versioning](https://semver.org/); until v1.0, minor versions may contain breaking changes.

## Unreleased

### Added

- `axon.Mount(app, cmds...)` mounts existing Cobra commands, so a CLI can move to Axon one command at a time.
- `agent install codex` registers the CLI as an MCP server in `~/.codex/config.toml` (or `$CODEX_HOME`). It edits only its own table and preserves the rest of the file byte for byte, including CRLF line endings.
- Runnable GoDoc examples.

### Fixed

- `agent install claude-desktop` now writes to the config that the Windows MSIX build of Claude Desktop actually reads (`%LOCALAPPDATA%\Packages\Claude_*\LocalCache\Roaming\Claude`). This build comes from the official installer, the Microsoft Store and WinGet. Results now include a note on what to do next, such as restarting Claude Desktop.

## v0.1.0 (2026-11-16)

The first public prototype.

### Added

- **Typed commands:** `axon.Command(app, path, description, func(*axon.Context, In) (Out, error))`. Arguments, flags, defaults, environment variables, enums, required fields and JSON Schema are all derived from struct tags.
- **Output for humans and machines:** tables and key/value text on a terminal, JSON when piped, plus `-o json|ndjson|text`, `--fields`, `--envelope` and `AXON_OUTPUT`.
- **Structured errors and stable exit codes:** `axon.NotFound`, `axon.Usage`, `axon.PermissionDenied`, `axon.Unavailable`, `axon.Errorf`; exit codes 0–6.
- **Risk levels:** `axon.Write()` and `axon.Destructive()`. Destructive commands prompt on a terminal and otherwise require `--yes` (exit code 4). `axon.SupportsDryRun()` adds `--dry-run`.
- **Built-in commands:**
  - `schema [command]` prints an `axon/v0` manifest.
  - `skills export [dir]` writes an Agent Skill, split into an index and reference files when large.
  - `mcp serve [--allow-destructive]` runs an MCP stdio server without extra dependencies (protocol versions 2024-11-05 to 2025-11-25).
  - `agent install|uninstall|list` registers the CLI with Claude Code, Claude Desktop and Cursor.
- `examples/notes`, a complete example CLI.
- `bench/`, a reproducible benchmark (MCP vs Skill + CLI vs CLI, and Axon vs plain Cobra) with honest results.
