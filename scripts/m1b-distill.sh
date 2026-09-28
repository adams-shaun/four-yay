#!/usr/bin/env bash
# M1b: clairvoyant vs honest search teacher, distilled into a seat-visible
# student (docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §15).
#
# Every heavy step runs under the shared lock in a MemoryMax scope; each
# generation chunk is one lock hold (~25 min at 8 workers), so other jobs get
# turns. Steps are idempotent (a finished output is skipped).
#
# Usage, from a worktree with .cards:
#   scripts/m1b-distill.sh build
#   scripts/m1b-distill.sh gen          # teacher corpora: eval + 2 train chunks per arm
#   scripts/m1b-distill.sh train        # students (clair, honest, bc, *-r2, pn12 diag arms)
#   scripts/m1b-distill.sh eval         # student alone; student inside honest search
# Overrides: M (work dir), LOCK.
set -u
REPO=$(cd "$(dirname "$0")/.." && pwd)
M=${M:-/mnt/sata/gorge-training/spellbench-work/m1b}
LOCK=${LOCK:-/mnt/sata/gorge-training/spellbench-work/heavy.lock}
CARDS=$REPO/.cards
heavy() { flock -o "$LOCK" systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=2GiB GOMAXPROCS=8 GOTMPDIR=/mnt/sata/gorge-training/gotmp "$@"; }
mkdir -p "$M/bin" "$M/clair" "$M/honest" "$M/students" "$M/evals"

# gen ARM NAME BASESEED PAIRS: az (clairvoyant) or az-redeal (honest, fresh deal per simulation)
# vs bot, 50 simulations, moves sampled by visits on turns <= 4, no root noise, on the SpellBench
# 8-deck mirror pool; records every searched decision (-az-corpus).
gen() {
	local arm=$1 name=$2 base=$3 pairs=$4 d policy world=""
	d=$M/$arm/$name
	[ -f "$d/v.jsonl.gz" ] && return
	rm -rf "$d"; mkdir -p "$d"
	if [ "$arm" = clair ]; then policy=az; world="-az-world clairvoyant"; else policy=az-redeal; fi
	# shellcheck disable=SC2086
	heavy "$M/bin/botbench" -dir "$CARDS" -spellbench $policy,bot -spellbench-pairs "$pairs" -spellbench-base-seed "$base" \
		-spellbench-out "$d/sb" $world -az-sims 50 -az-explore -az-no-noise -az-corpus "$d/v.jsonl.gz" -workers 8 >"$d/out.txt" 2>"$d/err.txt"
}

# train NAME CORPORA EVALCORPORA extra...: mz student, embed/hidden 128, value head 32, seed 1,
# 12 epochs, lr 0.1, clip 5, value target 0.95*outcome + 0.05*root value.
train() {
	local n=$1 c=$2 ev=$3
	shift 3
	[ -f "$M/students/$n.done" ] && return
	heavy "$M/bin/policytrain" -visits-corpus "$c" -visits-eval "$ev" -visits-report "$M/students/$n.json" \
		-out "$M/students/$n.gpol" -epochs 12 -lr 0.1 -clip 5 -value-weight 1 -value-blend 0.05 -seed 1 "$@" \
		>"$M/students/$n.txt" 2>&1 && touch "$M/students/$n.done"
}

# student NAME CKPT: the search-free student (-az-world prior) vs bot and sb-heuristic, 800 games each.
student() {
	local d=$M/evals/main-$1
	[ -s "$d/winrate.txt" ] && return
	rm -rf "$d"; mkdir -p "$d"
	heavy "$M/bin/botbench" -dir "$CARDS" -spellbench az,bot,sb-heuristic -spellbench-with az -spellbench-pairs 50 \
		-spellbench-base-seed 29001 -spellbench-out "$d/sb" -az-world prior -az-label "student-main-$1" -checkpoint "$2" -workers 8 >"$d/out.txt" 2>"$d/err.txt"
	python3 "$REPO/scripts/m1b-winrate.py" "$d/sb/matches.jsonl" -name "student-main-$1" | tee "$d/winrate.txt"
}

# search NAME CKPT|none extra...: honest search (az-redeal, 25 simulations) with the student as
# prior and leaf (none = uniform prior, heuristic leaf) vs bot, 1200 games on one seed block.
search() {
	local n=$1 ck=$2 d=$M/evals/search-$1
	shift 2
	[ -s "$d/winrate.txt" ] && return
	rm -rf "$d"; mkdir -p "$d"
	local ckarg=()
	[ "$ck" != none ] && ckarg=(-checkpoint "$ck")
	heavy "$M/bin/botbench" -dir "$CARDS" -spellbench az-redeal,bot -spellbench-pairs 75 -spellbench-base-seed 39001 \
		-spellbench-out "$d/sb" -az-sims 25 -az-label "search-$n" "${ckarg[@]}" "$@" -workers 8 >"$d/out.txt" 2>"$d/err.txt"
	python3 "$REPO/scripts/m1b-winrate.py" "$d/sb/matches.jsonl" -name "search-$n" | tee "$d/winrate.txt"
}

C=$M/clair/c1/v.jsonl.gz,$M/clair/c2/v.jsonl.gz
H=$M/honest/h1/v.jsonl.gz,$M/honest/h2/v.jsonl.gz
EV=$M/clair/eval/v.jsonl.gz,$M/honest/eval/v.jsonl.gz
S=$M/students
case "${1:-}" in
build)
	cd "$REPO" && heavy go build -o "$M/bin/botbench" ./cmd/botbench && heavy go build -o "$M/bin/policytrain" ./cmd/policytrain
	;;
gen)
	for arm in clair honest; do
		gen $arm eval 19001 10
		gen $arm "${arm:0:1}1" 11001 80
		gen $arm "${arm:0:1}2" 11002 80
	done
	;;
train)
	train clair "$C" "$EV" -visits-max-games 2500
	train honest "$H" "$EV" -visits-max-games 2500
	train bc "$C" "$EV" -visits-max-games 2500 -visits-label bot
	train clair-r2 "$C" "$EV" -visits-max-games 2500 -residual-init 2
	train honest-r2 "$H" "$EV" -visits-max-games 2500 -residual-init 2
	train bc-r2 "$C" "$EV" -visits-max-games 2500 -residual-init 2 -visits-label bot
	train c1-mz "$M/clair/c1/v.jsonl.gz" "$M/clair/eval/v.jsonl.gz"
	train c1-diag "$M/clair/c1/v.jsonl.gz" "$M/clair/eval/v.jsonl.gz" -visits-diag
	train h1-mz "$M/honest/h1/v.jsonl.gz" "$M/honest/eval/v.jsonl.gz"
	train h1-diag "$M/honest/h1/v.jsonl.gz" "$M/honest/eval/v.jsonl.gz" -visits-diag
	;;
eval)
	for n in clair honest bc clair-r2 honest-r2 bc-r2; do student $n "$S/$n.gpol"; done
	search none none
	for n in clair honest bc; do search $n "$S/$n.gpol"; done
	for n in clair honest bc; do search $n-hl "$S/$n.gpol" -az-heuristic-leaf; done
	;;
*)
	echo "usage: $0 build|gen|train|eval" >&2
	exit 2
	;;
esac
