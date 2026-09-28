#!/usr/bin/env python3
"""M1b: win rate of one policy in a botbench -spellbench ledger, with CI95.

usage: m1b-winrate.py <matches.jsonl> [<matches.jsonl> ...] [-name NAME]

Reads spellbench-match-ledger/v1 rows. For every matchup containing NAME
(default: the first seat name containing "az"), prints the natural-game win
rate (draws count half) with a normal-approximation CI95 over games, and the
paired CI95 over seat-swapped pairs (both games of a pair share one seed:
common random numbers), plus the truncated/halted counts that are excluded.
"""
import json, math, sys
from collections import defaultdict

args = sys.argv[1:]
name = None
if "-name" in args:
    i = args.index("-name"); name = args[i + 1]; del args[i:i + 2]
rows = [json.loads(l) for p in args for l in open(p)]
if name is None:
    name = next(s["name"] for r in rows for s in r["seats"] if "az" in s["name"])
by = defaultdict(list)
for r in rows:
    names = [s["name"] for s in r["seats"]]
    if name not in names:
        continue
    opp = [n for n in names if n != name][0]
    by[opp].append(r)
for opp, rs in sorted(by.items()):
    pairs = defaultdict(list); trunc = halt = 0; scores = []
    for r in rs:
        if r["classification"] != "natural":
            trunc += r["classification"] == "truncated"; halt += r["classification"] == "halted"
            continue
        me = next(s["seat"] for s in r["seats"] if s["name"] == name)
        sc = 0.5 if r["outcome"] == "draw" else float(r["winner"] == me)
        scores.append(sc); pairs[(r["matchup_index"], r["pair_index"], r["game_seed"])].append(sc)
    n = len(scores); p = sum(scores) / n
    ci = 1.96 * math.sqrt(p * (1 - p) / n)
    pm = [sum(v) / len(v) for v in pairs.values() if len(v) == 2]
    m = sum(pm) / len(pm); sd = math.sqrt(sum((x - m) ** 2 for x in pm) / (len(pm) - 1)) if len(pm) > 1 else 0
    pci = 1.96 * sd / math.sqrt(len(pm)) if pm else 0
    print(f"{name} vs {opp}: {p*100:.1f}% [{(p-ci)*100:.1f}, {(p+ci)*100:.1f}] over {n} games; "
          f"paired {m*100:.1f}% ±{pci*100:.1f} over {len(pm)} pairs; truncated {trunc} halted {halt}")
