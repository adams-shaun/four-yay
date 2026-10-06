#!/usr/bin/env bash
# ledger_lane_smoke.sh — `make ledger` must not defeat the Go test cache, and its
# lane run must be capped and serialised on the heavy lock (operator, 2026-10-05).
#
#   scripts/tests/ledger_lane_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d /tmp/ledger-lane.XXXXXX)
trap 'rm -rf "$TMP"' EXIT
fail=0
check() { # check <desc> <cmd...>
	local d=$1
	shift
	if "$@"; then printf 'ok   %s\n' "$d"; else printf 'FAIL %s\n' "$d"; fail=1; fi
}

target=$(sed -n '/^ledger:/,/^$/p' "$ROOT/Makefile")
check "Makefile ledger target has no -count=1" bash -c '! grep -q -- "-count=1" <<<"$1"' _ "$target"
check "Makefile ledger target runs scripts/ledger-lane.sh" grep -q 'scripts/ledger-lane.sh' <<<"$target"
body=$(grep -v '^#' "$ROOT/scripts/ledger-lane.sh")
check "ledger-lane.sh has no -count=1" bash -c '! grep -q -- "-count=1" <<<"$1"' _ "$body"
check "ledger-lane.sh runs under a MemoryMax/CPUQuota scope" grep -q 'MemoryMax=2G -p CPUQuota=200%' <<<"$body"
check "ledger-lane.sh caps GOMAXPROCS/GOMEMLIMIT" grep -q 'GOMAXPROCS=2 GOMEMLIMIT=1536MiB' <<<"$body"

# Contention: while another process holds the lock, the script gives up with
# exit 99 and leaves the previous lane file untouched.
export GORGE_HEAVY_LOCK=$TMP/heavy.lock
printf 'previous lane\n' >"$TMP/lane.txt"
(
	exec 9>"$GORGE_HEAVY_LOCK"
	flock -n 9 && sleep 5
) &
holder=$!
sleep 1
LEDGER_LANE_WAIT=1 "$ROOT/scripts/ledger-lane.sh" "$TMP/lane.txt" 2>"$TMP/err"
rc=$?
kill "$holder" 2>/dev/null
wait "$holder" 2>/dev/null
check "lock held elsewhere -> exit 99 (got $rc)" test "$rc" -eq 99
check "lane file untouched on lock timeout" grep -qx 'previous lane' "$TMP/lane.txt"

exit $fail
