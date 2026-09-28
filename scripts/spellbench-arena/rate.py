"""Rate one or more arena run dirs with SpellBench's own leaderboard code.

usage: rate.py OUT.md RUN_DIR [RUN_DIR...]

Merges the ledgers (game ids are prefixed per run so they stay unique),
fits the anchored Bradley-Terry ratings with paired bootstrap exactly as
`spellbench run` does, and writes the markdown. It skips only the canonical
JSON write, which rejects a sign-test p-value whose exact denominator
exceeds 2^53 (a lopsided matchup, e.g. 120-8) -- the ratings are unaffected.
"""
import json, sys
from pathlib import Path
from spellbench.arena import leaderboard, registry, store

out = sys.argv[1]
rows, entries, cfg = [], {}, None
for i, d in enumerate(sys.argv[2:]):
    d = Path(d)
    cfg = cfg or json.loads((d / "config.json").read_text())
    for e in registry.read_registry(d / "registry.json"):
        entries[e.bot_id] = e
    for n, line in enumerate((d / "matches.jsonl").read_text().splitlines()):
        v = json.loads(line)
        if len(sys.argv) > 3:
            v["game_id"] = f"r{i}-{v['game_id']}"
            v["matchup_index"] = v["matchup_index"] + 1000 * i
        rows.append(store.LedgerRow.from_json(v, f"{d}[{n}]"))
anchor = [e for e in entries.values() if e.name == cfg.get("rating_anchor", "uniform")][0]
doc, md = leaderboard.build_leaderboard(rows, list(entries.values()), anchor_bot_id=anchor.bot_id,
    base_seed=cfg["base_seed"], bootstrap_replicates=cfg["bootstrap_replicates"], format=cfg["format"])
Path(out).write_text(md)
print("\n".join(md.splitlines()[:40]))
