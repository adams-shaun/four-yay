#!/usr/bin/env python3
"""Rate a gorge SpellBench workup with SpellBench's own leaderboard code.

cmd/botbench -spellbench writes a SpellBench match ledger (matches.jsonl,
schema spellbench-match-ledger/v1). This script feeds one or more such
ledgers (e.g. a cheap run and a separately played az run of the same round
robin) to spellbench.arena.leaderboard.build_leaderboard -- the anchored
Bradley-Terry MM fit, draws half, one virtual draw per matchup, paired
bootstrap CI over seat-swapped pairs, sign tests and per-deck slices -- and
writes leaderboard.json and LEADERBOARD.md.

It needs the spellbench Python package importable (run it with the venv the
package is installed in):

    /mnt/sata/gorge-training/sbvenv/bin/python scripts/spellbench-rate.py \\
        --anchor sb-uniform --out OUTDIR RUNDIR [RUNDIR...]

Ledger rows are validated by SpellBench's own parser (store.parse_ledger);
truncated and halted rows are kept in the ledger and excluded from the fit,
exactly as SpellBench excludes them.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from spellbench.arena import leaderboard, registry, store

# Tag names mirror the benchmark's training_style_tags vocabulary.
_TAGS = {
    "sb-uniform": ("baseline",),
    "sb-first": ("baseline",),
    "sb-uniform-manual": ("baseline",),
    "sb-heuristic": ("heuristic",),
    "sb-heuristic-manual": ("heuristic",),
    "sb-uniform-planned": ("baseline",),
    "sb-heuristic-planned": ("heuristic",),
    "sb-tactical": ("heuristic",),
    "sb-tactical-planned": ("heuristic",),
    "sb-tactical-noearly": ("heuristic",),
    "sb-tactical-notiming": ("heuristic",),
    "sb-tactical-norace": ("heuristic",),
    "sb-tactical-alt": ("heuristic",),
    "bot": ("heuristic",),
}


def _tags(name: str) -> tuple[str, ...]:
    if name.startswith("sb-tactical"):
        return ("heuristic",)
    if name.startswith("az-"):
        return ("search",)
    return _TAGS.get(name, ())


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("rundirs", nargs="+", type=Path, help="directories holding matches.jsonl")
    ap.add_argument("--anchor", default="sb-uniform", help="ledger name of the anchor bot (Elo 1000)")
    ap.add_argument("--base-seed", type=int, default=20260926, help="statistics base seed (the tournament's)")
    ap.add_argument("--replicates", type=int, default=2000, help="bootstrap replicates (the benchmark's 2000)")
    ap.add_argument("--format", default="pauper-bo1")
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--bots", default="", help="comma list of ledger names: keep only games where both seats are listed")
    ap.add_argument("--max-pair", type=int, default=-1,
                    help="keep only pair_index <= this (a pairs=8 run's pairs 0..31 are exactly the pairs=4 run)")
    args = ap.parse_args(argv)

    raw: dict[str, dict] = {}
    for d in args.rundirs:
        for line in (d / "matches.jsonl").read_text(encoding="utf-8").splitlines():
            if not line.strip():
                continue
            row = json.loads(line)
            prev = raw.get(row["game_id"])
            if prev is not None and prev != row:
                print(f"conflicting rows for {row['game_id']} across run dirs", file=sys.stderr)
                return 1
            raw[row["game_id"]] = row
    keep = set(filter(None, args.bots.split(",")))
    selected = []
    for k in sorted(raw):
        row = raw[k]
        if keep and not all(seat["name"] in keep for seat in row["seats"]):
            continue
        if args.max_pair >= 0 and row["pair_index"] > args.max_pair:
            continue
        selected.append(row)
    rows = store.parse_ledger(selected)

    seen: dict[str, registry.RegistryEntry] = {}
    for row in rows:
        for s in row.seats:
            if s.bot_id not in seen:
                seen[s.bot_id] = registry.RegistryEntry(
                    bot_id=s.bot_id, name=s.name, version=s.version, engine="gorge",
                    training_style_tags=_tags(s.name), owner="gorge",
                )
    entries = sorted(seen.values(), key=lambda e: e.name)
    anchor = [e.bot_id for e in entries if e.name == args.anchor]
    if not anchor:
        print(f"anchor {args.anchor!r} is not in the ledger (bots: {[e.name for e in entries]})", file=sys.stderr)
        return 1

    document, markdown = leaderboard.build_leaderboard(
        rows, entries, anchor_bot_id=anchor[0], base_seed=args.base_seed,
        bootstrap_replicates=args.replicates, format=args.format,
    )
    args.out.mkdir(parents=True, exist_ok=True)
    (args.out / "leaderboard.json").write_text(json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    (args.out / "LEADERBOARD.md").write_text(markdown, encoding="utf-8")
    (args.out / "matches.jsonl").write_text(
        "".join(json.dumps(r.to_json(), sort_keys=True) + "\n" for r in rows), encoding="utf-8")
    sys.stdout.write(markdown)
    return 0


if __name__ == "__main__":
    sys.exit(main())
