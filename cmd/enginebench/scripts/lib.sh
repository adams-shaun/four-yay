# lib.sh: shared settings for the enginebench scripts (sourced, not run).
#
# Every knob is an environment variable with a default:
#   BENCH_DIR   binaries, source snapshots, results, profiles
#               (default /mnt/sata/gorge-training/enginebench: rebuildable scratch)
#   DECKS       workload deck directory (default: the committed testdata/decks)
#   CARDS       corpus directory (default: the repo's .cards)
#   HEAVY_LOCK  flock file serialising heavy jobs (default $BENCH_DIR/heavy.lock;
#               point it at the box's shared heavy lock when one is in use)
#   MEM_MAX     systemd scope MemoryMax (default 24G)
#   BENCH_GOMEMLIMIT  GOMEMLIMIT inside the scope (default 16GiB)
#   NO_SCOPE    set to run without a systemd scope (flock only)

REPO=$(git rev-parse --show-toplevel)
BENCH_DIR=${BENCH_DIR:-/mnt/sata/gorge-training/enginebench}
DECKS=${DECKS:-$REPO/cmd/enginebench/testdata/decks}
CARDS=${CARDS:-$REPO/.cards}
CARDS=$(cd "$CARDS" && pwd -P)
mkdir -p "$BENCH_DIR"

# heavy CMD...: run CMD capped (systemd scope) and serialised (flock -o, so the
# lock fd is not inherited by anything CMD leaves running).
heavy() {
	local lock=${HEAVY_LOCK:-$BENCH_DIR/heavy.lock}
	if [ -z "${NO_SCOPE:-}" ] && command -v systemd-run >/dev/null 2>&1; then
		flock -o "$lock" systemd-run --user --scope -q -p MemoryMax="${MEM_MAX:-24G}" \
			env GOMEMLIMIT="${BENCH_GOMEMLIMIT:-16GiB}" "$@"
	else
		flock -o "$lock" env GOMEMLIMIT="${BENCH_GOMEMLIMIT:-16GiB}" "$@"
	fi
}

# workload_args: the flags every run passes explicitly, so a binary built at an
# older revision (whose defaults point elsewhere) plays the same workload.
workload_args() {
	echo -decks "$DECKS" -burn "$DECKS/burn.json" -cards "$CARDS"
}

# The default row set (the 2026-10-05 profiling loop's rows): ';'-separated
# enginebench argument strings. SECS overrides each row's -secs.
DEFAULT_ROWS="-row random -pair A;-row random -pair B;-row bot -pair A;-row sampler"
