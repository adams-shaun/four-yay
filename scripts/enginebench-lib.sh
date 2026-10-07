# enginebench-lib.sh: shared settings for the enginebench scripts (sourced, not run).
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
#   BUDGET      wall seconds for ONE script's whole measured phase -- every row
#               and rep together, builds excluded (default 60, operator
#               2026-10-06). Each run's -secs is derived from it (budget_secs),
#               and a run that would start past the deadline is skipped.
#   LOAD_S      per-run startup estimate (corpus load) budget_secs reserves
#               (default 3)
#   GATE_FLAG   the broker's gate-active flag (default: the main checkout's
#               .ds4/reward/gate-active); heavy() never runs while it is set

REPO=$(git rev-parse --show-toplevel)
BENCH_DIR=${BENCH_DIR:-/mnt/sata/gorge-training/enginebench}
DECKS=${DECKS:-$REPO/cmd/enginebench/testdata/decks}
CARDS=${CARDS:-$REPO/.cards}
CARDS=$(cd "$CARDS" && pwd -P)
mkdir -p "$BENCH_DIR"

GATE_FLAG=${GATE_FLAG:-$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")/.ds4/reward/gate-active}

# gate_active: the broker's gate flag is set and younger than its 1800 s TTL
# (scripts/broker.sh gate_active; an older flag is abandoned).
gate_active() {
	[ -e "$GATE_FLAG" ] || return 1
	[ $(($(date +%s) - $(stat -c %Y "$GATE_FLAG" 2>/dev/null || echo 0))) -le 1800 ]
}

# heavy CMD...: run CMD capped (systemd scope) and serialised (flock -o, so the
# lock fd is not inherited by anything CMD leaves running).
#
# A bench YIELDS to a landing gate (operator, 2026-10-06): five seat benches
# queued on the shared heavy lock made each merge wait 10+ minutes for it. So
# heavy() waits while a gate is active, and if one starts while it is queued,
# it drops the lock without running (exit 75 inside the lock) and waits again.
heavy() {
	local lock=${HEAVY_LOCK:-$BENCH_DIR/heavy.lock} rc
	local -a run=(env GOMEMLIMIT="${BENCH_GOMEMLIMIT:-16GiB}" "$@")
	if [ -z "${NO_SCOPE:-}" ] && command -v systemd-run >/dev/null 2>&1; then
		run=(systemd-run --user --scope -q -p MemoryMax="${MEM_MAX:-24G}" "${run[@]}")
	fi
	while :; do
		while gate_active; do sleep 5; done
		flock -o "$lock" bash -c 'if [ -e "$1" ] && [ $(($(date +%s) - $(stat -c %Y "$1"))) -le 1800 ]; then exit 75; fi; shift; exec "$@"' _ "$GATE_FLAG" "${run[@]}"
		rc=$?
		[ "$rc" -ne 75 ] && return "$rc"
	done
}

BUDGET=${BUDGET:-60}
LOAD_S=${LOAD_S:-3}

# budget_secs RUNS: the -secs each of RUNS runs gets so all of them, plus
# LOAD_S of startup apiece, fit in BUDGET (at least 1 s each).
budget_secs() {
	awk -v b="$BUDGET" -v l="$LOAD_S" -v n="$1" 'BEGIN { s = (b - n * l) / n; if (s < 1) s = 1; printf "%.1f", s }'
}

# budget_start: start the BUDGET clock (call after the builds).
budget_start() { BUDGET_DEADLINE=$(($(date +%s) + BUDGET)); }

# budget_left: true while the BUDGET clock has time left; otherwise it says so.
budget_left() {
	[ "$(date +%s)" -lt "${BUDGET_DEADLINE:?budget_start not called}" ] && return 0
	echo "BUDGET ${BUDGET}s spent: skipping the remaining runs" >&2
	return 1
}

# workload_args: the flags every run passes explicitly, so a binary built at an
# older revision (whose defaults point elsewhere) plays the same workload.
workload_args() {
	echo -decks "$DECKS" -burn "$DECKS/burn.json" -cards "$CARDS"
}

# The default row set (the 2026-10-05 profiling loop's rows): ';'-separated
# enginebench argument strings. SECS overrides each row's -secs.
DEFAULT_ROWS="-row random -pair A;-row random -pair B;-row bot -pair A;-row sampler"
