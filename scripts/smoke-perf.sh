#!/usr/bin/env bash
# smoke-perf.sh [BASE] [CAND]: a pinned, report-only throughput smoke of the
# whole-game path, comparing two revisions of botbench.
#
# BASE defaults to main, CAND to the working tree (.). Both are built the way
# scripts/enginebench-build.sh builds its revisions: BASE is exported with
# `git archive` into a scratch tree (no checkout, so the shared checkout's HEAD
# never moves), CAND is built from the working tree. Every scenario runs both
# revisions, BASE then CAND, on one pinned core, and each run's output is
# streamed live. A summary table follows with both revisions' wall/user time
# and ms/asked and their ratio.
#
# It is a smoke test of throughput health, not a benchmark: no threshold is
# enforced. It exits non-zero only when a game does not run to completion (a
# non-zero botbench exit, or the @@ STALLED watchdog notice) on either revision.
#
#   SMOKE_PERF_GAMES   override every scenario's -games to this N
#   SMOKE_PERF_SEED    base seed (default 7)
#   SMOKE_PERF_BUDGET  soft warning bound in seconds (default 300)
#   SMOKE_PERF_DIR     scratch directory (default ${TMPDIR:-/tmp}/gorge-smokeperf)
set -uo pipefail

repo=$(git rev-parse --show-toplevel)
cd "$repo"
base_rev=${1:-${BASE:-main}}
cand_rev=${2:-${CAND:-.}}
seed=${SMOKE_PERF_SEED:-7}
budget=${SMOKE_PERF_BUDGET:-300}
scratch=${SMOKE_PERF_DIR:-${TMPDIR:-/tmp}/gorge-smokeperf}
bindir=$scratch/bin
mkdir -p "$bindir"

if [ -n "${SMOKE_PERF_GAMES:-}" ]; then
	g1=$SMOKE_PERF_GAMES g2=$SMOKE_PERF_GAMES g3=$SMOKE_PERF_GAMES g4=$SMOKE_PERF_GAMES
else
	g1=8 g2=2 g3=8 g4=8
fi

build_bin() {
	local rev=$1 out lbl sha src
	if [ "$rev" = "." ]; then
		lbl="wt-$(git rev-parse --short=12 HEAD)"
		out="$bindir/botbench-$lbl"
		(cd "$repo" && go build -o "$out" ./cmd/botbench) || return 1
		printf '%s\n' "$out"
		return
	fi
	sha=$(git rev-parse --verify "$rev^{commit}") || return 1
	lbl=${sha:0:12}
	out="$bindir/botbench-$lbl"
	if [ ! -x "$out" ]; then
		src="$scratch/src/$lbl"
		if [ ! -f "$src/go.mod" ]; then
			mkdir -p "$src.tmp"
			git archive "$sha" | tar -x -C "$src.tmp"
			mv "$src.tmp" "$src"
		fi
		(cd "$src" && go build -o "$out" ./cmd/botbench) || return 1
	fi
	printf '%s\n' "$out"
}

base_bin=$(build_bin "$base_rev") || { echo "smoke-perf: cannot build BASE $base_rev" >&2; exit 1; }
cand_bin=$(build_bin "$cand_rev") || { echo "smoke-perf: cannot build CAND $cand_rev" >&2; exit 1; }
echo "smoke-perf: base=$base_rev ($base_bin)  cand=$cand_rev ($cand_bin)"
echo "smoke-perf: pin=cpu9 GOMAXPROCS=1 seed=$seed games=$g1/$g2/$g3/$g4"

declare -A R_WALL R_USER R_MS R_FAIL
scenarios=()
start=$(date +%s)

run_rev() { # name side bin args...
	local name=$1 side=$2 binv=$3
	shift 3
	echo "--- $name [$side] ---"
	local log="$scratch/run-$name-$side.log" tfile="$scratch/run-$name-$side.time"
	: >"$log"
	/usr/bin/time -f '%e %U %S' -o "$tfile" \
		taskset -c 9 env GOMAXPROCS=1 GOMEMLIMIT=2GiB \
		"$binv" -dir .cards -format constructed -workers 1 -seed "$seed" "$@" 2>&1 | tee "$log"
	local rc=${PIPESTATUS[0]}
	local wall user ms fail=""
	read -r wall user _ <"$tfile"
	ms=$(sed -n 's/.*ms\/asked decision total: mean \([0-9.]*\).*/\1/p' "$log" | head -1)
	if [ "$rc" -ne 0 ]; then fail="exit $rc"; fi
	if grep -q '@@ STALLED' "$log"; then fail="${fail:+$fail; }stalled"; fi
	R_WALL[$name-$side]=$wall
	R_USER[$name-$side]=$user
	R_MS[$name-$side]=$ms
	R_FAIL[$name-$side]=$fail
	printf '    %s: wall=%ss user=%ss ms/asked=%s%s\n' "$side" "$wall" "$user" "${ms:-n/a}" "${fail:+ FAIL($fail)}"
}

run_scenario() { # name args...
	local name=$1
	shift
	scenarios+=("$name")
	echo
	echo "##### scenario: $name #####"
	run_rev "$name" base "$base_bin" "$@"
	run_rev "$name" cand "$cand_bin" "$@"
}

run_scenario "random play"        -a sb-uniform -b sb-uniform -games "$g1"
run_scenario "random decks"       -a bot -b bot -pairs coverage -games "$g2"
run_scenario "heuristics vs search" -a bot -b search -hosted-root -games "$g3"
run_scenario "search vs random"   -a search -b sb-uniform -hosted-root -games "$g4"

anyfail=0
printf '\n%-20s %14s %14s %13s %13s %7s  %s\n' \
	"scenario" "base w/u(s)" "cand w/u(s)" "base ms/ask" "cand ms/ask" "ratio" "result"
for name in "${scenarios[@]}"; do
	bw=${R_WALL[$name-base]} bu=${R_USER[$name-base]} cw=${R_WALL[$name-cand]} cu=${R_USER[$name-cand]}
	bms=${R_MS[$name-base]} cms=${R_MS[$name-cand]}
	if [ -n "$bms" ] && [ -n "$cms" ] && [ "$bms" != "0" ]; then
		ratio=$(awk -v c="$cms" -v b="$bms" 'BEGIN{printf "%.3f", c/b}')
	else
		ratio="n/a"
	fi
	fail="${R_FAIL[$name-base]} ${R_FAIL[$name-cand]}"
	if [ -n "${fail// }" ]; then
		res="FAIL(${fail})"; anyfail=1
	else
		res="PASS"
	fi
	printf '%-20s %6s/%-6s %6s/%-6s %13s %13s %7s  %s\n' \
		"$name" "$bw" "$bu" "$cw" "$cu" "${bms:-n/a}" "${cms:-n/a}" "$ratio" "$res"
done

elapsed=$(( $(date +%s) - start ))
printf 'total elapsed: %ss' "$elapsed"
if [ "$elapsed" -gt "$budget" ]; then
	printf ' (over SMOKE_PERF_BUDGET=%ss)' "$budget"
fi
printf '\n'
exit "$anyfail"
