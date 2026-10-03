#!/usr/bin/env python3
"""compliance-summary.py RUNDIR [SET...] -- aggregate one compliance pass.

Reads what scripts/compliance-pass.sh leaves per set under RUNDIR/<SET>/:
gen's skips (scen.jsonl.skips.jsonl), plan.log and diff's verdict lines
(verdicts.jsonl, the STALE scenarios only). Prints one line per set, the
unsupported primitives that keep cards from a scenario (ranked by cards),
the other skip reasons, and every recomputed verdict that is not AGREE.
"""
import collections
import glob
import json
import os
import sys


def lines(path):
    if not os.path.exists(path):
        return []
    with open(path) as f:
        return [json.loads(l) for l in f if l.strip()]


def main():
    run = sys.argv[1]
    sets = sys.argv[2:] or sorted(os.path.basename(d.rstrip("/")) for d in glob.glob(run + "/*/"))
    prim = collections.defaultdict(set)
    other = collections.defaultdict(set)
    bad = {}
    for s in sets:
        d = os.path.join(run, s)
        if not os.path.isdir(d):
            continue
        cnt = collections.Counter()
        for v in lines(os.path.join(d, "verdicts.jsonl")):
            vv = v.get("verdict") or {}
            st = vv.get("status", "?")
            cnt[st] += 1
            if st != "AGREE":
                rest = {k: x for k, x in vv.items() if k != "status"}
                bad.setdefault(v.get("card"), (s, st, json.dumps(rest)[:220]))
        skips = lines(os.path.join(d, "scen.jsonl.skips.jsonl"))
        for k in skips:
            r = k.get("reason", "")
            if r.startswith("unsupported ["):
                for p in r[len("unsupported ["):-1].split():
                    prim[p].add(k.get("card"))
            else:
                other[r[:60]].add(k.get("card"))
        scen = len(lines(os.path.join(d, "scen.jsonl")))
        plan = ""
        if os.path.exists(os.path.join(d, "plan.log")):
            with open(os.path.join(d, "plan.log")) as f:
                plan = f.readline().strip()
        print(f"{s:5} scenarios={scen} skips={len(skips)} {plan} recomputed={dict(cnt)}")
    print("\n== unsupported primitives (cards)")
    for p, cs in sorted(prim.items(), key=lambda x: (-len(x[1]), x[0])):
        print(f"{len(cs):4} {p}  e.g. {sorted(cs)[:3]}")
    print("\n== other skips")
    for r, cs in sorted(other.items(), key=lambda x: (-len(x[1]), x[0])):
        print(f"{len(cs):4} {r}  e.g. {sorted(cs)[:3]}")
    print(f"\n== recomputed verdicts that are not AGREE: {len(bad)} cards")
    for c, (s, st, d) in sorted(bad.items(), key=lambda x: (x[1][1], x[1][0], x[0])):
        print(f"{s:5} {st:10} {c}: {d}")


if __name__ == "__main__":
    main()
