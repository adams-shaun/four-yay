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
    scripts/reward_collect.py hotspots --repo R --format md [--max-rows N]
                                                     # the same table as
                                                     # markdown, with the durable
                                                     # "why it collides" notes
                                                     # from scripts/hotfiles-notes.json
                                                     # merged into each row
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


def _integrates(repo: Path, a: str, b: str) -> bool:
    """True when branch `a` has merged branch `b`'s work into itself.

    The signal is the deliberate merge commit: some merge in `main..a` (a
    commit with two parents, i.e. an integration rather than a fast-forward)
    has a parent `p` that is a point on `b`'s OWN branch line -- `p` lies on
    `b`'s first-parent history (`rev-list --first-parent main..b`), i.e. it
    was a trunk tip of `b` that `a` pulled in, and it is not in main's
    history. Reachability is not enough and would spoil the mutual-merge
    signal (measured 2026-10-01 on a scratch repo): a branch that merges a
    third branch `cc` carries `cc`'s tip only as a merge's SECOND parent, so
    two INDEPENDENT branches that each merged the same unlanded `cc` both
    have `cc`'s tip in their history and, under a plain is-ancestor test,
    each read as "integrating" the other -- the pair collapsed to one editor
    and a genuine conflict went unflagged. `cc`'s tip is on neither branch's
    first-parent line, so the first-parent test rejects it, while the real
    mutual fork wt/cpu-derived <-> wt/cpu-legal still passes because its
    integrated parent 939c4f642 is a trunk tip of cpu-legal that cpu-derived
    merged. A plain feature branch that forked from `b` shares `b`'s commits
    without ever merging them, but its `b`-only ancestors are ordinary
    single-parent commits, not merge parents, so it is not mistaken for an
    integration either. Membership in `rev-list --first-parent main..b`
    already implies both "reachable from b" and "not reachable from main",
    so no separate is-ancestor call is needed.

    `a` and `b` are the two ends of a MUTUAL merge when each integrates the
    other (see `live_branch_files`); that pair is one workstream, not two
    racing editors.
    """
    b_own = set(git(repo, "rev-list", "--first-parent", f"main..{b}").split())
    for m in git(repo, "log", "--merges", "--format=%H", f"main..{a}").split():
        # `rev-list --parents -n1 <merge>` lists the commit then its parents.
        parents = git(repo, "rev-list", "--parents", "-n1", m).split()[1:]
        if any(p in b_own for p in parents):
            return True
    return False


def _mutual_merge_head(
    repo: Path,
    group: list[tuple[str, str, str]],
) -> tuple[str, str, str]:
    """The one representative of a mutually-merged editor group.

    Every member integrates every other, so any of them carries the whole
    workstream and landing one first makes the rest merge clean. Pick the
    head deterministically: most commits ahead of main, then branch name, so
    the answer does not depend on iteration order.
    """
    def ahead(ref: str) -> int:
        return int(git(repo, "rev-list", "--count", f"main..{ref}").strip() or "0")

    return max(group, key=lambda e: (ahead(e[1]), e[0]))


