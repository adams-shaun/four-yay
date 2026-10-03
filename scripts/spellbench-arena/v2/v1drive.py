"""Drive the old v1 bridge directly: v1 heuristic vs v1-style uniform, mirror decks."""
import json, subprocess, sys, random
B = sys.argv[1]
n_per_deck = int(sys.argv[2]) if len(sys.argv) > 2 else 8
p = subprocess.Popen([B, "--no-x-kernel-v5"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
rid = 0
def call(m):
    global rid
    rid += 1; m["request_id"] = f"h-{rid}"; m["protocol"] = "spellbench/v1"
    p.stdin.write(json.dumps(m) + "\n"); p.stdin.flush()
    return json.loads(p.stdout.readline())
def heur(c):
    for k in ("play_land", "cast_spell"):
        for x in c:
            if x["semantic"]["kind"] == k: return x["candidate_id"]
    for x in c:
        if x["semantic"]["kind"] in ("activate_mana_ability", "activate_ability"): return x["candidate_id"]
    for x in c:
        s = x["semantic"]
        if s["kind"] == "choose_attacker_inclusion" and s["include"] is True: return x["candidate_id"]
    for x in c:
        s = x["semantic"]
        if s["kind"] == "choose_blocker_inclusion" and s["include"] is False: return x["candidate_id"]
    return 0
call({"request_type": "hello"})
decks = ["Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "CawGates", "Faeries"]
rng = random.Random(7)
hw = n = 0
for d in decks:
    for g in range(n_per_deck):
        hseat = "p0" if g % 2 == 0 else "p1"
        r = call({"request_type": "reset", "game_id": f"g{d}{g}", "format": "pauper-bo1",
                  "seats": [{"seat": "p0", "deck": {"catalog_id": d}}, {"seat": "p1", "deck": {"catalog_id": d}}],
                  "game_seed": rng.randrange(1 << 50), "max_decisions": 5000, "max_steps": 100000})
        while r["response_type"] == "decision":
            c = r["candidates"]
            i = heur(c) if r["acting_seat"] == hseat else rng.randrange(len(c))
            r = call({"request_type": "step", "game_id": f"g{d}{g}", "expected_step": r["step"],
                      "selection": {"candidate_id": i, "semantic_echo": c[i]["semantic"]}})
        n += 1
        if r.get("winner") == hseat: hw += 1
        if r.get("classification") != "natural": print("nonnatural", r.get("reason"))
print(f"v1 bridge: heuristic wins {hw}/{n}")
