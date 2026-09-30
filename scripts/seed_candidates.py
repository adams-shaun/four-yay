#!/usr/bin/env python3
"""seed_candidates.py — generate reward-ranked candidate work, deterministically.

The seed agent calls this twice per cycle: once to refresh
.ds4/reward/candidates.jsonl, and once with --emit-tickets to get the top N as
tab-separated ticket rows (id, axis, title, brief body) it can file.

Every generator reads a measurement, never an opinion, and every ticket body
carries the measurement and the command that produced it — a brief whose "Done
means" names a real command is the difference between one round and five.

    scripts/seed_candidates.py --repo R --state-dir S --ledger L
    scripts/seed_candidates.py --repo R --state-dir S --ledger L --emit-tickets --top 3
    scripts/seed_candidates.py --selftest
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import re
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent


def _load(name: str):
    """Import a sibling script by path.

    The module must be registered in sys.modules BEFORE it is executed:
    @dataclass resolves its own module through sys.modules, and an unregistered
    module makes the decorator fail with an AttributeError on None.
    """
    if name in sys.modules:
        return sys.modules[name]
    spec = importlib.util.spec_from_file_location(name, HERE / f"{name}.py")
    m = importlib.util.module_from_spec(spec)
    sys.modules[name] = m
    spec.loader.exec_module(m)
    return m


rc = _load("reward_collect")
reward = _load("reward")

# Expected cost is in seat-rounds: a round is what a ticket costs the fleet
# whether it lands or not, so it is the denominator the ranking divides by.
COST_SPLIT_FILE = 3.0
COST_SEQUENCE = 1.0
COST_ORACLE_BATCH = 2.0
COST_PROFILE = 2.0
COST_CHECKLIST = 2.0
COST_GATE_TRIM = 2.0

MERGE_FIX_RATE_ALARM = 0.25
GATE_WALL_ALARM_S = 240.0
CONTEXT_ALARM_BYTES = 80_000
BIG_FILE_ALARM_LINES = 4000


def slug(s: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", s.lower()).strip("-")[:60]


# The daemon accepts an issue id as a single safe path component (ledger
# issues.py ISSUE_ID_RE); replicating the shape here keeps a worktree branch
# name from ever reaching a Depends-On line.
_ISSUE_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$")


def held_issue_ids(repo: Path, branches: tuple[str, ...]) -> list[str]:
    """Branch names of a hot group that are REAL open tickets in the ledger.

    A daemon-created branch is named after its issue id, so the name IS the
    issue id. An id is emitted only when `.ds4/issues/<id>.md` exists: the
    daemon reads a Depends-On value as an unmet dependency until it resolves,
    so a name no issue file declares -- a hand-named branch like `loop-design`,
    or a ticket from another repo's worktree -- would block the new ticket
    FOREVER, which is a worse failure than the collision it prevents.
    """
    issues = repo / ".ds4" / "issues"
    out: list[str] = []
    for b in branches:
        if _ISSUE_ID_RE.fullmatch(b) and (issues / f"{b}.md").is_file():
            if b not in out:
                out.append(b)
    return out


SEQUENCING_PARA = """\nSequencing: this ticket is filed with a `Depends-On:` line naming the tickets
whose branches hold the group's files right now. The daemon holds a ticket with
unmet dependencies out of dispatch until they close, so this work lands AFTER
the holders instead of colliding with them in `merge_fix`. Only ids that
resolve in `.ds4/issues/` are listed -- a dependency nobody can find blocks a
ticket forever, so a hand-named branch with no ticket file is reported here
instead of being trusted.
"""


def latest(ledger: list, axis: str, metric: str):
    vals = [r for r in ledger if r.axis == axis and r.metric == metric]
    return max(vals, key=lambda r: r.ts) if vals else None


def cand(cid, axis, title, body, est_delta, est_cost, evidence) -> dict:
    return {
        "id": cid,
        "axis": axis,
        "title": title,
        "body": body,
        "est_delta": est_delta,
        "est_cost": est_cost,
        "evidence": evidence,
        "status": "open",
    }


def generate(repo: Path, state_dir: Path, ledger_path: Path) -> list[dict]:
    rows = reward.read_ledger(ledger_path)
    out: list[dict] = []

    # ---- flow (1000x): contended files, and the merge_fix rate they cause.
    # Group by the SET of contending branches, not by file. Five files held by
    # the same three branches are ONE root cause and one ticket; the first
    # version filed one ticket per file and sent five briefs about the same
    # three idle branches, which is the whack-a-mole this repo has a rule
    # against. Branches that are idle are excluded here: the idle-branch
    # candidate below owns them, and a ticket to "split a file" cannot fix a
    # file whose real problem is that nobody is finishing the branch.
    idle_names = {b for b, _, _ in rc.idle_branches(repo)}
    groups: dict[tuple[str, ...], list[str]] = {}
    for f, branches in rc.hotspots(repo):
        live = tuple(sorted(b for b in branches if b not in idle_names))
        if len(live) < rc.HOTSPOT_BRANCHES:
            continue
        groups.setdefault(live, []).append(f)
    for branches, files in sorted(groups.items(), key=lambda kv: (-len(kv[1]), kv[0]))[:3]:
        flist = "\n".join(f"- `{f}`" for f in sorted(files))
        holders = held_issue_ids(repo, branches)
        unresolved = [b for b in branches if b not in holders]
        if holders:
            sequenced = SEQUENCING_PARA + "\nDepends-On: " + ", ".join(holders) + "\n"
            if unresolved:
                sequenced += (
                    "\nSequencing note: branch(es) `"
                    + "`, `".join(unresolved)
                    + "` have NO ticket file in `.ds4/issues/` and so are not on the\n"
                    "`Depends-On:` line (an unresolvable id blocks a ticket forever).\n"
                    "Sequence them by hand against this ticket before landing.\n"
                )
        else:
            sequenced = (
                "\nSequencing note: branches `"
                + "`, `".join(branches)
                + "` have NO ticket file in `.ds4/issues/`, so none can be\n"
                "named in a `Depends-On:` line (an unresolvable id blocks a ticket\n"
                "forever). Sequence them by hand before landing this work.\n"
            )
        out.append(
            cand(
                "flow-hotspot-" + slug("-".join(branches)),
                "flow",
                f"{len(files)} file(s) contended by {len(branches)} live branches: {', '.join(branches)}",
                f"""# {len(branches)} live branches are editing the same {len(files)} file(s)

