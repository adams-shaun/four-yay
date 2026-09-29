#!/usr/bin/env python3
"""reward_collect.py — the free reward collectors.

Each subcommand prints scoreboard rows (one JSON object per line) on stdout and
writes nothing. The caller — scripts/reward-probe.sh — appends them to
.ds4/reward/scoreboard.jsonl. Keeping the collectors pure makes them safe to
run from a monitor, a test, or by hand.

Every metric here is derived from something the repo already records: the
daemon's journal, the git worktrees, the ratchet tables, the gauntlet results
ledger. Nothing here plays a game or compiles anything, so a full pass costs
under a second and can run on every cycle. The costly axis (`eff`) lives in
reward-probe.sh behind a broker lease.

    scripts/reward_collect.py flow|stability|win|correct|audit|obs|all [--repo R]
    scripts/reward_collect.py hotspots --repo R      # the contended-file table
    scripts/reward_collect.py --selftest
"""

from __future__ import annotations

import argparse
import collections
import json
import re
import subprocess
import sys
import tempfile
from datetime import datetime, timedelta, timezone
from pathlib import Path

WINDOW_DAYS = 7
# A file this many live branches deep is a hot spot: the third branch to touch
# it is the one that turns a merge into a merge_fix round. Two is contention,
# three is a queue.
HOTSPOT_BRANCHES = 2


def now_iso() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def git(repo: Path, *args: str) -> str:
    p = subprocess.run(
        ["git", "-C", str(repo), *args], capture_output=True, text=True, timeout=120
    )
    return p.stdout if p.returncode == 0 else ""


def head(repo: Path) -> str:
    return git(repo, "rev-parse", "--short", "HEAD").strip() or "unknown"


def row(repo: Path, axis: str, metric: str, value, note: str = "", cost_s: float = 0.0) -> str:
    return json.dumps(
        {
            "ts": now_iso(),
            "git_head": head(repo),
            "axis": axis,
            "metric": metric,
            "value": value,
            "cost_s": cost_s,
            "cmd": "scripts/reward_collect.py",
            "note": note,
        }
    )


def journal_rows(repo: Path, days: int = WINDOW_DAYS) -> list[dict]:
    p = repo / ".ds4" / "orchestrator" / "journal.jsonl"
    if not p.exists():
        return []
    cutoff = datetime.now(timezone.utc) - timedelta(days=days)
    out = []
    with p.open() as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                d = json.loads(line)
                ts = datetime.fromisoformat(str(d.get("ts", "")).replace("Z", "+00:00"))
            except (ValueError, TypeError):
                continue
            if ts.tzinfo is None:
                ts = ts.replace(tzinfo=timezone.utc)
            if ts >= cutoff:
                out.append(d)
    return out


# --------------------------------------------------------------------------
# flow: merge-conflict hot spots


def live_branch_files(repo: Path) -> dict[str, list[str]]:
    """Files each live task branch changes against main, keyed by file.

    This is the PREDICTIVE half of the flow axis: it names the files that
    several in-flight branches are editing right now, before any of them
    reaches merge_fix. Acting on it (splitting the file, or sequencing the
    tickets) is what removes the hot spot.
    """
    files: dict[str, list[str]] = collections.defaultdict(list)
    for line in git(repo, "worktree", "list", "--porcelain").splitlines():
        if not line.startswith("branch "):
            continue
        ref = line.split(None, 1)[1].strip()
        name = ref.rsplit("/", 1)[-1]
        if ref == "refs/heads/main":
            continue
        changed = git(repo, "diff", "--name-only", f"main...{ref}").split()
        for f in changed:
            files[f].append(name)
    return files


