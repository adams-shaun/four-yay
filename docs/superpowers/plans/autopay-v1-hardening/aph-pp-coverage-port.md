# Auto-pay: port the passing PP-matrix audit tests and make PP-20 assert planned casts succeed

Suggested issue ID: `aph-pp-coverage-port`
Priority: 2
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: autopay-exec-harden
Changes offers: no (tests only)

## Goal

The 2026-09-25 acceptance report mapped several PP rows to smoke tests. The
gaps audit wrote focused tests for them; the ones listed below PASS on main
and should be landed (renamed `TestPaymentPlan…`, no `Audit`) so the matrix is
really asserted and the rest of this wave has a regression net. Also make the
PP-20 hosted lane fail if a planned cast does not succeed: today it checks only
completion and replay, and its decks have no dual lands, which is why the
dual-land executor P0 (fixed in 7fb66ee5f) shipped.

`autopay-exec-harden` (in flight: the residual-`chooseCast` free-cast fix, the
per-step production check and the choose-flow flag leaks) must merge first: it
changes the fallback/interruption behaviour several of these tests observe.

Port from `git show 124ed89fb:<path>` (throwaway; never merge):

| PP | Test on the proof branch | Package |
|---|---|---|
| 01/03 | `TestPaymentPlanAuditPrefersMountainOverBadlandsAndLeavesItUntapped` (+ add the exact PP-01 board: empty pool, Island/Swamp/Mountain, `{1}{U}{B}`) | rules |
| 05 | `TestPaymentPlanAuditFixedMultiOutputLeavesSurplusFloating` | rules |
| 06 | `TestPaymentPlanAuditSourceEligibilityShapes` (drop the sick-without-haste subtest unless `cr302-6-sick-mana` has merged; then keep it) | rules |
| 07 | `TestPaymentPlanAuditLegalityGatesSuppressPlans` | rules |
| 09 | `TestPaymentPlanAuditProducerAndPoolExclusions` (restricted pool, life-cost, restricted producer, irrelevant producer; its "TapsForMana trigger declines" case must be re-expressed per spec §3.2: the matched source is deferred) | rules |
| 10 | determinism part of `TestPaymentPlanAuditSearchLimitDeterministicAndTimed` (two calls, identical outcome and nodes; no timing assertion) | rules |
| 11 | `TestPaymentPlanAuditEngineRejectsForgedWitnessBeforeMutation` | rules |
| 12 | `TestPaymentPlanAuditTargetsPrecedePlannedActivation` | rules |
| 13 | `TestPaymentPlanAuditPlannedMatchesManualExecution`, `TestPaymentPlanAuditExecutionHonoursWitnessPoolSpend` | rules |
| 14 | `TestPaymentPlanAuditCostRaisedAfterOfferFallsBackBeforeTapping`, `…BlinkedSourceFallsBack`, `…ActivationInterruptionCancelsRemainingSteps`, `…TappedSourceFallsBackWithoutSubstitution` (only those not already landed by `autopay-exec-harden`; check its tests first) | rules |
| 15 | `TestPaymentPlanAuditCloneAtTargetAskFinishesIdentically` | rules |
| 15 | `TestPaymentPlanAuditUndoRepostsAndReacceptsAPlannedIntent`, `TestPaymentPlanAuditRestartReplaysPersistedPlannedIntents` (incl. tamper refusal), from `host/payment_plan_audit_test.go` | host |
| 16 | `TestPaymentPlanAuditNewSeqRejectsPreviousOffer` | rules |
| 17 | `TestPaymentPlanAuditSelectorIsFencedAndNeverCrashesTheMatch`, from `host/httpapi/payment_plan_audit_test.go` | host/httpapi |
| 20 | extend `runPaymentPlanHumanSeat` in `host/payment_plan_acceptance_test.go`: after each planned submission, when the seat next holds priority, assert the planned card is on the stack or has resolved (not back in hand); add a two-seat run on `ur-delver` vs `uw-control` | host |

(`TestPaymentPlanAuditFeedbackCaptureReplaysPlannedIntents` already landed in
`cmd/repro/feedback_planned_replay_test.go`.)

## Evidence

- Proof run on main 0d307f44c: `go test ./rules ./seat ./decision ./host ./host/httpapi ./cmd/repro -run 'PaymentPlanAudit|AutoPayManaBug' -count=1 -v`;
  the listed tests PASS, 0 SKIP.
- The gaps census (174 auto-pay games) found 195 / 1,429 planned casts
  reversed on dual-land decks while PP-20 passed; the P0 was fixed in
  7fb66ee5f and the strengthened lane must have caught it.

