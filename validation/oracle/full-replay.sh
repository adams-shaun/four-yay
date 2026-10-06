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
# Phase 1, parallel: gen + XMage replay per set. Each set is independent and
# the replay is mostly JVM start-up (about 10 s a set), so the serial loop
# spent ~8 of its ~12 minutes waiting. FULL_REPLAY_JOBS sets the width; each
# job's JVM scope is XMAGE_ORACLE_MEM (default 3G here) with the heap 1G
# under it, so 6 jobs stay inside the heavy lock's budget; each JVM gets a
# private copy of the H2 card DB (XMAGE_ORACLE_PRIVATE_DB).
jobs=${FULL_REPLAY_JOBS:-6}
export XMAGE_ORACLE_MEM=${XMAGE_ORACLE_MEM:-3G} XMAGE_ORACLE_PRIVATE_DB=1
mem_g=${XMAGE_ORACLE_MEM%[Gg]}
export XMAGE_ORACLE_HEAP=${XMAGE_ORACLE_HEAP:-$(( mem_g > 2 ? mem_g - 1 : 1 ))g}
export out level repo
one_set() {
  s=$1; d=$out/$s; mkdir -p "$d"
  GOMEMLIMIT=2GiB GOMAXPROCS=4 "$out/oraclediff" gen -level "$level" -manifest compliance/manifests/$s.json -out "$d/scen.jsonl" >"$d/gen.log" 2>&1 || { echo "$s gen FAILED"; return; }
  "$repo/scripts/xmage-oracle-run.sh" "$d/scen.jsonl" "$d/xmage.jsonl" || { echo "$s xmage FAILED"; rm -f "$d/xmage.jsonl"; }
}
export -f one_set
for f in compliance/printed/*.json; do basename "$f" .json; done | xargs -P "$jobs" -I{} bash -c 'one_set "$@"' _ {}
# Phase 2, serial: diff -write shares compliance/verdicts/<letter>.jsonl
# across sets, so it must not run concurrently.
for f in compliance/printed/*.json; do
  s=$(basename "$f" .json); d=$out/$s
  [ -s "$d/scen.jsonl" ] && [ -f "$d/xmage.jsonl" ] || continue
  "$out/oraclediff" diff -scenarios "$d/scen.jsonl" -xmage "$d/xmage.jsonl" -out "$d/verdicts.jsonl" -write compliance/verdicts -xmage-ref "$ref" >"$d/diff.log" 2>&1 || { echo "$s diff FAILED"; tail -3 "$d/diff.log"; continue; }
  echo "$s $(tr '\n' ' ' <"$d/diff.log" | cut -c1-160)"
done
"$out/oraclediff" triage -apply >"$out/triage.txt" 2>&1
echo done
