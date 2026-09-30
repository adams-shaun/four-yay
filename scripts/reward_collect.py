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
import os
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


def ds4_dir(repo: Path, leaf: str) -> Path | None:
    """Resolve a repo-level shared-state path under `.ds4/` from anywhere.

    The measured repo may be a task worktree (a seat re-runs this script with
    `--repo .`), and a worktree's own `.ds4/` is partial: the controller copies
    only what the brief needs, while `orchestrator/` and `reward/` live only in
    the shared checkout. So the shared state is resolved through the git common
    dir first and `repo` itself is the fallback -- the same roots the issue
    store in `closed_issue_ids` already walks -- and the FIRST root where
    `<root>/.ds4/<leaf>` exists wins, so a copy the controller did place in the
    worktree still wins over the shared one. None means the state is absent:
    callers fail open exactly as they did when the plain `repo / ".ds4"` path
    was missing. Path predicates swallow PermissionError, so a common-dir
    parent the caller cannot stat (a jail binding only the worktree) falls
    through to the fallback harmlessly.
    """
    roots: list[Path] = []
    common = git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir").strip()
    if common:
        roots.append(Path(common).parent)
    roots.append(repo)
    return next((r / ".ds4" / leaf for r in roots if (r / ".ds4" / leaf).exists()), None)


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
    p = ds4_dir(repo, "orchestrator/journal.jsonl")
    if p is None:
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


# A branch whose ticket is CLOSED is residue, not live work. The statuses are
# agentctl's CLOSED_STATUSES (agentctl/ledger/issues.py) mirrored here: a
# `merged` branch is an ancestor of main, and a `superseded` ticket's lingering
# worktree is something the daemon's own audit already names as residue
# (`closed_issue_worktree`). A human_needed, parked or waiting ticket still
# holds real work, so it stays live.
CLOSED_ISSUE_STATUSES = frozenset({"merged", "superseded"})


def closed_issue_ids(repo: Path) -> set[str]:
    """Issue ids whose ticket is closed (merged or superseded).

    Read from the repo's issue store, `.ds4/issues/*.md` front matter. The
    store is repo-level shared state that a task worktree deliberately does
    not carry, so the shared checkout is resolved through the git common dir
    first and `repo` itself is the fallback. Anything unreadable -- no store,
    no front matter, no status line -- contributes nothing, so the caller then
    sees every branch as live: the behaviour before this filter, and the safe
    direction for a measurement that cannot read the tickets.
    """
    store = ds4_dir(repo, "issues")
    if store is None:
        return set()
    out: set[str] = set()
    for p in sorted(store.glob("*.md")):
        status = ""
        try:
            with p.open() as fh:
                for line in fh:
                    if not line.strip():
                        break  # the front matter ends at the first blank line
                    m = re.match(r"status:\s*(\S+)", line)
                    if m:
                        status = m.group(1)
                        break
        except OSError:
            continue
        if status in CLOSED_ISSUE_STATUSES:
            out.add(p.stem)
    return out


def _tree_blobs(repo: Path, ref: str, paths: list[str]) -> dict[str, str]:
    """Blob sha per path in `ref`'s tree, one git call for the whole list."""
    out = git(repo, "ls-tree", "-r", ref, "--", *paths)
    blobs: dict[str, str] = {}
    for line in out.splitlines():
        meta, _, path = line.partition("\t")
        parts = meta.split()
        if len(parts) >= 3:
            blobs[path] = parts[2]
    return blobs


def _is_ancestor(repo: Path, ancestor: str, ref: str) -> bool:
    """True when `ancestor` is already contained in `ref`'s history.

    `git merge-base --is-ancestor` signals through its exit code, and the
    `git()` helper folds a non-zero exit into an empty string, so it cannot
    answer this. A subprocess run that reads the return code directly is the
    one honest way, and it is cheap: the caller only asks it of branches that
    already share a file.
    """
    p = subprocess.run(
        ["git", "-C", str(repo), "merge-base", "--is-ancestor", ancestor, ref],
        capture_output=True, text=True, timeout=120,
    )
    return p.returncode == 0


