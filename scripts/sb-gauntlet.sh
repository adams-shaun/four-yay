#!/usr/bin/env bash
# sb-gauntlet.sh — rate candidate SpellBench policy specs against a fixed
# reference set.
#
# usage: scripts/sb-gauntlet.sh <spec>[,<spec>...] [pairs] [decks] [catalog]
#
#   specs   comma list of candidate policy specs, each a registry spec
#           (internal/spellbench/registry): "bot", "sb-heuristic",
#           "bot+passguard", "bot+dec1+dec2", ... Every candidate plays
#           every reference; a candidate that IS a reference is skipped
#           with a note.
#   pairs   seat-swapped pairs per deck per matchup (default 4, the
#           benchmark's pairs_per_deck).
#   decks   comma list of catalog deck ids, case-insensitive against the
#           benchmark pool (default: the benchmark's 8-deck pool). Example:
#           `scripts/sb-gauntlet.sh bot+passguard 1 affinity,elves`.
#   catalog the SpellBench deck catalog to play (default pauper-kernel;
#           SB_GAUNTLET_CATALOG overrides). Outside pauper-kernel the decks
#           argument is passed to botbench case-insensitively but is NOT
#           normalized against the pauper pool -- deck ids are validated by
#           the catalog itself. Example:
#           `scripts/sb-gauntlet.sh bot 2 death-n-taxes,uw-tempo repo-constructed`.
#
# The references are `sb-uniform` (the Elo anchor), `sb-heuristic` and
# `bot`, plus every spec listed in
# /mnt/sata/gorge-training/spellbench-work/gauntlet/champions.txt when that
# file exists (one spec per line, # comments allowed).
#
# Schedule. Candidates play the references through `botbench -spellbench
# <all specs> -spellbench-with <candidate>`, which keeps the full round
# robin's seeds and indices (the -with filter only drops the matchups that
# do not include the candidate). Reference-vs-reference games are played
# once, among the references alone, and cached under
# /mnt/sata/gorge-training/spellbench-work/gauntlet/ref/<key>/ where <key>
# is the SHA-256 of the `git rev-parse HEAD:<path>` tree hashes of
# rules effects cards decision botpolicy internal/spellbench cmd/botbench
# plus the pairs and decks values -- so any engine or policy change
# invalidates the cache and replays the references.
#
# Rating. Every relevant ledger -- the cached reference games (their
# game_ids rewritten with a `ref-` prefix so the two schedules cannot
# collide) and every candidate's -with run -- is rated together with
# scripts/spellbench-rate.py, run with /mnt/sata/gorge-training/sbvenv/bin
# (the SpellBench package's anchored Bradley-Terry fit, anchor sb-uniform,
# the benchmark's 2000 bootstrap replicates).
#
# Output. A table per candidate: spec, Elo, CI95, W-L (rated games) and
# head-to-head W-L vs each reference. One JSON row per candidate is
# appended to /mnt/sata/gorge-training/spellbench-work/gauntlet/results.jsonl
# with the fields spec, elo, ci_lo, ci_hi, wins, losses, pairs, decks,
# git_head, key, ts.
#
# Exit code. 0 always, unless the run fails.
#
# Work root. /mnt/sata/gorge-training/spellbench-work/gauntlet by default;
# SB_GAUNTLET_DIR overrides it (cache, results.jsonl, champions.txt) -- e.g.
# where the mount is not writable.
#
# Resources. Every botbench invocation is wrapped in
#   flock -o /mnt/sata/gorge-training/spellbench-work/heavy.lock \
#     systemd-run --user --scope -q -p MemoryMax=4G \
#     env GOMEMLIMIT=2GiB GOMAXPROCS=8 <botbench> ... -workers $WORKERS
# with WORKERS defaulting to 8; SB_GAUNTLET_WORKERS overrides it (e.g.
# SB_GAUNTLET_WORKERS=2 for a smoke-sized run).

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
# SB_GAUNTLET_DIR overrides the gauntlet work root (cache, results.jsonl,
# champions.txt); the default is the shared training mount.
GDIR=${SB_GAUNTLET_DIR:-/mnt/sata/gorge-training/spellbench-work/gauntlet}
LOCK=/mnt/sata/gorge-training/spellbench-work/heavy.lock
SBPY=${SB_GAUNTLET_SBPY:-/mnt/sata/gorge-training/sbvenv/bin}
WORKERS=${SB_GAUNTLET_WORKERS:-8}
POOL="Wildfire Rally Affinity Elves Spy Burn CawGates Faeries"
CATALOG=${SB_GAUNTLET_CATALOG:-${4:-pauper-kernel}}

CANDS_RAW=${1:?usage: scripts/sb-gauntlet.sh <spec>[,<spec>...] [pairs] [decks] [catalog]}
PAIRS=${2:-4}
DECKS_RAW=${3:-}

WORK=$(mktemp -d /tmp/sb-gauntlet.XXXXXX)
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$GDIR"

