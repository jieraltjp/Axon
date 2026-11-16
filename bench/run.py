#!/usr/bin/env python3
"""Compare how Claude Code uses the same Axon CLI through MCP, a Skill + CLI,
and the bare CLI.

Each run gets a fresh temporary project and a fresh notes store, so runs are
independent. Success is checked against the resulting state or answer, not
against what the model says it did.

Usage:
    go build -o bench/bin/notes ./examples/notes   # add .exe on Windows
    python bench/run.py --repeat 2 --model sonnet
"""

import argparse
import json
import os
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
BIN_DIR = ROOT / "bin"
NOTES = BIN_DIR / ("notes.exe" if os.name == "nt" else "notes")


def note_titles(store):
    if not store.exists():
        return None  # untouched: the seed data is still in effect
    return [n["title"] for n in json.loads(store.read_text(encoding="utf-8"))]


TASKS = [
    {
        "id": "find",
        "prompt": "What is the ID of the note about MCP token costs? Reply with only the ID.",
        "check": lambda result, store: result.strip().strip("`*. ") == "2",
    },
    {
        "id": "add",
        "prompt": 'Add a note titled "Benchmark run" with the tag bench.',
        "check": lambda result, store: any(
            n["title"] == "Benchmark run" and "bench" in n["tags"]
            for n in json.loads(store.read_text(encoding="utf-8"))
        ) if store.exists() else False,
    },
    {
        "id": "count",
        "prompt": "Which tag is used by the most notes? Reply with only the tag.",
        "check": lambda result, store: result.strip().strip("`*. ").lower() == "agents",
    },
    {
        "id": "delete",
        "prompt": "Delete the note about Go generics. I approve the deletion.",
        "check": lambda result, store: (t := note_titles(store)) is not None
        and "Learn Go generics" not in t and len(t) == 2,
    },
]

CONFIGS = ["mcp", "skill", "cli"]


def setup(config, workdir, store):
    """Prepare the project directory and return extra claude arguments."""
    env_store = str(store)
    if config == "mcp":
        mcp = {
            "mcpServers": {
                "notes": {
                    "command": str(NOTES),
                    "args": ["mcp", "serve", "--allow-destructive"],
                    "env": {"NOTES_FILE": env_store},
                }
            }
        }
        cfg = workdir / "mcp.json"
        cfg.write_text(json.dumps(mcp), encoding="utf-8")
        return ["--mcp-config", str(cfg), "--allowedTools", "mcp__notes"]
    if config == "skill":
        subprocess.run([str(NOTES), "skills", "export", str(workdir / ".claude" / "skills")],
                       check=True, capture_output=True, env={**os.environ, "NOTES_FILE": env_store})
        return ["--allowedTools", "Bash(notes:*)", "Skill", "Read"]
    # Bare CLI: the agent only knows the command exists.
    return ["--allowedTools", "Bash(notes:*)",
            "--append-system-prompt", "A `notes` command-line tool for managing notes is installed."]


def run_once(task, config, model):
    workdir = Path(tempfile.mkdtemp(prefix=f"axon-bench-{config}-"))
    store = workdir / "notes.json"
    try:
        extra = setup(config, workdir, store)
        env = {
            **os.environ,
            "NOTES_FILE": str(store),
            "PATH": str(BIN_DIR) + os.pathsep + os.environ.get("PATH", ""),
        }
        cmd = ["claude", "-p", task["prompt"], "--output-format", "json", "--model", model,
               "--setting-sources", "project", "--strict-mcp-config", "--no-session-persistence",
               *extra]
        started = time.time()
        proc = subprocess.run(cmd, cwd=workdir, env=env, capture_output=True, text=True,
                              encoding="utf-8", timeout=600, shell=(os.name == "nt"))
        elapsed = time.time() - started
        try:
            out = json.loads(proc.stdout)
        except json.JSONDecodeError:
            return {"task": task["id"], "config": config, "ok": False,
                    "error": (proc.stderr or proc.stdout)[-500:]}
        usage = out.get("usage", {})
        result = out.get("result", "") or ""
        return {
            "task": task["id"],
            "config": config,
            "ok": bool(task["check"](result, store)) and not out.get("is_error"),
            "input_tokens": usage.get("input_tokens", 0)
            + usage.get("cache_creation_input_tokens", 0)
            + usage.get("cache_read_input_tokens", 0),
            "output_tokens": usage.get("output_tokens", 0),
            "cost_usd": out.get("total_cost_usd", 0),
            "turns": out.get("num_turns", 0),
            "seconds": round(elapsed, 1),
            "result": result[:200],
        }
    finally:
        shutil.rmtree(workdir, ignore_errors=True)


def summarize(rows):
    lines = ["| Config | Success | Avg input tokens | Avg output tokens | Avg turns | Avg cost (USD) | Avg seconds |",
             "| --- | --- | --- | --- | --- | --- | --- |"]
    for config in CONFIGS:
        rs = [r for r in rows if r["config"] == config and "input_tokens" in r]
        all_rs = [r for r in rows if r["config"] == config]
        if not all_rs:
            continue
        ok = sum(r["ok"] for r in all_rs)
        avg = lambda k: statistics.mean(r[k] for r in rs) if rs else 0
        lines.append(f"| {config} | {ok}/{len(all_rs)} | {avg('input_tokens'):,.0f} | {avg('output_tokens'):,.0f} | "
                     f"{avg('turns'):.1f} | {avg('cost_usd'):.4f} | {avg('seconds'):.1f} |")
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repeat", type=int, default=1)
    ap.add_argument("--model", default="sonnet")
    ap.add_argument("--configs", default=",".join(CONFIGS))
    ap.add_argument("--out", default=str(ROOT / "results.json"))
    args = ap.parse_args()
    if not NOTES.exists():
        sys.exit(f"build the example first: go build -o {NOTES} ./examples/notes")

    rows = []
    for _ in range(args.repeat):
        for task in TASKS:
            for config in args.configs.split(","):
                row = run_once(task, config, args.model)
                rows.append(row)
                print(json.dumps(row, ensure_ascii=False), flush=True)
    Path(args.out).write_text(json.dumps(rows, indent=2, ensure_ascii=False), encoding="utf-8")
    print()
    print(summarize(rows))


if __name__ == "__main__":
    main()
