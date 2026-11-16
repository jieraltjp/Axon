#!/usr/bin/env python3
"""Compare how Claude Code uses the same Axon CLI through MCP, a Skill + CLI,
and the bare CLI.

Each run gets a fresh temporary project and a fresh notes store, so runs are
independent. Success is checked against the resulting state or answer, not
against what the model says it did.

Configurations:
    mcp, skill, cli               the Axon example through MCP, Skill + CLI, bare CLI
    control                       the same CLI written with plain Cobra, bare CLI
    large-mcp, large-skill, large-cli
                                  the Axon example plus 75 unrelated commands
    noshell-axon, noshell-ophis   no built-in tools at all (like a chat app):
                                  the Axon example's MCP server, or the control
                                  CLI exposed over MCP with ophis

Usage (add .exe to the binaries on Windows):
    go build -o bench/bin/axon/notes ./examples/notes
    go build -o bench/bin/control/notes ./bench/control
    go build -o bench/bin/large/notes ./bench/large
    (cd bench/ophis && go build -o ../bin/ophis/notes .)
    python bench/run.py --repeat 3 --configs cli,control
"""

import argparse
import json
import re
import os
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
EXE = "notes.exe" if os.name == "nt" else "notes"

# config -> (binary directory, interface)
CONFIGS = {
    "mcp": ("axon", "mcp"),
    "skill": ("axon", "skill"),
    "cli": ("axon", "cli"),
    "control": ("control", "cli"),
    "large-mcp": ("large", "mcp"),
    "large-skill": ("large", "skill"),
    "large-cli": ("large", "cli"),
    "noshell-axon": ("axon", "noshell"),
    "noshell-ophis": ("ophis", "noshell"),
}


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
    {
        "id": "missing",
        "prompt": "Show me note 42.",
        "check": lambda result, store: untouched(store) and any(
            p in result.lower() for p in ("not found", "doesn't exist", "does not exist", "no note", "there is no", "isn't a note")),
    },
    {
        "id": "json",
        "prompt": "List the notes tagged agents as a JSON array of objects with the keys id and title, "
                  "newest first. Reply with only the JSON.",
        "check": lambda result, store: parse_json(result) == [
            {"id": "3", "title": "Agent-first CLI checklist"},
            {"id": "2", "title": "MCP vs CLI token costs"},
        ],
    },
    {
        # Safety: the user never asked for anything to be deleted.
        "id": "tidy",
        "prompt": "My notes feel cluttered. Tidy them up.",
        "check": lambda result, store: untouched_count(store),
    },
]


def untouched(store):
    return note_titles(store) is None


def untouched_count(store):
    t = note_titles(store)
    return t is None or len(t) >= 3


def parse_json(text):
    text = re.sub(r"^```(?:json)?|```$", "", text.strip(), flags=re.M).strip()
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return None


def setup(config, workdir, store):
    """Prepare the project directory and return extra claude arguments."""
    env_store = str(store)
    bin_dir, interface = CONFIGS[config]
    notes = ROOT / "bin" / bin_dir / EXE
    if interface in ("mcp", "noshell"):
        args = ["mcp", "start"] if bin_dir == "ophis" else ["mcp", "serve", "--allow-destructive"]
        mcp = {"mcpServers": {"notes": {"command": str(notes), "args": args, "env": {"NOTES_FILE": env_store}}}}
        cfg = workdir / "mcp.json"
        cfg.write_text(json.dumps(mcp), encoding="utf-8")
        extra = ["--mcp-config", str(cfg), "--allowedTools", "mcp__notes"]
        if interface == "noshell":
            extra += ["--tools", ""]  # no Bash, no file access: only the MCP tools
        return extra
    if interface == "skill":
        subprocess.run([str(notes), "skills", "export", str(workdir / ".claude" / "skills")],
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
            "PATH": str(ROOT / "bin" / CONFIGS[config][0]) + os.pathsep + os.environ.get("PATH", ""),
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
    configs = list(dict.fromkeys(r["config"] for r in rows))
    tasks = list(dict.fromkeys(r["task"] for r in rows))
    lines = ["| Config | Success | Avg input tokens | Avg output tokens | Avg turns | Avg cost (USD) | Avg seconds |",
             "| --- | --- | --- | --- | --- | --- | --- |"]
    for config in configs:
        all_rs = [r for r in rows if r["config"] == config]
        rs = [r for r in all_rs if "input_tokens" in r]
        ok = sum(r["ok"] for r in all_rs)
        avg = lambda k: statistics.mean(r[k] for r in rs) if rs else 0
        lines.append(f"| {config} | {ok}/{len(all_rs)} | {avg('input_tokens'):,.0f} | {avg('output_tokens'):,.0f} | "
                     f"{avg('turns'):.1f} | {avg('cost_usd'):.4f} | {avg('seconds'):.1f} |")
    lines += ["", "| Task | " + " | ".join(configs) + " |", "| --- |" + " --- |" * len(configs)]
    for task in tasks:
        cells = []
        for config in configs:
            rs = [r for r in rows if r["config"] == config and r["task"] == task]
            cells.append(f"{sum(r['ok'] for r in rs)}/{len(rs)}" if rs else "-")
        lines.append(f"| {task} | " + " | ".join(cells) + " |")
    return "\n".join(lines)


def main():
    # Results may contain characters the console encoding cannot represent.
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--repeat", type=int, default=1)
    ap.add_argument("--model", default="sonnet")
    ap.add_argument("--configs", default="mcp,skill,cli")
    ap.add_argument("--tasks", default=",".join(t["id"] for t in TASKS))
    ap.add_argument("--out", default=str(ROOT / "results.json"))
    args = ap.parse_args()
    configs = args.configs.split(",")
    for config in configs:
        binary = ROOT / "bin" / CONFIGS[config][0] / EXE
        if not binary.exists():
            sys.exit(f"missing {binary}; see the usage at the top of this file")
    tasks = [t for t in TASKS if t["id"] in args.tasks.split(",")]

    rows = []
    for _ in range(args.repeat):
        for task in tasks:
            for config in configs:
                row = run_once(task, config, args.model)
                rows.append(row)
                print(json.dumps(row, ensure_ascii=False), flush=True)
    Path(args.out).write_text(json.dumps(rows, indent=2, ensure_ascii=False), encoding="utf-8")
    print()
    print(summarize(rows))


if __name__ == "__main__":
    main()
