#!/usr/bin/env bash
# profile.sh [REV]: CPU and allocation profiles of enginebench rows.
#
# Builds REV (default "." -- the working tree; -memprofile needs a revision
# that has it, 2026-10-05 or later) and runs each row in ROWS once with
# -cpuprofile and -memprofile. Profiles land in
# $BENCH_DIR/prof/<time>/<row>-<pair>.{cpu,mem} next to the binary they were
# taken with, and the script prints, per row:
#   - the CPU top (flat), engine frames only, and the share GC took
#   - the allocation top (alloc_space): the bytes that drive GC
# Dig further with the printed `go tool pprof` commands.
#
#   ROWS  ';'-separated enginebench argument strings (default: lib.sh DEFAULT_ROWS)
#   SECS  -secs for every timed row (default 15)
#   TOP   lines per table (default 25)
set -euo pipefail
. "$(dirname "$0")/lib.sh"
rev=${1:-.}
secs=${SECS:-15}
top=${TOP:-25}
bin=$("$(dirname "$0")/build.sh" "$rev")
dir=$BENCH_DIR/prof/$(date +%Y%m%dT%H%M%S)
mkdir -p "$dir"
cp "$bin" "$dir/enginebench"
echo "binary $bin -> $dir/enginebench"
IFS=';' read -r -a rows <<<"${ROWS:-$DEFAULT_ROWS}"
for row in "${rows[@]}"; do
	read -r -a args <<<"$row"
	name=$(echo "$row" | sed -E 's/-row //; s/-pair //; s/ -?/-/g')
	# shellcheck disable=SC2046
	heavy "$dir/enginebench" "${args[@]}" -secs "$secs" $(workload_args) \
		-cpuprofile "$dir/$name.cpu" -memprofile "$dir/$name.mem" -out "$dir/results.jsonl" \
		>/dev/null 2>>"$dir/stderr.log"
	echo
	echo "=================== $row"
	# Tables are cut with awk, which reads all of its input: head under
	# pipefail would SIGPIPE pprof and fail the run.
	cum=$(go tool pprof -top -cum -nodecount=400 "$dir/enginebench" "$dir/$name.cpu" 2>/dev/null)
	total=$(echo "$cum" | sed -n 's/.* of \([0-9.]*s\) total.*/\1/p' | head -n1)
	gc=$(echo "$cum" | awk '$NF=="runtime.gcDrain"{print $4" ("$5")"}')
	echo "CPU samples $total; runtime.gcDrain (GC marking) ${gc:-0s} cumulative"
	echo "--- CPU top (flat), runtime frames hidden"
	go tool pprof -top -nodecount=400 "$dir/enginebench" "$dir/$name.cpu" 2>/dev/null |
		awk -v n="$top" '/flat%/{on=1; print; next} on && $0 !~ / (runtime|internal\/runtime)[.\/]/ && k<n {print; k++}'
	echo "--- allocation top (alloc_space)"
	go tool pprof -sample_index=alloc_space -top -nodecount="$top" "$dir/enginebench" "$dir/$name.mem" 2>/dev/null | awk '/flat%/{on=1} on'
	echo "  go tool pprof -top -cum $dir/enginebench $dir/$name.cpu"
	echo "  go tool pprof -sample_index=alloc_space -peek 'FUNC' $dir/enginebench $dir/$name.mem"
done