def live_branch_files(repo: Path) -> dict[str, list[str]]:
    """Files several live task branches change AWAY from main, keyed by file.

    This is the PREDICTIVE half of the flow axis: it names the files that
    several in-flight branches are editing right now, before any of them
    reaches merge_fix. Acting on it (splitting the file, or sequencing the
    tickets) is what removes the hot spot.

    A file counts once per DISTINCT content a live branch would introduce.
    `git diff main...ref` is the branch's own work since the fork, but it
    lists a file the branch added even when main has since landed the
    identical content -- and a branch that merely carries the same blob as
    main, or the same blob as another live branch, cannot conflict with
    either. Counting those produced briefs to split a file no second branch
    was actually editing (measured 2026-09-29: two of three \"editors\" of
    docs/superpowers/specs/2026-09-28-scam-exe-combo-lines.md held main's
    exact blob; only one differed). So each branch is kept only for paths
    where its blob differs from main's, and identical blobs are deduped.

    "Live" means an OPEN ticket, or a hand branch with no ticket at all. The
    worktree enumeration cannot tell a closed ticket's worktree from an open
    one, so a superseded seat's branch kept counting as an editor and held a
    file "contended" that nobody would ever land against. Measured
    2026-09-30: scripts/reward_collect.py read as a two-branch hot spot whose
    two editors were both seats whose tickets had been superseded 18 minutes
    earlier -- a brief was minted to remove a contention between two dead
    branches (cli-20260930T000410Z-3208f470).

    A branch already CONTAINED in another editor of the same file is not a
    second editor of it: the descendant's merge takes the ancestor by
    construction, so landing either one first is clean and there is no
    resolver round to remove. Counting a stacked chain as contention minted
    briefs for an already-sequenced workstream (measured 2026-09-30: the
    searchbench replication -- `wt/gorge-searchbench` <= `wt/sbrep-search`
    <= `wt/sbrep`, one linear chain -- read as two and three editors of the
    same eleven files, so a `flow-hotspot` ticket asked to split files no
    second uncontrolled branch was editing). Each file keeps only its
    MAXIMAL editors -- those no other editor of that same file contains --
    so a third branch genuinely colliding with the ancestor still counts.
    """
    closed = closed_issue_ids(repo)
    editors: dict[str, list[tuple[str, str]]] = collections.defaultdict(list)
    seen_blob: dict[str, set[str]] = collections.defaultdict(set)
    for line in git(repo, "worktree", "list", "--porcelain").splitlines():
        if not line.startswith("branch "):
            continue
        ref = line.split(None, 1)[1].strip()
        name = ref.rsplit("/", 1)[-1]
        if ref == "refs/heads/main" or name in closed:
            continue
        # Own work since the fork, intersected with paths whose content DIFFERS
        # from main's tree now (a landed-elsewhere file is not a change).
        changed = git(repo, "diff", "--name-only", f"main...{ref}").split()
        differs = set(git(repo, "diff", "--name-only", "main", ref).split())
        targets = sorted(f for f in changed if f in differs)
        if not targets:
            continue
        blobs = _tree_blobs(repo, ref, targets)
        for f in targets:
            b = blobs.get(f, "")
            if b in seen_blob[f]:
                continue  # same content another live branch already contributes
            seen_blob[f].add(b)
            editors[f].append((name, ref))
    files: dict[str, list[str]] = {}
    for f, branch_set in editors.items():
        kept = sorted(
            name for name, ref in branch_set
            if not any(other != ref and _is_ancestor(repo, ref, other)
                       for _, other in branch_set)
        )
        if kept:
            files[f] = kept
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


def _proc_ppid(pid_dir: Path) -> int:
    """The parent pid from /proc/<pid>/stat's `comm` field, robust to a comm
    containing spaces or parens: split on the LAST `)`, which is the closing
    paren of `(comm)` -- everything after it is `state ppid ...`."""
    try:
        after = pid_dir.joinpath("stat").read_text().rsplit(")", 1)[1].split()
        return int(after[1])
    except (OSError, ValueError, IndexError):
        return -1


