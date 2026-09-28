"""replay.py SEED DECK0 DECK1 GAME_ID P0 P1 DUMP_STEP OUT

Replays one game on our agent_bridge_v1 (legacy reset, env_seed = game_seed) with
bots heuristic | uniform (python derivations, seed 11) | tac:<sbv1agent binary>,
dumps the decision at DUMP_STEP to OUT and, for a halted game, prints the last
decision. Deterministic: an arena ledger row replays exactly."""
import json, subprocess, sys
B="/mnt/sata/gorge-training/spellbench-work/arena/mtg-kernel/target/release/agent_bridge_v1"
seed,d0,d1,gid,p0,p1,dump,out=int(sys.argv[1]),sys.argv[2],sys.argv[3],sys.argv[4],sys.argv[5],sys.argv[6],int(sys.argv[7]),sys.argv[8]
def proc(cmd): return subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
eng=proc([B]); rid=[0]
def send(p,m):
    rid[0]+=1; m.update(protocol="spellbench/v1",request_id=f"r{rid[0]}")
    p.stdin.write(json.dumps(m)+"\n"); p.stdin.flush(); return json.loads(p.stdout.readline())
bots={}
for seat,spec in (("p0",p0),("p1",p1)):
    if spec.startswith("tac:"):
        a=proc([spec[4:],"-policy","tactical","-trace","/dev/stderr"]); send(a,{"request_type":"hello"})
        send(a,{"request_type":"game_start","game_id":gid,"seat":seat,"format":"pauper-bo1","decks":[{"catalog_id":d0},{"catalog_id":d1}],"engine":{"name":"x","version":"1","source_revision":None,"rules_snapshot_id":"r","card_pool_identity":"c"}})
        bots[seat]=a
    elif spec=="uniform":
        import hashlib
        st=[(11 ^ int.from_bytes(hashlib.sha256(gid.encode()).digest()[:8],"big")) & (2**64-1)]
        def nxt(st=st):
            st[0]=(st[0]+0x9E3779B97F4A7C15)&(2**64-1); z=st[0]
            z=((z^(z>>30))*0xBF58476D1CE4E5B9)&(2**64-1); z=((z^(z>>27))*0x94D049BB133111EB)&(2**64-1); return z^(z>>31)
        bots[seat]=nxt
    else: bots[seat]=None
def heur(d):
    cs=d["candidates"]
    for kind in ("play_land","cast_spell"):
        for c in cs:
            if c["semantic"]["kind"]==kind: return c["candidate_id"]
    for c in cs:
        if c["semantic"]["kind"] in ("activate_mana_ability","activate_ability"): return c["candidate_id"]
    for c in cs:
        if c["semantic"]["kind"]=="choose_attacker_inclusion" and c["semantic"]["include"]: return c["candidate_id"]
    for c in cs:
        if c["semantic"]["kind"]=="choose_blocker_inclusion" and not c["semantic"]["include"]: return c["candidate_id"]
    return 0
send(eng,{"request_type":"hello"})
r=send(eng,{"request_type":"reset","game_id":gid,"format":"pauper-bo1","seats":[{"seat":"p0","deck":{"catalog_id":d0}},{"seat":"p1","deck":{"catalog_id":d1}}],"game_seed":seed,"max_decisions":10000,"max_steps":100000})
while r["response_type"]=="decision":
    if r["step"]==dump: json.dump(r,open(out,"w"))
    a=bots[r["acting_seat"]]
    last=r
    if callable(a): pick=a()%len(r["candidates"])
    elif a: pick=send(a,{"request_type":"choose","game_id":gid,"decision":r})["selection"]["candidate_id"]
    else: pick=heur(r)
    r=send(eng,{"request_type":"step","game_id":gid,"expected_step":r["step"],"selection":{"candidate_id":pick,"semantic_echo":r["candidates"][pick]["semantic"]}})
print(r["outcome"], r["step_count"], r.get("reason"))
if r["classification"]=="halted":
    print("last decision:", last["step"], last["acting_seat"], last["state_summary"]["phase_step"], [ (c["semantic"]["kind"], (c["semantic"].get("source") or {}).get("card_name")) for c in last["candidates"]][:12], "picked", pick)
