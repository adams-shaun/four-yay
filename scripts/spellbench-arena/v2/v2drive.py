"""Drive agent_bridge_v2 directly: v2 heuristic vs uniform (random.Random), mirror decks."""
import json, subprocess, sys, random, hashlib, os
B = sys.argv[1]; n_per_deck = int(sys.argv[2])
p = subprocess.Popen([B], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
rid = 0
def call(m):
    global rid
    rid += 1; m["request_id"] = f"h-{rid}"; m["protocol"] = "spellbench/v2"
    p.stdin.write(json.dumps(m) + "\n"); p.stdin.flush()
    return json.loads(p.stdout.readline())
canon = lambda v: json.dumps(v, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
def heur(c):
    def rank(s):
        k = s["kind"]
        prefs = (k == "play_land", k == "cast_spell", k in ("activate_mana_ability", "activate_ability"),
                 k == "declare_attack" and s.get("defender") is not None, k == "declare_block" and s.get("attacker") is None)
        return prefs.index(True) if True in prefs else 9
    rs = [rank(x["semantic"]) for x in c]
    return rs.index(min(rs))
hello = call({"request_type": "hello", "protocol_minor": 0})
cat = {d["catalog_id"]: d["decklist"] for d in hello["catalog"]}
decks = ["Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "CawGates", "Faeries"]
names = sorted({r["name"] for d in decks for r in cat[d]})
dom = {"domain_id": "sha256:" + hashlib.sha256(canon(names).encode()).hexdigest(), "names": names}
rng = random.Random(int(sys.argv[3]) if len(sys.argv) > 3 else 7)
hw = n = 0
for d in decks:
    did = "sha256:" + hashlib.sha256(canon(sorted([{"count": r["count"], "name": r["name"]} for r in cat[d]], key=lambda r: r["name"])).encode()).hexdigest()
    for g in range(n_per_deck):
        hseat = "p0" if g % 2 == 0 else "p1"
        gid = f"g{d}{g}"
        r = call({"request_type": "reset", "game_id": gid, "format": "pauper-bo1",
                  "seats": [{"seat": s, "deck": {"deck_id": did, "catalog_id": d}} for s in ("p0", "p1")],
                  "rules": {"opponent_decklist": "visible", "mulligan": "none", "starting_player": "host_assigned", "starting_seat": "p0",
                            "card_name_domain": dom, "extensions": [], "probe": False},
                  "game_secret": os.urandom(32).hex() if False else "%064x" % rng.getrandbits(256), "max_decisions": 10000, "max_steps": 100000})
        while r["response_type"] == "decision":
            sd = r["seat_decision"]; c = sd["candidates"]
            i = heur(c) if sd["acting_seat"] == hseat else rng.randrange(len(c))
            r = call({"request_type": "step", "game_id": gid, "expected_step": r["step"],
                      "selection": {"candidate_id": i, "semantic_echo": c[i]["semantic"]}})
        n += 1
        if r.get("winner") == hseat: hw += 1
        if r.get("classification") != "natural": print("nonnatural", r)
print(f"v2 bridge: heuristic wins {hw}/{n}")