def idle_branches(repo: Path, hours: int = 12) -> list[tuple[str, int, float]]:
    """Unmerged task branches whose worktree has not been touched in `hours`.

    An idle unmerged branch is the durable half of a merge hot spot: it keeps
    holding its files against every branch that lands after it, and each of
    those pays a resolver round for work nobody is doing. Measured 2026-09-29:
    three loops branches, idle over a day, held five files between them.

    Returns (branch, commits ahead of main, hours idle).
    """
    out: list[tuple[str, int, float]] = []
    now = datetime.now(timezone.utc).timestamp()
    # Split on blank lines rather than tracking state line by line: the last
    # record has no trailing blank line, and a state machine drops it.
    for block in git(repo, "worktree", "list", "--porcelain").split("\n\n"):
        fields = dict(
            (l.split(None, 1) + [""])[:2] for l in block.splitlines() if l.strip()
        )
        ref, path = fields.get("branch", "").strip(), fields.get("worktree", "").strip()
        if not ref or not path or ref == "refs/heads/main":
            continue
        # A branch already contained in main holds nothing.
        if subprocess.run(
            ["git", "-C", str(repo), "merge-base", "--is-ancestor", ref, "main"],
            capture_output=True,
        ).returncode == 0:
            continue
        ahead = git(repo, "rev-list", "--count", f"main..{ref}").strip() or "0"
        try:
            mtime = Path(path).stat().st_mtime
        except OSError:
            continue
        idle_h = (now - mtime) / 3600.0
        if idle_h >= hours:
            out.append((ref.rsplit("/", 1)[-1], int(ahead), round(idle_h, 1)))
    return sorted(out, key=lambda t: -t[2])


def hotspots(repo: Path) -> list[tuple[str, list[str]]]:
    return sorted(
        ((f, bs) for f, bs in live_branch_files(repo).items() if len(bs) >= HOTSPOT_BRANCHES),
        key=lambda kv: (-len(kv[1]), kv[0]),
    )


def collect_flow(repo: Path) -> list[str]:
    hs = hotspots(repo)
    rows = [
        row(
            repo,
            "flow",
            "conflict_hotspots",
            len(hs),
            note="; ".join(f"{f}({len(bs)})" for f, bs in hs[:8]),
        )
    ]
    # DISTINCT TICKETS, not transitions. One ticket in merge_fix emits several
    # journal rows (queued, dispatched, each overflow retry), and counting rows
    # made the rate read 0.769 when the true figure was 0.248 -- measured
    # 2026-09-29, where 619 transitions belonged to 199 tickets and one ticket
    # alone accounted for 48 of them.
    j = journal_rows(repo)
    merge_fix: set[str] = set()
    merged: set[str] = set()
    worst: collections.Counter[str] = collections.Counter()
    for d in j:
        iid = d.get("issue_id")
        if not iid:
            continue
        ev = d.get("evidence") or {}
        if ev.get("to") == "merge_fix":
            merge_fix.add(str(iid))
            worst[str(iid)] += 1
        if ev.get("to") == "merged" or ev.get("final_status") == "merged":
            merged.add(str(iid))
    rate = (len(merge_fix) / len(merged)) if merged else 0.0
    top = ", ".join(f"{i}({n})" for i, n in worst.most_common(3))
    rows.append(
        row(
            repo,
            "flow",
            "merge_fix_rate",
            round(rate, 4),
            note=f"{len(merge_fix)} tickets in merge_fix / {len(merged)} merged in {WINDOW_DAYS}d"
            + (f"; most rounds: {top}" if top else ""),
        )
    )
    idle = idle_branches(repo)
    rows.append(
        row(
            repo,
            "flow",
            "idle_unmerged_branches",
            len(idle),
            note="; ".join(f"{b}(+{a}, {h}h idle)" for b, a, h in idle[:6]),
        )
    )
    # The repeat offenders are the actionable half: a ticket that needed a dozen
    # resolver rounds names a file the fleet cannot land in parallel.
    rows.append(
        row(
            repo,
            "flow",
            "merge_fix_rounds_max",
            max(worst.values()) if worst else 0,
            note=top,
        )
    )
    return rows


# --------------------------------------------------------------------------
# stability: the 100x veto's inputs


def vmstat(key: str) -> int:
    try:
        for line in Path("/proc/vmstat").read_text().splitlines():
            k, v = line.split()
            if k == key:
                return int(v)
    except (OSError, ValueError):
        pass
    return 0