Branches: {", ".join("`" + b + "`" for b in branches)}

Files they all touch:

{flist}

Measured by `scripts/reward_collect.py hotspots --repo .`, which lists every
file two or more live branches change against `main`. Each such file is where
the fleet loses whole paid rounds to `merge_fix` instead of to the work.
{sequenced}
## Goal

Remove the contention for the whole group, not one conflict. Either:

- sequence them: chain the tickets behind each other (`--depends`) so the second
  starts from the first's landing, which costs nothing and is right whenever the
  branches are all still moving, or
- split the files along the seam the branches are pulling apart, so two tickets
  touch two files (preferred when the seam is real and durable), or
- if a file cannot be split honestly, record it in the hot-file table in
  `.superpowers/ds4/gorge-context.md` so future briefs keep their changes to it
  small.

## Out of scope

Resolving the current conflicts; the daemon's merge_fix lane does that. This
ticket is about these files never being contended again. Do not touch the
branches' own work.

## Done means

`scripts/reward_collect.py hotspots --repo .` no longer lists these paths for
this branch set, and `go build ./... && go test ./...` is green. If the
resolution is sequencing or the hot-file table rather than a split, the commit
message says why a split was not honest.
""",
                est_delta=float(len(files) * len(branches)),
                est_cost=COST_SPLIT_FILE,
                evidence=f"{len(branches)} branches on {len(files)} files: {', '.join(branches)}",
            )
        )

    mf = latest(rows, "flow", "merge_fix_rate")
    if mf and mf.value >= MERGE_FIX_RATE_ALARM:
        out.append(
            cand(
                f"flow-mergefix-rate-{int(mf.value * 100)}",
                "flow",
                f"merge_fix rate is {mf.value:.0%} — sequence the contended briefs",
                f"""# Nearly every merge is going through merge_fix

Measured: `merge_fix_rate` = {mf.value:.3f} ({mf.note}). A `merge_fix` round is
a paid seat round that produces no new behaviour, so this is the single largest
throughput loss in the pipeline.

