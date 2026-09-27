#!/usr/bin/env bash
# Stage 0 of the AlphaZero-style MCTS effort
# (docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md §3): measure
# the generation-0 clairvoyant az seat -- no network, uniform prior,
# heuristic leaf -- for cost and strength against bot, BEFORE any training
# code (tickets 3-5) is built. It trains nothing and commits nothing.
#
# Resource rules (spec §3): this is THE one heavy job on the machine -- do
# not start it while another go test, botbench, sweep or training run is
# live. Every heavy step runs in a MemoryMax scope with GOMEMLIMIT=1GiB,
# pinned to two cores, and writes only under $OUT.
#
# Usage (from anywhere; the checkout is the one holding this script, at the
# commit to measure, with .cards fetched):
#   scripts/az-stage0.sh
# Overrides: OUT CORES MEM SEED GAMES_PER_PAIR SIMS_ARMS SMOKE_SIMS BENCHTIME
# A dry run that only proves every command line parses:
#   OUT=/mnt/sata/gorge-training/az/smoke GAMES_PER_PAIR=2 SIMS_ARMS=5 \
#     SMOKE_SIMS=5 BENCHTIME=1x MEM=4G scripts/az-stage0.sh
# Results: fill docs/superpowers/reports/<date>-az-stage0.md from
# docs/superpowers/reports/az-stage0-template.md.
set -euo pipefail

REPO=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
OUT=${OUT:-/mnt/sata/gorge-training/az/stage0}
CORES=${CORES:-12,28}
MEM=${MEM:-5G}
SEED=${SEED:-90000000}
GAMES_PER_PAIR=${GAMES_PER_PAIR:-40} # x 5 pairs = 200 games per arm
SIMS_ARMS=${SIMS_ARMS:-"25 100"}
SMOKE_SIMS=${SMOKE_SIMS:-25}
BENCHTIME=${BENCHTIME:-20x}
PAIRS=uw-tempo:mono-white-equipment,uw-tempo:mono-blue-tempo,uw-tempo:mono-black-aggro,uw-tempo:mono-red-prowess,uw-tempo:mono-green-stompy
NPAIRS=5

if [ ! -d "$REPO/.cards" ]; then
	echo "az-stage0: $REPO/.cards is missing; run make fetch-cards compile-cards first" >&2
	exit 1
fi
mkdir -p "$OUT/gotmp"
export GOTMPDIR="$OUT/gotmp"

# heavy runs one command in the capped scope, pinned to $CORES.
heavy() {
	systemd-run --user --scope -q -p MemoryMax="$MEM" \
		env GOMEMLIMIT=1GiB GOTMPDIR="$GOTMPDIR" GOFLAGS=-p=1 taskset -c "$CORES" "$@"
}

cd "$REPO"
{
	echo "commit $(git rev-parse HEAD)"
	echo "forge_ref $(/usr/bin/grep -E '^FORGE_REF' Makefile | head -1)"
	echo "dirty $(git status --porcelain | wc -l) path(s)"
	echo "knobs seed=$SEED games_per_pair=$GAMES_PER_PAIR sims_arms=[$SIMS_ARMS] smoke_sims=$SMOKE_SIMS benchtime=$BENCHTIME mem=$MEM cores=$CORES"
} | tee "$OUT/commit.txt"

# 1. Build once.
heavy go build -o "$OUT/botbench" ./cmd/botbench

# 2. Per-simulation and per-searched-decision cost (spec §3, §4): ns/sim and
#    allocs per searched decision, early and late positions.
heavy go test -p 1 -count=1 -run '^$' -bench 'BenchmarkSearchDecision' -benchmem -benchtime "$BENCHTIME" \
	./internal/azmcts/ | tee "$OUT/bench.txt"

# 3. Peak RSS of the new test binaries (spec §4: a multi-GB test binary is a
#    bug). -v is required: go test in package-list mode drops a passing
#    binary's stderr, which is where /usr/bin/time writes.
heavy go test -v -p 1 -count=1 -exec '/usr/bin/time -f peak-rss-kb=%M' ./internal/azmcts/ > "$OUT/rss-azmcts.txt" 2>&1
heavy go test -v -p 1 -count=1 -run 'TestAZ' -exec '/usr/bin/time -f peak-rss-kb=%M' ./cmd/botbench/ > "$OUT/rss-botbench.txt" 2>&1
/usr/bin/grep -hE '^peak-rss-kb=|^ok|^FAIL' "$OUT/rss-azmcts.txt" "$OUT/rss-botbench.txt"

