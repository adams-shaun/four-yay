#!/usr/bin/env bash
# heavy_lock.sh — the ONE definition of the HEAVY-class lock (operator, 2026-10-05).
#
# Every heavy runner serialises on the same flock file: scripts/heavy.sh,
# scripts/postmerge_batch.sh (the full suite and every bisect step),
# scripts/sb-gauntlet.sh, scripts/m1b-distill.sh, the agentctl "go test
# (affected)" gate and the post-merge `make ledger` hook. Seats stay unlocked
# (they are capped separately). The path is <main checkout>/.ds4/heavy.lock,
# derived from the shared git dir so a worktree copy of a script resolves to the
# same file as the main one. GORGE_HEAVY_LOCK overrides it (tests do).
#
# Sourced:   . scripts/heavy_lock.sh        # sets and exports GORGE_HEAVY_LOCK
# Executed:
#   scripts/heavy_lock.sh path              # print the lock path
#   scripts/heavy_lock.sh run [-w SECS] [-s] -- <cmd> [args...]
#       run <cmd> holding the lock (flock -o: the fd never reaches <cmd>).
#       -w SECS  wait at most SECS for the lock (default: wait forever)
#       -s       if the LOCK WAIT runs out, log it and exit 0 (the work is
#                regenerable, e.g. make ledger) instead of exiting 75
#       exit 75 = the lock was not obtained in time; <cmd> never ran.
#       The timeout is distinguished from a command that itself exits 75 by a
#       start marker the wrapper removes just before it execs <cmd>.
#
# A hand session wraps its heavy runs the same way, so it contends with the
# fleet instead of beside it:
#   scripts/heavy_lock.sh run -- systemd-run --user --scope -q -p MemoryMax=2G go test ...
#
# heavy.sh leases YIELD this lock while a broker gate bracket has them parked,
# so a gate that takes it is never waiting on a job the bracket itself froze.
if [ -z "${GORGE_HEAVY_LOCK:-}" ]; then
	_hl_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
	_hl_common=$(git -C "$_hl_dir" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || _hl_common=$_hl_dir/../.git
	GORGE_HEAVY_LOCK=$(dirname "$_hl_common")/.ds4/heavy.lock
	unset _hl_dir _hl_common
fi
export GORGE_HEAVY_LOCK

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	case ${1:-} in
	path) printf '%s\n' "$GORGE_HEAVY_LOCK" ;;
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
		mkdir -p "$(dirname "$GORGE_HEAVY_LOCK")"
		# flock -E 75 conflates "lock wait timed out" with "the command itself
		# exited 75", and the `-s` caller (post_merge `make ledger`) would then
		# silently swallow a real failure whose status happens to be 75. So the
		# command runs under a wrapper that removes a START marker just before
		# exec: a timed-out wait leaves the marker PRESENT, a command that ran
		# (and exited 75) removes it. The timeout is judged by the marker, never
		# by rc alone.
		started=$(mktemp "${TMPDIR:-/tmp}/heavy-lock.started.XXXXXX")
		flock_args=(-o -E 75)
		[ -n "$wait_s" ] && flock_args+=(-w "$wait_s")
		flock "${flock_args[@]}" "$GORGE_HEAVY_LOCK" bash -c 'rm -f -- "$1"; shift; exec "$@"' _ "$started" "$@"
		rc=$?
		# The wrapper removes the marker before it execs the command, so the marker
		# is ABSENT iff <cmd> really ran; a timed-out wait leaves it PRESENT.
		timed_out=0
		[ -e "$started" ] && timed_out=1
		rm -f -- "$started"
		if [ "$rc" = 75 ] && [ "$timed_out" = 1 ]; then
			printf 'heavy_lock.sh: %s still held after %ss; not run: %s\n' "$GORGE_HEAVY_LOCK" "${wait_s:-0}" "$1" >&2
			[ "$skip" = 1 ] && exit 0
		fi
		exit "$rc"
		;;
	*)
		printf 'usage: heavy_lock.sh {path | run [-w SECS] [-s] -- cmd...}\n' >&2
		exit 2
		;;
	esac
fi