## Goal

Cut the rate. The two levers, in order:

1. Sequencing: tickets that share a file must not be dispatched together. The
   repo context's hot-file list and the ticket `--depends` chain are the
   mechanisms that already exist.
2. Splitting: the files at the top of
   `scripts/reward_collect.py hotspots --repo .` are where the collisions
   happen; the biggest ones each have their own ticket.

## Out of scope

Changing the daemon's merge strategy, and raising `max_merge_rounds`.

## Done means

A later `scripts/reward-probe.sh free` records a `merge_fix_rate` below
{MERGE_FIX_RATE_ALARM:.2f}, and the commit message names the sequencing or
splitting change that did it.
""",
                est_delta=mf.value * 10,
                est_cost=COST_SEQUENCE,
                evidence=mf.note,
            )
        )

    idle = latest(rows, "flow", "idle_unmerged_branches")
    if idle and idle.value > 0 and idle.note:
        out.append(
            cand(
                f"flow-idle-branches-{int(idle.value)}",
                "flow",
                f"{int(idle.value)} idle unmerged branches are holding files against every landing",
                f"""# Idle unmerged branches are a standing merge tax

Measured: `idle_unmerged_branches` = {int(idle.value)} ({idle.note}).

An unmerged branch nobody is working on still holds its files: every ticket that
lands afterwards rebases across it, and the ones that share a file pay a resolver
round for work that is not progressing. This is the durable half of the hot-spot
problem -- `conflict_hotspots` names the files, this names why they stay hot.

## Goal

For each branch above, decide and act: land it if it is finished, rebase and
finish it if it is close, or park it explicitly (branch kept, worktree removed)
if it is not being worked on. Removing the worktree is what releases the files.

Check `git -C <worktree> status --short` FIRST -- seats often finish without
committing, and a WIP commit by explicit path comes before anything else. Never
`git checkout` inside another seat's worktree.

## Out of scope

Deleting a branch, and merging anything whose gates do not pass.

## Done means

`scripts/reward_collect.py flow --repo .` records a lower
`idle_unmerged_branches`, `git worktree list` has no entry for a branch that was
parked, and every decision is one line in the commit message.
""",
                est_delta=float(idle.value),
                est_cost=COST_SEQUENCE,
                evidence=idle.note,
            )
        )

    # ---- correct (1000x): validated cards with no oracle verdict.
    total, gaps, pgaps = rc.validated_set(repo)
    validated = max(0, total - gaps - pgaps)
    verdicts = latest(rows, "audit", "cards_with_oracle_verdict")
    have = int(verdicts.value) if verdicts else 0
    if validated > have:
        batch = 25
        out.append(
            cand(
                f"correct-oracle-batch-{have // batch}",
                "correct",
                f"Oracle-audit the next {batch} validated cards ({have}/{validated} covered)",
                f"""# The validated set is mostly unaudited

{validated} repo-deck cards are in the validated set — neither ratchet table
lists them, so the build claims full support. {have} scenarios exist under
`rules/testdata/oracle/`. A defect in a card we claim to support fully is
silently wrong rules in the set nobody is checking, which is why finding one is
the highest-weighted outcome in the loop.

## Goal

Drive the next {batch} validated cards through the oracle harness
(`docs/superpowers/specs/2026-09-29-cross-engine-oracle-audit.md`), newest
mechanics first, and record each verdict as a scenario under
`rules/testdata/oracle/<family>/`.

For every disagreement, append one row to `.ds4/reward/defects.jsonl`:

    {{"card": "<name>", "validated_set": true, "status": "open",
      "scenario": "rules/testdata/oracle/<family>/<file>.json",
      "expected": "<Oracle/CR reading>", "got": "<gorge behaviour>"}}

Oracle text plus the CR is the arbiter of which engine is right, not the other
engine.

## Out of scope

Fixing the defects found — each confirmed defect gets its own ticket, so a fix
round is never blocked behind an audit round.

## Done means

