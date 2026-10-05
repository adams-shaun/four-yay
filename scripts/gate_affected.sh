#!/usr/bin/env bash
# Per-ticket lean test gate (operator, 2026-10-05): target 1-2 min wall.
#
# The gate runs this script from the BASE commit
# (`git show {base}:scripts/gate_affected.sh | bash -s {base}`), so a branch
# cannot weaken its own gate by editing it.
#
# What it runs, in parallel:
#   - ./rules (always: nearly every ticket's behaviour surfaces there), minus
#     the process-global tests and the slow whole-corpus censuses in POSTMERGE;
#   - the two Kr8 tests in their own processes (as the old module gate did);
#   - every package whose directory the branch touched (testdata maps to its
#     package), plus ./internal/codeshape and ./view.
#
# The skipped tests are NOT dropped: scripts/postmerge_full.sh runs the full
# module suite on main after landings, and a red there is bisected and fixed
# at once (main red is never pre-existing).
set -euo pipefail
base=${1:?usage: gate_affected.sh <base>}
mb=$(git merge-base "$base" HEAD)

global='TestHeads|TestInvariantsUnderSeedFuzz|TestLargeEliminationSweepDoesNotTripLivelockWatcher'
kr8='TestKr8WorldsInFuzzGames|TestKr8HeadsCheckpointAll'
postmerge='TestCloneFidelityShort|TestCostStaticPlannedCastsNeverCostChange|TestChainTargetOfferCensusAgreesWithCastFlow|TestPaymentPlanOnePassMatchesReferenceOverAutoPayGameKernel'

pkgs=$(git diff --name-only "$mb" HEAD | while read -r f; do
  d=$(dirname "$f"); d=${d%%/testdata*}
  if [ -d "$d" ] && compgen -G "$d/*.go" >/dev/null; then echo "./$d"; fi
done | sort -u | /usr/bin/grep -v -x -E '\./rules' || true)
others=$(printf '%s\n' $pkgs ./internal/codeshape ./view | sort -u)
echo "gate_affected: rules + $(echo $others)"

go vet $others ./rules
go test -p=8 -skip "^($global|$kr8|$postmerge)$" ./rules/ & a=$!
go test -p=8 -run '^TestKr8WorldsInFuzzGames$' ./rules/ & b=$!
go test -p=8 -run '^TestKr8HeadsCheckpointAll$' ./rules/ & c=$!
go test -p=8 -skip "^($global)$" $others & d=$!
rc=0
for p in "$a" "$b" "$c" "$d"; do wait "$p" || rc=1; done
exit $rc
