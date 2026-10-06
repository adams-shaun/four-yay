#!/usr/bin/env bash
# Post-merge full suite in batches, bisect on red (operator, 2026-10-05).
#
# agentctl lands with landing.push = false behind the lean per-ticket gate
# (scripts/gate_affected.sh). This loop runs scripts/postmerge_full.sh once on
# main's head, then again on whatever landed since the last run -- never once
# per commit. A green run pushes exactly the commit it tested.
#
# A red run is bisected, not re-run: the failing packages/tests are replayed
# (only those, never the full suite) over the first-parent commits between
# the last green and the red head, and the first commit that fails them is
# logged as the culprit (CULPRIT lines, also in $CULPRITS). While main stays
# red, a new head first re-checks just those failing tests; the full suite
# runs again only once they pass.
#
# A red run also PAUSES the agentctl pipeline (writes $PAUSE, the file the
# daemon checks every tick) so nothing else lands on a red main while the
# operator bisects and fixes. The pause is ours only if the file was absent:
# it carries a "postmerge_batch" reason, and the next GREEN run removes it.
# A pause anyone else wrote (fleet halt, operator) is never touched.
#
#   scripts/postmerge_batch.sh            # loop forever
#   GORGE_HEAVY_LOCK=/path/to/lock scripts/postmerge_batch.sh
#   scripts/postmerge_batch.sh --parse-fails <go-test-output>   # self-test aid
set -uo pipefail
repo=$(git rev-parse --show-toplevel)
. "$(dirname "$0")/heavy_lock.sh"  # the one HEAVY lock definition
LOCK=${LOCK:-$GORGE_HEAVY_LOCK}
LOG=${LOG:-$repo/.ds4/postmerge-batch.log}
CULPRITS=${CULPRITS:-$repo/.ds4/postmerge-culprits.log}
OUT=$repo/.ds4/postmerge-full.out
PAUSE=${PAUSE:-$repo/.ds4/orchestrator/pause}
wt=$repo/.worktrees/postmerge-full
SCOPE=(systemd-run --user --scope -q -p MemoryMax=24G env GOMEMLIMIT=16GiB GOGC=200 GOFLAGS="-p=2 -trimpath" GORGE_ORACLEGEN_FULL_TARGET_AUDIT=1)

say() { echo "$(date '+%F %T') $*" | tee -a "$LOG"; }

pause_pipeline() {
  [ -e "$PAUSE" ] && return 0
  echo "postmerge_batch RED $1 $(date -u +%FT%TZ): main->origin full suite failed; bisecting (see $LOG)" >"$PAUSE"
  say "PAUSED pipeline: RED ${1:0:9}"
}
resume_pipeline() {
  if [ -e "$PAUSE" ] && /usr/bin/grep -q '^postmerge_batch ' "$PAUSE"; then
    rm -f "$PAUSE"
    say "RESUMED pipeline: GREEN ${1:0:9}"
  fi
}

