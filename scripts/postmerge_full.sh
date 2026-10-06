#!/usr/bin/env bash
# Post-merge full suite (operator, 2026-10-05): the counterpart of the lean
# per-ticket gate in scripts/gate_affected.sh. Runs the whole module suite,
# including the tests the per-ticket gate skips, on one commit of main.
#
#   scripts/postmerge_full.sh <worktree> <sha>
#
# The worktree must be a dedicated one made by scripts/agent-worktree.sh (it
# needs .cards); it is detached onto <sha>. Exit status is the suite's.
set -euo pipefail
wt=${1:?worktree}; sha=${2:?sha}
git -C "$wt" switch -q --detach "$sha"
cd "$wt"
skip='^(TestHeads|TestInvariantsUnderSeedFuzz[0-9]*)$'
# Opt-in exhaustive audits that the lean per-ticket gate leaves out
# (db57ab8e6 made the oraclegen target audit opt-in); the full suite runs them.
export GORGE_ORACLEGEN_FULL_TARGET_AUDIT=1
go vet -p=8 ./...
# Per-test budget (operator, 2026-10-05): every test fits 2 GB RSS, 2 vCPU,
# 1 min wall. Test binaries record their own peak RSS through internal/testbudget;
# cmd/testbudget reads those persistent records and the per-test wall from the
# -json stream (tee reprints the plain text the batch's failure parser reads).
# The RSS directory survives runs so cached packages retain their valid record;
# GORGE_TESTBUDGET_RSS=0 keeps the cache and checks wall only.
budget=$PWD/.ds4/scratch/testbudget
mkdir -p "$budget"
go build -o "$budget/testbudget" ./cmd/testbudget
rss_flag=()
if [ "${GORGE_TESTBUDGET_RSS:-1}" = 1 ]; then
	export GORGE_TESTBUDGET_RSS_DIR=$budget/rss
	rss_flag=(-rss-dir "$budget/rss")
fi
go test -p=8 -json -skip "$skip" ./... | "$budget/testbudget" tee -events "$budget/events.json"
"$budget/testbudget" check -events "$budget/events.json" "${rss_flag[@]}"
go test ./rules -run '^TestHeads$'
make sim
# Shell smokes run in the post-merge batch, not the lean per-ticket gate (~63s).
bash scripts/tests/reward_loop.sh
