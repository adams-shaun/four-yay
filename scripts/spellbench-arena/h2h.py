"""h2h.py RUN_DIR [BOT...]: head-to-heads with Wilson 95% CIs, split pairs (seat-swapped pairs each side won once: identical deterministic policies split every pair), per-deck rows for BOT, and the summed -stats counters (RUN_DIR.stats) incl. hint_lookups."""
import json, sys, collections, math, os
run = sys.argv[1]; focus = sys.argv[2:]
rows = [json.loads(l) for l in open(os.path.join(run, "matches.jsonl"))]
cls = collections.Counter(r["classification"] for r in rows)
def wilson(w, n):
    if n == 0: return (0, 0)
    z = 1.96; p = w / n; d = 1 + z*z/n
    c = (p + z*z/(2*n)) / d; h = z*math.sqrt(p*(1-p)/n + z*z/(4*n*n)) / d
    return (100*(c-h), 100*(c+h))
h2h = collections.defaultdict(lambda: collections.Counter())
pairs = collections.defaultdict(list)
deck = collections.defaultdict(collections.Counter)
for r in rows:
    names = [s["name"] for s in r["seats"]]
    key = tuple(sorted(names))
    w = r["winner"]; wn = names[0] if w == "p0" else names[1] if w == "p1" else None
    if r["classification"] != "natural": h2h[key]["other"] += 1; continue
    h2h[key][wn or "draw"] += 1
    pairs[(key, r["matchup_index"], r["pair_index"])].append(wn)
    for i, s in enumerate(r["seats"]):
        deck[(s["name"], r["decks"][i]["catalog_id"], names[1-i])]["W" if wn == s["name"] else "L"] += 1
print("classification", dict(cls))
for key, c in sorted(h2h.items()):
    a, b = key; n = c[a] + c[b]
    lo, hi = wilson(c[a], n)
    splits = sum(1 for (k, _, _), ws in pairs.items() if k == key and len(ws) == 2 and ws[0] != ws[1])
    npairs = sum(1 for (k, _, _) in pairs if k == key)
    print(f"{a:>12} vs {b:<12} {c[a]:4d}-{c[b]:<4d} {100*c[a]/max(n,1):5.1f}% [{lo:4.1f},{hi:4.1f}]  split pairs {splits}/{npairs}" + (f"  other {c['other']}" if c['other'] else ""))
for f in focus:
    print(f"-- per deck: {f}")
    agg = collections.defaultdict(collections.Counter)
    for (n, dk, opp), c in deck.items():
        if n == f: agg[dk]["W"] += c["W"]; agg[dk]["L"] += c["L"]
    for dk, c in sorted(agg.items()): print(f"   {dk:10s} {c['W']}-{c['L']}")
st = [f for f in os.listdir(os.path.dirname(run.rstrip('/'))) if f.startswith(os.path.basename(run.rstrip('/'))) and f.endswith(".stats")]
for f in st:
    tot = collections.Counter()
    for l in open(os.path.join(os.path.dirname(run.rstrip('/')), f)):
        d = json.loads(l)
        for k in ("decisions", "fallbacks", "kernel_missing", "wire_errors", "retries", "hint_lookups"): tot[(d["bot"], k)] += d.get(k, 0)
        tot[(d["bot"], "procs")] += 1
    for b in sorted({b for b, _ in tot}):
        print("stats", b, {k: tot[(b, k)] for k in ("procs", "decisions", "fallbacks", "kernel_missing", "wire_errors", "retries", "hint_lookups")})
