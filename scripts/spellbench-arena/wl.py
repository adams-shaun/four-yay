"""Win/loss summary of an arena ledger: usage wl.py matches.jsonl [BOT] (per-deck rows for BOT)."""
import json,sys,collections
m=collections.Counter(); cls=collections.Counter(); deck=collections.defaultdict(collections.Counter)
for l in open(sys.argv[1]):
    r=json.loads(l); cls[r['classification']]+=1
    names=[s['name'] for s in r['seats']]
    key=tuple(sorted(names))
    if r['classification']!='natural': continue
    w=r['winner']
    wn=names[0] if w=='p0' else names[1] if w=='p1' else 'draw'
    m[(key,wn)]+=1
    for i,s in enumerate(r['seats']):
        deck[(s['name'],r['decks'][i]['catalog_id'])]['W' if wn==s['name'] else 'L']+=1
print(dict(cls))
for key in sorted({k for k,_ in m}):
    print(key, {w:c for (k,w),c in m.items() if k==key})
if len(sys.argv)>2:
    for (n,dk),c in sorted(deck.items()):
        if n==sys.argv[2]: print(n,dk,dict(c))