def _merge_conflict_paths(repo: Path, left: str, right: str) -> set[str] | None:
    """Return paths that conflict when two live branch tips are merged.

    `None` means Git could not produce a merge result; callers fail open and
    retain the overlap rather than hiding a possible conflict.
    """
    p = subprocess.run(
        ["git", "-C", str(repo), "merge-tree", "--write-tree", "--name-only", left, right],
        capture_output=True, text=True, timeout=120,
    )
    if p.returncode == 0:
        return set()
    if p.returncode != 1:
        return None
    # --name-only emits the conflicted paths between the result tree id and
    # the blank line that starts the informational merge messages.
    lines = p.stdout.splitlines()
    try:
        end = lines.index("")
    except ValueError:
        return None
    return set(lines[1:end])


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
    That maximal-editor pass runs BEFORE the identical-blob dedup: run the
    other way round, a stacked ancestor sharing its descendant's blob is
    chosen as the representative, then the ancestry pass discards it as
    non-maximal and the descendant it contained is already gone -- losing a
    real editor.

    Two branches that have MUTUALLY MERGED are ONE workstream, not two
    editors: a long-lived workstream forks into heads that integrate each
    other by hand (`Merge branch 'wt/cpu-legal' into HEAD` on one, `Merge
    branch 'wt/cpu-derived' into HEAD` on the other), so landing either first
    is clean and there is no resolver round to remove. The maximal-editor
    pass only drops an ANCESTOR, and neither head of such a fork contains the
    other, so both survived and each shared file read as contended (measured
    2026-10-01: `wt/cpu-derived` and `wt/cpu-legal` are two heads of one perf
    workstream and shared thirteen files, minting a brief to split files no
    second uncontrolled branch was editing). A group whose members each
    integrate the other is collapsed to one representative first, computed
    from the merge commits (`_integrates`); a third branch that genuinely
    collided with the pair is in neither group and stays a second editor.

    A surviving editor that is a pure CARRIER of the surviving set's common
    ancestor is not a second editor either: an editor E whose blob for the
    file equals, for every other surviving editor O, the blob at the
    merge-base of E and O, changed nothing in this file since it forked from
    O, so it cannot conflict with O whatever the landing order. The
    identical-blob dedup cannot catch this -- it only drops editors that
    agree with EACH OTHER, never one that agrees with the common ancestor --
    and the blob-vs-main filter cannot see it either, because the shared base
    is typically AHEAD of main. Measured 2026-10-01: `wt/cpu-redeal` and
    `wt/sbrep-fast` both descend from `wt/perf-tip`, which has no worktree of
    its own (its tip is shared with differently named worktrees) and is
    therefore never enumerated as an editor; 8 of the 13 files the pair
    shared were ones one branch never edited past perf-tip, each reading as a
    second editor and minting briefs to split files no second uncontrolled
    branch was editing. The merge-base is computed pairwise (two refs), not
    with the multi-ref form, which can emit several best common ancestors on
    criss-cross histories; an empty merge-base (unrelated histories) means
    E is not a carrier. The carrier is measured against the MAXIMAL
    survivors, not the raw editor list. If the pass reduces the set to
    nothing, the original maximal list is kept: that can only mean every
    editor carries one identical blob, which the dedup below collapses to one
    anyway -- two editors with different blobs cannot both be pairwise
    carriers, and a lone surviving editor is never a carrier.
    """
    closed = closed_issue_ids(repo)
    editors: dict[str, list[tuple[str, str, str]]] = collections.defaultdict(list)
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
            editors[f].append((name, ref, blobs.get(f, "")))
    files: dict[str, list[str]] = {}
    for f, branch_set in editors.items():
        # FIRST collapse a mutually-merging group to ONE representative: its
        # members each integrate the other, so they are one workstream, not
        # independent editors. Union by the `_integrates` relation (checked
        # both ways) so a fork of three heads that all merged each other is
        # one group too. A third branch that genuinely raced the pair is in
        # neither group and survives this pass.
        n = len(branch_set)
        parent = list(range(n))

        def find(i: int) -> int:
            while parent[i] != i:
                parent[i] = parent[parent[i]]
                i = parent[i]
            return i

        for i in range(n):
            for j in range(i + 1, n):
                ri, rj = branch_set[i][1], branch_set[j][1]
                if _integrates(repo, ri, rj) and _integrates(repo, rj, ri):
                    parent[find(i)] = find(j)
        groups: dict[int, list[tuple[str, str, str]]] = collections.defaultdict(list)
        for i, e in enumerate(branch_set):
            groups[find(i)].append(e)
        collapsed = [
            _mutual_merge_head(repo, g) if len(g) > 1 else g[0]
            for g in groups.values()
        ]
        # THEN drop the branches another editor of this file contains: the
        # descendant's merge takes the ancestor by construction, so the two
        # cannot conflict whatever their blobs are. Doing this BEFORE the
        # identical-blob dedup is the whole point: with the dedup first, a
        # stacked ancestor that shares its descendant's blob is kept as the
        # blob's representative and the ancestry pass then correctly discards
        # it -- but the descendant it contained was already gone, so the file
        # loses a real editor (another branch descending from the ancestor made
        # the ancestor non-maximal), and a genuine two-branch collision goes
        # unflagged. Measured 2026-09-30 against the real functions (scratch
        # repos, A <= B with identical blobs, C descending from A): dedup-first
        # reported no editor but C and no hot spot; maximal-editors-first
        # reports B and C and the hot spot.
        maximal = [
            (name, ref, blob) for name, ref, blob in collapsed
            if not any(other != ref and _is_ancestor(repo, ref, other)
                       for _, other, _ in collapsed)
        ]
        # THEN drop a pure CARRIER of the surviving set's common ancestor: an
        # editor whose blob equals the blob at its pairwise merge-base with
        # EVERY other surviving editor changed nothing in this file since it
        # forked from them, so it cannot conflict with any of them whatever
        # the landing order. The identical-blob dedup below only catches
        # editors that agree with EACH OTHER, never one that agrees with the
        # common ancestor (typically an unenumerated integration branch that
        # is ahead of main, so the blob-vs-main filter above cannot see it
        # either). The pass runs on the MAXIMAL survivors, never drops the
        # only editor, and an empty reduction keeps the original list -- that
        # can only mean every editor carries one identical blob, which the
        # dedup below collapses to one anyway. See the docstring for the
        # measured 2026-10-01 live-repo case.
        if len(maximal) > 1:
            non_carriers = []
            for name, ref, blob in maximal:
                carrier = True
                for _, other, _ in maximal:
                    if other == ref:
                        continue
                    mb = git(repo, "merge-base", ref, other).strip()
                    if not mb or blob != _tree_blobs(repo, mb, [f]).get(f, ""):
                        carrier = False
                        break
                if not carrier:
                    non_carriers.append((name, ref, blob))
            if non_carriers:
                maximal = non_carriers
        # THEN dedup distinct branches that contribute identical content: two
        # unrelated branches with the same blob cannot conflict. They survive
        # the ancestry pass, so the representative the dedup keeps is always a
        # maximal editor and the file cannot lose it.
        kept: list[str] = []
        seen: set[str] = set()
        for name, _ref, blob in sorted(maximal):
            if blob in seen:
                continue  # same content another live branch already contributes
            seen.add(blob)
            kept.append(name)
        if kept:
            # A shared path is only a resolver hotspot when at least one pair
            # of independent editors actually conflicts in Git's merge. Clean
            # overlaps are still recorded in the durable notes when useful,
            # but do not consume a merge_fix round and should not inflate the
            # live conflict metric.
            refs = {name: ref for name, ref, _blob in branch_set}
            conflict = len(kept) < 2
            for i, left in enumerate(kept):
                for right in kept[i + 1:]:
                    conflicts = _merge_conflict_paths(repo, refs[left], refs[right])
                    if conflicts is None or f in conflicts:
                        conflict = True
                        break
                if conflict:
                    break
            if conflict:
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


def load_hotfiles_notes(repo: Path) -> list[dict]:
    """The durable half of the hot-file table, from the TRACKED notes file.

    The live half (which files are contended RIGHT NOW, by which branches)
    is `hotspots()`; the durable half ("why it collides" -- the workstream
    knowledge that does not expire with a branch) lives in
    `<repo>/scripts/hotfiles-notes.json`, one entry
    `{"files": [...], "note": "..."}` per collider or collider group. A seat
    filing a new contended pair edits that file in its own commit and
    re-renders; the prose is never hand-copied into a report again.

    A missing or malformed file means no notes, not a failure: the renderer
    then emits the mechanical default for every row. Fail open, like the
    other readers here.
    """
    p = repo / "scripts" / "hotfiles-notes.json"
    try:
        entries = json.loads(p.read_text())
    except (OSError, ValueError):
        return []
    if not isinstance(entries, list):
        return []
    return [
        e
        for e in entries
        if isinstance(e, dict)
        and isinstance(e.get("files"), list)
        and isinstance(e.get("note"), str)
        and e["files"]
        and e["note"]
    ]


def render_hotspots_md(
    hs: list[tuple[str, list[str]]],
    notes: list[dict],
    max_rows: int | None = None,
) -> str:
    """The hot-file table as markdown: one row per `hotspots()` entry, ALL
    rows (no truncation unless `max_rows` is given, which the AGENTS.md embed
    uses to keep its section small), each row's `why` cell taken from the
    first notes entry whose `files` list contains the file, else the
    mechanical default naming the live branches. Notes entries whose files
    are not in the rendered rows are appended after the table so the durable
    knowledge stays visible even when its file is quiet today.
    """

    def cell(text: str) -> str:
        return text.replace("|", "\\|")

    def note_for(f: str) -> str | None:
        hits = [e["note"] for e in notes if f in e["files"]]
        return hits[0] if hits else None

    lines = ["| file | why it collides |", "|---|---|"]
    shown = hs if max_rows is None else hs[:max_rows]
    for f, bs in shown:
        note = note_for(f)
        why = cell(note) if note else f"{len(bs)} live branches: {', '.join(bs)}"
        lines.append(f"| `{f}` | {why} |")
    rest = len(hs) - len(shown)
    if rest:
        lines.append(f"… and {rest} more; run the command for the live list")
    shown_files = {f for f, _ in shown}
    quiet = [e for e in notes if not any(f in shown_files for f in e["files"])]
    if quiet:
        lines.append("")
        lines.append("Durable notes for files not shown above:")
        for e in quiet:
            files = ", ".join(f"`{f}`" for f in e["files"])
            lines.append(f"- {files} — {cell(e['note'])}")
    return "\n".join(lines)


def hotspots_md(repo: Path, max_rows: int | None = None) -> str:
    return render_hotspots_md(hotspots(repo), load_hotfiles_notes(repo), max_rows)


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


def _live_agent_pids(proc: Path | None = None) -> dict[int, str]:
    """Every live pi-agent-shaped process: pid -> its `--cwd` ("" when absent).

    One scan feeds both ownership signals: the `--cwd` path a dev gorged's
    `-dir` is tested against, and the pid set the gorged's ancestor chain is
    walked against. Matching on the `pi-agent` process ROLE (not a name list)
    is what makes a future launcher covered without editing this function."""
    proc = proc or Path(os.environ.get("GORGE_PROC_DIR", "/proc"))
    pids: dict[int, str] = {}
    try:
        entries = list(proc.iterdir())
    except OSError:
        return pids
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
        pids[int(d.name)] = cwd
    return pids


def _agent_ancestor(proc: Path, pid: int, agent_pids: dict[int, str]) -> bool:
    """True when `pid`'s ancestor chain (walking `ppid` up to init) contains a
    live pi-agent process.

    This is the ownership signal that a gorged launched FROM an agent's own
    shell is caught by, even when its `-dir` is outside that agent's worktree
    (a `/tmp` fixture dir) and its immediate parent is not a `.sh` script but
    an inline `bash -c` from the agent's session. The 2026-10-01 stability
    event was exactly this shape: the ui24 fixture server on :8095
    (`-dir /tmp/gorge-ui24-...`, parent `bash -c ...`, real grandparent the
    live `agent-...-fda041b7` pi-agent) matched NEITHER the `-dir` signal nor
    the script-supervisor signal, so it counted as a second standing instance.

    Deliberately structural and role-based: any future agent-launched server,
    whatever its data dir or parent binary, is covered because the agent that
    spawned it is still on its ancestor chain. Bounded by the process tree
    depth and fails closed (an unreadable/gone pid ends the walk).
    """
    seen: set[int] = set()
    cur = pid
    while cur > 1 and cur not in seen:
        seen.add(cur)
        if cur in agent_pids:
            return True
        try:
            after = proc.joinpath(str(cur), "stat").read_text().rsplit(")", 1)[1].split()
            cur = int(after[1])
        except (OSError, ValueError, IndexError):
            return False
    return False


def _agent_seat_ids(agent_pids: dict[int, str]) -> set[str]:
    """The id suffix of every live agent's worktree (`--cwd` basename).

    A seat names its scratch/state dir under `/tmp/gorge-<something-unique>`
    (AGENTS.md) and, in practice, ends that name with its OWN id -- the same
    suffix as its worktree and its issue id. The 2026-10-01 stability event
    is the example: worktree `agent-20260929T124737Z-fda041b7`, fixture dir
    `/tmp/gorge-ui24-fda041b7`. The suffix survives the launcher's exit and
    the server's reparenting to systemd, which is exactly why neither the
    `-dir`-in-worktree signal nor the live-ancestor signal catches it.

    Derived from the live agents themselves, never a hardcoded list, so a
    seat with a new id is covered; only ids of at least 6 characters count,
    which excludes a generic trailing token.
    """
    ids: set[str] = set()
    for cwd in agent_pids.values():
        if not cwd:
            continue
        tail = Path(cwd).name.rsplit("-", 1)[-1]
        if len(tail) >= 6:
            ids.add(tail)
    return ids


def _dir_names_agent(gdir: str, seat_ids: set[str]) -> bool:
    """True when a gorged `-dir` names a live agent by its id suffix: the dir
    basename IS that id or ENDS WITH `-<id>`. Tight by design -- a substring
    anywhere would let an unrelated path match -- and it is the seat's own
    naming that carries the tie, not a list in this file."""
    if not gdir:
        return False
    base = Path(gdir).name
    return any(base == sid or base.endswith("-" + sid) for sid in seat_ids)


def _owner_dirs(agent_pids: dict[int, str]) -> list[Path]:
    """The resolved `--cwd` of every live agent, for the `-dir`-in-worktree
    ownership signal."""
    owners: list[Path] = []
    for cwd in agent_pids.values():
        if not cwd:
            continue
        try:
            owners.append(Path(cwd).resolve())
        except OSError:
            pass
    return owners


def gorged_owned(p: dict, proc_root: Path, agent_pids: dict[int, str],
                 seat_ids: set[str], owners: list[Path]) -> bool:
    """True when a gorged process is owned by a currently-live agent.

    One home for the ownership rule, so `collect_gorged_hygiene` and the
    selftest both read it (a per-pid predicate the test can isolate each
    signal with, rather than only the aggregate excess). Four signals, tried
    in order from the most durable to the least:

      1. the ancestor chain still contains a live pi-agent -- an inline
         `bash -c` launcher is covered, no name needed;
      2. the `-dir` ends with a live seat's own id -- survives the launcher
         exiting and the server reparenting to systemd;
      3. the `-dir` resolves inside a live agent's worktree (`--cwd`);
      4. the parent is a live, non-init `.sh` script (the smoke.sh shape).
    """
    if _agent_ancestor(proc_root, p["pid"], agent_pids):
        return True
    if _dir_names_agent(p["dir"], seat_ids):
        return True
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


def collect_gorged_hygiene(proc: Path | None = None) -> tuple[int, int, str, str]:
    """(standing_excess, unsafe_launches, standing_note, unsafe_note): the
    two gorged-process hygiene failing signals (operator ruling
    2026-09-30), each with its own evidence note.

    standing_note names exactly the UNOWNED gorged processes -- the list
    standing_excess is computed from (the surplus is that list minus the
    one allowed demo) -- and unsafe_note names exactly the init-reparented
    ones. Neither note is capped: a truncated note is how an offending pid
    gets hidden precisely when the metric fires, so both carry every entry
    whenever their metric is non-zero.

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
        return 0, 0, "no gorged process running", "no gorged process running"
    proc_root = proc or Path(os.environ.get("GORGE_PROC_DIR", "/proc"))
    agent_pids = _live_agent_pids(proc)
    seat_ids = _agent_seat_ids(agent_pids)
    owners = _owner_dirs(agent_pids)

    def owned(p: dict) -> bool:
        return gorged_owned(p, proc_root, agent_pids, seat_ids, owners)

    def entry(p: dict) -> str:
        return f"pid={p['pid']} addr={p['addr']} dir={p['dir']}"

    standing = [p for p in procs if not owned(p)]
    unsafe_procs = [p for p in procs if p["ppid"] == 1]
    excess = max(0, len(standing) - 1)
    unsafe = len(unsafe_procs)
    standing_note = "; ".join(entry(p) for p in standing) or "no standing gorged process"
    unsafe_note = "; ".join(entry(p) for p in unsafe_procs) or "no init-reparented gorged process"
    return excess, unsafe, standing_note, unsafe_note


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
    excess, _, standing_note, _ = collect_gorged_hygiene()
    rows.append(row(repo, "stability", "standing_gorged_excess", excess, note=standing_note))
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