# heavy wraps one botbench invocation in the fleet's resource gate.
heavy() {
	flock -o "$LOCK" systemd-run --user --scope -q -p MemoryMax=4G \
		env GOMEMLIMIT=2GiB GOMAXPROCS=8 "$@"
}

# normalize_decks maps each comma token case-insensitively onto the
# benchmark pool, so `affinity,elves` means `Affinity,Elves` -- but only on
# the pauper-kernel catalog, whose ids ARE the pool's. On another catalog
# the token passes through verbatim (still unspaced, still nonempty) and
# botbench validates it against that catalog's deck directory.
normalize_decks() {
	local out="" tok match p
	local -a toks
	IFS=',' read -ra toks <<<"$1"
	for tok in "${toks[@]}"; do
		tok="${tok//[[:space:]]/}"
		[ -z "$tok" ] && continue
		if [ "$CATALOG" = "pauper-kernel" ]; then
			match=""
			for p in $POOL; do
				if [ "${p,,}" = "${tok,,}" ]; then match="$p"; fi
			done
			# No pool match: pass the token through verbatim and let botbench
			# validate it against the catalog (the pool list is not the whole
			# catalog).
			[ -z "$match" ] && match="$tok"
			tok="$match"
		fi
		out+="${out:+,}$tok"
	done
	printf '%s' "$out"
}

DECKS=""
if [ -n "$DECKS_RAW" ]; then
	DECKS=$(normalize_decks "$DECKS_RAW")
fi

# The reference set: the three pins plus the champions file.
REFS=(sb-uniform sb-heuristic bot)
declare -A REFSET=()
for r in "${REFS[@]}"; do REFSET[$r]=1; done
if [ -f "$GDIR/champions.txt" ]; then
	CHAMPKEY=$(sha256sum "$GDIR/champions.txt" | cut -c1-16)
	while IFS= read -r line; do
		line="${line%%#*}"
		line="${line//[[:space:]]/}"
		[ -z "$line" ] && continue
		if [ -z "${REFSET[$line]+x}" ]; then
			REFS+=("$line")
			REFSET[$line]=1
		fi
	done <"$GDIR/champions.txt"
fi

# The candidates, as given.
mapfile -t CANDS < <(tr ',' '\n' <<<"$CANDS_RAW" | sed 's/[[:space:]]//g' | grep -v '^$')

# The bot list every run schedules over: references first, then candidates,
# deduplicated in order.
declare -A SEEN=()
declare -A PLAYED=()
BOTS=()
for b in "${REFS[@]}" "${CANDS[@]}"; do
	if [ -n "${SEEN[$b]+x}" ]; then continue; fi
	SEEN[$b]=1
	BOTS+=("$b")
done
BOTLIST=$(IFS=,; echo "${BOTS[*]}")
REFLIST=$(IFS=,; echo "${REFS[*]}")

# Build the binary the runs share (one build, many games).
(cd "$ROOT" && go build -o "$WORK/botbench" ./cmd/botbench)

# The cache key: HEAD tree hashes of everything that can change an outcome,
# plus the shape of the schedule. The champions file's CONTENT is part of
# the key too: with a ref cache already on disk, a new champion line must
# get its own ref-vs-ref games, not ride a cache built without it.
keysrc=""
for p in rules effects cards decision botpolicy internal/spellbench cmd/botbench; do
	keysrc+="$(git -C "$ROOT" rev-parse "HEAD:$p")
"
done
keysrc+="${CHAMPKEY-}
"
KEY=$(printf '%s|%s|%s|%s' "$keysrc" "$PAIRS" "$DECKS" "$CATALOG" | sha256sum | cut -c1-16)
CACHE="$GDIR/ref/$KEY"

run_bench() { # run_bench <botlist> <out> [extra -spellbench-* filters...]
	local list=$1 out=$2
	shift 2
	local -a cmd=("$WORK/botbench" -spellbench "$list" -spellbench-catalog "$CATALOG" -spellbench-pairs "$PAIRS")
	if [ -n "$DECKS" ]; then cmd+=(-spellbench-decks "$DECKS"); fi
	cmd+=(-spellbench-out "$out" -workers "$WORKERS" "$@")
	heavy "${cmd[@]}"
}

# Reference-vs-reference games: played once, among the references alone,
# cached until the engine, the policies, the pairs or the decks move.
if [ -f "$CACHE/matches.jsonl" ] && [ -f "$CACHE/games.jsonl" ]; then
	echo "sb-gauntlet: reference cache hit $KEY"
else
	echo "sb-gauntlet: playing the reference set ($REFLIST)"
	run_bench "$REFLIST" "$CACHE"
fi

# Rating input: the cached reference games under ref-prefixed game ids (the
# references-only schedule numbers its matchups differently from the full
# one; the prefix keeps game_ids unique across the merged ledgers).
mkdir -p "$WORK/ratedref"
sed 's/"game_id":"m/"game_id":"ref-m/g' "$CACHE/matches.jsonl" >"$WORK/ratedref/matches.jsonl"

