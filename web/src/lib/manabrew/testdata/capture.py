#!/usr/bin/env python3
"""Plays a vs-bot game on a -manabrew gorged through the NATIVE intent route
and, at every decision for the human seat, captures the native view+decision
and the ManaBrew /state response for the same instant. Output: a JSON list of
{"native": {"view", "decision"}, "manabrew": [state, prompt?]} records."""
import json, sys, time, urllib.request, urllib.error

BASE = sys.argv[1]
OUT = sys.argv[2]
HUMAN = sys.argv[3] if len(sys.argv) > 3 else "mono-red-prowess"
BOT = sys.argv[4] if len(sys.argv) > 4 else "mono-green-stompy"
MAX_TURN = int(sys.argv[5]) if len(sys.argv) > 5 else 9


def req(method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(BASE + path, data=data, method=method)
    if data is not None:
        r.add_header("Content-Type", "application/json")
    if token:
        r.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(r, timeout=20) as resp:
            raw = resp.read()
            return resp.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw.decode()


st, g = req("POST", "/api/games", {"format": "constructed", "human_deck": HUMAN, "bot_deck": BOT, "mulligans": 1})
assert st == 200, (st, g)
t, k, tok = g["table"], g["match"], g["token"]
q = f"?seat=0&token={tok}"
records = []
seen_kinds = {}
last_seq = None
repeat = 0
while True:
    st, d = req("GET", f"/api/tables/{t}/matches/{k}/pending{q}")
    st2, v = req("GET", f"/api/tables/{t}/matches/{k}/view{q}")
    if st2 == 200 and v.get("over"):
        _, mb = req("GET", f"/api/manabrew/v0/tables/{t}/matches/{k}/state?token={tok}")
        records.append({"native": {"view": v, "decision": None}, "manabrew": mb})
        break
    if st != 200:
        time.sleep(0.15)
        continue
    if st2 != 200 or v.get("turn", 0) > MAX_TURN:
        break
    _, mb = req("GET", f"/api/manabrew/v0/tables/{t}/matches/{k}/state?token={tok}")
    records.append({"native": {"view": v, "decision": d}, "manabrew": mb})
    kind = d["kind"]
    opts = d["options"]
    choices = []
    if kind == "starting_player":
        choices = [0]
    elif kind == "mulligan":
        keep = [o for o in opts if o["kind"] == "keep"]
        choices = [keep[0]["index"]] if keep else [o["index"] for o in opts[: d["min"]]]
    elif kind == "priority":
        by = lambda kk: [o for o in opts if o["kind"] == kk]
        pick = None
        if d["seq"] != last_seq:
            for kk in ("play_land", "cast"):
                if by(kk):
                    pick = by(kk)[0]
                    break
            if pick is None and d.get("payment_actions") and by("activate") and v["step"] in ("main1", "main2"):
                pick = by("activate")[0]
        if pick is None:
            pick = by("pass")[0]
        choices = [pick["index"]]
    elif kind == "target":
        opp = [o for o in opts if o.get("player") == 1 and o["kind"] == "player"] or opts
        choices = [opp[0]["index"]]
    elif kind == "attackers":
        seen = set()
        for o in opts:
            if o["obj"] not in seen and not o.get("battle"):
                seen.add(o["obj"])
                choices.append(o["index"])
    elif kind == "blockers":
        choices = [opts[0]["index"]] if opts else []
    else:
        n = max(d["min"], 1 if d["max"] > 0 and opts else 0)
        choices = [o["index"] for o in opts[:n]]
    last_seq = d["seq"]
    st, body = req("POST", f"/api/tables/{t}/matches/{k}/intent", {"seq": d["seq"], "player": 0, "choices": choices}, tok)
    if st >= 300:
        # fall back to passing / the minimum
        print("rejected", kind, choices, body, file=sys.stderr)
        alt = [o["index"] for o in opts if o["kind"] == "pass"][:1] or [o["index"] for o in opts[: d["min"]]]
        req("POST", f"/api/tables/{t}/matches/{k}/intent", {"seq": d["seq"], "player": 0, "choices": alt}, tok)
json.dump(records, open(OUT, "w"))
print(len(records), "records", sorted({r["native"]["decision"]["kind"] for r in records if r["native"]["decision"]}))
