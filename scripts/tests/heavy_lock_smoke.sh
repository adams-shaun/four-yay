#!/usr/bin/env bash
# Prove heavy.sh and another script contend on the shared HEAVY lock.
set -euo pipefail
ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d)
export GORGE_ROOT=$ROOT GORGE_REWARD_DIR=$TMP/reward
export GORGE_HEAVY_LOCK=$TMP/heavy.lock
export HEAVY_START_FLOOR_MB=1 HEAVY_MAX_LEASES=5 LOAD_CEIL_FRAC=100
cleanup() {
  [ -z "${PID:-}" ] || kill "$PID" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

"$ROOT/scripts/heavy.sh" heavy -- bash -c 'sleep 4' >"$TMP/heavy.log" 2>&1 &
PID=$!
contended=0
for _ in $(seq 100); do
  if "$ROOT/scripts/tests/heavy_lock_contender.sh" >"$TMP/contender.log" 2>&1; then
    contended=1
    break
  fi
  sleep 0.05
done
[ "$contended" = 1 ] || { cat "$TMP/heavy.log"; echo 'heavy.sh never acquired the lock' >&2; exit 1; }
cat "$TMP/contender.log"
wait "$PID"
printf 'heavy lock contention passed\n'
