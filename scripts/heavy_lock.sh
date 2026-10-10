#!/usr/bin/env bash
# heavy_lock.sh — the ONE definition of the HEAVY-class lane semaphore
# (operator, 2026-10-05; lanes 2026-10-09).
#
# Every heavy runner draws a lane from the same pool: scripts/heavy.sh,
# scripts/postmerge_batch.sh (the full suite and every bisect step),
# scripts/sb-gauntlet.sh, scripts/m1b-distill.sh, the agentctl "go test
# (affected)" gate, scripts/driver_replay_batch.sh. (`make ledger` has its own
# lock, scripts/ledger-lane.sh.) Seats stay unlocked (they are capped
# separately). The pool has GORGE_HEAVY_LANES lanes (default 3, raised from a
# single lock when the box went from 64 to 120 GiB, so a per-ticket gate and
# the post-merge suite — or a suite and a heavy lease — can run at the same
# time; 2026-10-09 cli-20261009T224425Z-a610bfaf raised 2 -> 3: the batch
# cadence runs the full suite back to back with ~60 s gaps (each holding a
# lane 235-452 s), so a second heavy job (driver_replay_batch, another seat's
# gate) already made both lanes busy and the per-ticket gate paid its wait
# inside its own gate wall — the measured gate_wall_s pole was the WAIT, not
# the gate's test work: 3 of 5 gate runs on 2026-10-09 16:14-16:50 spent
# 245-330 s waiting for a lane before 20-125 s of work). Lane 1 IS the old
# single lock file, so a caller from a base that
# predates the lanes (an in-flight branch's own copy of a runner) still
# contends with every lane-1 holder exactly as before; only lane 2 is new.
# The base path is <main checkout>/.ds4/heavy.lock, derived from the shared
# git dir so a worktree copy of a script resolves to the same pool as the
# main one; lanes beyond 1 are <path>.laneN next to it. GORGE_HEAVY_LOCK
# overrides the base path (tests do). This file is the ONE definition; the
# affected gate cannot `git show {base}:...` a NEWER form of it (a base that
# predates this file has none at all), so it reproduces the lane acquisition
# inline — see .agentctl/config.toml.
#
# Sourced:   . scripts/heavy_lock.sh        # exports GORGE_HEAVY_LOCK and
#                                          # GORGE_HEAVY_LANES, defines the
#                                          # gorge_heavy_* helpers below
# Executed:
#   scripts/heavy_lock.sh path              # print the pool's base path
#   scripts/heavy_lock.sh lanes             # print every lane file, lane 1 first
#   scripts/heavy_lock.sh run [-w SECS] [-s] -- <cmd> [args...]
#       run <cmd> holding one lane (the lane fd never reaches <cmd>).
#       -w SECS  wait at most SECS for a lane (default: wait forever)
#       -s       if the lane WAIT runs out, log it and exit 0 (the work is
#                regenerable, e.g. make ledger) instead of exiting 75
#       exit 75 = no lane was free in time; <cmd> never ran.
#       <cmd> sees GORGE_HEAVY_LOCK_HELD=<base path>; a nested `run` (or any
#       script that checks it, e.g. ledger-lane.sh) then runs without locking
#       again instead of waiting on its own ancestor.
#
# A hand session wraps its heavy runs the same way, so it contends with the
# fleet instead of beside it:
#   scripts/heavy_lock.sh run -- systemd-run --user --scope -q -p MemoryMax=4G go test ...
#
# Sourced helpers (a command substitution would run them in a subshell whose
# fd 9 dies with it, so they are written for the CURRENT shell):
#   gorge_heavy_lanes          print the lane files, lane 1 first
#   gorge_heavy_acquire [-w S] take a free lane on fd 9 of THIS shell; on
#                              success GORGE_HEAVY_LANE names it and it is
#                              flocked; on timeout exit 75 (stderr note)
#   gorge_heavy_run [-w S] [-s] -- cmd...
#                              run cmd holding one lane (as `run` above);
#                              for sourced callers like m1b-distill.sh's
#                              heavy() wrapper, where exec would kill the
#                              caller, cmd runs as a child instead
#
# EVERY heavy runner YIELDS its lane while a broker gate bracket is active:
# heavy.sh leases park their job (heavy.sh:208-243), and gorge_heavy_run / the
# `run` exec branch park their child the same way from gorge_heavy_supervise
# below. The lane fd is unlocked (flock -u 9) ONLY while the child process
# group is STOPped, and re-taken (flock -w 1 9) before it is continued, so a
# gate that takes a lane is never waiting on a job the bracket itself froze.
# The bracket is the broker's gate-active flag (scripts/broker.sh:34), watched
# at the same path from every worktree (gorge_heavy_gate_active). A fresh lane
# is not TAKEN while the flag is live (gorge_heavy_acquire defers), so a bisect
# step or gauntlet starting mid-bracket cannot steal the gate's lane. The
# gate's OWN command is the inline acquisition in .agentctl/config.toml, never
# this file, so the deferral can never deadlock the gate.
# The child runs in its own process group (so it can be STOPped without
# stopping the wrapper); the wrapper therefore FORWARDS INT/TERM/HUP to that
# group (gorge_heavy_forward), or a Ctrl-C / `timeout` would orphan the job
# with no lane, and then RE-RAISES the signal on the caller once the job is
# reaped and the lane released, so a sourcing caller dies (or runs its own trap)
# exactly as it did before the wrapper existed. A SIGKILLed wrapper cannot: its
# child and supervisor keep running, and the supervisor resumes a parked child
# when the bracket clears.
GORGE_HEAVY_LANES=${GORGE_HEAVY_LANES:-3}
# The grace a gate bracket gets before a runner's child is STOPped, so a child
# with its own checkpoint loop can park first. Its OWN knob, NOT heavy.sh's
# PAUSE_GRACE_S: heavy.sh sources this file BEFORE it sets PAUSE_GRACE_S, so a
# shared name would be silently overwritten by whichever ran second.
GORGE_HEAVY_PAUSE_GRACE_S=${GORGE_HEAVY_PAUSE_GRACE_S:-60}
if [ -z "${GORGE_HEAVY_LOCK:-}" ] || [ -z "${GORGE_HEAVY_GATE_FLAG:-}" ]; then
	_hl_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
	_hl_common=$(git -C "$_hl_dir" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || _hl_common=$_hl_dir/../.git
	[ -n "${GORGE_HEAVY_LOCK:-}" ] || GORGE_HEAVY_LOCK=$(dirname "$_hl_common")/.ds4/heavy.lock
	# The broker's gate-active flag, derived from the shared git common dir so
	# every worktree watches the SAME flag. GORGE_REWARD_DIR overrides the
	# directory exactly as broker.sh's STATE does; the 1800 s TTL is applied in
	# gorge_heavy_gate_active below.
	[ -n "${GORGE_HEAVY_GATE_FLAG:-}" ] || GORGE_HEAVY_GATE_FLAG=${GORGE_REWARD_DIR:-$(dirname "$_hl_common")/.ds4/reward}/gate-active
	unset _hl_dir _hl_common
fi
export GORGE_HEAVY_LOCK GORGE_HEAVY_LANES GORGE_HEAVY_GATE_FLAG

gorge_heavy_lanes() {
	local i
	for ((i = 1; i <= GORGE_HEAVY_LANES; i++)); do
		if [ "$i" = 1 ]; then
			printf '%s\n' "$GORGE_HEAVY_LOCK"
		else
			printf '%s.lane%s\n' "$GORGE_HEAVY_LOCK" "$i"
		fi
	done
}

# gorge_heavy_gate_active: the broker's gate bracket is open. Same flag path
# and 1800 s stale-flag TTL as scripts/enginebench-lib.sh:29-38 and
# scripts/broker.sh's gate_active -- a flag older than the TTL is abandoned (a
# crashed daemon can strand one) and treated as inactive. broker.sh owns
# removing a stale flag; this helper only reads it.
gorge_heavy_gate_active() {
	[ -e "$GORGE_HEAVY_GATE_FLAG" ] || return 1
	[ $(($(date +%s) - $(stat -c %Y "$GORGE_HEAVY_GATE_FLAG" 2>/dev/null || echo 0))) -le 1800 ]
}

# gorge_heavy_supervise PID: park PID's process group while a gate bracket is
# open, yielding the lane on fd 9 for exactly that window. Runs in a background
# subshell (callers start it with `&`) that SHARES fd 9 with its caller, so
# flock -u/-w move the one open file description the lane lock lives on. The
# shape is heavy.sh's lease supervisor (heavy.sh:208-243) with the per-lease
# pause file replaced by gorge_heavy_gate_active. INVARIANT: the lane is
# released ONLY while the child group is STOPped (a job running beside a lane
# holder is the bug), and it is re-taken before the group is continued.
#
# The supervisor holds the lane's open file description too, so the lane is not
# free until it exits: every wait in it is a backgrounded command it `wait`s
# on, so the caller's SIGTERM is acted on at once (bash defers a trap until a
# FOREGROUND command returns, which would keep a finished job's lane for up to
# a whole grace period). Plain sleeps carry 9>&- so a stray one cannot pin it.
gorge_heavy_supervise() {
	_hl_job=$1 _hl_parked=0 _hl_bg=""
	# This runs in a background subshell that INHERITS the caller's shell
	# options (sb-gauntlet.sh runs `set -euo pipefail`), so disable errexit
	# here: a flock/kill probe returning non-zero must not abort the supervisor
	# and leave the child STOPped with no one to resume it.
	set +e
	# Orphan safety: whenever this supervisor ends while the child is parked
	# (signalled by its caller, or the caller's shell died and took the group
	# with it) it re-takes the lane best-effort and continues the child, so it
	# cannot stay frozen forever. If the PARENT is SIGKILLed the supervisor is
	# not signalled -- it keeps running and resumes the child when the bracket
	# clears, holding the lane until the child exits.
	trap 'exit 0' TERM INT HUP
	trap '_hl_supervise_done' EXIT
	while kill -0 "$_hl_job" 2>/dev/null; do
		if gorge_heavy_gate_active; then
			if [ "$_hl_parked" = 0 ]; then
				sleep "$GORGE_HEAVY_PAUSE_GRACE_S" 9>&- &
				_hl_bg=$!
				wait "$_hl_bg"
				# The bracket may have ended during the grace (a short gate);
				# do not stop a child that is no longer asked to pause.
				gorge_heavy_gate_active || continue
				kill -0 "$_hl_job" 2>/dev/null || break
				kill -STOP "-$_hl_job" 2>/dev/null || kill -STOP "$_hl_job" 2>/dev/null || true
				_hl_parked=1
				flock -u 9 || true
			fi
		elif [ "$_hl_parked" = 1 ]; then
			# Re-acquire in 1 s steps so a child that died while parked is
			# still noticed by the loop condition; it stays STOPPED until the
			# lane is ours again.
			flock -w 1 9 &
			_hl_bg=$!
			if wait "$_hl_bg"; then
				kill -CONT "-$_hl_job" 2>/dev/null || kill -CONT "$_hl_job" 2>/dev/null || true
				_hl_parked=0
			fi
		fi
		sleep 1 9>&- &
		_hl_bg=$!
		wait "$_hl_bg"
	done
}

_hl_supervise_done() {
	[ -z "$_hl_bg" ] || kill "$_hl_bg" 2>/dev/null
	if [ "$_hl_parked" = 1 ] && kill -0 "$_hl_job" 2>/dev/null; then
		flock -w 2 9 2>/dev/null
		kill -CONT "-$_hl_job" 2>/dev/null || kill -CONT "$_hl_job" 2>/dev/null
	fi
	return 0
}

# gorge_heavy_forward SIG JOB SUPERVISOR: the wrapper was signalled. The child
# runs in its OWN process group (job control, so the supervisor can STOP it
# without hitting this shell), which a group-directed signal to the wrapper
# (Ctrl-C, `timeout`) no longer reaches -- forward it, or the job is orphaned
# running with no lane. The supervisor is stopped first (its exit path re-takes
# the lane and continues a parked child) so it cannot STOP the dying child
# again. Every command is `|| true`: this runs inside a trap under set -e.
gorge_heavy_forward() {
	local sig=$1 job=$2 supervisor=$3
	kill -TERM "$supervisor" 2>/dev/null || true
	wait "$supervisor" 2>/dev/null || true
	kill "-$sig" -- "-$job" 2>/dev/null || kill "-$sig" "$job" 2>/dev/null || true
	kill -CONT -- "-$job" 2>/dev/null || kill -CONT "$job" 2>/dev/null || true
	return 0
}

# gorge_heavy_exec cmd...: run cmd as a supervised BACKGROUND child of this
# shell, which already holds a lane on fd 9, and return cmd's status. Both
# gorge_heavy_run and the `run` exec branch go through here. It closes fd 9
# (the lane is this shell's until then) before returning.
gorge_heavy_exec() {
	local had_m=0 job="" supervisor="" rc pending="" caught="" old_traps
	case $- in *m*) had_m=1 ;; esac
	old_traps=$(trap -p INT TERM HUP)
	# Installed BEFORE the fork, so a signal in the gap is not lost: until the
	# child exists the handler only records it. `caught` remembers the signal so
	# it can be RE-RAISED on the caller below: these traps replace the caller's
	# disposition, and a caller with no trap of its own (postmerge_batch.sh,
	# sb-gauntlet.sh, m1b-distill.sh) must still die on `kill -TERM`, not carry
	# on and misread the TERM-killed job as a failed suite.
	trap 'caught=INT; if [ -n "$job" ]; then gorge_heavy_forward INT "$job" "$supervisor"; else pending=INT; fi' INT
	trap 'caught=TERM; if [ -n "$job" ]; then gorge_heavy_forward TERM "$job" "$supervisor"; else pending=TERM; fi' TERM
	trap 'caught=HUP; if [ -n "$job" ]; then gorge_heavy_forward HUP "$job" "$supervisor"; else pending=HUP; fi' HUP
	# Background the child so a supervisor can STOP/continue its process group;
	# job control gives it its own group (heavy.sh:176-182). The child never
	# gets fd 9; the supervisor subshell keeps it (shared) so flock -u/-w move
	# the lane lock.
	set -m
	GORGE_HEAVY_LOCK_HELD=$GORGE_HEAVY_LOCK "$@" 9>&- &
	job=$!
	[ "$had_m" = 1 ] || set +m
	gorge_heavy_supervise "$job" &
	supervisor=$!
	[ -z "$pending" ] || gorge_heavy_forward "$pending" "$job" "$supervisor"
	# A trapped signal interrupts `wait`, so loop until the child is reaped.
	# `if wait` keeps a failing child from tripping a set -e caller.
	while :; do
		if wait "$job"; then rc=0; else rc=$?; fi
		kill -0 "$job" 2>/dev/null || break
	done
	# The job is done: stop the supervisor NOW (it holds the lane's open file
	# description, so the lane is not free until it is gone), then release.
	kill -TERM "$supervisor" 2>/dev/null || true
	wait "$supervisor" 2>/dev/null || true
	trap - INT TERM HUP
	[ -z "$old_traps" ] || eval "$old_traps"
	exec 9>&-
	# Re-raise a signal this wrapper swallowed, now that the child is reaped, the
	# lane is released and the caller's own traps are back: the default
	# disposition (or the caller's trap) then runs exactly as it would have
	# without the wrapper. BASHPID, not $$, so a `( ... gorge_heavy_run ... )`
	# subshell dies rather than its parent. If the caller's trap returns (or it
	# ignores the signal), carry on and return the child's status.
	[ -z "$caught" ] || kill -s "$caught" "$BASHPID" 2>/dev/null || true
	return $rc
}

