#!/usr/bin/env bash
# ledger-lane.sh — write the CR conformance lane's `go test -v` output for `make ledger`.
#
# Runs after every landing (post_merge in .agentctl/config.toml), so it must be
# cheap and bounded (operator, 2026-10-05):
#   - NO -count=1: an unchanged ./rules package is answered from the Go test
#     cache, so a landing that did not touch rules costs seconds, not a re-run
#     of the lane. The cached replay is byte-identical to the live -v output
#     except the final `ok ... (cached)` line, which cmd/ledger does not read.
#   - capped: 2 GB / 2 vCPU scope, GOMAXPROCS=2 GOMEMLIMIT=1536MiB, like every
#     other test run on this box.
#   - serialised on the shared heavy lock (flock -o so the fd is not inherited
#     by the test binary), so it does not run beside a post-merge full suite.
#
# The CR conformance gate's saved log (.ds4/orchestrator/gates/<id>/<tag>/
# CR-conformance.log) is NOT reused: it is truncated to 20000 bytes, carries no
# sha, and was produced on the branch before the merge, so it can differ from
# the landed tree.
#
#   scripts/ledger-lane.sh .ds4/lane-rules.txt
#
# A failing lane is not an error here (a FAIL leaf is a ledger row, exactly as
# before); failing to get the lock or to start the scope is, and exits non-zero
# instead of leaving a stale or empty lane file behind.
set -uo pipefail

OUT=${1:?usage: ledger-lane.sh <lane-output-file>}
# Same default as scripts/postmerge_batch.sh (/tmp/gorge-heavy.lock): a per-worktree
# path would never contend with it.
LOCK=${GORGE_HEAVY_LOCK:-/tmp/gorge-heavy.lock}
WAIT=${LEDGER_LANE_WAIT:-1800}

mkdir -p "$(dirname "$OUT")"

scope=()
if command -v systemd-run >/dev/null 2>&1; then
	scope=(systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200%)
fi

TMP=$(mktemp "$OUT.XXXXXX") || exit 2
trap 'rm -f "$TMP"' EXIT

# -E 99: a lock timeout is distinguishable from go test's own exit 1 (FAIL).
# Under `heavy_lock.sh run` (the post_merge hook) the caller already holds this
# lock; taking it again would wait on our own ancestor until $WAIT ran out.
lock=(flock -o -E 99 -w "$WAIT" "$LOCK")
[ "${GORGE_HEAVY_LOCK_HELD:-}" = "$LOCK" ] && lock=()
"${lock[@]}" "${scope[@]}" env GOMAXPROCS=2 GOMEMLIMIT=1536MiB \
	go test -timeout 2m ./rules -run TestCR -v >"$TMP"
rc=$?
if [ "$rc" -eq 99 ]; then
	printf 'ledger-lane.sh: could not take %s within %ss\n' "$LOCK" "$WAIT" >&2
	exit 99
fi
# Publication is keyed on the lane CONTENT, never the exit code: a launch
# failure cannot be told apart from a genuine test FAIL by rc alone, because
# systemd-run exits 1 when the user scope will not start and go test also
# exits 1 on FAIL. Only a real result line is publishable; anything else
# (scope refused, OOM-kill, missing go, build error that produced no leaf)
# exits non-zero and leaves the previous lane untouched, so an empty lane can
# never be installed and cmd/ledger can never rebuild from nothing.
if ! grep -q '^\(ok\|FAIL\|---\)' "$TMP"; then
	case "$rc" in
	0) rc=2 ;; # no result but a clean exit is still a launch failure
	esac
	printf 'ledger-lane.sh: lane run died (exit %s) with no result\n' "$rc" >&2
	exit "$rc"
fi
# `mktemp` makes the lane 0600; the old redirect produced the umask default,
# and the lane is a shared artifact. Restore that before publishing.
chmod 0644 "$TMP"
mv "$TMP" "$OUT"
trap - EXIT
