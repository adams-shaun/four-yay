#!/usr/bin/env bash
# broker.sh — the resource arbiter that keeps heavy work from starving a gate.
#
# Design: docs/superpowers/specs/2026-09-29-reward-loop-and-seed-agent-design.md §3.
#
# Three classes, three cgroup slices. `gate` (gates, merges, landings) is never
# paused and never capped below what it asks for. `probe` (reward measurement)
# and `heavy` (training, gauntlets, sweeps, cardfuzz) are preemptible and run
# only in headroom this script grants. cgroups do the bounding; the pause
# contract below handles the one thing cgroups cannot do, which is stopping a
# job without losing it.
#
#   broker.sh status [--json]     what the box looks like right now
#   broker.sh may-i <class>       exit 0 if <class> may start now, 1 with a reason
#   broker.sh gate-begin [tag]    a gate is starting: pause every heavy lease
#   broker.sh gate-end [tag]      the gate finished: resume
#   broker.sh pause-all [class]   pause a class (default heavy)
#   broker.sh resume-all [class]
#   broker.sh enforce             escalate overdue pauses, kill on memory floor
#   broker.sh reap                drop leases whose process is gone
#
# Pausing is two-tier and deliberately in this order:
#   1. cooperative — touch the lease's pause file; a job with a step loop
#      checkpoints and sleeps until it disappears. Nothing is lost.
#   2. SIGSTOP — after PAUSE_GRACE_S, for a job that ignored its pause file.
#      SIGSTOP holds memory, so it is a CPU remedy only.
# A lease that must go for MEMORY is killed, and the kill is recorded on the
# stability axis as a broker action (not as an OOM, which it prevented).
set -uo pipefail

ROOT=${GORGE_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}
STATE=${GORGE_REWARD_DIR:-$ROOT/.ds4/reward}
LEASES=$STATE/leases
GATEFLAG=$STATE/gate-active
SCORE=$STATE/scoreboard.jsonl
# The HEAVY lock is defined once, in heavy_lock.sh.
. "$(dirname "$0")/heavy_lock.sh"

# Floors in MiB of AVAILABLE memory (not free: page cache is reclaimable).
# The box had ~58 GiB until 2026-10-09, when the operator doubled DRAM to
# 120 GiB; with the heavy pool now at GORGE_HEAVY_LANES=3 lanes the worst
# concurrent heavy footprint is three 16 GiB-class jobs, so the START floor
# rises to 28 GiB (2026-10-09 cli-20261009T224425Z-a610bfaf: one more
# 16 GiB-class slot on top of the 2-lane 20 GiB floor). A gate's test
# binaries have peaked near 8 GiB, and two OOMs here (2026-09-27) came from a
# heavy job holding memory while a gate started, so heavy still needs a wide
# margin to START and a narrow one to be KILLED.
HEAVY_START_FLOOR_MB=${HEAVY_START_FLOOR_MB:-28672}
PROBE_START_FLOOR_MB=${PROBE_START_FLOOR_MB:-10240}
KILL_FLOOR_MB=${KILL_FLOOR_MB:-3072}
PAUSE_GRACE_S=${PAUSE_GRACE_S:-60}
# 3 = GORGE_HEAVY_LANES: each concurrent lease holds its own lane
# (scripts/heavy_lock.sh), so the cap and the lane count must move together.
HEAVY_MAX_LEASES=${HEAVY_MAX_LEASES:-3}
PROBE_MAX_LEASES=${PROBE_MAX_LEASES:-1}
# Load ceiling as a fraction of the core count; a box already saturated by
# seats and gates does not get a heavy job on top.
LOAD_CEIL_FRAC=${LOAD_CEIL_FRAC:-0.75}

mkdir -p "$LEASES"

now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
epoch() { date +%s; }
say() { printf 'broker: %s\n' "$*"; }
die() { printf 'broker: %s\n' "$*" >&2; exit 2; }

