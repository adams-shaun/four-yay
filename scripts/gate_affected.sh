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

# Default-build packages in a list of candidate directories, one per line.
#
# A directory whose every .go file is build-tagged (cmd/autopayaudit) is not a
# package in a default build: naming it to `go vet`/`go test` fails the whole
# run with "build constraints exclude all Go files" (9b9bd27a, 2026-10-06).
# `go list -e` reports such a package with no .GoFiles/.TestGoFiles/
# .XTestGoFiles and exits 0 (the -e tolerates the load error), so keep only
# candidates with at least one default-build file. The class -- not just
# cmd/autopayaudit -- is covered: any candidate the default build cannot
# compile is dropped, and scripts/tests/gate_affected_smoke.sh pins that
# behavior so the filter cannot be dropped by a later edit.
#
# Input: candidate package paths on stdin (./dir form). Output: the subset
# buildable under the default tags, order preserved.
gate_affected_default_build_pkgs() {
  local p
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    [ -n "$(go list -e -f '{{if or .GoFiles .TestGoFiles .XTestGoFiles}}y{{end}}' "$p" 2>/dev/null)" ] && echo "$p"
  done
  return 0
}

# Split the top-level ./rules tests into two concurrent `go test -run` patterns
# (see the call site below). Output: exactly two regexes, one per line, whose
# union is every top-level test. A test is bucketed by the character after
# "Test", buckets are balanced by test count, and the character set comes from
# `go test -list`, so a new test under an existing first character is always
# covered. Any surprise in the listing (empty list, a name shorter than five
# characters, no characters) returns nonzero and the caller falls back to the
# single unsplit run, so a malformed list can never silently drop a test.
#
# Measured 2026-10-08 (this box, 4 cores, `systemd-run ... CPUQuota=400%`,
# rules test binary pre-warmed): one `go test -p=1 -skip ... ./rules` 84 s at
# 241% CPU (the package does NOT saturate the cores: 1693 tests call
# t.Parallel, the rest run serially); the same run split two ways finished in
# 62 s wall (shards 59.5 s / 55.0 s, counts 3264 / 3267). Listing and splitting
# costs ~1 s.
rules_shard_patterns_from_list() {
  local list chars
  list=$(cat)
  [ -n "$list" ] || return 1
  printf '%s\n' "$list" | awk 'length($0) < 5 { exit 1 }' || return 1
  chars=$(printf '%s\n' "$list" | cut -c5 | sort | uniq -c | sort -rn)
  printf '%s\n' "$chars" | awk '
    function esc(c) { gsub(/[][\\^-]/, "\\\\&", c); return c }
    { cnt[NR] = $1; ch[NR] = $2 }
    END {
      l0 = l1 = 0
      for (i = 1; i <= NR; i++) {
        if (l0 <= l1) { c0 = c0 esc(ch[i]); l0 += cnt[i] }
        else          { c1 = c1 esc(ch[i]); l1 += cnt[i] }
      }
      print "^Test[" c0 "]"
      print "^Test[" c1 "]"
    }'
}

rules_shard_run_patterns() {
  go test -list '.*' ./rules/ 2>/dev/null | /usr/bin/grep '^Test' \
    | rules_shard_patterns_from_list
}

# When sourced by scripts/tests/gate_affected_smoke.sh, expose the helpers
# without running the gate. Detect sourcing structurally: an environment
# variable must never bypass the gate when the script is executed normally.
if (return 0 2>/dev/null); then return 0; fi

base=${1:?usage: gate_affected.sh <base>}
mb=$(git merge-base "$base" HEAD)

global='TestHeads|TestInvariantsUnderSeedFuzz[0-9]*|TestLargeEliminationSweepDoesNotTripLivelockWatcher'
kr8='TestKr8WorldsInFuzzGames|TestKr8HeadsCheckpointAll'
# The four are sharded into chunk tests (2026-10-05 per-test budget: 2 GB,
# 2 vCPU, 1 min each); the suffix patterns skip every chunk.
postmerge='TestCloneFidelityShort[0-9A-Za-z]*|TestCostStaticPlannedCastsNeverCostChange[0-9]*|TestChainTargetOfferCensusAgreesWithCastFlow[0-9]*|TestPaymentPlanOnePassMatchesReferenceOverAutoPayGameKernel[0-9A-Za-z]*'

pkgs=$(git diff --name-only "$mb" HEAD | while read -r f; do
  d=$(dirname "$f"); d=${d%%/testdata*}
  if [ -d "$d" ] && compgen -G "$d/*.go" >/dev/null; then echo "./$d"; fi
done | sort -u | /usr/bin/grep -v -x -E '\./rules' || true)
# Drop candidates the default build cannot compile (see the helper above).
if [ -n "$pkgs" ]; then
  pkgs=$(printf '%s\n' $pkgs | gate_affected_default_build_pkgs)
