"""Joint anchored Bradley-Terry over several ledgers (v1 and v2), cross-protocol, UNOFFICIAL.

Bots are keyed by name, except `first`, which is keyed name@protocol (v2 poses pass first, so v2 first always
passes and is a different bot). Draws count half, one virtual draw per matchup, anchor uniform = 1000 (as
spellbench's leaderboard). CI95: bootstrap resampling seat-swapped pairs within each matchup of each ledger.
usage: joint.py REPS SEED LEDGER[:tag] ...
"""
import json, math, random, sys, collections
reps, seed = int(sys.argv[1]), int(sys.argv[2])
groups = collections.defaultdict(list)  # (ledger, matchup, pair) -> [(a, b, score_a)]
for li, spec in enumerate(sys.argv[3:]):
    path, _, tag = spec.partition(':')
    for line in open(path):
        r = json.loads(line)
        if r['classification'] not in ('natural', 'forfeit'):
            continue
        proto = 'v1' if r['schema'].endswith('/v1') else 'v2'
        names = [s['name'] + ('@' + proto if s['name'] == 'first' else '') for s in r['seats']]
        w = r['winner']
        sa = 0.5 if w is None else (1.0 if w == 'p0' else 0.0)
        groups[(li, r['matchup_index'], r['pair_index'])].append((names[0], names[1], sa))
def fit(gs):
    W = collections.Counter(); N = collections.Counter(); bots = set()
    for g in gs:
        for a, b, s in g:
            bots |= {a, b}; W[a] += s; W[b] += 1 - s; N[frozenset((a, b))] += 1
    for k in list(N):
        a, b = tuple(k); W[a] += 0.5; W[b] += 0.5; N[k] += 1
    r = {x: 1.0 for x in bots}
    for _ in range(2000):
        new = {}
        for i in bots:
            den = sum(n / (r[i] + r[j]) for k, n in N.items() if i in k for j in k if j != i)
            new[i] = W[i] / den if den else r[i]
        s = new['uniform']
        new = {k: v / s for k, v in new.items()}
        if max(abs(math.log(new[k]) - math.log(r[k])) for k in bots) < 1e-10:
            r = new; break
        r = new
    return {k: 1000 + 400 * math.log10(v) for k, v in r.items()}, N
base, N = fit(groups.values())
games = collections.Counter()
for g in groups.values():
    for a, b, s in g: games[a] += 1; games[b] += 1
rng = random.Random(seed)
by_matchup = collections.defaultdict(list)
for k, g in groups.items(): by_matchup[k[:2]].append(g)
samples = collections.defaultdict(list)
for _ in range(reps):
    gs = [rng.choice(v) for v in by_matchup.values() for _ in v]
    e, _ = fit(gs)
    for k, v in e.items(): samples[k].append(v)
print('| Rank | Bot | Elo | CI95 | Games |\n|---:|---|---:|---|---:|')
for i, (k, v) in enumerate(sorted(base.items(), key=lambda x: -x[1])):
    s = sorted(samples[k]); lo, hi = s[int(0.025 * len(s))], s[int(0.975 * len(s)) - 1]
    print(f'| {i+1} | {k} | {v:.1f} | [{lo:.1f}, {hi:.1f}] | {games[k]} |')