def gorged_processes(proc: Path | None = None) -> list[dict]:
    """Every live `gorged` table server on the box: pid, ppid, `-addr` and
    `-dir`. Matched by cmdline FLAGS (`-addr` and `-tables` both present), not
    argv[0]/comm -- AGENTS.md's own caveat is that a locally rebuilt binary's
    comm need not be `gorged`, while a table server's own startup flags are
    stable regardless of what the binary is named.

    `proc` defaults to `$GORGE_PROC_DIR` or `/proc`, so a test harness can
    scope this to a fake tree instead of picking up the real box's processes
    (the same problem `PiHarness.running_seats`'s own `proc` param solves)."""
    proc = proc or Path(os.environ.get("GORGE_PROC_DIR", "/proc"))
    out = []
    try:
        entries = list(proc.iterdir())
    except OSError:
        return out
    for d in entries:
        if not d.name.isdigit():
            continue
        try:
            argv = d.joinpath("cmdline").read_bytes().split(b"\0")
        except OSError:
            continue
        args = [a.decode(errors="replace") for a in argv if a]
        if "-addr" not in args or "-tables" not in args:
            continue
        addr = next((args[i + 1] for i, a in enumerate(args[:-1]) if a == "-addr"), "")
        gdir = next((args[i + 1] for i, a in enumerate(args[:-1]) if a == "-dir"), "")
        out.append({"pid": int(d.name), "ppid": _proc_ppid(d), "addr": addr, "dir": gdir})
    return out


def _proc_is_script_supervisor(proc: Path, ppid: int) -> bool:
    """True when `ppid` is a LIVE, non-init process running a shell script
    (`<something>.sh` in its argv), read from the SAME `proc` root as the
    gorged it supervises -- never a hardcoded /proc, so a fake tree works.

    This is the second ownership signal: a gorged whose parent is such a
    process is supervised by a repo-side script that starts its farm with a
    plain `&` and reaps it from an EXIT trap (the `scripts/smoke.sh` shape),
    so it must not count as a standing instance. The check is deliberately
    structural rather than a name list: ANY live non-init script parent means
    the child is inside a supervised scope, so a future supervising script is
    covered without editing this function.

    Fails closed on every unknown (parent gone/reparented, cmdline
    unreadable). Notably the demo's parent is `systemd --user`, whose argv
    carries no `.sh` and whose cmdline may be unreadable -- either way this
    returns False and the intended standing instance is still counted.
    """
    if ppid <= 1:
        return False
    try:
        argv = proc.joinpath(str(ppid), "cmdline").read_bytes().split(b"\0")
    except OSError:
        return False
    return any(a.decode(errors="replace").rsplit("/", 1)[-1].endswith(".sh")
               for a in argv if a)


def _live_agent_dirs(proc: Path | None = None) -> list[Path]:
    """The `--cwd` of every live pi-agent-shaped process, for telling a dev
    gorged a SEAT is actively using apart from a standing one nobody owns."""
    proc = proc or Path(os.environ.get("GORGE_PROC_DIR", "/proc"))
    dirs: list[Path] = []
    try:
        entries = list(proc.iterdir())
    except OSError:
        return dirs
    for d in entries:
        if not d.name.isdigit():
            continue
        try:
            argv = d.joinpath("cmdline").read_bytes().split(b"\0")
        except OSError:
            continue
        args = [a.decode(errors="replace") for a in argv if a]
        if not any("pi-agent" in a for a in args):
            continue
        cwd = next((args[i + 1] for i, a in enumerate(args[:-1]) if a == "--cwd"), "")
        if cwd:
            try:
                dirs.append(Path(cwd).resolve())
            except OSError:
                pass
    return dirs