# Each candidate plays the matchups that include it, full-schedule seeds and
# indices kept. The candidate's own ledger is written UNDER the gauntlet root
# (not the scratch dir) so it survives the EXIT trap: cmd/traindash walks the
# root for any matches.jsonl, so a kept candidate dir renders a per-deck chart
# with no schema change and no new games. <git_head> keeps two heads' runs from
# overwriting one another; the spec segment is sanitised in case it holds a
# slash.
git_head=$(git -C "$ROOT" rev-parse HEAD)
RATEDIRS=("$WORK/ratedref")
PLAYED=()
for cand in "${CANDS[@]}"; do
	if [ -n "${REFSET[$cand]+x}" ]; then
		echo "sb-gauntlet: $cand is a reference; skipped"
		continue
	fi
	if [ -n "${PLAYED[$cand]+x}" ]; then continue; fi
	PLAYED[$cand]=1
	out="$GDIR/cand/${git_head:-unknown}/${cand//\//_}"
	mkdir -p "$out"
	echo "sb-gauntlet: playing $cand vs the references"
	run_bench "$BOTLIST" "$out" -spellbench-with "$cand"
	RATEDIRS+=("$out")
done
if [ "${#PLAYED[@]}" -eq 0 ]; then
	echo "sb-gauntlet: no candidate left to play (every spec is a reference)" >&2
	exit 1
fi

# Rate everything together with SpellBench's own leaderboard code.
"$SBPY/python3" "$ROOT/scripts/spellbench-rate.py" \
	--anchor sb-uniform --out "$WORK/rating" "${RATEDIRS[@]}"

# The table and the results rows, from the leaderboard document and the
# candidates' own games.
ts=$(date -u +%FT%TZ)
"$SBPY/python3" - "$WORK/rating" "$GDIR/results.jsonl" "$PAIRS" "${DECKS:-default-pool}" "$CATALOG" "$git_head" "$KEY" "$ts" "$REFLIST" "${CANDS[@]}" <<'PY'
import json
import sys
from pathlib import Path

rating, results_path, pairs, decks, catalog, git_head, key, ts, refs_arg = sys.argv[1:10]
cands = sys.argv[10:]
# The row's label names the catalog's pool. Only the default-pool fallback is
# ambiguous across catalogs, so it carries the raw catalog id as a suffix; an
# explicit decks argument already names real deck ids. "pauper-kernel" stays
# bare so historical rows keep reading exactly "default-pool".
label = decks
if decks == "default-pool" and catalog != "pauper-kernel":
    label = f"default-pool:{catalog}"
refs = set(refs_arg.split(","))
cand_root = Path(results_path).parent / "cand" / git_head
doc = json.loads((Path(rating) / "leaderboard.json").read_text())
rows = {r["name"]: r for r in doc["rows"]}


def fmt(milli, digits=1):
    if milli is None:
        return "-"
    return f"{milli / 1000:.{digits}f}"


# Head-to-head W-L per candidate, from the candidate's own games (natural
# outcomes only; draws, truncations and halts are excluded from both sides).
h2h = {}
for d in sorted(cand_root.glob("*/games.jsonl")):
    for line in d.read_text().splitlines():
        if not line.strip():
            continue
        g = json.loads(line)
        p0, p1, res = g["p0"], g["p1"], g["result"]
        win = res == "p0 wins" or res == "p1 wins"
        if not win:
            continue
        winner = p0 if res == "p0 wins" else p1
        loser = p1 if res == "p0 wins" else p0
        for cand in cands:
            if cand == winner:
                h2h.setdefault(cand, {}).setdefault(loser, [0, 0])[0] += 1
            elif cand == loser:
                h2h.setdefault(cand, {}).setdefault(winner, [0, 0])[1] += 1

print()
print(f"{'spec':<32} {'Elo':>7} {'CI95':>19} {'W-L':>9}  head-to-head vs the references")
for cand in cands:
    r = rows.get(cand, {})
    ci = r.get("ci95_elo_milli") or [None, None]
    wl = f"{r.get('wins', 0)}-{r.get('losses', 0)}"
    vs = "  ".join(
        f"{opp} {w}-{l}"
        for opp, (w, l) in sorted(h2h.get(cand, {}).items())
        if opp in refs
    )
    print(
        f"{cand:<32} {fmt(r.get('elo_milli')):>7} "
        f"[{fmt(ci[0])},{fmt(ci[1])}]     {wl:>9}  {vs}"
    )

with Path(results_path).open("a") as f:
    for cand in cands:
        r = rows.get(cand, {})
        ci = r.get("ci95_elo_milli") or [None, None]
        f.write(json.dumps({
            "spec": cand,
            "elo": None if r.get("elo_milli") is None else r["elo_milli"] / 1000,
            "ci_lo": None if ci[0] is None else ci[0] / 1000,
            "ci_hi": None if ci[1] is None else ci[1] / 1000,
            "wins": r.get("wins", 0), "losses": r.get("losses", 0),
            "pairs": int(pairs), "decks": label,
            "git_head": git_head, "key": key, "ts": ts,
        }) + "\n")
PY

echo "sb-gauntlet: results appended to $GDIR/results.jsonl"
exit 0