{batch} new scenario files exist, `go test ./rules -run TestOracle` is green
(or names the disagreements as failures), and every disagreement has a row in
`.ds4/reward/defects.jsonl`.
""",
                est_delta=1.0,
                est_cost=COST_ORACLE_BATCH,
                evidence=f"{have} verdicts for {validated} validated cards",
            )
        )

    # Any open defect in the validated set outranks everything else the loop
    # could do: it is a known-wrong rule in the set we claim is right.
    defects = state_dir / "defects.jsonl"
    if defects.exists():
        for line in defects.read_text().splitlines():
            try:
                d = json.loads(line)
            except ValueError:
                continue
            if not d.get("validated_set", True) or d.get("status") not in (None, "open"):
                continue
            card = str(d.get("card", "unknown"))
            out.append(
                cand(
                    f"correct-defect-{slug(card)}",
                    "correct",
                    f"Engine defect on a fully-supported card: {card}",
                    f"""# {card} behaves differently from the oracle

Recorded in `.ds4/reward/defects.jsonl`:

```json
{json.dumps(d, indent=2)}
```

{card} is in the validated set: neither ratchet table lists it, so the build
claims full support for it. The disagreement is therefore either a real engine
defect or a wrong expectation — Oracle text and the CR decide which.

## Goal

Decide, then fix. If gorge is wrong, fix the primitive (not the card) and add
the regression test the scenario describes. If the expectation is wrong, correct
the scenario and say so in the commit message.

## Out of scope

Any other card, and any behaviour outside the scenario's steps.

## Done means

The scenario passes, a test fails without the fix (shown in the report), and the
defect row's `status` is `fixed`.
""",
                    est_delta=5.0,
                    est_cost=1.0,
                    evidence=str(d.get("scenario", "")),
                )
            )

    # ---- steward (100x): the recurring tax.
    gate = latest(rows, "steward", "gate_wall_s")
    if gate and gate.value >= GATE_WALL_ALARM_S:
        out.append(
            cand(
                f"steward-gate-wall-{int(gate.value)}",
                "steward",
                f"Gate suite takes {gate.value:.0f}s — cut what every future ticket pays",
                f"""# The gate suite is the tax on every landing

Measured: `gate_wall_s` = {gate.value:.0f}s ({gate.note}), from the spread of
each gate's own log mtimes under `.ds4/orchestrator/gates/`.

Every ticket that ever lands again pays this, so it compounds differently from
a one-off cost.

## Goal

Find the slowest gate and cut it without reducing what it proves: cache what is
invariant, narrow a `-run` pattern that has grown to cover packages it no longer
needs to, or parallelise within the gate. Say which gate and how much.

## Out of scope

Removing a gate, lowering its coverage, or marking tests as skipped.

## Done means

A later `scripts/reward-probe.sh free` records a lower `gate_wall_s`, and the
same failures are still caught: name the test that used to fail and show it
still does.
""",
                est_delta=gate.value / 60.0,
                est_cost=COST_GATE_TRIM,
                evidence=gate.note,
            )
        )

    ctx = latest(rows, "steward", "agent_context_bytes")
    if ctx and ctx.value >= CONTEXT_ALARM_BYTES:
        out.append(
            cand(
                f"steward-context-{int(ctx.value / 1000)}k",
                "steward",
                f"Agent context is {ctx.value / 1000:.0f}KB — every seat pays it every turn",
                f"""# The agent context has grown past its budget

Measured: `agent_context_bytes` = {int(ctx.value)} ({ctx.note}). Every seat
reads this on every turn, and it was 290KB here once before it was cut to 64KB.

## Goal

Move detail out of the always-loaded files into `docs/agents/` pages the seat
reads only when it needs them, keeping every rule that is load-bearing.

## Out of scope

Deleting a rule. If a rule looks obsolete, say so in the report and leave it.

## Done means

`agent_context_bytes` is under {CONTEXT_ALARM_BYTES}, no rule was lost (list
what moved where), and `go test ./internal/testutil` is green.
""",
                est_delta=(ctx.value - CONTEXT_ALARM_BYTES) / 10000.0,
                est_cost=1.0,
                evidence=ctx.note,
            )
        )

    big = latest(rows, "steward", "oversized_files")
    if big and big.note:
        first = big.note.split(";")[0].strip()
        m = re.match(r"(.+)\((\d+)\)", first)
        if m and int(m.group(2)) >= BIG_FILE_ALARM_LINES:
            path, lines = m.group(1), int(m.group(2))
            out.append(
                cand(
                    f"steward-split-{slug(path)}",
                    "steward",
                    f"{path} is {lines} lines — split it along its seams",
                    f"""# {path} is the largest file in the tree