# The code-shape ratchets cmd/codeshape reports besides funcs_over_300. They
# ride in the funcs_over_300 row's note (each is also pinned shrink-only by
# internal/codeshape's TestCodeShapeOnlyShrinks), so the ledger keeps their
# history without each becoming a separately scored steward metric.
CODESHAPE_RATCHETS = (
    "engine_methods",
    "host_methods",
    "ctx_fields",
    "ctx_embeds",
    "resume_point_fields",
    "string_param_reads",
    "string_case_literals",
)


def codeshape_metrics(repo: Path) -> dict | None:
    """`go run ./cmd/codeshape` in repo, parsed; None if it cannot be measured.

    cmd/codeshape depends only on the standard library and internal/codeshape,
    so a peer's half-edited engine package cannot break the measurement. A
    checkout from before cmd/codeshape existed (or a box with no Go toolchain)
    yields None, and the caller records no row rather than a false zero.
    """
    if not (repo / "cmd" / "codeshape").is_dir():
        return None
    try:
        p = subprocess.run(
            ["go", "run", "./cmd/codeshape", "-root", str(repo)],
            cwd=repo, capture_output=True, text=True, timeout=300,
        )
    except (OSError, subprocess.TimeoutExpired) as e:
        print(f"reward_collect: codeshape: {e}", file=sys.stderr)
        return None
    if p.returncode != 0:
        print(f"reward_collect: codeshape failed: {p.stderr.strip()[:400]}", file=sys.stderr)
        return None
    try:
        return json.loads(p.stdout)
    except ValueError:
        print("reward_collect: codeshape printed no JSON", file=sys.stderr)
        return None


def codeshape_note(shape: dict, top: int = 6) -> str:
    """The funcs_over_300 row's note: the longest functions, then the ratchets.

    Format (seed_candidates.py parses the first entry):
        <file>:<line> <name>(<lines>); ... | engine_methods=N host_methods=N ...
    """
    longs = "; ".join(
        f"{f['file']}:{f['line']} {f['name']}({f['lines']})" for f in shape.get("long_funcs", [])[:top]
    )
    ratchets = " ".join(f"{k}={shape[k]}" for k in CODESHAPE_RATCHETS if k in shape)
    return f"{longs} | {ratchets}"


