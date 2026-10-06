#!/usr/bin/env bash
# Minimal second caller used by heavy_lock_smoke.sh to verify real flock contention.
set -euo pipefail
lock=${GORGE_HEAVY_LOCK:?GORGE_HEAVY_LOCK must name the shared lock}
if flock -o -n "$lock" true; then
  echo "acquired $lock (not yet contended)"
  exit 1
fi
echo "contended on $lock"