avail_mb() { awk '/^MemAvailable:/ {printf "%d", $2/1024}' /proc/meminfo; }
load1() { awk '{print $1}' /proc/loadavg; }
ncpu() { nproc; }
vmstat_val() { awk -v k="$1" '$1==k {print $2}' /proc/vmstat; }

# record_stability appends one stability row so a broker intervention is
# visible to reward.py's veto rather than only in this script's stderr.
record_stability() {
	local metric=$1 value=$2 note=${3:-}
	local head
	head=$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)
	mkdir -p "$(dirname "$SCORE")"
	printf '{"ts":"%s","git_head":"%s","axis":"stability","metric":"%s","value":%s,"cost_s":0,"cmd":"broker.sh","note":"%s"}\n' \
		"$(now)" "$head" "$metric" "$value" "$note" >>"$SCORE"
}

lease_files() {
	local class=${1:-}
	shopt -s nullglob
	local f
	for f in "$LEASES"/*.json; do
		if [ -n "$class" ]; then
			grep -q "\"class\":\"$class\"" "$f" || continue
		fi
		printf '%s\n' "$f"
	done
}

lease_field() { sed -n "s/.*\"$2\":\"\([^\"]*\)\".*/\1/p" "$1" | head -1; }
lease_pid() { sed -n 's/.*"pid":\([0-9]*\).*/\1/p' "$1" | head -1; }

alive() { [ -n "${1:-}" ] && kill -0 "$1" 2>/dev/null; }

reap() {
	local f pid n=0
	while IFS= read -r f; do
		pid=$(lease_pid "$f")
		if ! alive "$pid"; then
			rm -f "$f" "$(lease_field "$f" pause_file)" "$f.paused_at" "$f.stopped" "$f.supervised" "$f.parked"
			n=$((n + 1))
		fi
	done < <(lease_files)
	[ "$n" -gt 0 ] && say "reaped $n dead lease(s)"
	return 0
}

# gate_active also EXPIRES a stale flag. The release half of the bracket is a
# post-gates hook, so a crashed daemon, a killed tick or a halt can leave the
# flag behind -- and a stranded flag would hold every heavy job paused forever,
# which is its own kind of starvation. A flag older than the TTL is treated as
# abandoned: it is removed and heavy work resumes.
GATE_FLAG_TTL_S=${GATE_FLAG_TTL_S:-1800}
gate_active() {
	[ -e "$GATEFLAG" ] || return 1
	local age
	age=$(($(epoch) - $(stat -c %Y "$GATEFLAG" 2>/dev/null || echo 0)))
	if [ "$age" -gt "$GATE_FLAG_TTL_S" ]; then
		say "gate flag is ${age}s old (TTL ${GATE_FLAG_TTL_S}s): treating it as abandoned and resuming"
		rm -f "$GATEFLAG"
		resume_class heavy
		resume_class probe
		return 1
	fi
	return 0
}

pause_class() {
	local class=${1:-heavy} f pf pid n=0
	while IFS= read -r f; do
		pf=$(lease_field "$f" pause_file)
		pid=$(lease_pid "$f")
		alive "$pid" || continue
		[ -n "$pf" ] && : >"$pf"
		# Stamp when the pause was requested so enforce can time the grace.
		printf '%s\n' "$(epoch)" >"$f.paused_at"
		n=$((n + 1))
	done < <(lease_files "$class")
	say "paused $n $class lease(s)"
}

resume_class() {
	local class=${1:-heavy} f pf pid n=0
	while IFS= read -r f; do
		pf=$(lease_field "$f" pause_file)
		pid=$(lease_pid "$f")
		[ -n "$pf" ] && rm -f "$pf"
		rm -f "$f.paused_at"
		# A heavy.sh-supervised lease is continued by its own supervisor, which
		# first takes the HEAVY lock (heavy_lock.sh) back -- the lease yields it
		# while parked. CONTinuing it here would run the job without the lock and
		# drop the `.parked` marker the supervisor needs to re-take it.
		if [ ! -e "$f.supervised" ]; then
			rm -f "$f.parked"
			# Undo a SIGSTOP escalation if enforce applied one.
			alive "$pid" && kill -CONT "-$pid" 2>/dev/null || true
		fi
		n=$((n + 1))
	done < <(lease_files "$class")
	say "resumed $n $class lease(s)"
}

enforce() {
	reap
	local f pid at waited pf
	# 1. A paused lease that is still running after the grace gets SIGSTOP.
	#    A HEAVY lease launched through heavy.sh carries a `.supervised` marker:
	#    heavy.sh itself stops that job's group (after the same grace, so a
	#    checkpoint loop still parks first), so the pause is already handled and
	#    escalating it here would only record `gate_starved_minutes` for a job
	#    that WAS pausable -- a full stability veto for a handled pause
	#    (2026-10-03 head 2d4e01009). Defer to the supervisor; the broker's
	#    fallback still covers probe leases and any lease without the marker.
	while IFS= read -r f; do
		[ -e "$f.paused_at" ] || continue
		[ -e "$f.stopped" ] && continue
		if [ -e "$f.supervised" ]; then
			say "$(lease_field "$f" class) pid $(lease_pid "$f") is supervised by heavy.sh; not escalating"
			continue
		fi
		pid=$(lease_pid "$f")
		alive "$pid" || continue
		at=$(cat "$f.paused_at" 2>/dev/null || echo 0)
		waited=$(($(epoch) - at))
		[ "$waited" -lt "$PAUSE_GRACE_S" ] && continue
		kill -STOP "-$pid" 2>/dev/null || kill -STOP "$pid" 2>/dev/null || continue
		: >"$f.stopped"
		say "SIGSTOP $(lease_field "$f" class) pid $pid after ${waited}s (ignored its pause file)"
		record_stability gate_starved_minutes "$(awk -v w="$waited" 'BEGIN{printf "%.2f", w/60}')" "sigstop-escalation"
	done < <(lease_files)

	# 2. Below the kill floor, a held heavy lease is worse than no lease: its
	#    memory is what an OOM would take from the gate. Kill the newest one.
	local mb
	mb=$(avail_mb)
	if [ "$mb" -lt "$KILL_FLOOR_MB" ]; then
		local newest="" newest_t=0 t
		while IFS= read -r f; do
			t=$(stat -c %Y "$f" 2>/dev/null || echo 0)
			if [ "$t" -ge "$newest_t" ]; then
				newest=$f
				newest_t=$t
			fi
		done < <(lease_files heavy)
		if [ -n "$newest" ]; then
			pid=$(lease_pid "$newest")
			say "available memory ${mb}MiB under ${KILL_FLOOR_MB}MiB: killing heavy pid $pid"
			kill -CONT "-$pid" 2>/dev/null || true
			kill -TERM "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
			sleep 5
			alive "$pid" && { kill -KILL "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null; }
			record_stability broker_kills 1 "memory floor ${mb}MiB"
			rm -f "$newest" "$newest.paused_at" "$newest.stopped" "$newest.parked"
		else
			say "available memory ${mb}MiB under floor and no heavy lease to shed"
		fi
	fi
}

may_i() {
	local class=${1:?usage: broker.sh may-i <gate|probe|heavy>}
	reap
	case $class in
	gate)
		echo "yes: gate is never gated"
		return 0
		;;
	probe | heavy) ;;
	*) die "unknown class $class" ;;
	esac
	if gate_active; then
		echo "no: a gate is in flight ($(cat "$GATEFLAG" 2>/dev/null))"
		return 1
	fi
	local mb floor held cap
	mb=$(avail_mb)
	if [ "$class" = heavy ]; then
		floor=$HEAVY_START_FLOOR_MB
		cap=$HEAVY_MAX_LEASES
	else
		floor=$PROBE_START_FLOOR_MB
		cap=$PROBE_MAX_LEASES
	fi
	if [ "$mb" -lt "$floor" ]; then
		echo "no: available memory ${mb}MiB below ${floor}MiB floor for $class"
		return 1
	fi
	held=$(lease_files "$class" | wc -l)
	if [ "$held" -ge "$cap" ]; then
		echo "no: $held $class lease(s) already held (cap $cap)"
		return 1
	fi
	# Heavy work also waits for CPU headroom; a probe is short enough to ride
	# out a busy minute, and blocking it would starve the reward ledger.
	if [ "$class" = heavy ]; then
		local ceil
		ceil=$(awk -v n="$(ncpu)" -v f="$LOAD_CEIL_FRAC" 'BEGIN{printf "%.2f", n*f}')
		if awk -v l="$(load1)" -v c="$ceil" 'BEGIN{exit !(l>c)}'; then
			echo "no: load $(load1) above ceiling $ceil"
			return 1
		fi
	fi
	echo "yes: ${mb}MiB available, $held/$cap $class lease(s), load $(load1)"
	return 0
}