gorge_heavy_acquire() { # [-w SECS]: one free lane on fd 9, or exit 75
	local wait=""
	if [ "${1:-}" = -w ]; then
		wait=$2
	fi
	# Re-entrant: a command already running under this pool (marked by
	# GORGE_HEAVY_LOCK_HELD, exported by run/gorge_heavy_run) would otherwise
	# wait on its own ancestor until -w ran out — the post_merge `make ledger`
	# hook did exactly that through ledger-lane.sh, stalling the suite.
	if [ "${GORGE_HEAVY_LOCK_HELD:-}" = "$GORGE_HEAVY_LOCK" ]; then
		GORGE_HEAVY_LANE=$GORGE_HEAVY_LOCK
		return 0
	fi
	mkdir -p "$(dirname "$GORGE_HEAVY_LOCK")"
	# No -w means wait FOREVER (the old bare `flock` did); with -w the deadline
	# is absolute. Wait=0 is one nonblocking scan, like `flock -w 0`.
	local deadline=0 lane
	[ -n "$wait" ] && deadline=$(( $(date +%s) + wait ))
	while :; do
		# Do not TAKE a fresh lane while a gate bracket is open: a bisect step
		# or gauntlet starting mid-bracket must not steal the gate's lane. The
		# gate's own command is the INLINE acquisition in
		# .agentctl/config.toml, never this helper, so deferring here can never
		# deadlock the gate -- do NOT move the gate onto this helper.
		if ! gorge_heavy_gate_active; then
			while read -r lane; do
				# `: >>` first: a plain command's redirection failure is catchable,
				# where a failed `exec 9>` would exit this shell outright.
				: >>"$lane" || continue
				exec 9>"$lane"
				if flock -n 9 2>/dev/null; then
					GORGE_HEAVY_LANE=$lane
					return 0
				fi
			done < <(gorge_heavy_lanes)
		fi
		[ "$deadline" = 0 ] || [ "$(date +%s)" -lt "$deadline" ] || return 75
		sleep 0.2 9>&-
	done
}

