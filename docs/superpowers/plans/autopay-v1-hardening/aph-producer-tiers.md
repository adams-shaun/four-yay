# Auto-pay: classify every mana ability into normal / last-resort / deferred; plan from normal only

Suggested issue ID: `aph-producer-tiers`
Priority: 1
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: none
Changes offers: yes (fewer PaymentActions: rider-carrying producers stop funding plans)

## Goal

Spec §3.2 (amended) defines three source tiers. This ticket adds the one
rules-owned classifier and makes the planner fund plans from **normal**
abilities only. Last-resort abilities are classified (so later tickets can
admit them) but are NOT yet usable; deferred ones never are. After this ticket
no plan can spend unannounced life, damage, poison, counters or a target.

Today `paymentPlanAltOK` (`rules/payment_plan.go:398`) reads only life,
amount, API, `any` and the production total; `paymentPlanTapOnlyCost`
(`:394`) reads only the cost; nothing reads `ma.Sub`, `Condition*`,
`ConditionCheckSVar$`, targets, or `effMana`'s special-production parameters
(`TriggersWhenSpent$`, `AddsCounters$`, `AddsKeywords*$`, `AddsNoCounter$`,
`PersistentMana$`, `UnlessCost$`, `Defined$` …; `effects/misc.go` `effMana`
honours them). The fewest-sources rank then actively prefers Ancient Tomb
(`{C}{C}`, 2 damage to you) over two basics.

Implement:

1. `paymentPlanAbilityTier(p, id, ma) (tier, consequence, detail)` in
   `rules/payment_plan.go`, with an internal `paymentConsequence` struct
   mirroring spec §4's fields (`sacrifice`, `life`, `damage`, `noUntap`,
   `returnToHand`). Classification per spec §3.2:
   - **normal**: cost exactly `{T}`; no `SubAbility$`; no `Condition*` /
     `ConditionCheckSVar$` on the production; no target anywhere in the chain;
     no `RestrictValid$`; parameter keys within an allowlist (production and
     amount keys, presentation keys such as `SpellDescription`,
     `StackDescription`, `AILogic`, `PrecostDesc`, and the activation gates the
     window already evaluates in `appendAvailableManaAbilitiesGate` /
     `manaActivationGateHolds`, including `CheckSVar`/`SVarCompare` since
     d9153729b). Build the allowlist from what the gate code evaluates; any
     other key fails closed to deferred with `detail = "source:param:<key>"`.
   - **last resort** (classified, excluded in this ticket): the normal shape
     plus only these consequences: cost `Sac<1/CARDNAME…>` with or without
     `{T}` (Treasure's token spelling is `Sac<1/CARDNAME/this token>`);
     cost `PayLife<N>` literal; a `SubAbility$` that is exactly
     `DB$ DealDamage | Defined$ You | NumDmg$ <literal>` with no further
     `SubAbility$` or condition; cost `Return<1/CARDNAME>`; or the
     Undiscovered Paradise rider (`DB$ Pump | Defined$ Self | KW$ HIDDEN …
     return CARDNAME to its owner's hand …`). The source's OWN tap trigger
     (City of Brass) and OWN doesn't-untap replacement (Mana Vault) are
     classified by `aph-interference-scope`, not here.
   - **deferred**: everything else (spec §3.2 list).
2. `paymentPlanUnitAlternatives` / `paymentPlanAltOK` keep only normal
   abilities; carry the tier and consequence on the internal
   `plannedManaActivation` (not on the wire yet).
3. The `Produced$ Any` extension in `paymentPlanManaUnits` goes through the
   same classifier (an `Any` ability with a rider is not normal).
4. Detail strings: `source:rider`, `source:conditional`, `source:target`,
   `source:special_production`, `source:param:<key>`, `source:last_resort`.
   They surface through `PaymentPlanOutcome.Detail` only when no plan exists
   because of them (report the first reason deterministically); if
   `aph-cast-shape-gate` has not landed yet, add the `Detail` field yourself
   with the same meaning and let the second ticket to merge reconcile.

Keep the names/signatures of `paymentPlanManaUnits`,
`paymentPlanUnitAlternatives`, `paymentPlanStepAlternative`,
`paymentPlanTapOnlyCost` (the executor and census call them). Do NOT change
the shared `windowManaUnits` / `manaFreeCost` (attack/unless windows use them).

## Evidence

- Census proofs, `git show 6c0072d63:rules/autopay_fp_proofs_test.go`
  (helpers `fpCorpus`, `fpPlanUses`, `fpSubmitPlan`):
  `TestAutopayFPDamageRiderIsNotPlanned` (executing the `{2}` plan moved life
  20 -> 18 via Ancient Tomb), `TestAutopayFPHarmfulRiderFamilyIsNotPlanned`
  (8/8 fail: Tarnished Citadel, Cryptolith Fragment, Elves of Deep Shadow,
  Mox Poison, Rainbow Vale, Undiscovered Paradise, Witch Engine, Cabal Pit),
  `TestAutopayFPConditionalProductionIsNotPredictedAsFixed` (River of Tears
  after a land drop: plan promised U, execution produced B, cast reversed),
  `TestAutopayFPTriggersWhenSpentIsNotPlanned` (Pyromancer's Goggles).
  Convert their `t.Skip`/observation shape into plain assertions.
- Mirror proof, `git show 0da6e8e59:rules/payment_plan_mirror_findings_test.go`
  `TestPaymentPlanExcludesProducerWithSideEffect` (authored Tomb fixture
  `Produced$ C | Amount$ 2 | SubAbility$ DBHurt`, two Swamps; the witness
  selects the Tomb today).
- Fuzz proofs, `git show 4e5224d34:rules/autopay_fuzz_proofs_test.go`
  `TestPaymentPlanDoesNotSpendUnannouncedLife` (Ancient Tomb planned at 1
  life; the fuzz game lost to its own plan) and
  `TestPaymentPlanRejectsConditionalManaProduction` (lucky Gemstone Caverns
  planned as `{C}`, asked a colour at execution).
- Gaps proof, `git show 124ed89fb:rules/payment_plan_audit_test.go`
  `TestPaymentPlanAuditBugSideEffectProducerSpendsUnannouncedLife`.
- Measured: census (main 0d307f44c) 41 admitted abilities with a SubAbility
  rider and 14 with special-production parameters; gaps census 68 life lost
  over 36 planned payments; mirror 627 planned casts on main 7fb66ee5f with
  side effects between a planned tap and payment (Ancient Tomb 627 of them,
  then threshold/pain lands, Cryptolith Fragment killing a seat), plus Emrakul
  and eldrazi-stompy self-kills and Gemstone Caverns `choice_required`
  fallbacks.

## Files (symbols)

- `rules/payment_plan.go`: new `paymentPlanAbilityTier`, `paymentConsequence`,
  tier constants; `paymentPlanManaUnits`, `paymentPlanUnitAlternatives`,
  `paymentPlanAltOK`, `plannedManaActivation` (+tier, +consequence).
- `rules/autopay_census_test.go`: keep compiling; update its mirror of the
  planner gate order so its "Mirror disagreements" section stays `count: 0`.
- Tests: new `rules/payment_plan_tiers_test.go`.

## Out of scope

- Admitting last-resort sources (`aph-last-resort-plans`).
- Combo/Chosen/ColorIdentity production (`aph-combo-chosen-identity`): classify
  a `Combo` painland half as last-resort damage:1 now, but it still produces
  no alternatives until that ticket.
- Own-trigger / own-replacement / other-object interference
  (`aph-interference-scope`).
- Manual payment and the shared `windowManaUnits` census.

## Done means

- `go test ./rules -run 'TestPaymentPlanTiers' -count=1 -v` passes with 0 SKIP:
  - the four FP proofs, `ExcludesProducerWithSideEffect`,
    `DoesNotSpendUnannouncedLife`, `RejectsConditionalManaProduction` and the
    gaps side-effect proof, as plain assertions (for Ancient Tomb with only a
    `{2}` probe: `Reason == "insufficient"`; with two Swamps: the plan uses
    the Swamps and life is unchanged after executing it);
  - a classification table over corpus producers asserting the tier (and
    consequence) exactly: normal -- Island, Sol Ring, Birds of Paradise,
    Llanowar Elves, Gilded Lotus, Mind Stone's `{T}: Add {C}`; last resort --
    Ancient Tomb (damage 2), Tarnished Citadel's Any ability (damage 3),
    Adarkar Wastes' coloured ability (damage 1), Elves of Deep Shadow
    (damage 1), Mana Confluence (life 1), Horizon Canopy (life 1), Lotus Petal
    (sacrifice), the Treasure and Gold token abilities (sacrifice), Undiscovered
    Paradise (return to hand); deferred -- Witch Engine, Cryptolith Fragment,
    Mox Poison, Rainbow Vale, River of Tears, Gemstone Caverns, Pyromancer's
    Goggles, Ashnod's Altar, Chrome Mox, Cavern of Souls' restricted ability.
- Existing `TestPaymentPlan*` pass unchanged.
- Census: `GORGE_AUTOPAY_CENSUS=<scratch>/c go test ./rules -run TestAutopayManaCensus -count=1 -v`
  then `findings.md` sections "Admitted with a SubAbility rider" and
  "Admitted with special-production parameters" both read `count: 0`, and
  "Mirror disagreements" reads `count: 0`.
- Mirror sweep `go run ./cmd/paymirror -dir .cards -games 300 -seed 1000 -seats 2,4 -formats constructed,random -workers 4 -out <scratch>/m1`
  prints no "side effects of planned activations" entries and no
  `a_fallback=choice_required` from a rider or conditional producer.

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