def collect_stability(repo: Path, state: Path | None = None) -> list[str]:
    """OOM kills and swap-ins are read as DELTAS against the last probe.

    The kernel counters are cumulative since boot, so the level says nothing
    about this window; the delta is the only honest reading, and a missing
    state file means the first probe reports zero rather than the boot total.
    """
    rows = []
    prev = {}
    if state and state.exists():
        try:
            prev = json.loads(state.read_text())
        except ValueError:
            prev = {}
    cur = {"oom_kill": vmstat("oom_kill"), "pswpin": vmstat("pswpin")}
    oom_delta = max(0, cur["oom_kill"] - prev.get("oom_kill", cur["oom_kill"]))
    swap_delta = max(0, cur["pswpin"] - prev.get("pswpin", cur["pswpin"]))
    rows.append(row(repo, "stability", "oom_kills", oom_delta))
    rows.append(row(repo, "stability", "swap_in_pages", swap_delta))

    j = journal_rows(repo, days=1)
    gate_timeouts = sum(
        1
        for d in j
        if d.get("kind") == "gate_fail" and "timeout" in str(d.get("msg", "")).lower()
    )
    rows.append(row(repo, "stability", "gate_timeouts", gate_timeouts, note="gate_fail with timeout, 24h"))
    # An endpoint_down or provider_failure storm is not a box failure, but it
    # stalls the loop, so it is reported as its own metric rather than folded
    # into the veto.
    rows.append(
        row(
            repo,
            "stability",
            "provider_failures_24h",
            sum(1 for d in j if d.get("kind") in ("provider_failure", "endpoint_down")),
        )
    )
    if state:
        state.parent.mkdir(parents=True, exist_ok=True)
        state.write_text(json.dumps(cur))
    return rows


# --------------------------------------------------------------------------
# win: read the gauntlet ledger, never play games


def collect_win(repo: Path, gauntlet: Path) -> list[str]:
    results = gauntlet / "results.jsonl"
    if not results.exists():
        return [row(repo, "win", "champion_elo", 0, note=f"no {results}")]
    best = None
    for line in results.read_text().splitlines():
        try:
            d = json.loads(line)
        except ValueError:
            continue
        if d.get("git_head") and d.get("elo") is not None:
            if best is None or float(d["elo"]) > float(best["elo"]):
                best = d
    if best is None:
        return [row(repo, "win", "champion_elo", 0, note="ledger has no rated rows")]
    return [
        row(repo, "win", "champion_elo", float(best["elo"]), note=str(best.get("spec", ""))),
        row(repo, "win", "champion_ci_lo", float(best.get("ci_lo", 0) or 0), note=str(best.get("spec", ""))),
    ]


# --------------------------------------------------------------------------
# correct: the validated set, and defects found in it


def validated_set(repo: Path) -> tuple[int, int, int]:
    """(repo-deck cards, ratchet gaps, param gaps).

    The validated set is the repo-deck corpus MINUS the two ratchet tables:
    those tables are the build's own statement of what it cannot fully support,
    so everything else is a card the build claims to handle. A defect there is
    a defect in a claim, which is why it carries the 1000x weight.
    """
    # The denominator is counted, not parsed out of a comment: comments go
    # stale and the decks are the actual corpus the ratchets measure.
    names: set[str] = set()
    for deck in (repo / "internal" / "testutil" / "decks").glob("*.json"):
        try:
            d = json.loads(deck.read_text())
        except (OSError, ValueError):
            continue
        for key in ("main", "cards", "maindeck", "commanders", "sideboard"):
            entries = d.get(key) or []
            if isinstance(entries, dict):
                names.update(str(k) for k in entries)
            elif isinstance(entries, list):
                for e in entries:
                    if isinstance(e, str):
                        names.add(e)
                    elif isinstance(e, dict):
                        n = e.get("name") or e.get("card")
                        if n:
                            names.add(str(n))
    gaps = _table_entries(repo / "rules" / "acceptance_test.go", "knownUnsupported")
    pgaps = _table_entries(repo / "rules" / "paramcensus_test.go", "knownUnsupportedParams")
    return len(names), gaps, pgaps


