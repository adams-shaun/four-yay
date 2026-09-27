# Auto-pay: PotentialMana must see type-granted intrinsic mana (Urborg, Prismatic Omen)

Suggested issue ID: `aph-potential-granted-intrinsics`
Priority: 2
Kind: `payment-plan`
Size: S
Lane: pipeline
Depends-On: cr302-6-sick-mana
Changes offers: yes (more PaymentActions; `potential_actions` in views gains casts)

## Goal

Spec §5: the hypothetical discovery path must be a candidate SUPERSET.
`PotentialMana` (`rules/potential.go:64-100`) walks only
`o.Face().ManaAbilities()` (`:82`), so a CR 305.6 intrinsic granted by a
layer-4 basic-land-type change (Urborg, Tomb of Yawgmoth making every land a
Swamp; Prismatic Omen; Dryad of the Ilysian Grove) never reaches candidate
discovery: no plan is offered although the payment windows can fund it
(`appendAvailableManaAbilitiesGate`'s land-type branch in
`rules/mana_activation.go:355`). The same under-bound feeds
`PlayerView.potential_actions` (`PotentialActions`, `potential.go:207`), which
the web client's castable-after-tap stop logic reads.

Use the same ability set the payment windows use (the shared
`availableManaAbilitiesUsing`/gate path), keeping the fixpoint, the
single-tap-per-source rule and the unbounded treatment of indeterminate
production. `cr302-6-sick-mana` edits the same gate (and possibly this file);
land after it and inherit its sickness rule rather than re-implementing it.

## Evidence

- Gaps proof, `git show 124ed89fb:rules/payment_plan_audit_test.go`
  `TestPaymentPlanAuditGapUrborgGrantedIntrinsicPlan`: Urborg + Mountain,
  `{B}{B}` instant: `no payment action for 1 in []decision.PaymentAction(nil)`.
- The census (`TestAutopayObserveTypeGrantedIntrinsic`,
  `git show 6c0072d63:rules/autopay_fp_proofs_test.go`) confirms the planner
  would admit the granted intrinsic (`appendAvailableManaAbilitiesGate`); only
  discovery misses it (a safe false negative today).
- Once offered, the Mountain's granted `{B}` is its SECOND intrinsic ability;
  `TestPaymentPlanGrantedLandTypeExecutesWitnessedColour` on main covers the
  executor side.

## Files (symbols)

- `rules/potential.go`: `PotentialMana` (and `addPotentialMana` only if the
  shared set needs it).
- Tests: new `rules/payment_plan_potential_test.go`.

## Out of scope

- Planner changes; web changes.

## Done means

- `go test ./rules -run 'TestPaymentPlanPotential|TestPotential' -count=1 -v`
  passes, 0 SKIP: Urborg + Mountain offers the `{B}{B}` action, its witness
  uses Urborg and the Mountain's granted `{B}`, Submit produces `[B B]` with
  the spell on the stack, and `PotentialActions(0)` lists the cast.
- `go test ./rules -run 'TestHeads|TestEveryRepoDeck|TestRepoDeckGamesReplayExactly' -count=1`
  unchanged (if a legacy bot reads `PotentialActions`/`PotentialMana` and a
  head moves, STOP and report).
- `go test ./view ./host -count=1` passes (views carry `potential_actions`).

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
