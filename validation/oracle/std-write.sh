#!/usr/bin/env bash
# std-write.sh WORKTREE OLDRUN NEWRUN SET... -- regenerate scenarios on
# WORKTREE's gorge, rerun XMage only where a set's scenario file changed since
# OLDRUN (always for sets named in FORCE_XMAGE="A B"), then diff -write into
# WORKTREE/compliance/verdicts.
set -uo pipefail
. "$(dirname "$0")/common.sh"
W=${1:?worktree} old=${2:?oldrun} new=${3:?newrun}; shift 3
cd "$W" && go build -o "$new/oraclediff" ./cmd/oraclediff || exit 1
for s in "$@"; do
  d=$new/$s; mkdir -p "$d"
  "$new/oraclediff" gen -manifest compliance/manifests/$s.json -out "$d/scen.jsonl" >"$d/gen.log" 2>&1 || { echo "$s gen FAILED"; continue; }
  if [ -f "$old/$s/xmage.jsonl" ] && cmp -s "$d/scen.jsonl" "$old/$s/scen.jsonl" && [[ " ${FORCE_XMAGE:-} " != *" $s "* ]]; then
    cp "$old/$s/xmage.jsonl" "$d/xmage.jsonl"; how=reused
  else
    "$W/scripts/xmage-oracle-run.sh" "$d/scen.jsonl" "$d/xmage.jsonl" || { echo "$s xmage FAILED"; continue; }; how=rerun
  fi
  "$new/oraclediff" diff -scenarios "$d/scen.jsonl" -xmage "$d/xmage.jsonl" -out "$d/verdicts.jsonl" -write compliance/verdicts -xmage-ref "$ref" >"$d/diff.log" 2>&1 || { echo "$s diff FAILED"; tail -3 "$d/diff.log"; continue; }
  echo "$s xmage=$how $(tr '\n' ' ' <"$d/diff.log" | tr -s ' ' | tail -c 200)"
done
echo "== done"
