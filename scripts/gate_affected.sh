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
# Any change can move compliance verdicts: generator/harness edits did
# (a407ddeff, FDN:A) and so did rules edits that reshape target asks
# (40c7823d7/c97355932 staled FRA:A). compliance/gate and compliance/adopt
# are the two tests that notice; they run on every ticket, in parallel with
# ./rules, so a stale verdict parks the ticket for a host replay instead of
# turning main red.
others=$(printf '%s\n' $others ./compliance/gate ./compliance/adopt | sort -u)
# Trajectory-pinned packages: tests that replay seeded bot games and pin a
# finding by (seed, event seq), or replay a committed recorded game event for
# event. Any engine change that emits, drops or reorders an event renumbers
# them even when TestHeads is honestly re-pinned; 2eaca9010 (ExcessDamage
# history events) re-pinned heads 4/6/8 and turned main red in
# ./internal/paymirror unseen by this gate. They run when the branch moves a
# chain head or touches engine code (rules/effects/events/state, non-test Go).
# The cardfuzz half is only its two seed-pinned finding tests.
traj=0
if git diff --name-only "$mb" HEAD | /usr/bin/grep -v -E '_test\.go$' | /usr/bin/grep -q -E \
  '^rules/testdata/heads/|^(rules|effects|events|state)/([^/]+/)*[^/]+\.go$'; then
  traj=1
  others=$(printf '%s\n' $others ./internal/paymirror ./cmd/repro | sort -u)
fi
echo "gate_affected: rules + $(echo $others)$([ $traj = 1 ] && echo ' + cardfuzz findings')"

go vet $others ./rules
go test -p=8 -skip "^($global|$kr8|$postmerge)$" ./rules/ & a=$!
go test -p=8 -run '^TestKr8WorldsInFuzzGames$' ./rules/ & b=$!
go test -p=8 -run '^TestKr8HeadsCheckpointAll$' ./rules/ & c=$!
go test -p=8 -skip "^($global)$" $others & d=$!
# Event-text changes (any new or reworded event) move the committed
# overshoot capture and the searchprobe digests; e2e19ebae and 5fa9f31a both
# broke them unseen by this gate on 2026-10-05. Both checks are seconds.
go test -p=8 ./internal/searchprobe/ & e=$!
go test -p=8 -run '^TestCommittedOvershootCaptureReplaysToTheParkedAsk$' ./host/ & f=$!
pids="$a $b $c $d $e $f"
if [ "$traj" = 1 ]; then
  go test -p=8 -run '^(TestRoundTenFindings|TestForbiddenRitualRepeatYesFinding)$' ./cmd/cardfuzz/ & g=$!
  pids="$pids $g"
fi
rc=0
for p in $pids; do wait "$p" || rc=1; done
exit $rc
