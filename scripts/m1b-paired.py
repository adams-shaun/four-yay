#!/usr/bin/env python3
"""M1b: paired difference between two botbench -spellbench arms on the SAME schedule.

usage: m1b-paired.py <armA matches.jsonl> <armB matches.jsonl>

Both ledgers must be the same base seed and schedule (the arm's policy is the
non-"bot" seat). For every game id present and natural in both, the arm's score
(win 1, draw .5, loss 0) is differenced; prints the mean difference A-B with a
CI95 over games (same seed and seat for both arms: common random numbers).
"""
import json, math, sys

def scores(path):
    out = {}
    for l in open(path):
        r = json.loads(l)
        if r["classification"] != "natural":
            continue
        arm = next(s for s in r["seats"] if s["name"] != "bot")
        sc = 0.5 if r["outcome"] == "draw" else float(r["winner"] == arm["seat"])
        out[(r["game_id"], r["game_seed"])] = sc
    return out

a, b = scores(sys.argv[1]), scores(sys.argv[2])
keys = sorted(set(a) & set(b))
d = [a[k] - b[k] for k in keys]
n = len(d); m = sum(d) / n
sd = math.sqrt(sum((x - m) ** 2 for x in d) / (n - 1))
ci = 1.96 * sd / math.sqrt(n)
print(f"A-B = {m*100:+.1f}pp [{(m-ci)*100:+.1f}, {(m+ci)*100:+.1f}] over {n} shared games "
      f"(A {sum(a[k] for k in keys)/n*100:.1f}%, B {sum(b[k] for k in keys)/n*100:.1f}%)")
