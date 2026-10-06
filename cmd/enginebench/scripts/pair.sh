#!/usr/bin/env bash
# pair.sh BASE CAND [OUT]: a paired engine-speed comparison of two revisions.
#
# Builds BASE and CAND (any git revision, or "." for the working tree), then
# for each row in ROWS runs REPS repetitions of BASE and CAND back to back,
# alternating which goes first (ABBA) so load drift on a shared box falls on
# both. Results append to OUT (default $BENCH_DIR/results/pair-<time>.jsonl),
# labelled "base" and "cand", and summarize.py prints per-row medians and
# the median of the per-rep CAND/BASE ratios.
#
#   ROWS  ';'-separated enginebench argument strings (default: lib.sh DEFAULT_ROWS)
#   REPS  repetitions per row (default 3)
#   SECS  -secs for every timed row (default 10)
#   GOGC  passed through to both binaries if set
set -euo pipefail
. "$(dirname "$0")/lib.sh"
base_rev=${1:?usage: pair.sh BASE CAND [OUT]}
cand_rev=${2:?usage: pair.sh BASE CAND [OUT]}
mkdir -p "$BENCH_DIR/results"
out=${3:-$BENCH_DIR/results/pair-$(date +%Y%m%dT%H%M%S).jsonl}
reps=${REPS:-3}
secs=${SECS:-10}
base=$("$(dirname "$0")/build.sh" "$base_rev")
cand=$("$(dirname "$0")/build.sh" "$cand_rev")
echo "base $base_rev -> $base"
echo "cand $cand_rev -> $cand"
echo "results -> $out"
IFS=';' read -r -a rows <<<"${ROWS:-$DEFAULT_ROWS}"
run1() { # run1 LABEL BIN REP ROWARGS...
	local label=$1 bin=$2 rep=$3
	shift 3
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
python3 "$(dirname "$0")/summarize.py" "$out"
