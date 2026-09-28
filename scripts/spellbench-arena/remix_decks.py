"""Add the remix decks (the sb-generic unseen-card test) to a PRIVATE mtg-kernel clone.

usage: remix_decks.py KERNEL_CLONE

The kernel compiles its deck catalog from data/runtime_decks_v1.json against a
frozen table in mtg-kernel/build.rs, and agent_bridge_v1 accepts a fixed list of
catalog ids. This appends the decks below to all three (FNV-1a deck hashes
computed, checked against a shipped deck first); commit the result in the clone
(build.rs derives source identity from git) and build agent_bridge_v1 there.
Remix* decks were the development unseen set, Remix2* the held-out one. Every
card is a kernel card-DB row; most nonland cards are sideboard-only or
Terror-only, which hints.go never saw."""
import json, re, hashlib, sys
R = sys.argv[1]  # a PRIVATE git clone of the kernel branch the arena builds (never the arena checkout)
DECKS = {
 "RemixRed": {"Mountain": 20, "Gorilla Shaman": 4, "Burning-Tree Emissary": 4, "Voldaren Epicure": 4, "Guttersnipe": 4,
   "Clockwork Percussionist": 4, "Gut Shot": 4, "Searing Blaze": 4, "Cast into the Fire": 4, "Breath Weapon": 2,
   "Red Elemental Blast": 2, "Pyroblast": 2, "Masked Meower": 2},
 "RemixGreen": {"Forest": 14, "Snow-Covered Forest": 4, "Gingerbread Cabin": 4, "Healer of the Glade": 4, "Vitu-Ghazi Inspector": 4,
   "Troublemaker Ouphe": 4, "Spinewoods Paladin": 4, "Monstrous Emergence": 4, "Weather the Storm": 2, "Llanowar Elves": 4,
   "Generous Ent": 3, "Sagu Wildling": 3, "Masked Vandal": 2, "Avenging Hunter": 4},
 "RemixUB": {"Island": 12, "Swamp": 6, "Duress": 4, "Extract a Confession": 2, "Fume Spitter": 4, "Deep Analysis": 2,
   "Thought Scour": 4, "Ponder": 4, "Piracy Charm": 2, "Hydroblast": 2, "Envelop": 3, "Deem Inferior": 3,
   "Cryptic Serpent": 3, "Tolarian Terror": 4, "Murmuring Mystic": 2, "Faerie Macabre": 1, "Counterspell": 2},
 "Remix2Izzet": {"Island": 8, "Mountain": 8, "Great Furnace": 2, "Gut Shot": 4, "Searing Blaze": 2, "Pyroblast": 2,
   "Hydroblast": 2, "Steel Sabotage": 2, "Annul": 2, "Piracy Charm": 3, "Ponder": 4, "Thought Scour": 4, "Mental Note": 2,
   "Murmuring Mystic": 4, "Tolarian Terror": 4, "Gorilla Shaman": 2, "Guttersnipe": 4, "Cast into the Fire": 1},
 "Remix2Golgari": {"Swamp": 8, "Forest": 9, "Gingerbread Cabin": 1, "Vitu-Ghazi Inspector": 4, "Healer of the Glade": 2,
   "Troublemaker Ouphe": 4, "Fume Spitter": 2, "Spinewoods Paladin": 4, "Unexpected Fangs": 2, "Monstrous Emergence": 3,
   "Extract a Confession": 3, "Duress": 2, "Faerie Macabre": 2, "Relic of Progenitus": 2, "Weather the Storm": 2,
   "Mesmeric Fiend": 4, "Sagu Wildling": 2, "Masked Vandal": 4},
 "Remix2MonoU": {"Island": 18, "Annul": 2, "Envelop": 2, "Hydroblast": 2, "Blue Elemental Blast": 2, "Steel Sabotage": 2,
   "Deem Inferior": 3, "Deep Analysis": 3, "Mental Note": 3, "Ponder": 4, "Piracy Charm": 2, "Cryptic Serpent": 4,
   "Tolarian Terror": 4, "Murmuring Mystic": 4, "Counterspell": 3, "Brainstorm": 2},
 "RemixBG": {"Forest": 10, "Swamp": 8, "Fume Spitter": 4, "Healer of the Glade": 4, "Vitu-Ghazi Inspector": 4, "Troublemaker Ouphe": 4,
   "Unexpected Fangs": 4, "Monstrous Emergence": 4, "Duress": 4, "Extract a Confession": 2, "Cast Down": 4, "Masked Vandal": 4,
   "Sagu Wildling": 2, "Spinewoods Paladin": 2},
}
def fnv1a64(b):
    h = 0xcbf29ce484222325
    for x in b: h = ((h ^ x) * 0x100000001b3) & 0xFFFFFFFFFFFFFFFF
    return h
