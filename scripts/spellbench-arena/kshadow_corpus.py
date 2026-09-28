"""kshadow_corpus.py OUT_DIR SEED_FIRST SEED_LAST P0 P1 [DECK...]

Plays mirror games on our agent_bridge_v1 (legacy reset, env_seed = game_seed)
and records every decision of BOTH seats, raw (x_kernel_v5 included), with the
pick, one gzip JSON-lines file per game: OUT_DIR/<deck>-<seed>.jsonl.gz.
Line 1 is the header (seed, decks, bots), then one line per decision
{"step","seat","decision","pick"}, then the terminal.

Bots: heuristic | first | tac:<sbv1agent binary> (-policy tactical) |
      bin:<binary>:<policy>[:ARG,ARG...] (any sbv1agent build, policy, flags).

Match mode (no recording, parallel, seat-swapped pairs):
  kshadow_corpus.py --match OUT.jsonl WORKERS SEED_FIRST SEED_LAST A B [DECK...]
plays A as p0 and B as p1, then B as p0 and A as p1, per (seed, deck), and
appends one JSON line per game; prints A's record.
Both seats' own observations are recorded, so the opponent's hand at any
decision is recoverable from its most recent own_hand: the shadow-fidelity
check reads the hidden zones from there (the agent under test never does).
"""
import gzip, json, os, subprocess, sys

B = os.environ.get("MTG_KERNEL_BRIDGE",
                   "/mnt/sata/gorge-training/spellbench-work/arena/mtg-kernel/target/release/agent_bridge_v1")
POOL = ["Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "CawGates", "Faeries"]


def proc(cmd):
    return subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, bufsize=1)


class Wire:
    def __init__(self, p):
        self.p, self.rid = p, 0

    def send(self, m):
        self.rid += 1
        m.update(protocol="spellbench/v1", request_id=f"r{self.rid}")
        self.p.stdin.write(json.dumps(m) + "\n")
        self.p.stdin.flush()
        line = self.p.stdout.readline()
        if not line:
            raise RuntimeError("peer closed")
        return json.loads(line)


def heur(d):
    cs = d["candidates"]
    for kind in ("play_land", "cast_spell"):
        for c in cs:
            if c["semantic"]["kind"] == kind:
                return c["candidate_id"]
    for c in cs:
        if c["semantic"]["kind"] in ("activate_mana_ability", "activate_ability"):
            return c["candidate_id"]
    for c in cs:
        if c["semantic"]["kind"] == "choose_attacker_inclusion" and c["semantic"]["include"]:
            return c["candidate_id"]
    for c in cs:
        if c["semantic"]["kind"] == "choose_blocker_inclusion" and not c["semantic"]["include"]:
            return c["candidate_id"]
    return 0