# arm NAME GAMES_PER_PAIR BOTBENCH_ARGS... -- one matrix run on the eval block.
arm() {
	local name=$1 games=$2
	shift 2
	heavy /usr/bin/time -f "wall_s=%e peak_rss_kb=%M" -o "$OUT/$name.time" \
		"$OUT/botbench" -pairs "$PAIRS" -games "$games" -seed "$SEED" -workers 2 -dir "$REPO/.cards" "$@" \
		> "$OUT/$name.txt"
	echo "== $name: $(cat "$OUT/$name.time")"
	/usr/bin/grep -E '^pooled .* win rate|^az cost report|^ms/searched|^counters|^searched by kind|^  skipped ' "$OUT/$name.txt" || true
}

# 4. Smoke: one game per pair at the cheaper setting, so a broken seat fails
#    in minutes, not after hours.
arm smoke 1 -a az -b bot -az-world clairvoyant -az-sims "$SMOKE_SIMS"

# 5. The arms: control (bot vs bot, same block) and gen-0 az at each budget.
arm control "$GAMES_PER_PAIR" -a bot -b bot
for sims in $SIMS_ARMS; do
	arm "az$sims" "$GAMES_PER_PAIR" -a az -b bot -az-world clairvoyant -az-sims "$sims"
done

# pooled NAME POLICY -- "rate lo hi" (percent) from NAME.txt's pooled line,
# or nothing when the arm has no rate (every game stalled).
pooled() {
	sed -n "s/^pooled $2 win rate: \([0-9.]*\)%  95% CI \[\([0-9.]*\)%, \([0-9.]*\)%\].*/\1 \2 \3/p" "$OUT/$1.txt"
}

# 6. Throughput (games per hour on the two granted cores) and the kill
#    criterion, operationalised (controller decision 2026-09-27; the
#    operator may override): KILL if no az arm's pooled win-rate 95% CI lies
#    entirely above the same-seed bot-vs-bot control arm's pooled win rate.
{
	for name in control $(for s in $SIMS_ARMS; do echo "az$s"; done); do
		wall=$(sed -n 's/^wall_s=\([0-9.]*\).*/\1/p' "$OUT/$name.time")
		awk -v n="$name" -v g=$((GAMES_PER_PAIR * NPAIRS)) -v w="$wall" \
			'BEGIN { printf "%s: %d games in %.0f s = %.1f games/h (2 cores)\n", n, g, w, (w > 0 ? g * 3600 / w : 0) }'
	done
	read -r crate _ _ <<<"$(pooled control bot)" || true
	if [ -z "${crate:-}" ]; then
		echo "verdict: UNDECIDED -- the control arm has no pooled rate (every game stalled)"
	else
		echo "control pooled bot win rate: ${crate}%"
		pass=""
		for s in $SIMS_ARMS; do
			read -r rate lo hi <<<"$(pooled "az$s" az)" || true
			if [ -z "${rate:-}" ]; then
				echo "az$s: no pooled rate (every game stalled)"
				continue
			fi
			above=$(awk -v lo="$lo" -v c="$crate" 'BEGIN { print (lo > c) ? "yes" : "no" }')
			echo "az$s pooled az win rate: ${rate}% CI [${lo}%, ${hi}%]; CI entirely above control ${crate}%: $above"
			if [ "$above" = yes ]; then pass="$pass az$s"; fi
			rate="" lo="" hi=""
		done
		if [ -n "$pass" ]; then
			echo "verdict: PASS --$pass CI lies entirely above the control's ${crate}%"
		else
			echo "verdict: KILL -- no az arm's pooled win-rate 95% CI lies entirely above the same-seed bot-vs-bot control arm's pooled win rate (${crate}%)"
		fi
	fi
} | tee "$OUT/summary.txt"
echo "az-stage0: done; fill docs/superpowers/reports/<date>-az-stage0.md from docs/superpowers/reports/az-stage0-template.md and $OUT"