status() {
	reap
	local json=0
	[ "${1:-}" = "--json" ] && json=1
	local mb ga leases n
	mb=$(avail_mb)
	ga=false
	gate_active && ga=true
	n=$(lease_files | wc -l)
	if [ "$json" = 1 ]; then
		printf '{"ts":"%s","avail_mb":%s,"load1":%s,"ncpu":%s,"gate_active":%s,"oom_kill_total":%s,"pswpin_total":%s,"leases":[' \
			"$(now)" "$mb" "$(load1)" "$(ncpu)" "$ga" "$(vmstat_val oom_kill)" "$(vmstat_val pswpin)"
		local first=1 f
		while IFS= read -r f; do
			[ "$first" = 1 ] || printf ','
			first=0
			printf '{"class":"%s","pid":%s,"paused":%s,"stopped":%s,"cmd":"%s"}' \
				"$(lease_field "$f" class)" "$(lease_pid "$f")" \
				"$([ -e "$f.paused_at" ] && echo true || echo false)" \
				"$([ -e "$f.stopped" ] && echo true || echo false)" \
				"$(lease_field "$f" cmd)"
		done < <(lease_files)
		printf ']}\n'
		return 0
	fi
	printf 'available %sMiB  load %s/%s  gate_active %s  leases %s  oom_kill_total %s\n' \
		"$mb" "$(load1)" "$(ncpu)" "$ga" "$n" "$(vmstat_val oom_kill)"
	local f
	while IFS= read -r f; do
		printf '  %-6s pid %-7s %s%s  %s\n' "$(lease_field "$f" class)" "$(lease_pid "$f")" \
			"$([ -e "$f.paused_at" ] && echo PAUSED || echo running)" \
			"$([ -e "$f.stopped" ] && echo '+STOPPED' || echo '')" \
			"$(lease_field "$f" cmd)"
	done < <(lease_files)
}

cmd=${1:-status}
shift || true
case $cmd in
status) status "$@" ;;
heavy-lock) printf '%s\n' "$GORGE_HEAVY_LOCK" ;;
may-i) may_i "$@" ;;
gate-begin)
	# The HEAVY lock (heavy_lock.sh) is NOT taken here: a gate bracket pauses the
	# leases that hold it, and heavy.sh's supervisor yields the lock while a
	# lease is parked, so the gate that follows can take it. enforce needs no
	# lock either: a killed lease closes its fd and so releases it.
	# Both preemptible classes yield: a probe competes with a gate for memory
	# exactly as a training run does, and a gate is short.
	printf '%s %s\n' "$(now)" "${1:-gate}" >"$GATEFLAG"
	pause_class heavy
	pause_class probe
	;;
gate-end)
	rm -f "$GATEFLAG"
	resume_class heavy
	resume_class probe
	;;
pause-all) pause_class "${1:-heavy}" ;;
resume-all) resume_class "${1:-heavy}" ;;
enforce) enforce ;;
reap) reap ;;
*) die "usage: broker.sh {status|heavy-lock|may-i <class>|gate-begin|gate-end|pause-all|resume-all|enforce|reap}" ;;
esac