gorge_heavy_run() { # [-w SECS] [-s] -- cmd...: run cmd holding one lane
	local wait_s="" skip=0
	while [ $# -gt 0 ]; do
		case $1 in
		-w)
			wait_s=$2
			shift 2
			;;
		-s)
			skip=1
			shift
			;;
		--)
			shift
			break
			;;
		-*)
			printf 'heavy_lock.sh: unknown option %s\n' "$1" >&2
			return 2
			;;
		*)
			# First non-option argument: the command starts here. The
			# script callers (sb-gauntlet, m1b-distill, postmerge_batch)
			# pass `gorge_heavy_run systemd-run ...` with no `--`, so an
			# argument that is not an option must end option parsing --
			# treating it as an unknown option reds every heavy runner.
			break
			;;
		esac
	done
	[ $# -gt 0 ] || {
		printf 'heavy_lock.sh run: no command given (did you forget --?)\n' >&2
		return 2
	}
	if [ "${GORGE_HEAVY_LOCK_HELD:-}" = "$GORGE_HEAVY_LOCK" ]; then
		"$@"
		return $?
	fi
	if gorge_heavy_acquire ${wait_s:+-w "$wait_s"}; then
		if gorge_heavy_exec "$@"; then return 0; else return $?; fi
	fi
	printf 'heavy_lock.sh: %s still held after %ss; not run: %s\n' "$GORGE_HEAVY_LOCK" "${wait_s:-0}" "$1" >&2
	[ "$skip" = 1 ] && return 0
	return 75
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	case ${1:-} in
	path) printf '%s\n' "$GORGE_HEAVY_LOCK" ;;
	lanes) gorge_heavy_lanes ;;
	run)
		shift
		wait_s=""
		skip=0
		while [ $# -gt 0 ]; do
			case $1 in
			-w)
				wait_s=$2
				shift 2
				;;
			-s)
				skip=1
				shift
				;;
			--)
				shift
				break
				;;
			*)
				printf 'heavy_lock.sh: unknown option %s\n' "$1" >&2
				exit 2
				;;
			esac
		done
		[ $# -gt 0 ] || {
			printf 'heavy_lock.sh run: no command given (did you forget --?)\n' >&2
			exit 2
		}
		if [ "${GORGE_HEAVY_LOCK_HELD:-}" = "$GORGE_HEAVY_LOCK" ]; then
			exec "$@"
		fi
		# Background child, NOT exec: exec'ing with 9>&- would close the lane fd
		# and RELEASE the flock at exec time (a flock dies with the last fd of
		# its open description). The lane is this shell's until the child exits.
		# Backgrounding (heavy.sh:180) is what lets the supervisor STOP the
		# child's process group and yield the lane while a gate bracket is open.
		if gorge_heavy_acquire ${wait_s:+-w "$wait_s"}; then
			gorge_heavy_exec "$@"
			exit $?
		fi
		printf 'heavy_lock.sh: %s still held after %ss; not run: %s\n' "$GORGE_HEAVY_LOCK" "${wait_s:-0}" "$1" >&2
		[ "$skip" = 1 ] && exit 0
		exit 75
		;;
	*)
		printf 'usage: heavy_lock.sh {path | lanes | run [-w SECS] [-s] -- cmd...}\n' >&2
		exit 2
		;;
	esac
fi
