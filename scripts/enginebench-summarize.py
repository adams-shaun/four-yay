#!/usr/bin/env python3
"""enginebench-summarize.py OUT.jsonl: per-row medians and paired ratios of a pair.sh run.

Throughput is per CPU-second, because a loaded shared box makes wall time
include waiting for a core:
  random/bot rows   turns / cpu_secs
  sampler/az rows   sims_per_cpu_s
  clone/step rows   1e6 / us_per_op_cpu_gc (ops per CPU-second, GC paid)

The ratio column is the median over reps of cand/base from the SAME rep, so a
load swing that hits one rep hits both sides of its ratio. For sampler rows it
also checks determinism: two runs that reached the same root count must report
the same Work and Extra counters.
"""
import collections
import json
import statistics
import sys


def throughput(r):
    d = r.get("detail") or {}
    if d.get("Turns") and r.get("cpu_secs"):
        return d["Turns"] / r["cpu_secs"], "turns/cpu-s"
    if r.get("sims_per_cpu_s"):
        return r["sims_per_cpu_s"], "sims/cpu-s"
    if r.get("us_per_op_cpu_gc"):
        return 1e6 / r["us_per_op_cpu_gc"], "ops/cpu-s"
    return None, ""


def key(r):
    # The auto-pay bot reports its own row name ("bot-autopay").
    return (r["row"], r.get("pair", ""))


def main(path):
    vals = collections.defaultdict(lambda: collections.defaultdict(dict))
    units = {}
    errs = []
    work = collections.defaultdict(dict)
    for line in open(path):
        r = json.loads(line)
        if r.get("err"):
            errs.append(f'{r.get("label")} rep={r.get("rep")} {r["row"]}: {r["err"]}')
            continue
        t, unit = throughput(r)
        if t is None:
            continue
        k = key(r)
        units[k] = unit
        vals[k][r.get("rep", 0)][r.get("label", "")] = t
        d = r.get("detail") or {}
        if r["row"] == "sampler" and "Roots" in d:
            sig = (d.get("Work"), d.get("Extra"))
            prev = work[k].get(d["Roots"])
            if prev is not None and prev != sig:
                errs.append(f"{k}: NONDETERMINISM at roots={d['Roots']}: {prev} vs {sig}")
            work[k][d["Roots"]] = sig
    labels = sorted({lab for k in vals for rep in vals[k].values() for lab in rep})
    print(f"{'row':<22}{'unit':<13}" + "".join(f"{lab + ' med':>12}" for lab in labels) + f"{'cand/base':>11}  per-rep ratios")
    for k in sorted(vals):
        reps = vals[k]
        meds = []
        for lab in labels:
            xs = [rep[lab] for rep in reps.values() if lab in rep]
            meds.append(statistics.median(xs) if xs else float("nan"))
        ratios = [rep["cand"] / rep["base"] for rep in reps.values() if "cand" in rep and "base" in rep]
        rmed = f"{statistics.median(ratios):.3f}" if ratios else "-"
        print(f"{' '.join(k):<22}{units[k]:<13}" + "".join(f"{m:>12.0f}" for m in meds) + f"{rmed:>11}  " + " ".join(f"{x:.3f}" for x in ratios))
    for e in errs:
        print("!!", e)
    return 1 if errs else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