fi
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

# Gate wall is the longest chain, so the phases below overlap everything that
# does not depend on another phase (measured on the 2026-10-06 gate logs: for
# a ticket that moves engine code the `$others` packages summed to ~115 s of
# test time run one after another, longer than the ~70 s ./rules run).
#
# Vet and the ./rules test build overlap: vet type-checks from source and the
# build compiles, so they share no work (go-build has already warmed the
# non-test packages). Both must pass before any test starts, as before.
# GOMAXPROCS=6/-p=6: the scope's CPUQuota is 800% (8 cores) but the gate env
# exports GOMAXPROCS=2, which caps each compile action to two threads. Measured
# on a fresh `rules/mana.go` edit, inside the gate's own scope
# (`systemd-run -p MemoryMax=8G -p CPUQuota=800%`): `go vet -p=2` over the
# `$others` set plus ./rules took 75.9 s / 147 cpu-s; `GOMAXPROCS=6 go vet -p=6`
# took 18.8 s / 68 cpu-s, peak RSS 3.7 GiB (under the 8 GiB scope). The quota,
# not the flag, is the ceiling -- at 800% the extra -p is real parallelism.
GOMAXPROCS=6 go vet -p=6 $others ./rules & v=$!
# Build the ./rules test binary ONCE before the concurrent rules runs.
# They are separate `go test` processes, and a process does not see a compile
# another one is still running, so each used to compile and link the same
# (large) test variant itself. Measured under a 200% cpu cap after a rules
# edit: three concurrent runs 112 s wall / 200 cpu-s, build-once-then-run
# 38 s / 62 cpu-s (the three then hit the build cache). The runs below are
# unchanged, so the result cache and the reported output are too.
GOMAXPROCS=6 go test -c -o /dev/null ./rules/ & w=$!
rc=0
wait "$v" || rc=1
wait "$w" || rc=1
[ "$rc" = 0 ] || exit 1

# The main ./rules run is the longest single test job in the gate and it does
# not use the whole scope quota (241% of 400% measured above), so split it in
# two by test name and run the halves concurrently. `-skip` still removes the
# process-global tests (they run in their own gate or post-merge); the two
# `-run` patterns are a complete, disjoint partition of every remaining test
# (verified by construction in rules_shard_run_patterns, which falls back to
# the unsplit run if it cannot list the tests). Both shards keep -p=1 so the
# number of test binaries does not grow: the two short Kr8 runs (b, c) have
# finished by the time the long shards are at their peak.
shard1=; shard2=
{ read -r shard1; read -r shard2; } < <(rules_shard_run_patterns) || true
if [ -n "$shard1" ] && [ -n "$shard2" ]; then
  go test -p=1 -skip "^($global|$kr8|$postmerge)$" -run "$shard1" ./rules/ & a1=$!
  go test -p=1 -skip "^($global|$kr8|$postmerge)$" -run "$shard2" ./rules/ & a2=$!
else
  go test -p=1 -skip "^($global|$kr8|$postmerge)$" ./rules/ & a1=$!
  a2=
fi
go test -p=1 -run '^TestKr8WorldsInFuzzGames$' ./rules/ & b=$!
go test -p=1 -run '^TestKr8HeadsCheckpointAll$' ./rules/ & c=$!
# -p=6: the $others packages are independent test binaries; with -p=1 they
# ran strictly one at a time and were the long pole of the gate. Measured on
# the `$others` set alone under the gate scope (800% quota, test results
# expired with `go clean -testcache`): -p=2 62.3 s, -p=4 40.8 s, -p=6 32.5 s,
# peak RSS ~1.0 GiB. At most six others binaries run here (the two Kr8 runs
# have already finished, ./rules is two concurrent shards), so the scope stays
# well under its 8 GiB MemoryMax.
GOMAXPROCS=6 go test -p=6 -skip "^($global)$" $others & d=$!
# Event-text changes (any new or reworded event) move the committed
# overshoot capture and the searchprobe digests; e2e19ebae and 5fa9f31a both
# broke them unseen by this gate on 2026-10-05. Both checks are seconds, so
# they start as soon as the two short Kr8 runs free their slots instead of
# waiting for ./rules and every other package to finish.
wait "$b" || rc=1
wait "$c" || rc=1
go test -p=1 ./internal/searchprobe/ & e=$!
go test -p=1 -run '^TestCommittedOvershootCaptureReplaysToTheParkedAsk$' ./host/ & f=$!
pids="$a1 $a2 $d $e $f"
if [ "$traj" = 1 ]; then
  go test -p=1 -run '^(TestRoundTenFindings|TestForbiddenRitualRepeatYesFinding)$' ./cmd/cardfuzz/ & g=$!
  pids="$pids $g"
fi
for p in $pids; do wait "$p" || rc=1; done
exit "$rc"
