"""Summarize a gorgebridge run: leaderboard rows, key head-to-heads, terminations, sbagent stats."""
import json, sys, glob, collections, statistics as st
run, logdir = sys.argv[1], sys.argv[2]
lb = open(run + '/LEADERBOARD.md').read().split('\n')
print('\n'.join(lb[:16]))
rows = [json.loads(l) for l in open(run + '/matches.jsonl')]
print('classifications', collections.Counter(r['classification'] for r in rows), 'reasons', collections.Counter(r['reason'] for r in rows if r['classification'] != 'natural'))
def h2h(a, b):
    w = l = d = 0; per = collections.defaultdict(lambda: [0, 0])
    for r in rows:
        names = [s['name'] for s in r['seats']]
        if sorted(names) != sorted([a, b]) or r['classification'] not in ('natural', 'forfeit'): continue
        dk = r['decks'][0]['catalog_id']; per[dk][1] += 1
        if r['winner'] is None: d += 1
        elif names[0 if r['winner'] == 'p0' else 1] == a: w += 1; per[dk][0] += 1
        else: l += 1
    print(f'H2H {a} vs {b}: {w}-{d}-{l}  per deck ' + ' '.join(f'{k}:{v[0]}/{v[1]}' for k, v in sorted(per.items())))
for a, b in [('sb-search-lite-atk', 'heuristic'), ('sb-search-lite-atk', 'sb-tactical'), ('sb-search-lite-atk', 'uniform'), ('sb-tactical', 'heuristic'), ('heuristic', 'uniform')]:
    h2h(a, b)
agg = collections.defaultdict(collections.Counter); lat = collections.defaultdict(list); reasons = collections.defaultdict(collections.Counter)
for f in glob.glob(logdir + '/sbagent-*.log'):
    for line in open(f):
        if not line.startswith('sbagent-stats:'): continue
        d = json.loads(line.split(':', 1)[1]); b = d['Bot']; L = d['Latency']
        if not L.get('decisions'): continue
        a = agg[b]; a['procs'] += 1; a['decisions'] += L['decisions']; a['shadow_fallbacks'] += d['ShadowFallbacks']
        a['policy_fallbacks'] += d['PolicyFallbacks']; a['errors'] += sum(d['Errors'].values()); a['over_1s'] += L.get('over_1s', 0); a['over_10s'] += L.get('over_10s', 0)
        a['clock_stops'] += d['Shadow']['shadow'].get('clock_stops', 0)
        lat[b].append((L['mean_ms'], L['decisions'], L['p99_ms'], L['max_ms']))
        for k, v in d['Shadow']['shadow']['fallback_reasons'].items(): reasons[b][k] += v
for b in sorted(agg):
    xs = lat[b]; n = sum(x[1] for x in xs)
    print(b, dict(agg[b]), f"mean_ms={sum(x[0]*x[1] for x in xs)/n:.1f} p99(max over games)={max(x[2] for x in xs):.0f} p99(median over games)={st.median(x[2] for x in xs):.0f} max_ms={max(x[3] for x in xs):.0f}")
    print('  fallback reasons', reasons[b].most_common(8))
