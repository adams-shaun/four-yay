#!/usr/bin/env python3
"""reward.py — score gorge's reward ledger, and rank candidate work by it.

The ledger is .ds4/reward/scoreboard.jsonl, one JSON object per measurement:

    {"ts": "2026-09-29T21:40:00Z", "git_head": "2486f6a3", "axis": "eff",
     "metric": "ms_per_searched_decision_p50", "value": 18.4,
     "cost_s": 214, "cmd": "scripts/reward-probe.sh eff"}

Rows are append-only and never rewritten. This script only reads them.

Scoring (docs/superpowers/specs/2026-09-29-reward-loop-and-seed-agent-design.md
§2.1). Each axis contributes its primary metric's delta between the newest
git_head present and the previous distinct git_head, normalised by that
metric's own historical scale so Elo, milliseconds and card counts are
comparable, times the axis weight:

    score = 1*win + 10*eff + 1*obs + 1*audit - 100*stability_penalty

stability_penalty is a veto, not a smooth term: any OOM kill, daemon
crash-restart or gate timeout in the window makes it at least 1, which makes
the scalar negative however much strength the window bought.

Usage:
    scripts/reward.py --json [--ledger PATH]
    scripts/reward.py --md
    scripts/reward.py rank [--candidates PATH] [--json]
    scripts/reward.py --selftest
"""

from __future__ import annotations

import argparse
import json
import statistics
import sys
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

DEFAULT_LEDGER = Path(".ds4/reward/scoreboard.jsonl")
DEFAULT_CANDIDATES = Path(".ds4/reward/candidates.jsonl")

WEIGHTS = {
    "correct": 1000.0,
    "flow": 1000.0,
    "steward": 100.0,
    "win": 1.0,
    "eff": 10.0,
    "obs": 1.0,
    "audit": 1.0,
}
STABILITY_WEIGHT = 100.0


@dataclass(frozen=True)
class MetricSpec:
    """One axis's primary metric.

    direction is +1 when larger is better, -1 when smaller is better. floor is
    the smallest historical scale the normaliser will divide by, so a ledger
    with one or two rows cannot produce an unbounded delta.
    """

    axis: str
    metric: str
    direction: int
    floor: float


# The first spec listed for an axis wins when present; later ones are
# fallbacks, so a probe that cannot compute elo_per_ms still moves `eff`.
METRICS: tuple[MetricSpec, ...] = (
    # A confirmed engine defect on a card the build CLAIMS to support fully is
    # the worst thing this repo can contain: it is silently wrong rules, in the
    # set nobody is checking any more. Finding one is worth 1000x, and so is
    # closing it, so the loop hunts them and then fixes them.
    MetricSpec("correct", "validated_defects_found_cum", +1, 1.0),
    MetricSpec("correct", "validated_defects_closed_cum", +1, 1.0),
    # Merge-flow hot spots: a file several live branches all edit is where the
    # pipeline loses whole rounds to merge_fix, and every such round is paid
    # work that produces nothing. Fewer contended files and a lower merge_fix
    # rate are worth as much as a correctness win, because both are throughput
    # the fleet is otherwise burning.
    MetricSpec("flow", "conflict_hotspots", -1, 1.0),
    MetricSpec("flow", "merge_fix_rate", -1, 0.02),
    # Stewardship: the recurring tax each landed change leaves on every future
    # iteration. Gate wall time is paid by every ticket that ever lands again;
    # the agent context is paid by every seat on every turn (it was 290 KB here
    # once); an oversized file is paid by every agent that has to hold it in
    # context to change one line. All three are levels, not events, so the loop
    # is paid for driving them DOWN and charged for letting them grow.
    MetricSpec("steward", "gate_wall_s", -1, 5.0),
    MetricSpec("steward", "agent_context_bytes", -1, 512.0),
    MetricSpec("steward", "oversized_files", -1, 1.0),
    MetricSpec("win", "champion_elo", +1, 10.0),
    MetricSpec("eff", "elo_per_ms", +1, 0.05),
    MetricSpec("eff", "ms_per_searched_decision_p50", -1, 0.5),
    MetricSpec("eff", "ms_per_game", -1, 1.0),
    MetricSpec("obs", "observable_facts_exposed", +1, 1.0),
    MetricSpec("audit", "cards_with_oracle_verdict", +1, 10.0),
)

