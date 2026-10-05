#!/usr/bin/env bash
# Full gate every N landings, then push (operator, 2026-10-05).
#
# agentctl lands with landing.push = false behind the lean per-ticket gate
# (scripts/gate_affected.sh). This loop watches local main: once it is N or
# more first-parent commits ahead of origin/main (or any commit has waited
# MAX_WAIT_MIN), it runs scripts/postmerge_full.sh on main's head in a
# dedicated worktree and pushes exactly that commit when the suite passes.
# A red suite is NOT pushed: it is logged as RED and the loop waits for a new
# head (the operator bisects and fixes on main).
#
#   scripts/postmerge_batch.sh            # loop forever
#   N=5 MAX_WAIT_MIN=60 LOCK=/path/heavy.lock scripts/postmerge_batch.sh
set -uo pipefail
repo=$(git rev-parse --show-toplevel)
N=${N:-5}
MAX_WAIT_MIN=${MAX_WAIT_MIN:-60}
LOCK=${LOCK:-/tmp/gorge-heavy.lock}
LOG=${LOG:-$repo/.ds4/postmerge-batch.log}
wt=$repo/.worktrees/postmerge-full
cd "$repo"

say() { echo "$(date '+%F %T') $*" | tee -a "$LOG"; }

[ -d "$wt" ] || scripts/agent-worktree.sh postmerge-full main >/dev/null

last_red=""
while true; do
  git fetch -q origin main 2>/dev/null || true
  head=$(git rev-parse main)
  ahead=$(git rev-list --first-parent --count origin/main..main)
  oldest=$(git log --first-parent --reverse --format=%ct origin/main..main | head -n1)
  waited=$(( oldest ? ($(date +%s) - oldest) / 60 : 0 ))
  if [ "$ahead" -gt 0 ] && [ "$head" != "$last_red" ] && { [ "$ahead" -ge "$N" ] || [ "$waited" -ge "$MAX_WAIT_MIN" ]; }; then
    say "FULL start ${head:0:9} ($ahead ahead, oldest ${waited}m)"
    s=$(date +%s)
    if flock -o "$LOCK" systemd-run --user --scope -q -p MemoryMax=24G \
         env GOMEMLIMIT=16GiB GOGC=200 scripts/postmerge_full.sh "$wt" "$head" \
         >"$repo/.ds4/postmerge-full.out" 2>&1; then
      if git push -q origin "$head:refs/heads/main"; then
        say "GREEN ${head:0:9} pushed ($(( $(date +%s) - s ))s)"
      else
        say "GREEN ${head:0:9} push FAILED"
      fi
    else
      last_red=$head
      say "RED ${head:0:9} not pushed ($(( $(date +%s) - s ))s): $(/usr/bin/grep -E '^(--- FAIL|FAIL|panic)' "$repo/.ds4/postmerge-full.out" | head -n5 | tr '\n' ' ')"
    fi
  fi
  sleep 60
done