def collect_gorged_hygiene(proc: Path | None = None) -> tuple[int, int, str]:
    """(standing_excess, unsafe_launches, note): the two gorged-process
    hygiene failing signals (operator ruling 2026-09-30).

    standing_excess: gorged processes NOT inside any currently-live agent's
    own worktree -- the box's standing/demo instances -- above the documented
    default of exactly one (AGENTS.md: "8080-8081: the demo", singular). A
    second such instance (observed: a `-manabrew` table server parked on
    :8081 alongside the intended :8080 demo, 4 tables instead of the
    configured 1) is the failing signal, at the ordinary stability weight.

    unsafe_launches: gorged processes reparented to init (`ppid == 1`) --
    detached from whatever process created them, so killing that process's
    tree would never have reaped this one. That is a launch-TIME defect
    (nothing wired a trap or a supervising scope) independent of whether the
    box is harmed by it right now, which is why it is a stewardship cost
    (100x) rather than a stability veto: allowing an agent to create a
    gorged instance with no safeguard ensuring its own cleanup.
    """
    procs = gorged_processes(proc)
    if not procs:
        return 0, 0, "no gorged process running"
    proc_root = proc or Path(os.environ.get("GORGE_PROC_DIR", "/proc"))
    owners = _live_agent_dirs(proc)

    def owned(p: dict) -> bool:
        gdir = p["dir"]
        if gdir:
            try:
                gp = Path(gdir).resolve()
            except OSError:
                gp = None
            if gp is not None and any(
                    gp == o or gp.is_relative_to(o) or o.is_relative_to(gp) for o in owners):
                return True
        return _proc_is_script_supervisor(proc_root, p["ppid"])

    standing = [p for p in procs if not owned(p)]
    excess = max(0, len(standing) - 1)
    unsafe = sum(1 for p in procs if p["ppid"] == 1)
    note = "; ".join(f"pid={p['pid']} addr={p['addr']} dir={p['dir']}" for p in procs[:6])
    return excess, unsafe, note


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
    excess, _, gorged_note = collect_gorged_hygiene()
    rows.append(row(repo, "stability", "standing_gorged_excess", excess, note=gorged_note))
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
    root = ds4_dir(repo, "orchestrator/gates")
    if root is None:
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
        # Exclusion is by path components RELATIVE to the measured root, not by
        # an absolute substring: a task agent runs this probe from inside its
        # own `.worktrees/<id>` worktree, where every file's absolute path
        # contains `/.worktrees/`, so an absolute test read 0 vacuously and
        # hid real oversized-file regressions. Nested copies inside the
        # measured root are still dropped — the landing checkout's
        # `.worktrees/` trees (which would double-count sibling agent
        # checkouts), the `.ds4` staging copy of the whole tree, the
        # gitignored `.cards/` corpus, `vendor/` and `node_modules/`.
        rel = f.relative_to(repo)
        if any(part in (".worktrees", ".cards", ".ds4", "vendor", "node_modules") for part in rel.parts):
            continue
        try:
            n = sum(1 for _ in f.open("rb"))
        except OSError:
            continue
        if n > 1500:
            big.append((n, str(rel)))
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
    _, unsafe, gorged_note = collect_gorged_hygiene()
    rows.append(row(repo, "steward", "unsafe_gorged_launches", unsafe, note=gorged_note))
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

        # A second fixture whose ABSOLUTE path contains /.worktrees/ — the
        # shape of a task-agent worktree, where the old absolute-substring
        # exclusion dropped every file and the metric read 0 vacuously. The
        # repo's own big.go must be counted; a NESTED .worktrees/ tree inside
        # the measured root must stay excluded.
        wtrepo = Path(td) / ".worktrees" / "agent-x"
        (wtrepo / "rules").mkdir(parents=True)
        (wtrepo / "big.go").write_text("// line\n" * 1600)
        (wtrepo / "small.go").write_text("// line\n" * 10)
        nested = wtrepo / ".worktrees" / "other" / "big.go"
        nested.parent.mkdir(parents=True)
        nested.write_text("// line\n" * 1600)
        check("worktree fixture's absolute path really contains /.worktrees/",
              "/.worktrees/" in str(wtrepo) + "/", str(wtrepo))
        check("nested fixture file really exceeds the 1500-line threshold",
              sum(1 for _ in nested.open("rb")) > 1500)
        wst = [json.loads(r) for r in collect_steward(wtrepo)]
        wbig = next(r for r in wst if r["metric"] == "oversized_files")
        check("worktree-shaped repo counts its own big.go",
              wbig["value"] == 1 and "big.go" in wbig["note"], wbig)
        check("worktree-shaped repo does not count the nested .worktrees copy",
              wbig["value"] == 1 and ".worktrees" not in wbig["note"], wbig)

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

        # live_branch_files over the same repo: a branch whose ticket is
        # closed is residue, not a live editor. The real case (2026-09-30):
        # two seats each fixed the same reward_collect.py bug; the operator
        # superseded both tickets 18 minutes later, but both worktrees
        # lingered on, so the file kept reading as a two-branch hot spot and a
        # brief was minted to remove a contention between two dead branches.
        check("with no issue store every branch is live (fail-open)",
              closed_issue_ids(gr) == set(), closed_issue_ids(gr))
        gone = Path(td) / "wt-gone"
        run("worktree", "add", "-q", "-b", "wt/gone", str(gone))
        (gone / "h.txt").write_text("residue")
        subprocess.run(["git", "-C", str(gone), "add", "h.txt"], capture_output=True)
        subprocess.run(
            ["git", "-C", str(gone), "-c", "user.email=t@t", "-c", "user.name=t",
             "commit", "-qm", "residue"], capture_output=True)
        live = Path(td) / "wt-live"
        run("worktree", "add", "-q", "-b", "wt/live", str(live))
        live2 = Path(td) / "wt-live2"
        run("worktree", "add", "-q", "-b", "wt/live2", str(live2))
        for wt, txt in ((live, "active"), (live2, "active too")):
            (wt / "g2.txt").write_text(txt)
            subprocess.run(["git", "-C", str(wt), "add", "g2.txt"], capture_output=True)
            subprocess.run(
                ["git", "-C", str(wt), "-c", "user.email=t@t", "-c", "user.name=t",
                 "commit", "-qm", f"active {txt}"], capture_output=True)
        check("the residue branch really changes h.txt against main",
              "h.txt" in git(gr, "diff", "--name-only", "main...wt/gone").split(),
              git(gr, "diff", "--name-only", "main...wt/gone"))
        check("both live branches really change g2.txt against main",
              all("g2.txt" in git(gr, "diff", "--name-only", f"main...wt/{b}").split()
                  for b in ("live", "live2")))
        store = gr / ".ds4" / "issues"
        store.mkdir(parents=True)
        (store / "gone.md").write_text(
            "---\nid: gone\ntitle: t\nstatus: superseded\n---\n\n## Report\n")
        (store / "live.md").write_text(
            "---\nid: live\ntitle: t\nstatus: dispatched\n---\n\n## Report\n")
        (store / "live2.md").write_text(
            "---\nid: live2\ntitle: t\nstatus: review\n---\n\n## Report\n")
        check("the store marks gone closed and the live pair open",
              closed_issue_ids(gr) == {"gone"}, closed_issue_ids(gr))
        lb = live_branch_files(gr)
        check("a closed ticket's branch is not a live editor",
              sorted(lb.get("g2.txt", [])) == ["live", "live2"] and "h.txt" not in lb, lb)
        hs = hotspots(gr)
        check("the live pair is still a hot spot, the residue is not",
              hs == [("g2.txt", sorted(["live", "live2"]))], hs)

        # The reward collectors must read repo-level shared state from inside
        # a task worktree (2026-09-30): a seat re-runs this script with
        # `--repo .` from its worktree, whose own .ds4 is PARTIAL -- the
        # controller copies only what the brief needs, and orchestrator/ and
        # reward/ exist only in the shared checkout -- so a plain
        # `repo / ".ds4"` read returned [] and 0.0 vacuously and every
        # flow/steward/correct metric scored perfect from any seat. The
        # resolution goes through the git common dir first, exactly as the
        # issue store above already does.
        orch = gr / ".ds4" / "orchestrator"
        orch.mkdir(parents=True)
        jr = orch / "journal.jsonl"
        jr.write_text(
            json.dumps({"ts": now_iso(), "kind": "transition",
                        "issue_id": "t-9", "evidence": {"to": "merge_fix"}}) + "\n")
        tag = orch / "gates" / "iss-x" / "gate"
        tag.mkdir(parents=True)
        (tag / "a.log").write_text("x")
        (tag / "b.log").write_text("y")
        os.utime(tag / "a.log", (10**6, 10**6))
        os.utime(tag / "b.log", (10**6 + 12, 10**6 + 12))
        (gr / ".ds4" / "reward").mkdir(exist_ok=True)
        (wt / ".ds4").mkdir(exist_ok=True)  # a seat's PARTIAL .ds4: briefs only
        (wt / ".ds4" / "notes.txt").write_text("brief and report copies")
        check("fixture: the worktree has a .ds4 without the shared state, the shared repo has it",
              (wt / ".ds4").is_dir() and not (wt / ".ds4" / "orchestrator").exists()
              and jr.is_file() and (gr / ".ds4" / "reward").is_dir()
              and len(list((orch / "gates").glob("*/*"))) == 1)
        check("journal_rows resolves the shared journal from the worktree",
              [r.get("issue_id") for r in journal_rows(wt)] == ["t-9"], journal_rows(wt))
        check("gate_wall_seconds resolves the shared gate logs from the worktree",
              gate_wall_seconds(wt) == 12.0, gate_wall_seconds(wt))
        check("the state-dir default resolves the shared reward dir from the worktree",
              ds4_dir(wt, "reward") == gr / ".ds4" / "reward")
        empty = Path(td) / "no-state"
        empty.mkdir()
        check("with no .ds4 anywhere the readers fail open as before",
              journal_rows(empty) == [] and gate_wall_seconds(empty) == 0.0
              and ds4_dir(empty, "reward") is None)

        # live_branch_files counts DISTINCT content a live branch would add, not
        # branches that merely carry a file. A file main already holds (same
        # blob) is not a change, and two branches contributing the same blob are
        # not two editors -- otherwise a stacked worktree files briefs to split
        # a file nobody is editing (the bug this test pins).
        hr = Path(td) / "hotrepo"
        hr.mkdir()
        hrun = lambda *a: subprocess.run(  # noqa: E731
            ["git", "-C", str(hr), *a], capture_output=True, text=True
        )
        hcommit = lambda wt, msg: subprocess.run(  # noqa: E731
            ["git", "-C", str(wt), "-c", "user.email=t@t", "-c", "user.name=t",
             "commit", "-qm", msg], capture_output=True
        )
        subprocess.run(["git", "init", "-q", "-b", "main", str(hr)], capture_output=True)
        hrun("config", "user.email", "t@t")
        hrun("config", "user.name", "t")
        (hr / "base.txt").write_text("base\n")
        hrun("add", "base.txt")
        hrun("commit", "-qm", "init")
        base = hrun("rev-parse", "HEAD").stdout.strip()

        # wt/land adds shared.txt; main then LANDS the identical blob.
        wtl = Path(td) / "hot-land"
        hrun("worktree", "add", "-q", "-b", "wt/land", str(wtl), base)
        (wtl / "shared.txt").write_text("same\n")
        subprocess.run(["git", "-C", str(wtl), "add", "shared.txt"], capture_output=True)
        hcommit(wtl, "add shared")
        hrun("merge", "-q", "--no-ff", "-m", "land shared", "wt/land")
        main_blob = hrun("rev-parse", "main:shared.txt").stdout.strip()
        check("precondition: main landed the shared blob", len(main_blob) == 40, main_blob)

        # wt/dup re-adds the SAME blob from the pre-main fork: not an editor.
        wtd = Path(td) / "hot-dup"
        hrun("worktree", "add", "-q", "-b", "wt/dup", str(wtd), base)
        (wtd / "shared.txt").write_text("same\n")
        subprocess.run(["git", "-C", str(wtd), "add", "shared.txt"], capture_output=True)
        hcommit(wtd, "re-add shared")

        one = live_branch_files(hr)
        check("a branch holding main's own blob is not a second editor",
              "shared.txt" not in one, one)

        # wt/other adds a DIFFERENT blob: now exactly two distinct contents.
        wto = Path(td) / "hot-other"
        hrun("worktree", "add", "-q", "-b", "wt/other", str(wto), base)
        (wto / "shared.txt").write_text("other\n")
        subprocess.run(["git", "-C", str(wto), "add", "shared.txt"], capture_output=True)
        hcommit(wto, "change shared")

        # wt/other2 adds yet another distinct blob; only NOW are there two
        # distinct contents beyond main's, so only now is the file hot.
        wto2 = Path(td) / "hot-other2"
        hrun("worktree", "add", "-q", "-b", "wt/other2", str(wto2), base)
        (wto2 / "shared.txt").write_text("other2\n")
        subprocess.run(["git", "-C", str(wto2), "add", "shared.txt"], capture_output=True)
        hcommit(wto2, "change shared again")

        two = live_branch_files(hr)
        check("two distinct contents make the file a hot spot",
              len(two.get("shared.txt", [])) == 2, two.get("shared.txt"))
        check("the landed-identical branch is still excluded",
              "dup" not in two.get("shared.txt", []), two.get("shared.txt"))
        check("hotspots reports it", any(f == "shared.txt" for f, _ in hotspots(hr)), hotspots(hr))

        # A STACKED chain is not contention: wt/chain-a <= wt/chain-b <=
        # wt/chain-c each edit chain.txt, but every earlier branch is contained
        # in the next, so the chain merges clean however it lands. The real
        # case (2026-09-30): the searchbench replication is one such chain
        # (`wt/gorge-searchbench` <= `wt/sbrep-search` <= `wt/sbrep`) and read
        # as two and three editors of the same eleven files, minting a brief
        # to split files no second uncontrolled branch was editing. Only the
        # MAXIMAL editor of a file may count.
        wca = Path(td) / "hot-chain-a"
        hrun("worktree", "add", "-q", "-b", "wt/chain-a", str(wca), base)
        (wca / "chain.txt").write_text("a\n")
        subprocess.run(["git", "-C", str(wca), "add", "chain.txt"], capture_output=True)
        hcommit(wca, "chain a")
        wcb = Path(td) / "hot-chain-b"
        hrun("worktree", "add", "-q", "-b", "wt/chain-b", str(wcb), "wt/chain-a")
        (wcb / "chain.txt").write_text("b\n")
        subprocess.run(["git", "-C", str(wcb), "add", "chain.txt"], capture_output=True)
        hcommit(wcb, "chain b")
        wcc = Path(td) / "hot-chain-c"
        hrun("worktree", "add", "-q", "-b", "wt/chain-c", str(wcc), "wt/chain-b")
        (wcc / "chain.txt").write_text("c\n")
        subprocess.run(["git", "-C", str(wcc), "add", "chain.txt"], capture_output=True)
        hcommit(wcc, "chain c")
        check("precondition: the chain really stacks a <= b <= c",
              all(hrun("merge-base", "--is-ancestor", a, b).returncode == 0
                  for a, b in (("wt/chain-a", "wt/chain-b"),
                               ("wt/chain-b", "wt/chain-c"),
                               ("wt/chain-a", "wt/chain-c"))))
        check("precondition: every chain branch really changes chain.txt",
              all("chain.txt" in git(hr, "diff", "--name-only", "main...wt/chain-%s" % s).split()
                  for s in "abc"))
        chained = live_branch_files(hr)
        check("a stacked chain counts only its maximal editor",
              chained.get("chain.txt") == ["chain-c"], chained.get("chain.txt"))
        check("the stacked chain is not a hot spot",
              not any(f == "chain.txt" for f, _ in hotspots(hr)), hotspots(hr))

        # A sibling of the chain that also edits chain.txt is a REAL second
        # editor -- the guard that the maximal-editor rule did not simply drop
        # every non-first branch.
        wcx = Path(td) / "hot-chain-x"
        hrun("worktree", "add", "-q", "-b", "wt/chain-x", str(wcx), base)
        (wcx / "chain.txt").write_text("x\n")
        subprocess.run(["git", "-C", str(wcx), "add", "chain.txt"], capture_output=True)
        hcommit(wcx, "chain x")
        check("precondition: the sibling is NOT an ancestor of the chain",
              hrun("merge-base", "--is-ancestor", "wt/chain-x", "wt/chain-c").returncode != 0)
        sib = live_branch_files(hr)
        check("a sibling editor still makes the file a hot spot",
              sorted(sib.get("chain.txt", [])) == ["chain-c", "chain-x"], sib.get("chain.txt"))

        obs = [json.loads(r) for r in collect_obs(repo)]
        check("a missing checklist explains itself", "absent" in obs[0]["note"], obs)
        cl = repo / "internal" / "botobs"
        cl.mkdir(parents=True)
        (cl / "checklist.json").write_text(
            json.dumps({"facts": [{"id": "own_library_list", "exposed": True}, {"id": "opp_archetype", "exposed": False}]})
        )
        obs = [json.loads(r) for r in collect_obs(repo)]
        check("checklist exposure is counted", obs[0]["value"] == 1 and obs[1]["value"] == 2, obs)

        # gorged process hygiene (operator ruling 2026-09-30): a fake /proc
        # with the two real processes observed that day (the intended :8080
        # demo and a stray -manabrew instance parked on :8081), plus a live
        # pi-agent seat so a THIRD, agent-owned dev instance is not counted
        # as standing, and a fourth reparented-to-init instance for the
        # unsafe-launch signal.
        fproc = Path(td) / "proc"

        def fake_pid(pid: int, args: list[str], ppid: int) -> None:
            d = fproc / str(pid)
            d.mkdir(parents=True)
            d.joinpath("cmdline").write_bytes(b"\0".join(a.encode() for a in args) + b"\0")
            d.joinpath("stat").write_text(f"{pid} (gorged) S {ppid} {pid} {pid} 0 -1\n")

        fake_pid(101, ["bin/gorged", "-addr", "127.0.0.1:8080", "-dir", "/tmp/gorge-demo-pub",
                        "-tables", "1"], ppid=1)
        fake_pid(102, ["bin/gorged", "-addr", "127.0.0.1:8081", "-dir", "/tmp/gorge-demo-mb",
                        "-tables", "4", "-manabrew"], ppid=1)
        fake_pid(103, ["bin/gorged", "-addr", "127.0.0.1:8095", "-dir", "/tmp/gorge-dev-x",
                        "-tables", "1"], ppid=9001)
        fake_pid(9001, ["pi-agent", "--cwd", "/tmp/gorge-dev-x", "--name", "dev-x"], ppid=500)
        # A FIFTH instance whose parent is a live non-init repo-side script
        # (`scripts/smoke.sh` starts its farm with `&` and reaps it from an
        # EXIT trap). It must NOT count as standing: its real supervisor is
        # live and owns its cleanup, unlike the demo (whose parent systemd
        # has no `.sh` and is not caught). Without the supervision signal the
        # three standing instances 101/102/104 give an excess of 2.
        fake_pid(104, ["bin/gorged", "-addr", "127.0.0.1:8100", "-dir", "/tmp/gorge-smoke-public-PxZf9S",
                        "-tables", "1"], ppid=9002)
        fake_pid(9002, ["bash", "scripts/smoke.sh"], ppid=500)
        procs = gorged_processes(fproc)
        check("gorged_processes finds every table server by flags, not comm",
              {p["pid"] for p in procs} == {101, 102, 103, 104}, procs)
        # Precondition: the supervised instance really has a live, non-init
        # `.sh` parent in the fake tree -- otherwise the assertion below
        # would pass for the wrong reason (a missing parent pid).
        check("the smoke-supervised gorged's parent is a live non-init .sh script",
              _proc_is_script_supervisor(fproc, _proc_ppid(fproc / "104")) is True)
        excess, unsafe, _ = collect_gorged_hygiene(fproc)
        check("two standing instances against the default of one is an excess of 1",
              excess == 1, excess)
        check("the agent-owned dev instance does not count as standing", excess == 1, excess)
        check("a gorged supervised by a live repo-side script does not count as standing",
              excess == 1, excess)
        check("both init-reparented instances are unsafe launches, the supervised one is not",
              unsafe == 2, unsafe)

        empty_proc = Path(td) / "proc-empty"
        empty_proc.mkdir()
        check("no gorged process running is not a failure",
              collect_gorged_hygiene(empty_proc) == (0, 0, "no gorged process running"))

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
    state_dir = a.state_dir or (ds4_dir(repo, "reward") or (repo / ".ds4" / "reward"))

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
