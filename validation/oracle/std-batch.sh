#!/usr/bin/env bash
# std-batch.sh OUTROOT SET... -- per set: generate scenarios, replay them in
# XMage and diff, WITHOUT writing verdicts. A read-only survey of a set's
# standing; use std-write.sh or compliance-pass.sh to record verdicts.
set -uo pipefail
. "$(dirname "$0")/common.sh"
out=${1:?usage: std-batch.sh OUTROOT SET...}; shift
mkdir -p "$out"
cd "$repo"
go build -o "$out/oraclediff" ./cmd/oraclediff || exit 1
for s in "$@"; do
  d=$out/$s; mkdir -p "$d"
  echo "== $s $(date -u +%T)"
  "$out/oraclediff" gen -manifest compliance/manifests/$s.json -out "$d/scen.jsonl" >"$d/gen.log" 2>&1 || { echo "$s gen FAILED"; tail -3 "$d/gen.log"; continue; }
  scripts/xmage-oracle-run.sh "$d/scen.jsonl" "$d/xmage.jsonl" || { echo "$s xmage FAILED"; tail -5 "$d/xmage.jsonl.log"; continue; }
  "$out/oraclediff" diff -scenarios "$d/scen.jsonl" -xmage "$d/xmage.jsonl" -out "$d/verdicts.jsonl" >"$d/diff.log" 2>&1 || { echo "$s diff FAILED"; tail -3 "$d/diff.log"; }
  echo "$s scen=$(wc -l <"$d/scen.jsonl") skips=$(wc -l <"$d/scen.jsonl.skips.jsonl") $(tail -1 "$d/diff.log")"
done
echo "== done $(date -u +%T)"