def play(out_dir, seed, deck, specs, record=True, gid=None):
    gid = gid or f"{deck}-{seed}"
    path = os.path.join(out_dir, gid + ".jsonl.gz") if record else os.devnull
    eng = Wire(proc([B]))
    bots, procs = {}, []
    for seat, spec in zip(("p0", "p1"), specs):
        if spec.startswith("tac:") or spec.startswith("bin:"):
            extra = []
            if spec.startswith("tac:"):
                binary, policy = spec[4:], "tactical"
            else:
                parts = spec.split(":")
                binary, policy = parts[1], parts[2]
                if len(parts) > 3:
                    extra = parts[3].split(",")
            a = proc([binary, "-policy", policy, "-quiet"] + extra)
            procs.append(a)
            w = Wire(a)
            w.send({"request_type": "hello"})
            w.send({"request_type": "game_start", "game_id": gid, "seat": seat, "format": "pauper-bo1",
                    "decks": [{"catalog_id": deck}, {"catalog_id": deck}],
                    "engine": {"name": "mtg-kernel", "version": "1", "source_revision": None,
                               "rules_snapshot_id": "r", "card_pool_identity": "c"}})
            bots[seat] = w
        elif spec == "first":
            bots[seat] = "first"
        else:
            bots[seat] = "heuristic"
    eng.send({"request_type": "hello"})
    r = eng.send({"request_type": "reset", "game_id": gid, "format": "pauper-bo1",
                  "seats": [{"seat": "p0", "deck": {"catalog_id": deck}}, {"seat": "p1", "deck": {"catalog_id": deck}}],
                  "game_seed": seed, "max_decisions": 10000, "max_steps": 100000})
    with (gzip.open(path + ".tmp", "wt") if record else open(os.devnull, "w")) as f:
        f.write(json.dumps({"header": True, "game_id": gid, "seed": seed, "decks": [deck, deck], "bots": specs}) + "\n")
        n = 0
        while r["response_type"] == "decision":
            a = bots[r["acting_seat"]]
            if a == "first":
                pick = 0
            elif a == "heuristic":
                pick = heur(r)
            else:
                pick = a.send({"request_type": "choose", "game_id": gid, "decision": r})["selection"]["candidate_id"]
            f.write(json.dumps({"step": r["step"], "seat": r["acting_seat"], "decision": r, "pick": pick}) + "\n")
            n += 1
            r = eng.send({"request_type": "step", "game_id": gid, "expected_step": r["step"],
                          "selection": {"candidate_id": pick, "semantic_echo": r["candidates"][pick]["semantic"]}})
        f.write(json.dumps({"terminal": r}) + "\n")
    if record:
        os.rename(path + ".tmp", path)
    for w in bots.values():
        if isinstance(w, Wire):
            try:
                w.send({"request_type": "game_over", "game_id": gid, "terminal": {
                    "outcome": r.get("outcome"), "classification": r.get("classification"), "winner": r.get("winner"),
                    "reason": r.get("reason", ""), "step_count": r.get("step_count", 0),
                    "decision_count": r.get("decision_count", 0)}})
            except Exception:
                pass
            w.p.stdin.close()
    eng.p.stdin.close()
    for p in procs + [eng.p]:
        p.wait()
    return r.get("outcome"), r.get("winner"), n


def _match_one(args):
    seed, deck, a, b, swap = args
    specs = [b, a] if swap else [a, b]
    gid = f"{deck}-{seed}-{'ba' if swap else 'ab'}"
    try:
        outcome, winner, n = play(None, seed, deck, specs, record=False, gid=gid)
    except Exception as e:  # a crashed game is reported, never dropped
        return {"game": gid, "seed": seed, "deck": deck, "swap": swap, "error": str(e)}
    a_seat = "p1" if swap else "p0"
    return {"game": gid, "seed": seed, "deck": deck, "swap": swap, "outcome": outcome, "winner": winner,
            "a_won": winner == a_seat, "decisions": n}


def match():
    import multiprocessing
    out, workers, s0, s1, a, b = sys.argv[2], int(sys.argv[3]), int(sys.argv[4]), int(sys.argv[5]), sys.argv[6], sys.argv[7]
    decks = sys.argv[8:] or POOL
    jobs = [(seed, deck, a, b, swap) for seed in range(s0, s1 + 1) for deck in decks for swap in (False, True)]
    w = l = other = 0
    with multiprocessing.Pool(workers) as pool, open(out, "a") as f:
        for r in pool.imap_unordered(_match_one, jobs):
            f.write(json.dumps(r) + "\n")
            f.flush()
            if r.get("error") or r.get("winner") is None:
                other += 1
            elif r["a_won"]:
                w += 1
            else:
                l += 1
    n = w + l
    p = w / n if n else 0
    se = (p * (1 - p) / n) ** 0.5 if n else 0
    print(f"A {a}\nB {b}\nA won {w}-{l} ({100*p:.1f}% +- {196*se:.1f}), other {other}")


def main():
    if sys.argv[1] == "--match":
        return match()
    out, s0, s1, p0, p1 = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4], sys.argv[5]
    decks = sys.argv[6:] or POOL
    os.makedirs(out, exist_ok=True)
    for seed in range(s0, s1 + 1):
        for deck in decks:
            outcome, winner, n = play(out, seed, deck, [p0, p1])
            print(deck, seed, outcome, winner, n, flush=True)


if __name__ == "__main__":
    main()