Measured: {lines} lines ({int(big.value)} files are over 1500). Every agent that
changes one line of it pays for the whole file in context, and this repo's own
design guidance treats a file this large as a unit doing too much.

## Goal

Split it along a real seam into focused files, moving code only — no behaviour
change, no renamed exported symbols.

## Out of scope

Any behaviour change, and any rename that touches another package.

## Done means

`go build ./... && go test ./...` is green with no behaviour diff:
`scripts/reward-probe.sh free` records the same or fewer `oversized_files`, and
`go test ./rules -run TestHeads -v` still prints the pinned chain heads
unchanged.
""",
                    est_delta=lines / 1000.0,
                    est_cost=COST_SPLIT_FILE,
                    evidence=first,
                )
            )

    # ---- eff (10x): profile what the throughput probe measures.
    ms = latest(rows, "eff", "ms_per_game")
    if ms:
        out.append(
            cand(
                f"eff-profile-{int(ms.value * 100)}",
                "eff",
                f"Cut the top engine hotspot ({ms.value:.2f} ms/game today)",
                f"""# Engine throughput is {ms.value:.2f} ms/game

Measured by `scripts/reward-probe.sh eff` — botbench grind, one deck pinned to
one goroutine, so the number moves only when the ENGINE gets faster or slower.

Win rate per unit of compute is weighted ten times raw win rate here: the point
is to buy strength by making the engine cheaper, not by spending more of it.

## Goal

Profile the grind and remove the top hotspot:

    go build -o /tmp/botbench ./cmd/botbench
    scripts/heavy.sh probe --name prof -- /tmp/botbench -grind mono-red-prowess \\
        -grind-seconds 30 -cpuprofile /tmp/cpu.prof
    go tool pprof -top /tmp/cpu.prof | head -30

Follow this repo's hot-path rule: bitsets and dense slices, no maps, strings
compiled to masks.

## Out of scope

Changing any answer. A faster engine that plays differently is a defect, not an
optimisation.

## Done means

`go test ./rules -run TestHeads -v` prints the pinned chain heads UNCHANGED
(byte-identical replay proves no answer moved), a benchstat shows the hotspot's
improvement, and `scripts/reward-probe.sh eff` records a lower `ms_per_game`.
""",
                est_delta=1.0,
                est_cost=COST_PROFILE,
                evidence=f"{ms.value} ms/game",
            )
        )

    # ---- obs (1x): the axis has no denominator until the checklist exists.
    obs = latest(rows, "obs", "observable_facts_exposed")
    if obs and "absent" in (obs.note or ""):
        out.append(
            cand(
                "obs-checklist-bootstrap",
                "obs",
                "Enumerate what a human sees and ratchet the bot's view against it",
                """# The bots' state space has no denominator

`internal/botobs/checklist.json` does not exist, so the `obs` axis cannot be
measured. The questions that motivated it: can a bot see its own library as a
LIST (not the secret order)? Does it form an archetype prior for its own deck
and a posterior for the opponent's? The deck files already carry an
`archetype` field, so the self-prior is available and may simply be unread.

## Goal

1. Write `internal/botobs/checklist.json`: one entry per fact a human player has
   access to, each with `id`, `description`, `where` (the engine-side accessor a
   bot could read it from, or null if none exists) and `exposed` (whether a bot
   view actually carries it today).
2. Add a ratchet test asserting the measured exposed count equals the recorded
   one in both directions, the way the coverage ratchet does: a newly exposed
   fact that is not recorded fails, and a recorded fact that regresses fails.

Start from the seats' own observation path and the deck metadata; do not add any
new engine field in this ticket.

## Out of scope

Exposing anything new, and any policy change. This ticket measures; the gaps it
finds become their own tickets.

## Done means

