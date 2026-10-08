#!/usr/bin/env bash
# enginebench-pair.sh BASE CAND [OUT]: a paired engine-speed comparison of two revisions.
#
# Builds BASE and CAND (any git revision, or "." for the working tree), then
# for each row in ROWS runs REPS repetitions of BASE and CAND back to back,
# alternating which goes first (ABBA) so load drift on a shared box falls on
# both. Results append to OUT (default $BENCH_DIR/results/pair-<time>.jsonl),
# labelled "base" and "cand", and enginebench-summarize.py prints per-row medians and
# the median of the per-rep CAND/BASE ratios.
#
#   ROWS  ';'-separated enginebench argument strings (default: enginebench-lib.sh DEFAULT_ROWS)
#   REPS  repetitions per row (default 2: the smallest ABBA)
#   SECS  -secs for every timed row (default: BUDGET split over every run, enginebench-lib.sh)
#   GOGC  passed through to both binaries if set
set -euo pipefail
. "$(dirname "$0")/enginebench-lib.sh"
base_rev=${1:?usage: enginebench-pair.sh BASE CAND [OUT]}
cand_rev=${2:?usage: enginebench-pair.sh BASE CAND [OUT]}
mkdir -p "$BENCH_DIR/results"
out=${3:-$BENCH_DIR/results/pair-$(date +%Y%m%dT%H%M%S).jsonl}
reps=${REPS:-2}
base=$("$(dirname "$0")/enginebench-build.sh" "$base_rev")
cand=$("$(dirname "$0")/enginebench-build.sh" "$cand_rev")
echo "base $base_rev -> $base"
echo "cand $cand_rev -> $cand"
echo "results -> $out"
IFS=';' read -r -a rows <<<"${ROWS:-$DEFAULT_ROWS}"
secs=${SECS:-$(budget_secs $((${#rows[@]} * reps * 2)))}
echo "budget ${BUDGET}s: -secs $secs per run"
budget_start
run1() { # run1 LABEL BIN REP ROWARGS...
	local label=$1 bin=$2 rep=$3
	shift 3
	budget_left || return 0
	# shellcheck disable=SC2046
	heavy "$bin" "$@" -secs "$secs" $(workload_args) -label "$label" -rep "$rep" -out "$out" \
		>/dev/null 2>>"$out.stderr" || echo "FAIL $label rep=$rep $*" | tee -a "$out.stderr" >&2
}
for row in "${rows[@]}"; do
	read -r -a args <<<"$row"
	for rep in $(seq 1 "$reps"); do
		echo "$(date +%T) rep $rep/$reps: $row (load $(cut -d' ' -f1 /proc/loadavg))"
		if [ $((rep % 2)) -eq 1 ]; then
			run1 base "$base" "$rep" "${args[@]}"
			run1 cand "$cand" "$rep" "${args[@]}"
		else
			run1 cand "$cand" "$rep" "${args[@]}"
			run1 base "$base" "$rep" "${args[@]}"
		fi
	done
done
python3 "$(dirname "$0")/enginebench-summarize.py" "$out"
