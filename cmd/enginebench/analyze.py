#!/usr/bin/env python3
"""Summarise enginebench JSONL runs: medians and IQR per (row, build).

usage: analyze.py results.jsonl [--md]
"""
import json, statistics, sys
from collections import defaultdict

# docs/015 §1's gorge column (M1 Pro, gorge a4af596).
UPSTREAM = {
    ("random", "A", "turns_per_s"): 731, ("random", "B", "turns_per_s"): 1444,
    ("random", "burn", "turns_per_s"): 2703,
    ("bot", "A", "games_per_s"): 38, ("bot", "B", "games_per_s"): 58,
    ("clone", "", "us_per_op_cpu"): 14, ("step", "", "us_per_op_cpu"): 46,
    ("sampler", "", "sims_per_cpu_s"): 40,
}
# For a time-per-op metric smaller is faster.
LOWER_BETTER = {"us_per_op_cpu", "us_per_op_cpu_gc", "us_per_op", "ms_per_root"}

METRICS = {
    "random": ["turns_per_s", "games_per_s", "decisions_per_s", "turns_per_game", "wall_turns_per_s"],
    "bot": ["games_per_s", "turns_per_game", "decisions_per_s"],
    "bot-autopay": ["games_per_s", "turns_per_game", "decisions_per_s"],
    "clone": ["us_per_op_cpu", "us_per_op_cpu_gc", "us_per_op", "alloc_bytes_per_op", "retained_bytes_per_clone", "heap_live_mb"],
    "step": ["us_per_op_cpu", "us_per_op_cpu_gc", "alloc_bytes_per_op", "pass_us"],
    "sampler": ["sims_per_cpu_s", "sims_per_s", "ms_per_root", "roots"],
    "az100": ["sims_per_cpu_s", "sims_per_s", "ms_per_root", "roots", "steps_per_sim"],
    "az1000": ["sims_per_cpu_s", "sims_per_s", "ms_per_root", "roots", "steps_per_sim"],
}


def derived(r):
    d = r.get("detail") or {}
    if r.get("games"):
        r["turns_per_game"] = r["turns"] / r["games"]
    if r["row"] == "random" and r.get("secs"):
        r["wall_turns_per_s"] = r["turns"] / r["secs"]
    if r["row"] == "step" and d.get("PerOption"):
        ps = [o["USPerCPU"] for o in d["PerOption"] if o["Kind"] == "pass"]
        if ps:
            r["pass_us"] = statistics.median(ps)
    if r["row"].startswith("az") and d.get("Work"):
        r["steps_per_sim"] = d["Extra"] / d["Work"]
    return r


def q(xs):
    xs = sorted(xs)
    if len(xs) == 1:
        return xs[0], xs[0], xs[0]
    qs = statistics.quantiles(xs, n=4, method="inclusive")
    return qs[0], statistics.median(xs), qs[2]


def main():
    rows = defaultdict(lambda: defaultdict(list))
    loads = defaultdict(list)
    errs = []
    for line in open(sys.argv[1]):
        r = json.loads(line)
        if r.get("err"):
            errs.append((r["label"], r["row"], r["err"][:100]))
            continue
        r = derived(r)
        pair = r.get("pair", "") if r["row"] in ("random", "bot", "bot-autopay") else ""
        for m in METRICS.get(r["row"], []):
            if r.get(m) is not None:
                rows[(r["row"], pair, m)][r["label"]].append(r[m])
        loads[r["label"]].append(r["load1"])
    labels = ["a4af596", "e0496f062", "dev", "9803b0655", "1e27720d0", "churn-snap"]
    print("metric".ljust(42) + "".join(l.rjust(24) for l in labels))
    for key in sorted(rows):
        row, pair, m = key
        cells = []
        for l in labels:
            xs = rows[key].get(l)
            if not xs:
                cells.append("-".rjust(24))
                continue
            lo, md, hi = q(xs)
            cells.append(f"{md:.4g} [{lo:.3g},{hi:.3g}] n{len(xs)}".rjust(24))
        print(f"{row}/{pair}/{m}".ljust(42) + "".join(cells))
    print()
    print("machine factor (docs/015 M1 Pro / ours at a4af596) and main vs a4af596:")
    for (row, pair, m), up in UPSTREAM.items():
        a = rows.get((row, pair, m), {}).get("a4af596")
        b = rows.get((row, pair, m), {}).get("e0496f062")
        if not a:
            continue
        am = statistics.median(a)
        f = (am / up) if m in LOWER_BETTER else (up / am)
        line = f"  {row}/{pair}/{m}: upstream {up}, ours@a4af596 {am:.4g}, M1/box speed factor {f:.2f}"
        if b:
            bm = statistics.median(b)
            sp = (am / bm) if m in LOWER_BETTER else (bm / am)
            line += f"; main {bm:.4g}, main speedup {sp:.2f}x"
        print(line)
    print()
    for l in labels:
        if loads[l]:
            print(f"  load1 {l}: median {statistics.median(loads[l]):.1f} min {min(loads[l]):.1f} max {max(loads[l]):.1f}")
    for e in errs:
        print("  ERR", e)


main()