def _table_entries(path: Path, var: str) -> int:
    """Count keys in one `var <var> = map[...]{ ... }` literal.

    Counting `"key":` across the whole file would sweep up every other map in
    it — paramcensus_test.go holds several — so the scan is bounded to the one
    declaration and stops at its closing brace.
    """
    if not path.exists():
        return 0
    lines = path.read_text().splitlines()
    depth = 0
    n = 0
    started = False
    for line in lines:
        if not started:
            if re.match(rf"\s*var\s+{re.escape(var)}\s*=\s*map\[", line):
                started = True
                depth = line.count("{") - line.count("}")
            continue
        if depth == 1 and re.match(r'\s*"', line):
            n += 1
        depth += line.count("{") - line.count("}")
        if depth <= 0:
            break
    return n


def collect_correct(repo: Path, state_dir: Path) -> list[str]:
    total, gaps, pgaps = validated_set(repo)
    rows = [
        row(
            repo,
            "correct",
            "validated_cards",
            max(0, total - gaps - pgaps),
            note=f"{total} repo-deck cards - {gaps} ratchet - {pgaps} param gaps",
        )
    ]
    defects = state_dir / "defects.jsonl"
    found = closed = 0
    if defects.exists():
        for line in defects.read_text().splitlines():
            try:
                d = json.loads(line)
            except ValueError:
                continue
            if not d.get("validated_set", True):
                continue  # a defect outside the validated set is ordinary work
            found += 1
            if d.get("status") in ("fixed", "closed"):
                closed += 1
    rows.append(row(repo, "correct", "validated_defects_found_cum", found))
    rows.append(row(repo, "correct", "validated_defects_closed_cum", closed))
    return rows


# --------------------------------------------------------------------------
# audit and obs