cards = json.load(open(f"{R}/data/cards_v1.json"))["cards"]
idx = {c["name"]: i for i, c in enumerate(cards)}
cat = json.load(open(f"{R}/data/runtime_decks_v1.json"))
# self-check the hash on a shipped deck
d0 = cat["decks"][0]
ids0 = [m["card_id"] for m in d0["materialized_mainboard"]]
assert "0x%016x" % fnv1a64(json.dumps(ids0, separators=(",", ":")).encode()) == d0["runtime_deck_hash"], "hash contract"
cat["decks"] = [d for d in cat["decks"] if not d["id"].startswith("Remix")]
rows_rs = []
ORDER = ["RemixRed", "RemixGreen", "RemixUB", "RemixBG", "Remix2Izzet", "Remix2Golgari", "Remix2MonoU"]
for n, did in enumerate(ORDER):
    deck = DECKS[did]
    assert sum(deck.values()) == 60, (did, sum(deck.values()))
    mat = []
    for r, (name, cnt) in enumerate(deck.items(), 1):
        for c in range(1, cnt + 1):
            mat.append({"source_row_ordinal": r, "copy_ordinal": c, "name": name, "card_id": idx[name]})
    ids = [m["card_id"] for m in mat]
    h = fnv1a64(json.dumps(ids, separators=(",", ":")).encode())
    path = f"generic/remix/{did}.dek"
    sha = hashlib.sha256(json.dumps(deck).encode()).hexdigest()
    order = 10 + n
    cat["decks"].append({"canonical_pool_order": order, "id": did, "source_path": path, "source_sha256": sha,
        "mainboard_copy_count": 60, "unique_mainboard_cards": len(deck), "runtime_deck_hash": "0x%016x" % h,
        "materialized_mainboard": mat})
    rows_rs.append(f'        (\n            "{did}",\n            {order},\n            "{path}",\n            "{sha}",\n            {len(deck)},\n            0x{h:016x},\n        ),\n')
json.dump(cat, open(f"{R}/data/runtime_decks_v1.json", "w"), indent=2)
b = open(f"{R}/mtg-kernel/build.rs").read()
b = re.sub(r'const EXPECTED_DECKS: \[\(&str, u32, &str, &str, usize, u64\); \d+\] = \[', f'const EXPECTED_DECKS: [(&str, u32, &str, &str, usize, u64); {9+len(DECKS)}] = [', b)
b = re.sub(r'        \(\n            "Remix[^)]*\),\n', '', b)
anchor = '            0xd7a47ab2fa78dbaa,\n        ),\n'
assert anchor in b
b = b.replace(anchor, anchor + "".join(rows_rs))
open(f"{R}/mtg-kernel/build.rs", "w").write(b)
s = open(f"{R}/mtg-kernel/src/bin/agent_bridge_v1.rs").read()
s = re.sub(r'const DECKS: \[&str; \d+\] = \[\n    "Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "Terror", "CawGates", "Faeries",[^\]]*\];',
  'const DECKS: [&str; %d] = [\n    "Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "Terror", "CawGates", "Faeries",\n    %s\n];' % (9+len(DECKS), ", ".join(f'"{d}"' for d in ORDER)), s)
open(f"{R}/mtg-kernel/src/bin/agent_bridge_v1.rs", "w").write(s)
print("ok", list(DECKS))