def collect_steward(repo: Path, context_file: Path | None = None, shape: dict | None = None) -> list[str]:
    """The recurring tax: gate wall time, agent context size, code shape.

    Each is paid again on every future iteration, which is why they carry 100x
    and why they are measured as levels rather than as deltas of deltas.
    `shape` overrides the cmd/codeshape measurement (the self-test's hook).
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

    # Code shape, not file size (rules-engine refactor spec, W0). The metric
    # this replaced, `oversized_files` (files over 1500 lines, tests
    # included), rewarded SIZE splits -- the second half of a switch moved
    # verbatim into a *_rest.go, a grab-bag *_helpers.go -- which scatter one
    # concern across files without adding a boundary. A function over 300
    # lines is a concern with no seam, and only extracting that concern moves
    # this number. The rest of cmd/codeshape's ratchets ride in the note.
    # The steward history has no continuity across this change: old
    # `oversized_files` rows stay in the ledger but are no longer scored
    # (reward.py METRICS), and `funcs_over_300` starts its own series.
    if shape is None:
        shape = codeshape_metrics(repo)
    if shape is not None and "funcs_over_300" in shape:
        rows.append(row(repo, "steward", "funcs_over_300", shape["funcs_over_300"], note=codeshape_note(shape)))
    _, unsafe, _, unsafe_note = collect_gorged_hygiene()
    rows.append(row(repo, "steward", "unsafe_gorged_launches", unsafe, note=unsafe_note))
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

        # steward: gate wall time from log mtimes, context bytes, code shape.
        gd = repo / ".ds4" / "orchestrator" / "gates" / "issue-1" / "t0"
        gd.mkdir(parents=True)
        import os

        (gd / "a.log").write_text("x")
        (gd / "b.log").write_text("x")
        os.utime(gd / "a.log", (1000, 1000))
        os.utime(gd / "b.log", (1090, 1090))
        check("gate wall time is the log-mtime spread", gate_wall_seconds(repo) == 90.0, gate_wall_seconds(repo))
        (repo / "AGENTS.md").write_text("a" * 1000)
        shape = {
            "funcs_over_300": 2,
            "engine_methods": 2162,
            "host_methods": 96,
            "ctx_fields": 293,
            "ctx_embeds": 2,
            "resume_point_fields": 90,
            "string_param_reads": 2142,
            "string_case_literals": 2886,
            "long_funcs": [
                {"name": "effEffect", "file": "effects/misc.go", "line": 219, "lines": 1200},
                {"name": "(*Engine).cloneWith", "file": "rules/clone.go", "line": 103, "lines": 970},
            ],
        }
        stw = [json.loads(r) for r in collect_steward(repo, shape=shape)]
        check("agent context bytes counts AGENTS.md",
              next(r for r in stw if r["metric"] == "agent_context_bytes")["value"] == 1000, stw)
        check("the steward axis no longer records oversized_files",
              not any(r["metric"] == "oversized_files" for r in stw), stw)
        lf = next((r for r in stw if r["metric"] == "funcs_over_300"), None)
        check("funcs_over_300 carries codeshape's count", lf is not None and lf["value"] == 2, lf)
        check("funcs_over_300's note leads with the longest function",
              lf is not None and lf["note"].startswith("effects/misc.go:219 effEffect(1200); "), lf)
        check("funcs_over_300's note carries every ratchet",
              lf is not None and all(f"{k}={shape[k]}" in lf["note"] for k in CODESHAPE_RATCHETS), lf)
        # A checkout without cmd/codeshape (older main, a bare fixture) records
        # no funcs_over_300 row rather than a false zero.
        check("no cmd/codeshape means no funcs_over_300 row",
              not any(json.loads(r)["metric"] == "funcs_over_300" for r in collect_steward(repo)))
        # A stub `go` on PATH stands in for the toolchain: codeshape_metrics
        # runs `go run ./cmd/codeshape -root <repo>` in the repo and parses its
        # JSON, and a failing run yields None.
        (repo / "cmd" / "codeshape").mkdir(parents=True)
        stub = Path(td) / "stubbin"
        stub.mkdir()
        (stub / "go").write_text(
            "#!/bin/sh\n"
            "[ \"$1 $2 $3\" = \"run ./cmd/codeshape -root\" ] || exit 3\n"
            f"[ \"$PWD\" = \"{repo}\" ] || exit 4\n"
            "echo '{\"funcs_over_300\": 7, \"long_funcs\": []}'\n"
        )
        (stub / "go").chmod(0o755)
        old_path = os.environ.get("PATH", "")
        os.environ["PATH"] = f"{stub}:{old_path}"
        try:
            got = codeshape_metrics(repo)
            check("codeshape_metrics parses cmd/codeshape's JSON",
                  got is not None and got.get("funcs_over_300") == 7, got)
            (stub / "go").write_text("#!/bin/sh\nexit 1\n")
            check("a failing codeshape run yields None", codeshape_metrics(repo) is None)
        finally:
            os.environ["PATH"] = old_path

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
        (hr / "clean.txt").write_text("one\ntwo\nthree\nfour\n")
        hrun("add", "base.txt", "clean.txt")
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

        # Two branches append distinct, non-overlapping lines to the same
        # tracked file. They share an edited path, but Git merges the edits
        # cleanly, so this is not a resolver hotspot.
        clean_refs = []
        for name, line in (("clean-a", "left"), ("clean-b", "right")):
            wt_clean = Path(td) / f"hot-{name}"
            hrun("worktree", "add", "-q", "-b", f"wt/{name}", str(wt_clean), base)
            content = "ONE\ntwo\nthree\nfour\n" if line == "left" else "one\ntwo\nthree\nFOUR\n"
            (wt_clean / "clean.txt").write_text(content)
            subprocess.run(["git", "-C", str(wt_clean), "add", "clean.txt"], capture_output=True)
            hcommit(wt_clean, f"append {line}")
            clean_refs.append(f"wt/{name}")
        check("precondition: clean branch edits really overlap on clean.txt",
              "clean.txt" in git(hr, "diff", "--name-only", "main...wt/clean-a").split()
              and "clean.txt" in git(hr, "diff", "--name-only", "main...wt/clean-b").split())
        check("precondition: clean edits auto-merge",
              _merge_conflict_paths(hr, *clean_refs) == set())
        check("a cleanly merging shared path is not a hotspot",
              "clean.txt" not in live_branch_files(hr), live_branch_files(hr))

        # The hot-file table as a GENERATED artifact (agent-20261001T011159Z-
        # 80a8b059): `--format md` renders one markdown row per hotspots()
        # entry -- ALL rows, never the 8-row truncation the journal note
        # suffers -- with the durable "why it collides" note from the tracked
        # scripts/hotfiles-notes.json merged into each row. The prose copies
        # this replaces were hand-maintained in two places (AGENTS.md and the
        # gitignored gorge-context.md) and already drifted from the tool.
        # Precondition: shared.txt really is a TWO-branch hot spot here,
        # naming exactly the two live editors.
        hs_hr = hotspots(hr)
        check("fixture: shared.txt is a two-branch hot spot with both editors",
              hs_hr == [("shared.txt", sorted(["other", "other2"]))], hs_hr)
        md_plain = render_hotspots_md(hs_hr, [])
        check("the two-branch row names BOTH branches, not one",
              "| `shared.txt` | 2 live branches: other, other2 |" in md_plain.splitlines(),
              md_plain)
        check("no notes file means every row is the mechanical default",
              "live branches" in md_plain and "why it collides" in md_plain, md_plain)
        notes_hr = [
            {"files": ["shared.txt"], "note": "the shared editor seam"},
            {"files": ["quiet.go"], "note": "a quiet seam today"},
        ]
        md_noted = render_hotspots_md(hs_hr, notes_hr)
        check("a row covered by a notes entry shows the note, not the default",
              "| `shared.txt` | the shared editor seam |" in md_noted.splitlines(),
              md_noted)
        check("the branch names yield to the note on a noted row",
              "live branches" not in md_noted, md_noted)
        md_mixed = render_hotspots_md(
            [("a.go", ["x"]), ("b.go", ["y", "z"])],
            [{"files": ["a.go"], "note": "why a"}])
        check("a noted row shows the note", "| `a.go` | why a |" in md_mixed.splitlines(), md_mixed)
        check("an unnoted row shows the default",
              "| `b.go` | 2 live branches: y, z |" in md_mixed.splitlines(), md_mixed)
        check("a quiet notes entry is appended so the durable knowledge survives",
              "- `quiet.go` — a quiet seam today" in md_noted.splitlines(), md_noted)
        wide = [(f"f{i}.go", ["b1", "b2"]) for i in range(12)]
        md_wide = render_hotspots_md(wide, [])
        check("ALL rows render -- no 8-row truncation",
              sum(1 for l in md_wide.splitlines() if l.startswith("| `f")) == 12,
              md_wide)
        check("a --max-rows cut keeps the remainder count",
              render_hotspots_md(wide, [], max_rows=3).splitlines()[-1]
              == "… and 9 more; run the command for the live list",
              render_hotspots_md(wide, [], max_rows=3))
        (hr / "scripts").mkdir()
        (hr / "scripts" / "hotfiles-notes.json").write_text(json.dumps(notes_hr))
        md_repo = hotspots_md(hr)
        check("the full repo path loads the notes file and merges it",
              "| `shared.txt` | the shared editor seam |" in md_repo.splitlines(), md_repo)
        (hr / "scripts" / "hotfiles-notes.json").write_text("{not json")
        check("a malformed notes file fails open to the defaults",
              "| `shared.txt` | 2 live branches: other, other2 |" in hotspots_md(hr).splitlines(),
              hotspots_md(hr))

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

        # MUTUALLY-MERGING heads are ONE workstream, not two editors. A
        # long-lived perf workstream forks into wt/mutual-a and wt/mutual-b on
        # mm.txt, then each MERGES THE OTHER by hand (the real shape measured
        # 2026-10-01: wt/cpu-derived <-> wt/cpu-legal, thirteen shared files).
        # Neither head contains the other, so the maximal-editor pass alone
        # kept both and every file read as contended -- a brief to split files
        # no second uncontrolled branch was editing. Their own merge commits
        # are the signal that they are one workstream.
        # mm.txt lives on main so the two appends merge CLEAN (a prepend vs an
        # append): a conflicting merge would abort, leave neither head
        # integrating the other, and test nothing.
        (hr / "mm.txt").write_text("m\n")
        hrun("add", "mm.txt")
        hrun("commit", "-qm", "mm base")
        mmbase = hrun("rev-parse", "HEAD").stdout.strip()
        wma = Path(td) / "hot-mutual-a"
        hrun("worktree", "add", "-q", "-b", "wt/mutual-a", str(wma), mmbase)
        (wma / "mm.txt").write_text("a\nm\n")
        subprocess.run(["git", "-C", str(wma), "add", "mm.txt"], capture_output=True)
        hcommit(wma, "mutual a")
        wmb = Path(td) / "hot-mutual-b"
        hrun("worktree", "add", "-q", "-b", "wt/mutual-b", str(wmb), mmbase)
        (wmb / "mm.txt").write_text("m\nb\n")
        subprocess.run(["git", "-C", str(wmb), "add", "mm.txt"], capture_output=True)
        hcommit(wmb, "mutual b")
        # a merges b, b merges a -- the mutual pair.
        subprocess.run(["git", "-C", str(wma), "merge", "-q", "--no-ff", "-m",
                        "merge b into a", "wt/mutual-b"], capture_output=True)
        subprocess.run(["git", "-C", str(wmb), "merge", "-q", "--no-ff", "-m",
                        "merge a into b", "wt/mutual-a"], capture_output=True)
        # Each head then makes one more commit, so neither is an ANCESTOR of
        # the other while each still integrates the other's pre-merge tip -- the
        # real diverged-fork shape (cpu-derived/cpu-legal), not two aliases of
        # one commit graph. Each also lands a DIFFERENT further edit of mm.txt,
        # so the pair contributes two DISTINCT blobs and the identical-blob
        # dedup cannot remove them: only the mutual-merge collapse can, which is
        # what makes this test fail without it.
        (wma / "mm.txt").write_text("a\nm\nb\na2\n")
        (wma / "a-only.txt").write_text("a\n")
        subprocess.run(["git", "-C", str(wma), "add", "mm.txt", "a-only.txt"], capture_output=True)
        hcommit(wma, "mutual a head")
        (wmb / "mm.txt").write_text("a\nm\nb\nb2\n")
        (wmb / "b-only.txt").write_text("b\n")
        subprocess.run(["git", "-C", str(wmb), "add", "mm.txt", "b-only.txt"], capture_output=True)
        hcommit(wmb, "mutual b head")
        check("precondition: neither mutual head contains the other",
              hrun("merge-base", "--is-ancestor", "wt/mutual-a", "wt/mutual-b").returncode != 0
              and hrun("merge-base", "--is-ancestor", "wt/mutual-b", "wt/mutual-a").returncode != 0)
        check("precondition: each mutual head really integrates the other",
              _integrates(hr, "wt/mutual-a", "wt/mutual-b")
              and _integrates(hr, "wt/mutual-b", "wt/mutual-a"))
        a_mm = hrun("rev-parse", "wt/mutual-a:mm.txt").stdout.strip()
        b_mm = hrun("rev-parse", "wt/mutual-b:mm.txt").stdout.strip()
        check("precondition: the mutual heads contribute DISTINCT blobs "
              "(the dedup must not be what removes them)",
              a_mm and b_mm and a_mm != b_mm, (a_mm, b_mm))
        mm = live_branch_files(hr)
        check("a mutually-merging pair counts as ONE editor",
              len(mm.get("mm.txt", [])) == 1, mm.get("mm.txt"))
        check("the mutual pair is not a hot spot",
              not any(f == "mm.txt" for f, _ in hotspots(hr)), hotspots(hr))
        # A third branch that genuinely collides with the pair is still a
        # second editor: the collapse must not swallow an uncontrolled branch.
        wmx = Path(td) / "hot-mutual-x"
        hrun("worktree", "add", "-q", "-b", "wt/mutual-x", str(wmx), mmbase)
        (wmx / "mm.txt").write_text("x\nm\n")
        subprocess.run(["git", "-C", str(wmx), "add", "mm.txt"], capture_output=True)
        hcommit(wmx, "mutual x")
        check("precondition: the third branch merges neither mutual head",
              hrun("merge-base", "--is-ancestor", "wt/mutual-a", "wt/mutual-x").returncode != 0
              and hrun("merge-base", "--is-ancestor", "wt/mutual-b", "wt/mutual-x").returncode != 0)
        mmx = live_branch_files(hr)
        check("a third colliding branch makes the file hot again",
              len(mmx.get("mm.txt", [])) == 2, mmx.get("mm.txt"))

        # The mutual-merge signal must not be SPOOFABLE through a SHARED
        # merged parent. Two INDEPENDENT branches that each merge the SAME
        # unlanded third branch cc both carry cc's tip in their history (and
        # cc is unlanded, so it is not in main's), but cc arrived on each only
        # as a merge's SECOND parent -- under a plain is-ancestor test each
        # branch read as "integrating" the other, the pair collapsed to ONE
        # editor, and a genuine two-editor conflict on the shared file went
        # unflagged (the r2-review MAJOR, reproduced on a scratch repo).
        # The first-parent rule -- an integrated merge parent must be a trunk
        # tip of the OTHER branch -- rejects cc's tip on both sides, so the
        # pair stays two editors and the conflict stays hot.
        ws_base = hrun("rev-parse", "HEAD").stdout.strip()
        wsa = Path(td) / "hot-spoof-a"
        hrun("worktree", "add", "-q", "-b", "wt/spoof-a", str(wsa), ws_base)
        (wsa / "spoof.txt").write_text("a\n")
        subprocess.run(["git", "-C", str(wsa), "add", "spoof.txt"], capture_output=True)
        hcommit(wsa, "spoof a")
        wsb = Path(td) / "hot-spoof-b"
        hrun("worktree", "add", "-q", "-b", "wt/spoof-b", str(wsb), ws_base)
        (wsb / "spoof.txt").write_text("b\n")
        subprocess.run(["git", "-C", str(wsb), "add", "spoof.txt"], capture_output=True)
        hcommit(wsb, "spoof b")
        wsc = Path(td) / "hot-spoof-cc"
        hrun("worktree", "add", "-q", "-b", "wt/spoof-cc", str(wsc), ws_base)
        (wsc / "cc.txt").write_text("cc\n")
        subprocess.run(["git", "-C", str(wsc), "add", "cc.txt"], capture_output=True)
        hcommit(wsc, "spoof cc")
        subprocess.run(["git", "-C", str(wsa), "merge", "-q", "--no-ff", "-m",
                        "a merges cc", "wt/spoof-cc"], capture_output=True)
        subprocess.run(["git", "-C", str(wsb), "merge", "-q", "--no-ff", "-m",
                        "b merges cc", "wt/spoof-cc"], capture_output=True)
        cc_tip = hrun("rev-parse", "wt/spoof-cc").stdout.strip()
        check("precondition: the two spoof branches are independent",
              hrun("merge-base", "--is-ancestor", "wt/spoof-a", "wt/spoof-b").returncode != 0
              and hrun("merge-base", "--is-ancestor", "wt/spoof-b", "wt/spoof-a").returncode != 0)
        check("precondition: cc's unlanded tip is reachable from both branches",
              hrun("merge-base", "--is-ancestor", cc_tip, "wt/spoof-a").returncode == 0
              and hrun("merge-base", "--is-ancestor", cc_tip, "wt/spoof-b").returncode == 0
              and hrun("merge-base", "--is-ancestor", cc_tip, "main").returncode != 0)
        check("precondition: cc's tip is on NEITHER first-parent line "
              "(it arrived as a merge's second parent on each)",
              cc_tip not in git(hr, "rev-list", "--first-parent",
                                "main..wt/spoof-a").split()
              and cc_tip not in git(hr, "rev-list", "--first-parent",
                                    "main..wt/spoof-b").split())
        check("a shared merged parent does not fake mutual integration",
              not _integrates(hr, "wt/spoof-a", "wt/spoof-b")
              and not _integrates(hr, "wt/spoof-b", "wt/spoof-a"))
        a_shared = hrun("rev-parse", "wt/spoof-a:spoof.txt").stdout.strip()
        b_shared = hrun("rev-parse", "wt/spoof-b:spoof.txt").stdout.strip()
        check("precondition: the spoof pair contributes DISTINCT blobs "
              "(the dedup must not be what keeps them apart)",
              a_shared and b_shared and a_shared != b_shared, (a_shared, b_shared))
        spoof = live_branch_files(hr)
        check("a shared merged parent does not collapse a genuine pair",
              sorted(spoof.get("spoof.txt", [])) == ["spoof-a", "spoof-b"],
              spoof.get("spoof.txt"))
        check("the spoof-hidden conflict is still a hot spot",
              any(f == "shared.txt" for f, _ in hotspots(hr)), hotspots(hr))

        # An ancestor and its descendant that contribute the IDENTICAL blob,
        # plus a branch that descends from the ancestor with a DIFFERENT blob:
        # wt/blob-a <= wt/blob-b share one blob for blob.txt; wt/blob-c descends
        # from wt/blob-a carrying a different blob. blob-a is NON-maximal (it
        # contains both blob-b and blob-c) and the maximal-editor pass drops it
        # so blob-b and blob-c survive as its blob's representatives. But
        # blob-b never edited blob.txt (it committed only marker.txt), so it is
        # a pure CARRIER of blob-a's blob -- the pairwise merge-base with
        # blob-c is blob-a, where blob.txt still holds blob-b's exact blob --
        # and the carrier pass drops it as well: blob-b cannot conflict with
        # blob-c in either landing order, so the correct reading is one editor
        # (blob-c). The carrier rule subsumes the maximal-before-dedup ordering
        # concern FOR THIS CARRIER SHAPE (blob-b is dropped after the maximal
        # pass, never before it), and the ordering pass stays in place; the
        # blob-d/blob-e variant further below keeps a live ordering guard the
        # carrier pass cannot satisfy. The fixture was originally added by
        # d506db046 as the proof of the maximal-before-dedup ordering fix,
        # demonstrated with a blob-b that is in fact a carrier.
        wba = Path(td) / "hot-blob-a"
        hrun("worktree", "add", "-q", "-b", "wt/blob-a", str(wba), base)
        (wba / "blob.txt").write_text("same\n")
        subprocess.run(["git", "-C", str(wba), "add", "blob.txt"], capture_output=True)
        hcommit(wba, "blob a")
        wbb = Path(td) / "hot-blob-b"
        hrun("worktree", "add", "-q", "-b", "wt/blob-b", str(wbb), "wt/blob-a")
        # A real descendant commit that leaves blob.txt's blob UNCHANGED: it
        # edits a different file, so blob-b is genuinely ahead of blob-a while
        # contributing the identical blob for blob.txt (the dedup/ancestry
        # collision this fixture exists for). Writing blob.txt again would
        # stage nothing and the commit would fail, leaving the two branches on
        # one commit -- which is NOT the shape under test.
        (wbb / "marker.txt").write_text("b\n")
        subprocess.run(["git", "-C", str(wbb), "add", "marker.txt"], capture_output=True)
        hcommit(wbb, "blob b, identical blob, other file committed")
        wbc = Path(td) / "hot-blob-c"
        hrun("worktree", "add", "-q", "-b", "wt/blob-c", str(wbc), "wt/blob-a")
        (wbc / "blob.txt").write_text("other\n")
        subprocess.run(["git", "-C", str(wbc), "add", "blob.txt"], capture_output=True)
        hcommit(wbc, "blob c, descends from blob-a")
        check("precondition: blob-a is an ancestor of blob-b",
              hrun("merge-base", "--is-ancestor", "wt/blob-a", "wt/blob-b").returncode == 0)
        check("precondition: blob-a is an ancestor of blob-c",
              hrun("merge-base", "--is-ancestor", "wt/blob-a", "wt/blob-c").returncode == 0)
        check("precondition: blob-c is NOT an ancestor of blob-b",
              hrun("merge-base", "--is-ancestor", "wt/blob-c", "wt/blob-b").returncode != 0)
        a_blob = hrun("rev-parse", "wt/blob-a:blob.txt").stdout.strip()
        b_blob = hrun("rev-parse", "wt/blob-b:blob.txt").stdout.strip()
        c_blob = hrun("rev-parse", "wt/blob-c:blob.txt").stdout.strip()
        check("precondition: blob-a and blob-b really share one blob",
              a_blob == b_blob and a_blob, (a_blob, b_blob))
        check("precondition: blob-c really differs",
              c_blob != a_blob and c_blob, (c_blob, a_blob))
        check("precondition: every blob branch really changes blob.txt",
              all("blob.txt" in git(hr, "diff", "--name-only", "main...wt/blob-%s" % s).split()
                  for s in "abc"))
        deduped = live_branch_files(hr)
        check("a pure carrier of the shared base is not a second editor",
              sorted(deduped.get("blob.txt", [])) == ["blob-c"],
              deduped.get("blob.txt"))
        check("a lone surviving editor is not a hot spot",
              not any(f == "blob.txt" for f, _ in hotspots(hr)), hotspots(hr))

        # Variant: a genuine second editor the carrier pass must NOT swallow.
        # wt/blob-b2 also descends from wt/blob-a but CHANGES blob.txt to a
        # different blob, as blob-c already did -- neither is a carrier of the
        # common ancestor's blob, so both count and the file is a hot spot.
        wbb2 = Path(td) / "hot-blob-b2"
        hrun("worktree", "add", "-q", "-b", "wt/blob-b2", str(wbb2), "wt/blob-a")
        (wbb2 / "blob.txt").write_text("same2\n")
        subprocess.run(["git", "-C", str(wbb2), "add", "blob.txt"], capture_output=True)
        hcommit(wbb2, "blob b2, a genuine second editor")
        b2_blob = hrun("rev-parse", "wt/blob-b2:blob.txt").stdout.strip()
        check("precondition: blob-b2 descends from blob-a with a different blob",
              hrun("merge-base", "--is-ancestor", "wt/blob-a", "wt/blob-b2").returncode == 0
              and b2_blob not in (a_blob, c_blob) and b2_blob,
              (a_blob, b2_blob, c_blob))
        deduped2 = live_branch_files(hr)
        check("the carrier pass does not swallow a genuine editor",
              sorted(deduped2.get("blob.txt", [])) == ["blob-b2", "blob-c"],
              deduped2.get("blob.txt"))
        check("two genuine editors are still a hot spot",
              any(f == "blob.txt" for f, _ in hotspots(hr)), hotspots(hr))

        # Variant: the maximal-before-dedup ordering still needs its own guard,
        # one the carrier pass CANNOT provide. wt/blob-d forks from base (a
        # line unrelated to blob-a's) and adds the SAME blob.txt blob as
        # blob-a; wt/blob-e descends from blob-a with yet another blob. blob-a
        # is non-maximal (ancestor of blob-e), so with the dedup FIRST the
        # representative of the shared blob is blob-a, blob-d is dropped as its
        # identical-blob twin, and the maximal pass then drops blob-a -- the
        # file loses a real editor. Maximal-first keeps blob-d, and the
        # carrier pass cannot drop it either: the pairwise merge-base of
        # blob-d with the others is `base`, where blob.txt does not exist.
        wbd = Path(td) / "hot-blob-d"
        hrun("worktree", "add", "-q", "-b", "wt/blob-d", str(wbd), base)
        (wbd / "blob.txt").write_text("same\n")
        subprocess.run(["git", "-C", str(wbd), "add", "blob.txt"], capture_output=True)
        hcommit(wbd, "blob d, unrelated line, same blob as blob-a")
        wbe = Path(td) / "hot-blob-e"
        hrun("worktree", "add", "-q", "-b", "wt/blob-e", str(wbe), "wt/blob-a")
        (wbe / "blob.txt").write_text("other2\n")
        subprocess.run(["git", "-C", str(wbe), "add", "blob.txt"], capture_output=True)
        hcommit(wbe, "blob e, descends from blob-a")
        d_blob = hrun("rev-parse", "wt/blob-d:blob.txt").stdout.strip()
        e_blob = hrun("rev-parse", "wt/blob-e:blob.txt").stdout.strip()
        check("precondition: blob-d shares blob-a's blob and is not its descendant",
              d_blob == a_blob
              and hrun("merge-base", "--is-ancestor", "wt/blob-a", "wt/blob-d").returncode != 0)
        check("precondition: blob-e descends from blob-a with a fresh blob",
              hrun("merge-base", "--is-ancestor", "wt/blob-a", "wt/blob-e").returncode == 0
              and e_blob not in (a_blob, b2_blob, c_blob))
        deduped3 = live_branch_files(hr)
        check("maximal runs before the dedup: the ancestor's blob twin survives",
              sorted(deduped3.get("blob.txt", []))
              == ["blob-b", "blob-b2", "blob-c", "blob-e"],
              deduped3.get("blob.txt"))
        check("the ordering-shape file is still a hot spot",
              any(f == "blob.txt" for f, _ in hotspots(hr)), hotspots(hr))

        # The motivating geometry, measured 2026-10-01 on the live repo: two
        # live branches descend from a shared integration branch that has NO
        # worktree of its own, so it is never enumerated as an editor and its
        # content is never treated as the shared base. A branch that changed
        # nothing in the file since that shared base is a pure carrier and
        # cannot conflict with the file's real editor in either landing order.
        gt = Path(td) / "carrierrepo"
        gt.mkdir()
        grun = lambda *a: subprocess.run(  # noqa: E731
            ["git", "-C", str(gt), *a], capture_output=True, text=True
        )
        gcommit = lambda wt, msg: subprocess.run(  # noqa: E731
            ["git", "-C", str(wt), "-c", "user.email=t@t", "-c", "user.name=t",
             "commit", "-qm", msg], capture_output=True
        )
        subprocess.run(["git", "init", "-q", "-b", "main", str(gt)], capture_output=True)
        grun("config", "user.email", "t@t")
        grun("config", "user.name", "t")
        (gt / "base.txt").write_text("base\n")
        grun("add", "base.txt")
        grun("commit", "-qm", "init")
        gbase = grun("rev-parse", "HEAD").stdout.strip()
        # wt/perf-tip edits carrier.txt, then its worktree is REMOVED so the
        # ref remains but no worktree lists it -- exactly the live geometry
        # where the shared base is invisible to the worktree enumeration.
        wtpt = Path(td) / "carrier-perftip"
        grun("worktree", "add", "-q", "-b", "wt/perf-tip", str(wtpt), gbase)
        (wtpt / "carrier.txt").write_text("perf\n")
        subprocess.run(["git", "-C", str(wtpt), "add", "carrier.txt"], capture_output=True)
        gcommit(wtpt, "perf tip")
        grun("worktree", "remove", "--force", str(wtpt))
        check("precondition: wt/perf-tip has no worktree of its own",
              "wt/perf-tip" not in grun("worktree", "list", "--porcelain").stdout,
              grun("worktree", "list", "--porcelain").stdout)
        # wt/carrier forks from wt/perf-tip and commits only an UNRELATED file;
        # wt/editor forks from wt/perf-tip and changes carrier.txt.
        wtcarr = Path(td) / "carrier-carrier"
        grun("worktree", "add", "-q", "-b", "wt/carrier", str(wtcarr), "wt/perf-tip")
        (wtcarr / "other.txt").write_text("c\n")
        subprocess.run(["git", "-C", str(wtcarr), "add", "other.txt"], capture_output=True)
        gcommit(wtcarr, "unrelated commit, carrier.txt untouched")
        wted = Path(td) / "carrier-editor"
        grun("worktree", "add", "-q", "-b", "wt/editor", str(wted), "wt/perf-tip")
        (wted / "carrier.txt").write_text("edit\n")
        subprocess.run(["git", "-C", str(wted), "add", "carrier.txt"], capture_output=True)
        gcommit(wted, "edit carrier")
        pt_blob = grun("rev-parse", "wt/perf-tip:carrier.txt").stdout.strip()
        carr_blob = grun("rev-parse", "wt/carrier:carrier.txt").stdout.strip()
        ed_blob = grun("rev-parse", "wt/editor:carrier.txt").stdout.strip()
        check("precondition: wt/carrier pure-carries wt/perf-tip's blob",
              carr_blob == pt_blob and len(pt_blob) == 40, (pt_blob, carr_blob))
        check("precondition: wt/editor really edited carrier.txt",
              ed_blob != pt_blob and len(ed_blob) == 40, (pt_blob, ed_blob))
        carried = live_branch_files(gt)
        check("a pure carrier of an unenumerated shared base is not a second editor",
              carried.get("carrier.txt") == ["editor"], carried.get("carrier.txt"))
        check("the carried file is not a hot spot",
              not any(f == "carrier.txt" for f, _ in hotspots(gt)), hotspots(gt))

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
        # A SIXTH instance: the 2026-10-01 stability event's exact shape. The
        # seat launches the ui24 fixture gorged with `nohup ... &`, its shell
        # exits, and the server reparents to systemd -- so: `-dir` is a /tmp
        # fixture dir OUTSIDE the agent's `--cwd` worktree, the parent is not a
        # pi-agent (the chain is broken), and the parent is not a `.sh` file.
        # The ONLY surviving tie is that the seat named the dir with its own
        # id: worktree `agent-20260929T124737Z-fda041b7`, dir
        # `/tmp/gorge-ui24-fda041b7`.
        fake_pid(9005, ["bash", "/home/sadams/projects/ds4-harness/bin/pi-agent",
                        "--cwd", "/home/agent/agent-20260929T124737Z-fda041b7",
                        "--name", "impl-agent-20260929T124737Z-fda041b7-r1"], ppid=500)
        # ppid 1928974 is `systemd --user`, deliberately absent from the fake
        # tree: the walk ends there and finds no agent.
        fake_pid(105, ["bin/gorged", "-addr", "127.0.0.1:8095", "-dir", "/tmp/gorge-ui24-fda041b7",
                        "-tables", "1"], ppid=1928974)
        # A SEVENTH instance: a dev gorged whose launcher is STILL ALIVE and is
        # itself a pi-agent (an inline `bash -c` would do as well), but whose
        # `-dir` names no seat id and sits outside any worktree -- the case the
        # ancestor-chain signal exists for, isolated from the dir-names signal.
        fake_pid(9007, ["bash", "/home/sadams/projects/ds4-harness/bin/pi-agent",
                        "--cwd", "/home/agent/other-wt-abc12345", "--name", "agent-y"], ppid=500)
        fake_pid(106, ["bin/gorged", "-addr", "127.0.0.1:8096", "-dir", "/tmp/gorge-dev-unnamed",
                        "-tables", "1"], ppid=9007)
        procs = gorged_processes(fproc)
        check("gorged_processes finds every table server by flags, not comm",
              {p["pid"] for p in procs} == {101, 102, 103, 104, 105, 106}, procs)
        # Precondition: the reparented orphan is NOT reachable by the two
        # pre-existing signals -- its chain has no live agent and its parent is
        # not a `.sh` -- so the assertion below can only pass through the new
        # dir-names-the-seat signal (not for the wrong reason).
        check("precondition: the reparented orphan has no live agent ancestor",
              _agent_ancestor(fproc, 105, _live_agent_pids(fproc)) is False)
        check("precondition: the reparented orphan's parent is not a live .sh script",
              _proc_is_script_supervisor(fproc, _proc_ppid(fproc / "105")) is False)
        # Precondition: the live agent's id really is the suffix of the dir, or
        # the dir-names-agent assertion would pass vacuously.
        check("precondition: the fixture dir names the live seat's id",
              "fda041b7" in _agent_seat_ids(_live_agent_pids(fproc)),
              _agent_seat_ids(_live_agent_pids(fproc)))
        # Precondition: the supervised instance really has a live, non-init
        # `.sh` parent in the fake tree -- otherwise the assertion below
        # would pass for the wrong reason (a missing parent pid).
        check("the smoke-supervised gorged's parent is a live non-init .sh script",
              _proc_is_script_supervisor(fproc, _proc_ppid(fproc / "104")) is True)
        excess, unsafe, _, _ = collect_gorged_hygiene(fproc)
        check("two standing instances against the default of one is an excess of 1",
              excess == 1, excess)
        check("the agent-owned dev instance does not count as standing", excess == 1, excess)
        check("a gorged supervised by a live repo-side script does not count as standing",
              excess == 1, excess)
        # The class this ticket fixes: an agent-launched gorged whose `-dir` is
        # outside the agent's worktree, whose launcher has exited, and whose
        # server reparented to systemd. The dir still names the live seat's id,
        # so it is owned and the excess stays 1 (only the intended demo plus
        # the stray -manabrew are standing).
        check("a reparented dev gorged whose dir names a live seat does not count as standing",
              excess == 1, excess)
        # An ancestor-alive dev gorged that names no seat: only the ancestor
        # signal reaches it. Assert it is genuinely not a `-dir`/worktree match
        # first, so the check cannot pass through the other signals.
        check("precondition: the ancestor-only dev gorged's dir names no live seat",
              _dir_names_agent("/tmp/gorge-dev-unnamed", _agent_seat_ids(_live_agent_pids(fproc))) is False)
        check("an ancestor-alive dev gorged that names no seat does not count as standing",
              excess == 1, excess)
        # Per-pid assertions, so each signal is isolated (the aggregate excess
        # above cannot tell which signal owned which process). The two signals
        # are independent: disabling either must flip only its own process.
        def owned_pid(pid: int) -> bool:
            procs_by_pid = {p["pid"]: p for p in gorged_processes(fproc)}
            ap = _live_agent_pids(fproc)
            return gorged_owned(procs_by_pid[pid], fproc, ap,
                                _agent_seat_ids(ap), _owner_dirs(ap))

        check("the reparented orphan (105) is owned by the dir-names-seat signal",
              owned_pid(105) is True)
        check("the ancestor-alive dev gorged (106) is owned by the ancestor signal",
              owned_pid(106) is True)
        # A genuinely standing instance -- the stray -manabrew on :8081 -- must
        # stay unowned, or the veto could never fire at all.
        check("the stray -manabrew is NOT owned", owned_pid(102) is False)
        check("both init-reparented instances are unsafe launches, the supervised one is not",
              unsafe == 2, unsafe)
        # Per-metric evidence notes (agent-20261001T060854Z-d1315abb): each
        # metric's row must carry a note naming ONLY the processes that
        # metric counts, and no note may be truncated at six entries.
        check("precondition: the demo (101) and the stray -manabrew (102) are init-reparented",
              _proc_ppid(fproc / "101") == 1 and _proc_ppid(fproc / "102") == 1,
              (_proc_ppid(fproc / "101"), _proc_ppid(fproc / "102")))
        _, _, standing_note, unsafe_note = collect_gorged_hygiene(fproc)
        check("precondition: the stray -manabrew (102) is the only unowned instance besides the demo",
              owned_pid(102) is False and owned_pid(101) is False
              and all(owned_pid(p) for p in (103, 104, 105, 106)))
        check("standing_gorged_excess's note names exactly the standing instances (demo + stray)",
              all(f"pid={p}" in standing_note for p in (101, 102))
              and not any(f"pid={p}" in standing_note for p in (103, 104, 105, 106)),
              standing_note)
        check("unsafe_gorged_launches's note names exactly the init-reparented instances",
              all(f"pid={p}" in unsafe_note for p in (101, 102))
              and not any(f"pid={p}" in unsafe_note for p in (103, 104, 105, 106)),
              unsafe_note)

        empty_proc = Path(td) / "proc-empty"
        empty_proc.mkdir()
        check("no gorged process running is not a failure",
              collect_gorged_hygiene(empty_proc)
              == (0, 0, "no gorged process running", "no gorged process running"))

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
    ap.add_argument("--format", choices=["plain", "md"], default="plain",
                    help="hotspots only: plain text (default) or a markdown "
                         "table merged with hotfiles-notes.json")
    ap.add_argument("--max-rows", type=int, default=None,
                    help="hotspots --format md: render at most this many live "
                         "rows plus a remainder count (AGENTS.md embed)")
    ap.add_argument("--selftest", action="store_true")
    a = ap.parse_args(argv)
    if a.selftest:
        return selftest()
    repo = a.repo.resolve()
    state_dir = a.state_dir or (ds4_dir(repo, "reward") or (repo / ".ds4" / "reward"))

    if a.axis == "hotspots":
        if a.format == "md":
            print(hotspots_md(repo, a.max_rows))
        else:
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
