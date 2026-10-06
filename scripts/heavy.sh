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
#                 repository heavy lock for heavy, none for probe)
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

# The lock path is defined once, in heavy_lock.sh.
. "$(dirname "$0")/heavy_lock.sh"
[ "${1:-}" = --print-heavy-lock ] && {
	printf '%s\n' "${LOCK:-$GORGE_HEAVY_LOCK}"
	exit 0
}

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
	--print-heavy-lock)
		printf '%s\n' "${LOCK:-$GORGE_HEAVY_LOCK}"
		exit 0
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
	LOCK=${LOCK:-$GORGE_HEAVY_LOCK}
	SLICE=gorge-heavy.slice
else
	MEM=${MEM:-8G}
	WEIGHT=${WEIGHT:-60}
	SLICE=gorge-probe.slice
fi
NAME=${NAME:-$(basename "$1")}

mkdir -p "$LEASES"

# --wait S is ONE budget: the broker wait and the lock wait draw on it together.
deadline=$(($(date +%s) + WAIT))

# 1. Ask the broker FIRST, before the lock. A job parked in this wait holds no
#    lock: the agentctl affected gate (`heavy_lock.sh run -w 1500`) brackets
#    itself with `broker.sh gate-begin`, which is exactly what keeps `may-i`
#    refusing here. If this loop held the HEAVY lock, the gate would run out its
#    whole wait on a holder the bracket itself froze and go red (the l0-CRITICAL
#    class, observed again 2026-10-06). Without --wait a refusal is exit 3, so a
#    caller (a probe in the seed cycle) treats "no headroom" as "not now".
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

# 2. Serialise the class ONCE the broker has approved. The lock lives on fd 9
#    of THIS shell, never of the job (it is launched with 9>&-), so a child
#    cannot hold it after we die or freeze whoever waits on it (an inherited
#    tick.lock froze the daemon here on 2026-09-22). It is held on an fd, not
#    through `flock -o <cmd>`, because the pause supervisor below must RELEASE
#    it while the job is parked and take it back before the job continues. The
#    wait draws on what is LEFT of the --wait budget, so `--wait S` stays ONE
#    budget instead of two S-second waits.
if [ "$CLASS" = heavy ]; then
	mkdir -p "$(dirname "$LOCK")"
	exec 9>"$LOCK" || {
		printf 'heavy.sh: cannot open lock %s\n' "$LOCK" >&2
		exit 2
	}
	remaining=$((deadline - $(date +%s)))
	[ "$remaining" -gt 0 ] || remaining=0
	flock -w "$remaining" -E 3 9
	rc=$?
	if [ "$rc" != 0 ]; then
		printf 'heavy.sh: another %s job holds %s\n' "$CLASS" "$LOCK" >&2
		exit 3
	fi
fi

# 3. The lease. The HEAVY lock (fd 9) is held until this shell exits.
PAUSE_FILE=$STATE/pause-$$.flag
LEASE=$LEASES/$$.json
cleanup() {
	rm -f "$LEASE" "$LEASE.paused_at" "$LEASE.stopped" "$LEASE.supervised" "$LEASE.parked" "$PAUSE_FILE"
}
trap cleanup EXIT INT TERM

# 4. Job control on, so the job gets its own process group and the broker can
#    SIGSTOP or kill the whole tree by negative pid.
set -m
scope=(systemd-run --user --scope -q --slice="$SLICE" --unit="gorge-$CLASS-$$"
	-p "MemoryMax=$MEM" -p "CPUWeight=$WEIGHT")
[ -n "$CPUS" ] && scope+=(-p "AllowedCPUs=$CPUS")
GORGE_PAUSE_FILE=$PAUSE_FILE GORGE_BROKER_CLASS=$CLASS "${scope[@]}" -- "$@" 9>&- &
job=$!
set +m

# The lease is written after the launch so the pid is the real one. Keep the
# JSON on one line and quoted exactly the way broker.sh's field reader expects.
cmdline=$(printf '%s ' "$@" | sed 's/[[:space:]]*$//' | tr -d '"' | cut -c1-300)
printf '{"class":"%s","name":"%s","pid":%s,"started":"%s","pause_file":"%s","slice":"%s","mem":"%s","cmd":"%s"}\n' \
	"$CLASS" "$NAME" "$job" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$PAUSE_FILE" "$SLICE" "$MEM" "$cmdline" >"$LEASE"

printf 'heavy.sh: %s lease %s pid %s slice %s mem %s\n' "$CLASS" "$(basename "$LEASE")" "$job" "$SLICE" "$MEM"

# 5. Cooperative pause supervision for the HEAVY class. heavy.sh is the one
#    place every heavy job runs through, so it is where "heavy leases must be
#    pausable throughout" is guaranteed: the job needs no pause loop of its
#    own. Without this, a long CPU-bound heavy command (`go test ./...`
#    -timeout 120m) ignores its pause file, `broker.sh enforce` SIGSTOPs it
#    after the grace and records `gate_starved_minutes`, and that single
#    escalation is a full stability veto (2026-10-03 head 2d4e01009).
#
#    The supervisor waits out PAUSE_GRACE_S so a heavy job with its own
#    checkpoint loop parks itself first (nothing is signalled mid-step), then
#    stops the job's process group and drops `$LEASE.parked`; it continues the
#    group when the pause file disappears. `$LEASE.supervised` is written at
#    lease time and is what makes broker.sh defer to this supervisor instead
#    of escalating: the pause is handled, so it is not starvation. The probe
#    class stays on the broker's fallback -- a probe is short and bounded, and
#    broker_smoke.sh still exercises the fallback through a probe lease.
PAUSE_GRACE_S=${PAUSE_GRACE_S:-60}
if [ "$CLASS" = heavy ]; then
	: >"$LEASE.supervised"
	(
		# This subshell shares fd 9 (the HEAVY lock) with heavy.sh. While the job
		# is parked it YIELDS the lock (flock -u releases the shared open file
		# description), so a gate that takes the lock is never waiting on a job
		# the bracket itself froze; it takes the lock back before the job runs
		# again. Its own sleeps close fd 9 so a stray sleep cannot pin the lock.
		while kill -0 "$job" 2>/dev/null; do
			if [ -e "$PAUSE_FILE" ]; then
				if [ ! -e "$LEASE.parked" ]; then
					sleep "$PAUSE_GRACE_S" 9>&-
					# The pause may have ended during the grace (a short gate).
					# Do not stop a job that is no longer asked to pause.
					[ -e "$PAUSE_FILE" ] || continue
					kill -0 "$job" 2>/dev/null || break
					kill -STOP "-$job" 2>/dev/null || kill -STOP "$job" 2>/dev/null || true
					: >"$LEASE.parked"
					flock -u 9
				fi
			elif [ -e "$LEASE.parked" ]; then
				# Wait for the lock in 1 s steps so a job that died while parked is
				# still noticed by the loop condition. The job stays STOPPED until
				# the lock is ours again.
				if flock -w 1 9; then
					kill -CONT "-$job" 2>/dev/null || true
					rm -f "$LEASE.parked"
				fi
			fi
			sleep 1 9>&-
		done
		rm -f "$LEASE.parked"
	) &
	SUPERVISOR=$!
fi

wait "$job"
rc=$?
[ -n "${SUPERVISOR:-}" ] && kill "$SUPERVISOR" 2>/dev/null
printf 'heavy.sh: %s finished rc=%s\n' "$NAME" "$rc"
exit "$rc"
