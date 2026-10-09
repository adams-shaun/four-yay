#!/usr/bin/env bash
# heavy_lock.sh — the ONE definition of the HEAVY-class lane semaphore
# (operator, 2026-10-05; lanes 2026-10-09).
#
# Every heavy runner draws a lane from the same pool: scripts/heavy.sh,
# scripts/postmerge_batch.sh (the full suite and every bisect step),
# scripts/sb-gauntlet.sh, scripts/m1b-distill.sh, the agentctl "go test
# (affected)" gate, scripts/driver_replay_batch.sh. (`make ledger` has its own
# lock, scripts/ledger-lane.sh.) Seats stay unlocked (they are capped
# separately). The pool has GORGE_HEAVY_LANES lanes (default 2, raised from a
# single lock when the box went from 64 to 120 GiB, so a per-ticket gate and
# the post-merge suite — or a suite and a heavy lease — can run at the same
# time). Lane 1 IS the old single lock file, so a caller from a base that
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
# heavy.sh leases YIELD their lane while a broker gate bracket has them
# parked (flock -u 9 on the lane fd), so a gate that takes a lane is never
# waiting on a job the bracket itself froze.
GORGE_HEAVY_LANES=${GORGE_HEAVY_LANES:-2}
if [ -z "${GORGE_HEAVY_LOCK:-}" ]; then
	_hl_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
	_hl_common=$(git -C "$_hl_dir" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || _hl_common=$_hl_dir/../.git
	GORGE_HEAVY_LOCK=$(dirname "$_hl_common")/.ds4/heavy.lock
	unset _hl_dir _hl_common
fi
export GORGE_HEAVY_LOCK GORGE_HEAVY_LANES

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
		*)
			printf 'heavy_lock.sh: unknown option %s\n' "$1" >&2
			return 2
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
		GORGE_HEAVY_LOCK_HELD=$GORGE_HEAVY_LOCK "$@" 9>&-
		local rc=$?
		exec 9>&- # the lane is this shell's until fd 9 closes; release it now
		return $rc
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
		# Foreground child, NOT exec: exec'ing with 9>&- would close the lane fd
		# and RELEASE the flock at exec time (a flock dies with the last fd of
		# its open description). The lane is this shell's until the child exits;
		# `flock -o` had the same shape (cmd was flock(1)'s child there).
		if gorge_heavy_acquire ${wait_s:+-w "$wait_s"}; then
			GORGE_HEAVY_LOCK_HELD=$GORGE_HEAVY_LOCK "$@" 9>&-
			rc=$?
			exec 9>&-
			exit $rc
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
