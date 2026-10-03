import sys,json,collections,glob
agg=collections.Counter(); fr=collections.Counter(); lat=[]
for f in glob.glob(sys.argv[1]+'/sbagent-*.log'):
  for l in open(f):
    if not l.startswith('sbagent-stats:'): continue
    d=json.loads(l.split(':',1)[1]); bot=d['Bot']; t=d['Shadow'].get('tactical') or {}
    L=d['Latency']; agg[bot+' decisions']+=L.get('decisions',0); agg[bot+' fallbacks']+=d['ShadowFallbacks']
    if L.get('decisions'): lat.append((bot,L['mean_ms'],L['p99_ms'],L['max_ms'],L['decisions']))
    for k,v in (t.get('AbortsByCause') or {}).items(): fr[bot+' abort:'+k]+=v
    for k,v in d['Shadow']['shadow']['fallback_reasons'].items(): fr[bot+' '+k]+=v
    agg[bot+' lostplays']+=t.get('LostPlays',0); agg[bot+' lowered']+=t.get('LoweredCasts',0)+t.get('LoweredAbilities',0)
print(dict(agg)); print(fr.most_common(15))
for b in sorted({x[0] for x in lat}):
  xs=[x for x in lat if x[0]==b]; n=sum(x[4] for x in xs)
  print(b,'mean_ms',round(sum(x[1]*x[4] for x in xs)/n,1),'max p99 per proc',max(x[2] for x in xs),'max',max(x[3] for x in xs))
