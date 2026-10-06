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
skip='^(TestHeads|TestInvariantsUnderSeedFuzz[0-9]*|TestLargeEliminationSweepDoesNotTripLivelockWatcher)$'
# Opt-in exhaustive audits that the lean per-ticket gate leaves out
# (db57ab8e6 made the oraclegen target audit opt-in); the full suite runs them.
export GORGE_ORACLEGEN_FULL_TARGET_AUDIT=1
go vet -p=8 ./...
go test -p=8 -skip "$skip" ./...
go test ./rules -run '^TestHeads$'
make sim