## Files (symbols)

- New files only: `rules/payment_plan_pp_matrix_test.go`,
  `host/payment_plan_pp_matrix_test.go`, `host/httpapi/payment_plan_fence_test.go`;
  `host/payment_plan_acceptance_test.go` `runPaymentPlanHumanSeat` (extend).
- Copy the proof branch's small helpers (`auditAsk`, `auditAction`,
  `auditSubmitPlan`, …) under non-`audit` names into the new rules test file.

## Out of scope

- The failing (bug) probes: they land with their fix tickets.
- Changing engine code. If a ported test fails on current main, do not "fix"
  the engine here: drop it from the port and name it in your report.

## Done means

- `go test ./rules ./host ./host/httpapi -run 'Test.*PaymentPlan' -count=1 -v`
  runs every ported test with 0 SKIP and passes.
- The strengthened PP-20 lane passes on current main, and has teeth: after
  COMMITTING your work, run `git revert --no-commit 5308ef397` in your
  worktree, run the lane, record the failure line it prints, then
  `git reset --hard HEAD` (safe only because your work is committed). Report
  the failure line.
- Name every ported test and its PP row in the commit message.

## Standing rules

- Authority: `docs/superpowers/specs/2026-09-24-cast-payment-plans.md` as
  amended 2026-09-26 (read the Amendment section first). Where this brief and
  the spec disagree, stop and report; do not pick one silently.
- Work in your own worktree (`scripts/agent-worktree.sh <ticket-id>`), stage
  explicit paths only, never `git add -A`, and add no `Co-Authored-By` or other
  attribution trailer (the hook rejects them).
- Never commit `.cards` content or Forge script text. Tests use authored IR
  (`newFixtureDeck`, `onBoard`, `card` in package `rules`) or the linked corpus
  through `testutil.CorpusRegistry` / the `fpCorpus` pattern, which reads a
  card by name at test time. Confirm your worktree has `.cards` and that your
  test runs report 0 SKIP; a corpus-dependent test that skips is not a pass.
- Do not add or grow any AGENTS.md "Known approximations" row. A deviation you
  cannot close goes in the commit message and your report.
- Goldens: if `TestHeads`, `TestRepoDeckGamesReplayExactly`,
  `TestEveryRepoDeck*`, `make sim`, `rules.TestPaymentPlanDecisionMadeGolden`
  or `decision.TestPaymentPlanIdentityIsIndependentOfPresentation` changes,
  STOP and report. No golden-update approval exists for this wave. (A planned
  intent log recorded before your change may legitimately diverge on replay,
  spec §7; tests that record and replay in-test must still pass.)
- The proof tests cited in Evidence live on throwaway branches. Read them with
  `git show <sha>:<path>` and port what you need into a NEW test file named
  for this ticket (for rules: `rules/payment_plan_<slug>_test.go`; the
  in-flight `autopay-exec-harden` branch rewrites `rules/payment_plan_test.go`).
  Never merge or cherry-pick a proof branch. Nothing under `/mnt/sata` is
  reachable from your worktree; everything you need is in this brief.
- `rules/autopay_census_test.go` (env-gated, but compiled with every
  `go test ./rules`) calls planner internals: `paymentPlanCastCandidate`,
  `paymentPlanCastShapeOK`, `paymentPlanHasTargetDependentModifier`,
  `paymentPlanManaInterference`, `paymentPlanManaUnits`,
  `paymentPlanUnitAlternatives`, `paymentPlanTapOnlyCost`. If you change one
  of those signatures, update the census's call so it still compiles and still
  measures the same thing.
- `rules/cast.go`'s planned executor (`paymentPlanCheck`,
  `paymentPlanStepReady`, `executePlannedManaActivation` on the
  `autopay-exec-harden` branch) calls `paymentPlanManaUnits`,
  `paymentPlanUnitAlternatives`, `paymentPlanStepAlternative`,
  `paymentPlanManaInterference`, `paymentPlanPoolOK`. Keep those names and
  signatures unless your brief says otherwise.
- Search the corpus with `/usr/bin/grep` (bare `grep` is ugrep and skips
  `.cards/`). Line numbers in this brief are at main `6c711fece`; navigate by
  symbol.
- Gates before you hand back: `go vet ./...`; the brief's targeted tests;
  `go test ./rules ./decision ./seat ./host ./host/httpapi ./cmd/repro -run 'PaymentPlan|AutoPayMana|AutoMana' -count=1`;
  `go test ./rules -run 'TestHeads|TestEveryRepoDeck|TestRepoDeckGamesReplayExactly' -count=1`;
  plus the sweep your brief names.

