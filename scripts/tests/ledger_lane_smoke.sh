#!/usr/bin/env bash
# ledger_lane_smoke.sh — `make ledger` must not defeat the Go test cache, and its
# lane run must be capped and serialised on its own ledger lock (operator, 2026-10-05/06).
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
check "ledger-lane.sh runs under a MemoryMax/CPUQuota scope" grep -q 'MemoryMax=4G -p CPUQuota=400%' <<<"$body"
check "ledger-lane.sh caps GOMAXPROCS/GOMEMLIMIT" grep -q 'GOMAXPROCS=4 GOMEMLIMIT=3GiB' <<<"$body"

# Contention: while another process holds the lock, the script gives up with
# exit 99 and leaves the previous lane file untouched.
export LEDGER_LOCK=$TMP/ledger.lock
printf 'previous lane\n' >"$TMP/lane.txt"
(
	exec 9>"$LEDGER_LOCK"
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

# A scope-launch failure exits 1 (systemd-run cannot start the user scope)
# with no test output. rc=1 is also go test's FAIL code, so publication must
# be keyed on the lane CONTENT: an empty lane must never replace the previous
# one, or cmd/ledger rebuilds from nothing and silently drops every row.
mkdir -p "$TMP/bin"
cat >"$TMP/bin/systemd-run" <<'EOS'
#!/usr/bin/env bash
echo 'Failed to connect to bus: No such file or directory' >&2
exit 1
EOS
chmod +x "$TMP/bin/systemd-run"
printf 'previous lane\n' >"$TMP/lane.txt"
PATH="$TMP/bin:$PATH" LEDGER_LOCK=$TMP/launch.lock LEDGER_LANE_WAIT=1 \
	"$ROOT/scripts/ledger-lane.sh" "$TMP/lane.txt" 2>"$TMP/err2"
rc=$?
check "scope launch failure -> non-zero exit (got $rc)" test "$rc" -ne 0
check "scope launch failure leaves lane untouched" grep -qx 'previous lane' "$TMP/lane.txt"

# Published lane is world-readable (mktemp's 0600 is not). Stub `go` to emit a
# real result line; the systemd-run stub passes the command through.
printf 'previous lane\n' >"$TMP/lane.txt"
cat >"$TMP/bin/systemd-run" <<'EOS'
#!/usr/bin/env bash
while [ $# -gt 0 ] && [ "$1" != env ]; do shift; done
shift
exec env "$@"
EOS
cat >"$TMP/bin/go" <<'EOS'
#!/usr/bin/env bash
echo 'ok  github.com/adams-shaun/gorge/rules  0.001s'
EOS
chmod +x "$TMP/bin/systemd-run" "$TMP/bin/go"
PATH="$TMP/bin:/usr/bin:/bin" LEDGER_LOCK=$TMP/mode.lock LEDGER_LANE_WAIT=1 \
	"$ROOT/scripts/ledger-lane.sh" "$TMP/lane.txt" 2>/dev/null || true
mode=$(stat -c '%a' "$TMP/lane.txt")
check "published lane is 0644 (got $mode)" test "$mode" = 644

# The lane must NOT take the shared heavy lock (operator, 2026-10-06): a held
# heavy lock must not delay it.
printf 'previous lane\n' >"$TMP/lane.txt"
(
	exec 9>"$TMP/heavy.lock"
	flock -n 9 && sleep 5
) &
holder=$!
sleep 1
PATH="$TMP/bin:/usr/bin:/bin" GORGE_HEAVY_LOCK=$TMP/heavy.lock LEDGER_LOCK=$TMP/own.lock LEDGER_LANE_WAIT=1 \
	"$ROOT/scripts/ledger-lane.sh" "$TMP/lane.txt" 2>"$TMP/err3"
rc=$?
kill "$holder" 2>/dev/null
wait "$holder" 2>/dev/null
check "a held heavy lock does not block the lane (got $rc)" test "$rc" -eq 0
check "the lane published while the heavy lock was held" grep -q '^ok' "$TMP/lane.txt"

exit $fail
