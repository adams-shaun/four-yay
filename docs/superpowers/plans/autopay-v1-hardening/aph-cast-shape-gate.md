# Auto-pay: withhold plans from casts whose cost the witness cannot bind

Suggested issue ID: `aph-cast-shape-gate`
Priority: 1
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: none
Changes offers: yes (fewer PaymentActions; no new ones)

## Goal

Spec §3.1 (amended) and PP-08: the planner offers no plan for a cast whose
announced cost or result depends on something the V1 witness cannot carry.
Today three families slip through and produce plans that fall back, abort, or
(with the executor bug being fixed in `autopay-exec-harden`) resolve unpaid:

1. **The spell ability's own `Cost$` non-mana parts.** `PlanCastPayment`
   prices `e.offerCostFor(p, id, e.rawBaseCost(p, id), spellScope(""))`
   (`rules/payment_plan.go:53`); `rawBaseCost` (`rules/mana.go:1522`) parses
   only `Face.ManaCost`. The cast path folds `A:SP$ … | Cost$ 2 G Sac<1/Land>`
   with `withSpellAbilityExtras` (`rules/cast.go:2221`, applied in
   `beginCastWithPayment` for plain casts), so `paymentPlanCostOK`
   (`payment_plan.go:242`) never sees `Sac`, `Discard`, `PayLife`, `Exile`,
   `tapXType` … and admits them. `ValidateCastPayment` (`:233`) repeats the
   same raw pricing. Also check the cost-static non-mana extra that
   `beginCast` folds (`foldAdditionalCost` doc: Soul Immolation's
   `Cost$ Blight<X>` carried in the modifier composition's `extra`); INFERRED
   that `offerCostFor` does not surface it -- verify and decline when present.
2. **Contributions and announcement-time costs.** `paymentPlanCastShapeOK`
   (`payment_plan.go:81-108`) checks AlternateAdditionalCost, optional-cost
   statics, Escalate, Strive and spend readers only. Add: Convoke and
   Improvise read through `e.hasCastConvoke(id)` / `e.hasCastImprovise(id)`
   (`rules/cast.go:1011`, `:1025`; they read the stack-zone derivation, so a
   grant such as Inspiring Statuary's Improvise counts; the comment claiming no
   corpus card grants Improvise is wrong, fix it); Delve via
   `e.HasKeyword(id, "Delve")`; Gift (`Face.HasKeyword("Gift")`, as `giftAsk`
   `cast.go:3409` reads it); Spree/Tiered -- any mode carrying `ModeCost$`
   (use `modeCost`/`modeCostUnparseable` in `rules/spree.go:36`, `:59`, the
   helpers the Spree fold near `cast.go:4178-4200` uses); Replicate, Multikicker and Squad (their asks run
   in `continueCast`, `cast.go:3165`, on a plain Mode "" cast).
3. **Every mana-spent reader.** Beside `faceWantsConverge` / `faceWantsCastSpend`
   (`cast.go:6575`, `:6600`), decline a face whose text reads
   `ConditionManaSpent$`, `Count$Adamant`, `Count$EachSpentToCast`,
   `Count$TotalManaSpent` or `ManaSpentBy` (add a planner-local
   `faceReadsManaSpent(f)` using `Face.Mentions`/SVar scans). The engine does
   not evaluate several of these yet (`effects/conditions.go` lists
   `ConditionManaSpent$` as unimplemented; `effects/count.go` degrades
   `Adamant_`), so this is latent today but becomes a silent misplay the day
   one is implemented.

Also add `Detail string` to `PaymentPlanOutcome` (keep `Reason` exactly
`""`/`"unsupported"`/`"insufficient"`/`"search_limit"`, which existing tests
compare) and set a machine-readable detail for each decline in this ticket:
`shape:additional_cost`, `shape:contribution`, `shape:modal_cost`,
`shape:gift`, `shape:optional_cost`, `shape:mana_spent_reader`,
`shape:target_dependent_cost`, `cost:<kind>` for the existing
`paymentPlanCostOK` exclusions. Later tickets add `source:*` and
`global_mana_effect` details; `aph-plan-diagnostics` aggregates them.

## Evidence

- Gaps audit proofs, `git show 124ed89fb:rules/payment_plan_audit_test.go`:
  `TestPaymentPlanAuditExcludedCastShapes` (X/hybrid/Phyrexian/snow pass;
  sacrifice, convoke and delve additional-cost casts received a plan -- FAIL),
  `TestPaymentPlanAuditUngatedShapesPoseExtraAsks` (corpus Village Rites: plan
  offered, after Submit the pending ask is "Sacrifice a permanent to cast
  Village Rites"; Stoke the Flames: "Choose creatures to help pay"),
  `TestPaymentPlanAuditSpreeCastGetsNoPlan` (Caught in the Crossfire planned,
  fell back `cost_changed`).
- Mirror proofs, `git show 0da6e8e59:rules/payment_plan_mirror_findings_test.go`:
  `TestPaymentPlanWithholdsSpellAbilityAdditionalCosts` (authored Rite/Thrill/
  Toll fixtures: `Cost$ B Sac<1/Creature>`, `Discard<1/Card>`, `PayLife<2>`)
  and `TestPaymentPlanWithholdsCostContributionKeywords` (delve, improvise,
  convoke); all six subtests fail on main: e.g. `plan for delve = &{… Generic:1 …}
  (reason ""), want unsupported`.
- Fuzz proofs, `git show 4e5224d34:rules/autopay_fuzz_proofs_test.go`:
  `TestPaymentPlanSpreeOfferIsExecutable` ("one source: Spree spell needing
  {W}+{1} was offered a one-source plan"; "two sources: planned Spree cast fell
  back (cost_changed)"), `TestPaymentPlanExcludesCastContributions` (convoke,
  improvise, delve, granted improvise), `TestPaymentPlanOfferedForAdditionalCostSpells`
  (Harrow `Cost$ 2 G Sac<1/Land>`, Thrill of Possibility `Discard<1/Card>`).
- Measured impact on main 7fb66ee5f: mirror sweep (2,400 games) 297 planned
  casts ended mid-cast at a priority decision (Gurmag Angler, Village Rites,
  Eldritch Evolution, Murderous Cut, Thrill of Possibility …), 57 `cost_changed`
  fallbacks from convoke/improvise/Spree, 114 casts aborted before payment
  (Spree with no affordable mode; Dance of the Tumbleweeds re-selected five
  times in one game). Fuzz `-autopay all`: 195 `planfb cost_changed` records
  over 72 cards from contributions, 102 records over 20 Spree/Tiered cards.
- Out of the gaps story `autopay-mana-spent-rider-exclusions`: corpus counts
  28 files with `ConditionManaSpent$`, 19 with `Count$Adamant`.

## Files (symbols)

- `rules/payment_plan.go`: `PlanCastPayment` (price
  `withSpellAbilityExtras(f, rawBaseCost)` through `offerCostFor`, then
  `paymentPlanCostOK`; set `Detail`), `ValidateCastPayment` (same pricing),
  `paymentPlanCastShapeOK` (return a detail, e.g. via a new
  `paymentPlanCastShapeDetail(p, id) string`; keep a bool wrapper for the
  census), `PaymentPlanOutcome` (+`Detail`).
- Read-only reuse: `rules/spree.go` `modeCost`, `modeCostUnparseable`;
  `cards.Face.KeywordParam` (Replicate/Multikicker/Squad, as `cast.go:722-761`
  read them); `rules/cast.go` `withSpellAbilityExtras`, `hasCastConvoke`,
  `hasCastImprovise`, `faceWantsConverge`,
  `faceWantsCastSpend`; `rules/cast.go` comment on `hasCastImprovise` (fix the
  false "no corpus card grants Improvise" sentence only).
- Tests: new `rules/payment_plan_shape_gate_test.go`.

## Out of scope

- Supporting any of these shapes in a plan.
- The executor's mid-cast priority defect (ticket `autopay-exec-harden`).
- `sunburstGrantOut` and the `ValidTarget$` cost-static over-reach: those are
  loosened in `aph-interference-scope`; do not touch them here.
- The ordinary cast flow and its asks.

## Done means

- `go test ./rules -run 'TestPaymentPlanShapeGate' -count=1 -v` passes, with
  0 SKIP, covering (port or re-author): the three `Cost$` fixtures (sacrifice,
  discard, life) plus an `Exile<>` and a `tapXType<>` one; delve, improvise,
  convoke, granted improvise (corpus Inspiring Statuary on board), Spree (one
  and two sources), Tiered, Gift, Replicate, Multikicker, Squad; the five
  mana-spent readers on authored faces; each asserts
  `PlanCastPayment(...).Plan == nil`, `Reason == "unsupported"` and the named
  `Detail`, and that the legacy `cast` option (after floating, where needed)
  is still offered unchanged.
- The X/hybrid/Phyrexian/snow cases still decline;
  `TestPaymentPlanPriorityExcludesAlternativeCostCast` still passes; the
  existing `TestPaymentPlan*` suite passes unchanged.
- Real acceptance (quote before/after in the report):
  `go run ./cmd/paymirror -dir .cards -games 300 -seed 1000 -seats 2,4 -formats constructed,random -workers 4 -out <scratch>/m1`
  and `-games 150 -seed 2000 -formats commander -out <scratch>/m2` report no
  `a_fallback=cost_changed` and no `cast_aborted_before_payment` attributable
  to these shapes (any remaining line must name a different cause in your
  report).

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
