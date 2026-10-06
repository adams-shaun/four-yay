#!/usr/bin/env bash
# full-replay.sh OUT -- regenerate every printed set's scenarios in the
# current tree, replay ALL of them in XMage (no plan, no cache) and
# `oraclediff diff -write` into compliance/verdicts, then apply triage.
# Run after any XMage driver (tools/xmageoracle) or generator-wide change:
# compliance-pass.sh's plan only replays scenarios whose sha or XMAGE_REF
# changed, so verdicts written by an old driver would otherwise look fresh.
# Run from the worktree whose verdicts should change.
# ORACLE_LEVEL (default B) is the generator level. B is a superset of A,
# and compliance/verdicts holds level-B rows since e5dab3b06: a level-A
# replay drops every level-B row from the per-card verdict files, and
# verdict-compare then reads each one as a regression (3085 on
# 2026-10-06, driver-batch-20261006T094206Z).
set -uo pipefail
. "$(dirname "$0")/common.sh"
out=${1:?usage: full-replay.sh OUTDIR}; mkdir -p "$out"
level=${ORACLE_LEVEL:-B}
go build -o "$out/oraclediff" ./cmd/oraclediff || exit 1
for f in compliance/printed/*.json; do
  s=$(basename "$f" .json); d=$out/$s; mkdir -p "$d"
  "$out/oraclediff" gen -level "$level" -manifest compliance/manifests/$s.json -out "$d/scen.jsonl" >"$d/gen.log" 2>&1 || { echo "$s gen FAILED"; continue; }
  "$repo/scripts/xmage-oracle-run.sh" "$d/scen.jsonl" "$d/xmage.jsonl" || { echo "$s xmage FAILED"; continue; }
  "$out/oraclediff" diff -scenarios "$d/scen.jsonl" -xmage "$d/xmage.jsonl" -out "$d/verdicts.jsonl" -write compliance/verdicts -xmage-ref "$ref" >"$d/diff.log" 2>&1 || { echo "$s diff FAILED"; tail -3 "$d/diff.log"; continue; }
  echo "$s $(tr '\n' ' ' <"$d/diff.log" | cut -c1-160)"
done
"$out/oraclediff" triage -apply >"$out/triage.txt" 2>&1
echo done