`go test ./internal/botobs` is green, the checklist has at least the six facts
named in the design doc's §6, and `scripts/reward-probe.sh free` records
`observable_facts_exposed` and `observable_facts_total`.
""",
                est_delta=3.0,
                est_cost=COST_CHECKLIST,
                evidence="checklist.json absent",
            )
        )

    return out


def ticket_rows(cands: list[dict], ledger_path: Path, cand_path: Path, top: int) -> list[str]:
    ranked = reward.rank_candidates(cand_path, reward.score(reward.read_ledger(ledger_path)))
    by_id = {c["id"]: c for c in cands}
    rows = []
    for c in ranked[:top]:
        full = by_id.get(c["id"], c)
        body = str(full.get("body", "")).replace("\\", "\\\\").replace("\n", "\\n")
        rows.append(f"{full['id']}\t{full['axis']}\t{full['title']}\t{body}")
    return rows


def selftest() -> int:
    fails = []

    def check(name, cond, detail=""):
        print(("ok   " if cond else "FAIL ") + name + ("" if cond else f" {detail}"))
        if not cond:
            fails.append(name)

    with tempfile.TemporaryDirectory() as td:
        repo = Path(td) / "repo"
        (repo / "rules").mkdir(parents=True)
        state = Path(td) / "state"
        state.mkdir()
        ledger = state / "scoreboard.jsonl"
        ledger.write_text(
            "\n".join(
                json.dumps(
                    {"ts": "2026-09-29T00:00:0%d" % i, "git_head": "aaa", "axis": a,
                     "metric": m, "value": v, "note": n}
                )
                for i, (a, m, v, n) in enumerate(
                    [
                        ("flow", "merge_fix_rate", 0.77, "626/814"),
                        ("steward", "gate_wall_s", 300.0, "median"),
                        ("steward", "agent_context_bytes", 120000, "AGENTS.md=..."),
                        ("steward", "oversized_files", 40, "rules/cast.go(12515); x.go(2)"),
                        ("eff", "ms_per_game", 8.04, "grind"),
                        ("obs", "observable_facts_exposed", 0, "checklist.json absent — no denominator"),
                        ("audit", "cards_with_oracle_verdict", 190, ""),
                    ]
                )
            )
            + "\n"
        )
        (state / "defects.jsonl").write_text(
            json.dumps({"card": "Lightning Bolt", "validated_set": True, "status": "open"}) + "\n"
        )
        # Hot spots group by branch SET: three files held by the same two live
        # branches are one ticket, and files held by an IDLE branch belong to the
        # idle-branch candidate instead.
        real_hot, real_idle = rc.hotspots, rc.idle_branches
        rc.hotspots = lambda _r: [
            ("a.go", ["br1", "br2"]),
            ("b.go", ["br1", "br2"]),
            ("c.go", ["br1", "br2"]),
            ("d.go", ["br3", "br4"]),
            ("e.go", ["sleepy1", "sleepy2"]),
        ]
        rc.idle_branches = lambda _r, hours=12: [("sleepy1", 1, 40.0), ("sleepy2", 1, 40.0)]
        # Sequencing fixture: exactly ONE of the big group's branches resolves
        # to an issue file, so the Depends-On line must name br1 only and the
        # prose note must name br2; the (br3, br4) group resolves nothing.
        issues_dir = repo / ".ds4" / "issues"
        issues_dir.mkdir(parents=True)
        (issues_dir / "br1.md").write_text("# br1\n")
        assert (issues_dir / "br1.md").is_file() and not (issues_dir / "br2.md").is_file()
        try:
            grouped = [c for c in generate(repo, state, ledger) if c["id"].startswith("flow-hotspot-")]
        finally:
            rc.hotspots, rc.idle_branches = real_hot, real_idle
        check("one ticket per contending branch SET, not per file", len(grouped) == 2, [c["id"] for c in grouped])
        big = next((c for c in grouped if "br1" in c["id"]), None)
        check("the group's ticket lists every file it holds",
              big and all(f in big["body"] for f in ("a.go", "b.go", "c.go")), big and big["id"])
        check("a group held by idle branches is left to the idle-branch candidate",
              not any("sleepy" in c["id"] for c in grouped), [c["id"] for c in grouped])
        dep = next((ln for ln in (big["body"].splitlines() if big else [])
                    if ln.startswith("Depends-On:")), None)
        check("a key branch WITH a ticket file is on the group's Depends-On line",
              dep == "Depends-On: br1", dep)
        check("a key branch with NO ticket file is prose-noted, never depended on",
              dep is not None and "br2" not in dep and "`br2`" in big["body"],
              [ln for ln in big["body"].splitlines() if "br2" in ln][:2])
        other = next((c for c in grouped if "br3" in c["id"]), None)
        check("a group with NO resolvable branch carries no Depends-On line",
              other is not None
              and not any(ln.startswith("Depends-On:") for ln in other["body"].splitlines())
              and "`br3`" in other["body"] and "`br4`" in other["body"], other and other["id"])

        cands = generate(repo, state, ledger)
        ids = [c["id"] for c in cands]
        check("a high merge_fix rate makes a flow candidate", any(i.startswith("flow-mergefix") for i in ids), ids)
        check("an open validated-set defect makes a correct candidate",
              any(i.startswith("correct-defect-lightning-bolt") for i in ids), ids)
        check("a slow gate suite makes a steward candidate", any(i.startswith("steward-gate-wall") for i in ids), ids)
        check("a fat context makes a steward candidate", any(i.startswith("steward-context") for i in ids), ids)
        check("the biggest file makes a split candidate", any(i.startswith("steward-split-rules-cast") for i in ids), ids)
        check("throughput makes an eff candidate", any(i.startswith("eff-profile") for i in ids), ids)
        check("a missing checklist makes the obs bootstrap candidate", "obs-checklist-bootstrap" in ids, ids)
        check("every candidate names a Done means",
              all("## Done means" in c["body"] for c in cands),
              [c["id"] for c in cands if "## Done means" not in c["body"]])
        check("every candidate carries evidence", all(c["evidence"] is not None for c in cands))

        cand_path = state / "candidates.jsonl"
        cand_path.write_text("\n".join(json.dumps(c) for c in cands) + "\n")
        rows = ticket_rows(cands, ledger, cand_path, 3)
        check("emit-tickets returns the cap", len(rows) == 3, len(rows))
        check("ticket rows are single-line TSV with 4 fields",
              all(len(r.split("\t")) == 4 and "\n" not in r for r in rows), rows[:1])
        top_axes = [r.split("\t")[1] for r in rows]
        check("the 1000x axes come first", set(top_axes[:2]) <= {"correct", "flow"}, top_axes)
        ranked = reward.rank_candidates(cand_path, reward.score(reward.read_ledger(ledger)))
        scores = [c["rank_score"] for c in ranked]
        check("ranking is monotone", scores == sorted(scores, reverse=True), scores)
        # A 100x steward item may legitimately outrank a 1000x one when the
        # 1000x item's own estimated delta is small; what must never happen is
        # a 1x axis outranking a 1000x axis at a comparable delta and cost.
        w = {c["id"]: reward.WEIGHTS[c["axis"]] for c in ranked}
        worst_1000 = min((s for c, s in zip(ranked, scores) if w[c["id"]] == 1000.0), default=0)
        best_1x = max((s for c, s in zip(ranked, scores) if w[c["id"]] == 1.0), default=0)
        check("no 1x candidate outranks every 1000x candidate", best_1x < worst_1000 or worst_1000 == 0,
              (best_1x, worst_1000))

        # An empty repo and an empty ledger must produce no candidates and no crash.
        empty = Path(td) / "empty"
        (empty / "rules").mkdir(parents=True)
        el = state / "empty.jsonl"
        el.write_text("")
        check("an empty ledger yields no candidates", generate(empty, state / "nope", el) == []
              or all(c["axis"] == "correct" for c in generate(empty, state / "nope", el)))

    print(f"\n{len(fails)} failure(s)")
    return 1 if fails else 0


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path("."))
    ap.add_argument("--state-dir", type=Path, default=Path(".ds4/reward"))
    ap.add_argument("--ledger", type=Path, default=Path(".ds4/reward/scoreboard.jsonl"))
    ap.add_argument("--emit-tickets", action="store_true")
    ap.add_argument("--top", type=int, default=3)
    ap.add_argument("--selftest", action="store_true")
    a = ap.parse_args(argv)
    if a.selftest:
        return selftest()
    cands = generate(a.repo.resolve(), a.state_dir, a.ledger)
    if a.emit_tickets:
        cp = a.state_dir / "candidates.jsonl"
        for r in ticket_rows(cands, a.ledger, cp, a.top):
            print(r)
        return 0
    for c in cands:
        print(json.dumps(c))
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