# parse_fails prints one "<pkg> <regex>" line per failing package from
# `go test` output (non -v): the top-level --- FAIL names seen before each
# "FAIL<TAB><pkg>" line. A package that failed with no named test (timeout,
# panic, build failure) gets regex "." (run the whole package).
parse_fails() {
  awk '
    /^--- FAIL: / { n = $3; if (!(n in seen)) { seen[n] = 1; t = t (t ? "|" : "") n } next }
    /^FAIL\t[^ \t]+/ {
      split($0, f, "\t"); p = f[2]
      if (p ~ /^github\.com\//) print p, (t ? "^(" t ")$" : ".")
      t = ""; delete seen
    }' "$1"
}

if [ "${1:-}" = "--print-heavy-lock" ]; then printf '%s\n' "$LOCK"; exit 0; fi
if [ "${1:-}" = "--parse-fails" ]; then parse_fails "$2"; exit 0; fi

# fails_at <sha> <pkg> <regex>: 0 if the tests FAIL at sha, 1 if they pass.
fails_at() {
  git -C "$wt" switch -q --detach "$1" || return 2
  ! (cd "$wt" && flock -o "$LOCK" "${SCOPE[@]}" go test -p=4 -run "$3" "$2" >/dev/null 2>&1)
}

# bisect <good> <bad> <pkg> <regex>: first first-parent commit in good..bad
# whose tests fail. Assumes they pass at good and fail at bad.
bisect() {
  local commits lo hi mid
  mapfile -t commits < <(git rev-list --first-parent --reverse "$1..$2")
  lo=0; hi=$((${#commits[@]} - 1))
  while [ "$lo" -lt "$hi" ]; do
    mid=$(( (lo + hi) / 2 ))
    if fails_at "${commits[$mid]}" "$3" "$4"; then hi=$mid; else lo=$((mid + 1)); fi
  done
  echo "${commits[$lo]}"
}

[ -d "$wt" ] || scripts/agent-worktree.sh postmerge-full main >/dev/null

last_tested=""   # head of the last completed full run (green or red)
last_green=$(git rev-parse origin/main)
red_fails=""     # "<pkg> <regex>" lines still failing on main
while true; do
  git fetch -q origin main 2>/dev/null || true
  head=$(git rev-parse main)
  if [ "$head" != "$last_tested" ] && [ "$(git rev-list --count origin/main..main)" -gt 0 ]; then
    # Still red? Re-check only the failing tests before paying for a full run.
    still=""
    while read -r pkg re; do
      [ -n "$pkg" ] || continue
      if fails_at "$head" "$pkg" "$re"; then still+="$pkg $re"$'\n'; fi
    done <<<"$red_fails"
    if [ -n "$still" ]; then
      last_tested=$head
      say "STILL RED ${head:0:9}: $(echo "$still" | awk '{print $1}' | sort -u | tr '\n' ' ')"
    else
      red_fails=""
      say "FULL start ${head:0:9} ($(git rev-list --first-parent --count "$last_green..$head") since last green ${last_green:0:9})"
      s=$(date +%s)
      if flock -o "$LOCK" "${SCOPE[@]}" scripts/postmerge_full.sh "$wt" "$head" >"$OUT" 2>&1; then
        last_tested=$head
        if git push -q origin "$head:refs/heads/main"; then
          last_green=$head
          say "GREEN ${head:0:9} pushed ($(( $(date +%s) - s ))s)"
          resume_pipeline "$head"
        else
          say "GREEN ${head:0:9} push FAILED"
        fi
      else
        last_tested=$head
        pause_pipeline "$head"
        say "RED ${head:0:9} ($(( $(date +%s) - s ))s): $(/usr/bin/grep -E '^(--- FAIL|FAIL|panic)' "$OUT" | head -n5 | tr '\n' ' ')"
        red_fails=$(parse_fails "$OUT")
        if [ -z "$red_fails" ]; then
          say "RED ${head:0:9}: no failing package parsed (vet/TestHeads/sim step?) -- see $OUT; not bisected"
        fi
        while read -r pkg re; do
          [ -n "$pkg" ] || continue
          if ! fails_at "$head" "$pkg" "$re"; then
            say "FLAKY ${head:0:9} $pkg $re: passes on rerun"
            continue
          fi
          if fails_at "$last_green" "$pkg" "$re"; then
            say "CULPRIT unknown for $pkg $re: already fails at last green ${last_green:0:9}"
            continue
          fi
          c=$(bisect "$last_green" "$head" "$pkg" "$re")
          msg="CULPRIT ${c:0:9} \"$(git log -1 --format=%s "$c")\" breaks $pkg $re (batch ${last_green:0:9}..${head:0:9})"
          say "$msg"; echo "$(date '+%F %T') $msg" >>"$CULPRITS"
        done <<<"$red_fails"
        git -C "$wt" switch -q --detach "$head"
      fi
    fi
  fi
  # The fleet's worktrees each key their own build-cache entries, so
  # GOCACHE grows ~1 TB/day here and Go's own 5-day trim never catches up
  # (2026-10-05: / hit 100% and git could not create worktrees). Drop
  # entries unused for 6h whenever / passes 80%.
  if [ "$(df --output=pcent / | tail -n1 | tr -dc 0-9)" -ge 80 ]; then
    gc=$(go env GOCACHE)
    nice find "$gc" -type f -mmin +360 -delete 2>/dev/null
    say "GOCACHE trimmed: / now $(df --output=pcent / | tail -n1 | tr -d ' ')"
  fi
  sleep 60
done
