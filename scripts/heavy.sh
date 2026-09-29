#!/usr/bin/env bash
# heavy.sh — run a heavy or probe job under a lease, a slice and a pause contract.
#
# Design: docs/superpowers/specs/2026-09-29-reward-loop-and-seed-agent-design.md §3.1.
#
#   scripts/heavy.sh heavy [options] -- <cmd> [args...]
#   scripts/heavy.sh probe [options] -- <cmd> [args...]
#
# Options:
#   --mem M       MemoryMax for the scope (default 6G heavy, 8G probe)
#   --cpus SPEC   AllowedCPUs for the scope (default: unset for probe, a
#                 subset for heavy so a sweep cannot take every core)
#   --weight N    CPUWeight (default 20 heavy, 60 probe; a gate's is 1000)
#   --wait S      wait up to S seconds for the broker to allow the class
#                 (default 0: refuse immediately with exit 3)
#   --lock PATH   flock path that serialises this class (default: the shared
#                 training lock for heavy, none for probe)
#   --name NAME   label recorded in the lease and the scope unit
#
# The job is told where its pause file is through GORGE_PAUSE_FILE. A job with
# a step loop — a training epoch, a gauntlet matchup — should check it at each
# checkpoint boundary:
#
#     while [ -e "${GORGE_PAUSE_FILE:-/nonexistent}" ]; do sleep 5; done
#
# A job that honours that is never stopped mid-step. A job that ignores it is
# SIGSTOPped by `broker.sh enforce` after the grace, and killed if the box
# crosses the memory floor. Honouring the file is strictly cheaper.
set -uo pipefail

ROOT=${GORGE_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}
STATE=${GORGE_REWARD_DIR:-$ROOT/.ds4/reward}
LEASES=$STATE/leases
BROKER=$ROOT/scripts/broker.sh

CLASS=${1:?usage: heavy.sh <heavy|probe> [options] -- <cmd>}
shift
case $CLASS in
heavy | probe) ;;
*)
	printf 'heavy.sh: class must be heavy or probe, got %s\n' "$CLASS" >&2
	exit 2
	;;
esac

MEM=""
CPUS=""
WEIGHT=""
WAIT=0
LOCK=""
NAME=""
while [ $# -gt 0 ]; do
	case $1 in
	--mem)
		MEM=$2
		shift 2
		;;
	--cpus)
		CPUS=$2
		shift 2
		;;
	--weight)
		WEIGHT=$2
		shift 2
		;;
	--wait)
		WAIT=$2
		shift 2
		;;
	--lock)
		LOCK=$2
		shift 2
		;;
	--name)
		NAME=$2
		shift 2
		;;
	--)
		shift
		break
		;;
	*)
		printf 'heavy.sh: unknown option %s\n' "$1" >&2
		exit 2
		;;
	esac
done
[ $# -gt 0 ] || {
	printf 'heavy.sh: no command given (did you forget --?)\n' >&2
	exit 2
}

if [ "$CLASS" = heavy ]; then
	MEM=${MEM:-6G}
	WEIGHT=${WEIGHT:-20}
	CPUS=${CPUS:-${GORGE_HEAVY_CPUS:-}}
	LOCK=${LOCK:-${GORGE_HEAVY_LOCK:-/mnt/sata/gorge-training/spellbench-work/heavy.lock}}
	SLICE=gorge-heavy.slice
else
	MEM=${MEM:-8G}
	WEIGHT=${WEIGHT:-60}
	SLICE=gorge-probe.slice
fi
NAME=${NAME:-$(basename "$1")}

mkdir -p "$LEASES"

# 1. Ask the broker. Without --wait a refusal is exit 3, so a caller (a probe
#    in the seed cycle) can treat "no headroom" as "not now", not as a failure.
deadline=$(($(date +%s) + WAIT))
while :; do
	if reason=$("$BROKER" may-i "$CLASS" 2>/dev/null); then
		printf 'heavy.sh: %s\n' "$reason"
		break
	fi
	if [ "$(date +%s)" -ge "$deadline" ]; then
		printf 'heavy.sh: refused: %s\n' "$reason" >&2
		exit 3
	fi
	sleep 10
done

# 2. Serialise the class with the same flock the existing heavy callers use.
#    -o matters: without it the lock fd is inherited by every child, and an
#    inherited tick.lock has frozen the daemon here before (2026-09-22).
if [ -n "$LOCK" ]; then
	mkdir -p "$(dirname "$LOCK")" 2>/dev/null || true
	exec 9>"$LOCK" || {
		printf 'heavy.sh: cannot open lock %s\n' "$LOCK" >&2
		exit 2
	}
	if ! flock -w "${WAIT:-0}" 9; then
		printf 'heavy.sh: another %s job holds %s\n' "$CLASS" "$LOCK" >&2
		exit 3
	fi
fi

PAUSE_FILE=$STATE/pause-$$.flag
LEASE=$LEASES/$$.json
cleanup() {
	rm -f "$LEASE" "$LEASE.paused_at" "$LEASE.stopped" "$PAUSE_FILE"
}
trap cleanup EXIT INT TERM

# 3. Job control on, so the job gets its own process group and the broker can
#    SIGSTOP or kill the whole tree by negative pid.
set -m
scope=(systemd-run --user --scope -q --slice="$SLICE" --unit="gorge-$CLASS-$$"
	-p "MemoryMax=$MEM" -p "CPUWeight=$WEIGHT")
[ -n "$CPUS" ] && scope+=(-p "AllowedCPUs=$CPUS")
GORGE_PAUSE_FILE=$PAUSE_FILE GORGE_BROKER_CLASS=$CLASS "${scope[@]}" -- "$@" &
job=$!
set +m

# The lease is written after the launch so the pid is the real one. Keep the
# JSON on one line and quoted exactly the way broker.sh's field reader expects.
cmdline=$(printf '%s ' "$@" | sed 's/[[:space:]]*$//' | tr -d '"' | cut -c1-300)
printf '{"class":"%s","name":"%s","pid":%s,"started":"%s","pause_file":"%s","slice":"%s","mem":"%s","cmd":"%s"}\n' \
	"$CLASS" "$NAME" "$job" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$PAUSE_FILE" "$SLICE" "$MEM" "$cmdline" >"$LEASE"

printf 'heavy.sh: %s lease %s pid %s slice %s mem %s\n' "$CLASS" "$(basename "$LEASE")" "$job" "$SLICE" "$MEM"
wait "$job"
rc=$?
printf 'heavy.sh: %s finished rc=%s\n' "$NAME" "$rc"
exit "$rc"