# Any of these above zero in the scored window is a stability failure. The
# value is the penalty each unit contributes, before STABILITY_WEIGHT.
STABILITY_METRICS = {
    "oom_kills": 1.0,
    "daemon_restarts": 1.0,
    "gate_timeouts": 1.0,
    "broker_kills": 0.5,
    "gate_starved_minutes": 0.05,
}


@dataclass
class Row:
    ts: str
    git_head: str
    axis: str
    metric: str
    value: float
    cost_s: float = 0.0
    cmd: str = ""
    note: str = ""


@dataclass
class AxisScore:
    axis: str
    metric: str | None = None
    value: float | None = None
    prev: float | None = None
    delta: float = 0.0
    scale: float = 0.0
    delta_norm: float = 0.0
    weight: float = 0.0
    contribution: float = 0.0
    reason: str = ""
    stale_heads: list[str] = field(default_factory=list)


def read_ledger(path: Path) -> list[Row]:
    rows: list[Row] = []
    if not path.exists():
        return rows
    for n, line in enumerate(path.read_text().splitlines(), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        try:
            d = json.loads(line)
            rows.append(
                Row(
                    ts=str(d["ts"]),
                    git_head=str(d["git_head"]),
                    axis=str(d["axis"]),
                    metric=str(d["metric"]),
                    value=float(d["value"]),
                    cost_s=float(d.get("cost_s", 0) or 0),
                    cmd=str(d.get("cmd", "")),
                    note=str(d.get("note", "")),
                )
            )
        except (KeyError, TypeError, ValueError) as e:
            # A malformed row is skipped loudly rather than failing the score:
            # the seed agent must still be able to act on the rows that parse.
            print(f"reward.py: {path}:{n}: skipped unreadable row ({e})", file=sys.stderr)
    return rows


def heads_in_order(rows: list[Row]) -> list[str]:
    """Distinct git_heads, oldest first, ordered by each head's first row."""
    seen: dict[str, str] = {}
    for r in rows:
        if r.git_head not in seen or r.ts < seen[r.git_head]:
            seen[r.git_head] = r.ts
    return [h for h, _ in sorted(seen.items(), key=lambda kv: (kv[1], kv[0]))]


def latest_value(rows: list[Row], metric: str, head: str) -> float | None:
    vals = [r for r in rows if r.metric == metric and r.git_head == head]
    if not vals:
        return None
    return max(vals, key=lambda r: r.ts).value


def historical_scale(rows: list[Row], metric: str, floor: float) -> float:
    """Median absolute per-head delta of a metric, floored.

    This is the axis's own noise scale: it is what makes a 30-Elo win-rate
    move and a 2 ms latency move comparable without hand-tuned units.
    """
    series = []
    for head in heads_in_order(rows):
        v = latest_value(rows, metric, head)
        if v is not None:
            series.append(v)
    deltas = [abs(b - a) for a, b in zip(series, series[1:]) if abs(b - a) > 0]
    if not deltas:
        return floor
    return max(floor, statistics.median(deltas))


# Axes whose metrics ADD rather than fall back to one another. `correct` sums,
# because finding a defect and closing it are two separate wins and the loop
# should be paid for both.
SUM_AXES = frozenset({"correct", "flow", "steward"})


def score_axis(rows: list[Row], axis: str) -> AxisScore:
    out = AxisScore(axis=axis, weight=WEIGHTS.get(axis, 0.0))
    axis_rows = [r for r in rows if r.axis == axis]
    if not axis_rows:
        out.reason = "no rows"
        return out
    heads = heads_in_order(axis_rows)
    if axis in SUM_AXES:
        parts, metrics = [], []
        for spec in (m for m in METRICS if m.axis == axis):
            cur_head = next(
                (h for h in reversed(heads) if latest_value(axis_rows, spec.metric, h) is not None),
                None,
            )
            if cur_head is None:
                continue
            cur = latest_value(axis_rows, spec.metric, cur_head)
            prev = next(
                (
                    v
                    for v in (
                        latest_value(axis_rows, spec.metric, h)
                        for h in reversed(heads[: heads.index(cur_head)])
                    )
                    if v is not None
                ),
                None,
            )
            metrics.append(spec.metric)
            if prev is None:
                continue
            scale = historical_scale(axis_rows, spec.metric, spec.floor)
            parts.append(((cur - prev) * spec.direction) / scale)
        if not metrics:
            out.reason = "no primary metric measured"
            return out
        out.metric = "+".join(metrics)
        out.delta_norm = sum(parts)
        out.contribution = out.weight * out.delta_norm
        if not parts:
            out.reason = "first measurement, no delta"
        return out
    for spec in (m for m in METRICS if m.axis == axis):
        cur_head = None
        for head in reversed(heads):
            if latest_value(axis_rows, spec.metric, head) is not None:
                cur_head = head
                break
        if cur_head is None:
            continue
        cur = latest_value(axis_rows, spec.metric, cur_head)
        prev = None
        for head in reversed(heads[: heads.index(cur_head)]):
            prev = latest_value(axis_rows, spec.metric, head)
            if prev is not None:
                break
        out.metric, out.value, out.prev = spec.metric, cur, prev
        out.scale = historical_scale(axis_rows, spec.metric, spec.floor)
        if prev is None:
            out.reason = "first measurement, no delta"
            return out
        out.delta = (cur - prev) * spec.direction
        out.delta_norm = out.delta / out.scale
        out.contribution = out.weight * out.delta_norm
        return out
    out.reason = "no primary metric measured"
    return out


def stability_penalty(rows: list[Row]) -> tuple[float, dict[str, float]]:
    """Penalty from the newest head's stability rows.

    Unlike the other axes this is a level, not a delta: an OOM kill in the
    current window is a failure on its own terms, not an improvement on the
    last window's two OOM kills.
    """
    axis_rows = [r for r in rows if r.axis == "stability"]
    if not axis_rows:
        return 0.0, {}
    head = heads_in_order(axis_rows)[-1]
    detail: dict[str, float] = {}
    penalty = 0.0
    for metric, unit in STABILITY_METRICS.items():
        v = latest_value(axis_rows, metric, head)
        if v is None or v <= 0:
            continue
        detail[metric] = v
        penalty += unit * v
    if detail and penalty < 1.0:
        penalty = 1.0  # any stability event is at least a full veto
    return penalty, detail


def score(rows: list[Row]) -> dict:
    heads = heads_in_order(rows)
    axes = {a: score_axis(rows, a) for a in WEIGHTS}
    pen, pen_detail = stability_penalty(rows)
    total = sum(a.contribution for a in axes.values()) - STABILITY_WEIGHT * pen
    return {
        "head": heads[-1] if heads else None,
        "prev_head": heads[-2] if len(heads) > 1 else None,
        "axes": {
            k: {
                "metric": v.metric,
                "value": v.value,
                "prev": v.prev,
                "delta": v.delta,
                "scale": v.scale,
                "delta_norm": v.delta_norm,
                "weight": v.weight,
                "contribution": v.contribution,
                "reason": v.reason,
            }
            for k, v in axes.items()
        },
        "stability": {"penalty": pen, "weight": STABILITY_WEIGHT, "detail": pen_detail},
        "score": total,
        "verdict": "REGRESSION" if total < 0 else ("FLAT" if total == 0 else "PROGRESS"),
        "blocked_by_stability": pen > 0,
    }


def as_markdown(s: dict) -> str:
    lines = [
        f"# reward scoreboard — head {s['head']} (vs {s['prev_head']})",
        "",
        f"**score {s['score']:+.2f} — {s['verdict']}**"
        + ("  **stability veto active**" if s["blocked_by_stability"] else ""),
        "",
        "| axis | metric | value | prev | delta | norm | weight | contribution |",
        "|---|---|---|---|---|---|---|---|",
    ]
    for axis, a in s["axes"].items():
        fmt = lambda x: "—" if x is None else f"{x:g}"  # noqa: E731
        lines.append(
            f"| {axis} | {a['metric'] or a['reason']} | {fmt(a['value'])} | "
            f"{fmt(a['prev'])} | {a['delta']:+g} | {a['delta_norm']:+.2f} | "
            f"{a['weight']:g}x | {a['contribution']:+.2f} |"
        )
    pen = s["stability"]
    detail = ", ".join(f"{k}={v:g}" for k, v in pen["detail"].items()) or "clean"
    lines += ["", f"stability: {detail} → penalty {pen['penalty']:g} x {pen['weight']:g}"]
    return "\n".join(lines) + "\n"


def rank_candidates(path: Path, s: dict) -> list[dict]:
    """Rank candidate work by weighted expected delta per unit of seat cost.

    This is the mechanism that keeps the loop from brute-forcing win rate: a
    candidate on the `eff` axis carries a 10x weight, so buying the same Elo
    more cheaply outranks buying more Elo expensively.
    """
    out = []
    if not path.exists():
        return out
    for n, line in enumerate(path.read_text().splitlines(), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        try:
            c = json.loads(line)
            if c.get("status") in ("queued", "done", "rejected"):
                continue
            axis = str(c.get("axis", ""))
            est_delta = float(c.get("est_delta", 0) or 0)
            est_cost = max(float(c.get("est_cost", 1) or 1), 0.1)
            weight = WEIGHTS.get(axis, 0.0)
            c["rank_score"] = weight * est_delta / est_cost
            out.append(c)
        except (TypeError, ValueError) as e:
            print(f"reward.py: {path}:{n}: skipped unreadable candidate ({e})", file=sys.stderr)
    # A stability veto outranks every opportunity: fixing the box comes first.
    out.sort(key=lambda c: (c.get("axis") != "stability", -c["rank_score"]))
    return out


# --------------------------------------------------------------------------
# selftest


def _rows(*triples) -> list[Row]:
    return [
        Row(ts=f"2026-09-{d:02d}T00:00:00Z", git_head=h, axis=a, metric=m, value=v)
        for d, (h, a, m, v) in enumerate(triples, start=1)
    ]


def selftest() -> int:
    fails = []

    def check(name, cond, detail=""):
        if cond:
            print(f"ok   {name}")
        else:
            print(f"FAIL {name} {detail}")
            fails.append(name)

    # One head: no delta anywhere, score flat, nothing divides by zero.
    s = score(_rows(("aaa", "win", "champion_elo", 1200.0)))
    check("one-head ledger scores flat", s["score"] == 0.0, s)
    check("one-head ledger explains itself", s["axes"]["win"]["reason"].startswith("first"), s)

    # A win-rate gain scores positive; the same gain on eff scores 10x more.
    win = score(_rows(("aaa", "win", "champion_elo", 1200.0), ("bbb", "win", "champion_elo", 1240.0)))
    check("win gain is positive", win["score"] > 0, win["score"])
    eff = score(
        _rows(
            ("aaa", "eff", "ms_per_searched_decision_p50", 20.0),
            ("bbb", "eff", "ms_per_searched_decision_p50", 18.0),
        )
    )
    check("lower latency is positive", eff["score"] > 0, eff["score"])
    check(
        "eff weighs 10x win for an equal normalised move",
        abs(eff["score"] / win["score"] - 10.0) < 1e-9,
        (eff["score"], win["score"]),
    )

    # Direction: a latency increase must be negative.
    worse = score(
        _rows(
            ("aaa", "eff", "ms_per_searched_decision_p50", 18.0),
            ("bbb", "eff", "ms_per_searched_decision_p50", 20.0),
        )
    )
    check("higher latency is negative", worse["score"] < 0, worse["score"])

    # The eff fallback chain: with no elo_per_ms, p50 still moves the axis.
    fb = score(
        _rows(
            ("aaa", "eff", "ms_per_game", 400.0),
            ("bbb", "eff", "ms_per_game", 380.0),
        )
    )
    check("eff falls back to ms_per_game", fb["axes"]["eff"]["metric"] == "ms_per_game", fb)

    # The 1000x axis: one confirmed defect found in the validated set outscores
    # a large win-rate window, and finding plus closing both count.
    found = score(
        _rows(
            ("aaa", "correct", "validated_defects_found_cum", 0.0),
            ("bbb", "correct", "validated_defects_found_cum", 1.0),
        )
    )
    check("finding a validated-set defect is positive", found["score"] > 0, found["score"])
    check(
        "one validated-set defect outscores a 40-Elo win window",
        found["score"] > win["score"] * 100,
        (found["score"], win["score"]),
    )
    both = score(
        _rows(
            ("aaa", "correct", "validated_defects_found_cum", 0.0),
            ("aaa", "correct", "validated_defects_closed_cum", 0.0),
            ("bbb", "correct", "validated_defects_found_cum", 1.0),
            ("bbb", "correct", "validated_defects_closed_cum", 1.0),
        )
    )
    check(
        "found and closed both count on the correct axis",
        abs(both["score"] - 2 * found["score"]) < 1e-9,
        (both["score"], found["score"]),
    )
    check("correct axis names both metrics", "+" in (both["axes"]["correct"]["metric"] or ""), both)

    # The flow axis: fewer contended files and a lower merge_fix rate both pay,
    # and both directions are inverted (smaller is better).
    flow = score(
        _rows(
            ("aaa", "flow", "conflict_hotspots", 6.0),
            ("bbb", "flow", "conflict_hotspots", 4.0),
        )
    )
    check("removing a conflict hotspot is positive", flow["score"] > 0, flow["score"])
    check("hotspot removal is weighted like correctness", flow["score"] > win["score"] * 100, flow["score"])
    worse_flow = score(
        _rows(
            ("aaa", "flow", "merge_fix_rate", 0.10),
            ("bbb", "flow", "merge_fix_rate", 0.20),
        )
    )
    check("a rising merge_fix rate is negative", worse_flow["score"] < 0, worse_flow["score"])

    # The steward axis: a slower gate suite or a fatter agent context is a tax
    # on every future iteration, and costs 100x.
    tax = score(
        _rows(
            ("aaa", "steward", "gate_wall_s", 120.0),
            ("bbb", "steward", "gate_wall_s", 180.0),
        )
    )
    check("a slower gate suite is negative", tax["score"] < 0, tax["score"])
    check("gate slowdown is weighted 100x", abs(tax["score"]) > abs(win["score"]) * 10, tax["score"])
    trim = score(
        _rows(
            ("aaa", "steward", "agent_context_bytes", 90000.0),
            ("bbb", "steward", "agent_context_bytes", 64000.0),
        )
    )
    check("trimming the agent context is positive", trim["score"] > 0, trim["score"])

    # A regression on the correct axis (a defect count that moves backwards
    # because a fix was reverted) must be negative, not merely flat.
    unfixed = score(
        _rows(
            ("aaa", "correct", "validated_defects_closed_cum", 3.0),
            ("bbb", "correct", "validated_defects_closed_cum", 2.0),
        )
    )
    check("losing a closed defect is negative", unfixed["score"] < 0, unfixed["score"])

    # The veto: a huge win-rate window with one OOM kill still scores negative.
    veto = score(
        _rows(
            ("aaa", "win", "champion_elo", 1000.0),
            ("bbb", "win", "champion_elo", 1400.0),
            ("bbb", "stability", "oom_kills", 1.0),
        )
    )
    check("stability veto overrides a large win", veto["score"] < 0, veto["score"])
    check("veto is reported", veto["blocked_by_stability"] and veto["verdict"] == "REGRESSION", veto)

    # Sub-unit stability counters still trip a full veto.
    starve = score(
        _rows(
            ("aaa", "win", "champion_elo", 1000.0),
            ("bbb", "win", "champion_elo", 1001.0),
            ("bbb", "stability", "gate_starved_minutes", 2.0),
        )
    )
    check("a small stability event is still a veto", starve["stability"]["penalty"] >= 1.0, starve)

    # A clean window with no stability rows is not penalised.
    check("no stability rows means no penalty", win["stability"]["penalty"] == 0.0, win)

    # Ranking: eff beats win at equal delta/cost; stability sorts first.
    with tempfile.TemporaryDirectory() as td:
        p = Path(td) / "candidates.jsonl"
        p.write_text(
            "\n".join(
                json.dumps(c)
                for c in [
                    {"id": "w1", "axis": "win", "est_delta": 1, "est_cost": 1},
                    {"id": "e1", "axis": "eff", "est_delta": 1, "est_cost": 1},
                    {"id": "e2", "axis": "eff", "est_delta": 1, "est_cost": 10},
                    {"id": "s1", "axis": "stability", "est_delta": 0, "est_cost": 1},
                    {"id": "q1", "axis": "eff", "est_delta": 99, "est_cost": 1, "status": "queued"},
                ]
            )
            + "\n"
        )
        r = rank_candidates(p, win)
        ids = [c["id"] for c in r]
        check("queued candidates are excluded", "q1" not in ids, ids)
        check("stability ranks first", ids[0] == "s1", ids)
        check("eff outranks win at equal cost", ids.index("e1") < ids.index("w1"), ids)
        check("cheap eff outranks expensive eff", ids.index("e1") < ids.index("e2"), ids)

    # A malformed row must not sink the score.
    with tempfile.TemporaryDirectory() as td:
        p = Path(td) / "scoreboard.jsonl"
        p.write_text(
            '{"ts":"1","git_head":"aaa","axis":"win","metric":"champion_elo","value":1000}\n'
            "not json\n"
            '{"ts":"2","git_head":"bbb","axis":"win","metric":"champion_elo","value":1010}\n'
        )
        rows = read_ledger(p)
        check("malformed rows are skipped", len(rows) == 2, len(rows))
        check("score survives a malformed ledger", score(rows)["score"] > 0)

    # Markdown rendering must not crash on an empty ledger.
    as_markdown(score([]))
    check("markdown renders an empty ledger", True)

    print(f"\n{len(fails)} failure(s)")
    return 1 if fails else 0


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("mode", nargs="?", default="score", choices=["score", "rank"])
    ap.add_argument("--ledger", type=Path, default=DEFAULT_LEDGER)
    ap.add_argument("--candidates", type=Path, default=DEFAULT_CANDIDATES)
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--md", action="store_true")
    ap.add_argument("--top", type=int, default=10)
    ap.add_argument("--selftest", action="store_true")
    a = ap.parse_args(argv)

    if a.selftest:
        return selftest()

    s = score(read_ledger(a.ledger))
    if a.mode == "rank":
        r = rank_candidates(a.candidates, s)[: a.top]
        if a.json:
            print(json.dumps(r, indent=2))
        else:
            for c in r:
                print(f"{c['rank_score']:8.3f}  {c.get('axis',''):9s} {c.get('id','')}  {c.get('title','')}")
        return 0
    if a.md:
        sys.stdout.write(as_markdown(s))
    else:
        print(json.dumps(s, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