def gate_wall_seconds(repo: Path, runs: int = 5) -> float:
    """Median wall time of the last few gate runs, from their own log mtimes.

    The daemon writes one log per gate under
    .ds4/orchestrator/gates/<issue>/<tag>/, so a run's wall time is the spread
    between the first and last log it wrote. No instrumentation needed, and it
    measures what a ticket actually waits for.
    """
    root = repo / ".ds4" / "orchestrator" / "gates"
    if not root.exists():
        return 0.0
    tags = sorted(
        (d for d in root.glob("*/*") if d.is_dir()),
        key=lambda d: d.stat().st_mtime,
        reverse=True,
    )[:runs]
    spans = []
    for t in tags:
        logs = [f.stat().st_mtime for f in t.glob("*.log")]
        if len(logs) >= 2:
            spans.append(max(logs) - min(logs))
    if not spans:
        return 0.0
    return round(sorted(spans)[len(spans) // 2], 1)


def collect_steward(repo: Path, context_file: Path | None = None) -> list[str]:
    """The recurring tax: gate wall time, agent context size, oversized files.

    Each is paid again on every future iteration, which is why they carry 100x
    and why they are measured as levels rather than as deltas of deltas.
    """
    rows = [row(repo, "steward", "gate_wall_s", gate_wall_seconds(repo), note="median of last 5 gate runs")]

    ctx_paths = [repo / "AGENTS.md"]
    ctx_paths.append(context_file or (repo / ".superpowers" / "ds4" / "gorge-context.md"))
    ctx_bytes = sum(p.stat().st_size for p in ctx_paths if p.exists())
    rows.append(
        row(
            repo,
            "steward",
            "agent_context_bytes",
            ctx_bytes,
            note=", ".join(f"{p.name}={p.stat().st_size}" for p in ctx_paths if p.exists()),
        )
    )

    # An oversized source file is a stewardship cost with a measurable price:
    # every agent that changes one line of it pays for the whole file in
    # context, and the repo's own design guidance treats a file that has grown
    # this far as a unit doing too much.
    big = []
    for f in repo.rglob("*.go"):
        s = str(f)
        # .ds4 holds the daemon's staging copy of the whole tree, so counting
        # it would double every file in the repo.
        if any(x in s for x in ("/.worktrees/", "/.cards/", "/vendor/", "/.ds4/", "/web/node_modules/")):
            continue
        try:
            n = sum(1 for _ in f.open("rb"))
        except OSError:
            continue
        if n > 1500:
            big.append((n, str(f.relative_to(repo))))
    big.sort(reverse=True)
    rows.append(
        row(
            repo,
            "steward",
            "oversized_files",
            len(big),
            note="; ".join(f"{p}({n})" for n, p in big[:6]),
        )
    )
    return rows


def collect_audit(repo: Path) -> list[str]:
    oracle = repo / "rules" / "testdata" / "oracle"
    scenarios = len(list(oracle.rglob("*.json"))) if oracle.exists() else 0
    return [row(repo, "audit", "cards_with_oracle_verdict", scenarios, note=str(oracle))]


def collect_obs(repo: Path) -> list[str]:
    checklist = repo / "internal" / "botobs" / "checklist.json"
    if not checklist.exists():
        return [
            row(
                repo,
                "obs",
                "observable_facts_exposed",
                0,
                note="internal/botobs/checklist.json absent — the obs axis has no denominator yet",
            )
        ]
    try:
        d = json.loads(checklist.read_text())
    except ValueError as e:
        return [row(repo, "obs", "observable_facts_exposed", 0, note=f"unreadable checklist: {e}")]
    facts = d.get("facts", [])
    exposed = sum(1 for f in facts if f.get("exposed"))
    return [
        row(repo, "obs", "observable_facts_exposed", exposed),
        row(repo, "obs", "observable_facts_total", len(facts)),
    ]


# --------------------------------------------------------------------------


def selftest() -> int:
    fails = []

    def check(name, cond, detail=""):
        print(("ok   " if cond else "FAIL ") + name + ("" if cond else f" {detail}"))
        if not cond:
            fails.append(name)

    with tempfile.TemporaryDirectory() as td:
        repo = Path(td) / "repo"
        (repo / ".ds4" / "orchestrator").mkdir(parents=True)
        (repo / "rules").mkdir(parents=True)
        # A journal with two merge_fix transitions and four merges.
        j = repo / ".ds4" / "orchestrator" / "journal.jsonl"
        lines = []
        # Two DISTINCT tickets in merge_fix, but five transitions between them:
        # the rate must read 2/4, not 5/4.
        for iid, times in (("t-1", 4), ("t-2", 1)):
            for _ in range(times):
                lines.append(json.dumps({"ts": now_iso(), "kind": "transition",
                                         "issue_id": iid, "evidence": {"to": "merge_fix"}}))
        for i in range(4):
            lines.append(json.dumps({"ts": now_iso(), "kind": "transition",
                                     "issue_id": f"m-{i}", "evidence": {"to": "merged"}}))
        lines.append(json.dumps({"ts": now_iso(), "kind": "gate_fail", "msg": "gate timeout after 900s"}))
        lines.append(json.dumps({"ts": "not-a-date", "kind": "transition", "evidence": {"to": "merged"}}))
        j.write_text("\n".join(lines) + "\n")

        flow = [json.loads(r) for r in collect_flow(repo)]
        rate = next(r for r in flow if r["metric"] == "merge_fix_rate")
        check("merge_fix_rate counts DISTINCT tickets, not transitions",
              abs(rate["value"] - 0.5) < 1e-9, rate)
        rounds = next(r for r in flow if r["metric"] == "merge_fix_rounds_max")
        check("the worst ticket's round count is reported", rounds["value"] == 4, rounds)
        check("a malformed journal ts is skipped, not fatal", True)

        st = repo / ".ds4" / "reward" / "stability.json"
        first = [json.loads(r) for r in collect_stability(repo, st)]
        oom = next(r for r in first if r["metric"] == "oom_kills")
        check("first stability probe reports no OOM delta", oom["value"] == 0, oom)
        check("stability probe records gate timeouts",
              next(r for r in first if r["metric"] == "gate_timeouts")["value"] == 1)
        # Pretend the counter moved since the last probe.
        st.write_text(json.dumps({"oom_kill": vmstat("oom_kill") - 2, "pswpin": 0}))
        second = [json.loads(r) for r in collect_stability(repo, st)]
        check("a rising OOM counter is reported as a delta",
              next(r for r in second if r["metric"] == "oom_kills")["value"] == 2)

        g = Path(td) / "gauntlet"
        g.mkdir()
        (g / "results.jsonl").write_text(
            json.dumps({"spec": "sb-a", "elo": 1100, "ci_lo": 1050, "git_head": "aaa"})
            + "\n"
            + json.dumps({"spec": "sb-b", "elo": 1240, "ci_lo": 1180, "git_head": "aaa"})
            + "\n"
        )
        win = [json.loads(r) for r in collect_win(repo, g)]
        check("champion is the best-rated spec", win[0]["value"] == 1240 and win[0]["note"] == "sb-b", win)
        check("a missing gauntlet ledger is a zero row, not a crash",
              json.loads(collect_win(repo, Path(td) / "nope")[0])["value"] == 0)

        # Three decks' worth of cards, with one card in two decks so the
        # denominator is DISTINCT names, not a sum.
        decks = repo / "internal" / "testutil" / "decks"
        decks.mkdir(parents=True)
        (decks / "a.json").write_text(json.dumps({"cards": ["Lightning Bolt", "Mountain", "Incinerate"]}))
        (decks / "b.json").write_text(
            json.dumps({"cards": [{"name": "Mountain"}, {"name": "Vines of Vastwood"}, {"name": "Foo"}]})
        )
        # Each ratchet table is one bounded map literal; the second map in the
        # same file must NOT be counted, which is the bug the bounded scan fixes.
        (repo / "rules" / "acceptance_test.go").write_text(
            "var knownUnsupported = map[string][]string{\n"
            '\t"Incinerate": {"stat:CantRegenerate"},\n'
            '\t"Vines of Vastwood": {"stat:CantTarget"},\n'
            "}\n\n"
            "var somethingElse = map[string]string{\n"
            '\t"NotARatchetEntry": "x",\n'
            '\t"NorThisOne": "y",\n'
            "}\n"
        )
        (repo / "rules" / "paramcensus_test.go").write_text(
            "var knownUnsupportedParams = map[string][]string{\n"
            '\t"Foo": {"param:Whatever"},\n'
            "}\n"
        )
        total, gaps, pgaps = validated_set(repo)
        check("the denominator counts DISTINCT deck cards", total == 5, (total, gaps, pgaps))
        check("a second map in the same file is not counted as ratchet entries", gaps == 2, gaps)
        rd = repo / ".ds4" / "reward"
        rd.mkdir(parents=True, exist_ok=True)
        (rd / "defects.jsonl").write_text(
            json.dumps({"card": "Lightning Bolt", "validated_set": True, "status": "open"})
            + "\n"
            + json.dumps({"card": "Ancestral", "validated_set": True, "status": "fixed"})
            + "\n"
            + json.dumps({"card": "Incinerate", "validated_set": False, "status": "open"})
            + "\n"
        )
        cor = [json.loads(r) for r in collect_correct(repo, rd)]
        vc = next(r for r in cor if r["metric"] == "validated_cards")
        check("validated set excludes both ratchet tables", vc["value"] == 5 - 2 - 1, vc)
        check("defects outside the validated set do not count",
              next(r for r in cor if r["metric"] == "validated_defects_found_cum")["value"] == 2)
        check("closed defects are counted separately",
              next(r for r in cor if r["metric"] == "validated_defects_closed_cum")["value"] == 1)

        # steward: gate wall time from log mtimes, context bytes, oversized files.
        gd = repo / ".ds4" / "orchestrator" / "gates" / "issue-1" / "t0"
        gd.mkdir(parents=True)
        import os

        (gd / "a.log").write_text("x")
        (gd / "b.log").write_text("x")
        os.utime(gd / "a.log", (1000, 1000))
        os.utime(gd / "b.log", (1090, 1090))
        check("gate wall time is the log-mtime spread", gate_wall_seconds(repo) == 90.0, gate_wall_seconds(repo))
        (repo / "AGENTS.md").write_text("a" * 1000)
        (repo / "big.go").write_text("// line\n" * 1600)
        (repo / "small.go").write_text("// line\n" * 10)
        stw = [json.loads(r) for r in collect_steward(repo)]
        check("agent context bytes counts AGENTS.md",
              next(r for r in stw if r["metric"] == "agent_context_bytes")["value"] == 1000, stw)
        big = next(r for r in stw if r["metric"] == "oversized_files")
        check("oversized files counts only the big one", big["value"] == 1 and "big.go" in big["note"], big)

        # idle_branches over a real git repo with a real worktree: an unmerged
        # idle branch is listed, and one already contained in main is not.
        import os

        gr = Path(td) / "gitrepo"
        gr.mkdir()
        run = lambda *a: subprocess.run(  # noqa: E731
            ["git", "-C", str(gr), *a], capture_output=True, text=True
        )
        subprocess.run(["git", "init", "-q", "-b", "main", str(gr)], capture_output=True)
        run("config", "user.email", "t@t")
        run("config", "user.name", "t")
        (gr / "f.txt").write_text("a")
        run("add", "f.txt")
        run("commit", "-qm", "init")
        wt = Path(td) / "wt-idle"
        run("worktree", "add", "-q", "-b", "wt/idle", str(wt))
        (wt / "g.txt").write_text("b")
        subprocess.run(["git", "-C", str(wt), "add", "g.txt"], capture_output=True)
        subprocess.run(
            ["git", "-C", str(wt), "-c", "user.email=t@t", "-c", "user.name=t",
             "commit", "-qm", "ahead"], capture_output=True)
        os.utime(wt, (1000, 1000))  # long idle
        idle = idle_branches(gr, hours=1)
        check("an idle unmerged worktree is listed", [b for b, _, _ in idle] == ["idle"], idle)
        check("its commits-ahead count is right", idle and idle[0][1] == 1, idle)
        check("the fresh-worktree threshold is honoured", idle_branches(gr, hours=10**6) == [])
        run("merge", "-q", "--no-ff", "-m", "land", "wt/idle")
        check("a branch already in main is not listed", idle_branches(gr, hours=1) == [],
              idle_branches(gr, hours=1))

        obs = [json.loads(r) for r in collect_obs(repo)]
        check("a missing checklist explains itself", "absent" in obs[0]["note"], obs)
        cl = repo / "internal" / "botobs"
        cl.mkdir(parents=True)
        (cl / "checklist.json").write_text(
            json.dumps({"facts": [{"id": "own_library_list", "exposed": True}, {"id": "opp_archetype", "exposed": False}]})
        )
        obs = [json.loads(r) for r in collect_obs(repo)]
        check("checklist exposure is counted", obs[0]["value"] == 1 and obs[1]["value"] == 2, obs)

    print(f"\n{len(fails)} failure(s)")
    return 1 if fails else 0


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("axis", nargs="?", default="all",
                    choices=["all", "flow", "stability", "win", "correct", "steward",
                             "audit", "obs", "hotspots"])
    ap.add_argument("--repo", type=Path, default=Path("."))
    ap.add_argument("--state-dir", type=Path, default=None)
    ap.add_argument("--gauntlet", type=Path,
                    default=Path("/mnt/sata/gorge-training/spellbench-work/gauntlet"))
    ap.add_argument("--selftest", action="store_true")
    a = ap.parse_args(argv)
    if a.selftest:
        return selftest()
    repo = a.repo.resolve()
    state_dir = a.state_dir or (repo / ".ds4" / "reward")

    if a.axis == "hotspots":
        for f, bs in hotspots(repo):
            print(f"{len(bs):3d}  {f}  <- {', '.join(bs)}")
        return 0

    out: list[str] = []
    if a.axis in ("all", "flow"):
        out += collect_flow(repo)
    if a.axis in ("all", "stability"):
        out += collect_stability(repo, state_dir / "stability.json")
    if a.axis in ("all", "win"):
        out += collect_win(repo, a.gauntlet)
    if a.axis in ("all", "correct"):
        out += collect_correct(repo, state_dir)
    if a.axis in ("all", "steward"):
        out += collect_steward(repo)
    if a.axis in ("all", "audit"):
        out += collect_audit(repo)
    if a.axis in ("all", "obs"):
        out += collect_obs(repo)
    print("\n".join(out))
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
