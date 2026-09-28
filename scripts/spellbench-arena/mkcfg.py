"""Write a SpellBench v1 tournament config shaped like the pauper-kernel benchmark.

usage: mkcfg.py OUT.json RUN_DIR PAIRS_PER_DECK BASE_SEED BOT...

BOT is one of
  builtin:NAME                     a python builtin (uniform gets seed 11, like the benchmark)
  go:NAME:POLICY[:ARG,ARG...]      sbv1agent -policy POLICY (binary: $SBV1AGENT)
  gobin:NAME:BINARY:POLICY[:ARGS]  a specific sbv1agent build (version A/B runs)

The engine is $MTG_KERNEL_BRIDGE (our agent_bridge_v1 rebuild). The deck pool,
seat-swapped mirror pairs, caps, timeouts and bootstrap match
spellbench/benchmarks/pauper-kernel/benchmark.json; the rating anchor is the
builtin uniform bot.
"""
import json, os, sys

out, run, pairs, seed = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
work = "/mnt/sata/gorge-training/spellbench-work/arena"
S = os.environ.get("SBV1AGENT", f"{work}/bin/sbv1agent")
B = os.environ.get("MTG_KERNEL_BRIDGE", f"{work}/mtg-kernel/target/release/agent_bridge_v1")
pool = ["Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "CawGates", "Faeries"]
bots = []
for spec in sys.argv[5:]:
    parts = spec.split(":")
    if parts[0] == "builtin":
        b = {"name": parts[1], "version": "1.0.0", "type": "builtin", "training_style_tags": ["baseline"]}
        if parts[1] == "uniform":
            b["seed"] = 11
    else:
        if parts[0] == "gobin":
            name, binary, policy, rest = parts[1], parts[2], parts[3], parts[4:]
        else:
            name, binary, policy, rest = parts[1], S, parts[2], parts[3:]
        extra = rest[0].split(",") if rest else []
        b = {"name": name, "version": "0.1.0", "type": "subprocess", "engine": "mtg-kernel", "owner": "gorge",
             "training_style_tags": ["heuristic"],
             "command": [binary, "-policy", policy, "-name", name, "-version", "0.1.0"] + extra}
    bots.append(b)
cfg = {"schema": "spellbench-tournament-config/v1", "tournament_dir": run, "format": "pauper-bo1",
       "deck_pool": [{"catalog_id": d} for d in pool], "engine": {"command": [B], "timeout_ms": 120000},
       "bots": bots, "pairs_per_matchup": pairs * len(pool), "base_seed": seed, "include_self_play": False,
       "rating_anchor": "uniform", "bootstrap_replicates": 2000, "choose_timeout_ms": 30000,
       "startup_timeout_ms": 120000, "max_decisions": 10000, "max_steps": 100000, "workers": 8}
json.dump(cfg, open(out, "w"), indent=1)
