# Auto-pay: scope mana/tap interference to the sources it can affect; one global reason for the rest

Suggested issue ID: `aph-interference-scope`
Priority: 1
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: aph-combo-chosen-identity, aph-cast-shape-gate
Changes offers: yes (plans appear on boards where any player controls a tap/mana/untap card; a few interfering sources leave plans)

## Goal

Spec §3.2 (amended): "A trigger or replacement on source S affects only S's
tier … Only a global effect whose scope the planner cannot prove declines every
plan." Today `paymentPlanManaInterference` (`rules/payment_plan.go:519-548`)
declines EVERY plan of EVERY player when ANY battlefield face has a `Taps` /
`TapsForMana` trigger, or a replacement whose event name merely contains
"mana" or "tap" (which catches `Untap`, i.e. every "doesn't untap" card, and
`LoseMana`), or when any effect-created `ProduceMana` replacement is active.

Implement:

1. **Per-source classification** (extend `aph-producer-tiers`' classifier;
   called for each candidate ability in `paymentPlanUnitAlternatives`):
   - the source's OWN `T:Mode$ Taps | ValidCard$ Card.Self` trigger whose
     effect is exactly `DealDamage | Defined$ You | NumDmg$ <literal>` (City
     of Brass) -> last resort, consequence `damage:N`;
   - the source's OWN `R:Event$ Untap | ValidCard$ Card.Self | Layer$ CantHappen`
     doesn't-untap replacement (Mana Vault, Grim Monolith, Basalt Monolith) ->
     last resort, consequence `no_untap`. Their other triggers (Mana Vault's
     upkeep/draw `Phase` triggers) do not act on a tap or a production and do
     not change the tier;
   - any other own trigger/replacement that acts on the tap or the mana ->
     deferred;
   - another object's `Taps`/`TapsForMana` trigger or `ProduceMana`
     replacement (printed on a face, or effect-created in `e.active()`) whose
     `ValidCard$` / `ValidActivator$` / `Activator$` / `TriggerZones$` can
     match THIS source's activation -> this source deferred (Wild Growth's
     enchanted land, Utopia Sprawl, Crypt Ghast's Swamps, Mana Flare, Mana
     Reflection -> all of its controller's sources, Manabarbs). Reuse the real
     matchers (`rules/trigmatch_tap.go` / `rules/trigger_match.go` for the trigger's ValidCard/Activator,
     `rules/replacement.go` for ProduceMana) against a hypothetical tap of the
     candidate; do not write a second filter grammar.
   - `Untap` and `LoseMana` replacements on other objects are ignored.
   Last-resort results stay unusable until `aph-last-resort-plans`.
2. **Global remainder**: keep `paymentPlanManaInterference()` with its current
   zero-argument signature (the in-flight executor calls it before each later
   step) but make it return true ONLY for an effect whose scope cannot be
   proved (an effect-created `ProduceMana` replacement with no matchable
   filter) or a `ManaConvert` static that reaches the payer (Celestial Dawn:
   `S:Mode$ ManaConvert | ValidPlayer$ You …`; find it the way the payment
   path does, `e.paymentConv(p, id, false)` in `rules/stack.go:684`). Since it
   is now player-dependent, add `paymentPlanGlobalManaEffect(p) (bool, detail)`
   used by `PlanCastPayment` and keep the zero-arg function as a thin wrapper
   over the acting players if the executor still needs it; set
   `Detail = "global_mana_effect:<card name>"`.
3. **Cast-shape over-reaches** (planner-local; do NOT change the shared
   capture gates, which feed pay-time `CastInfo` events and could move heads):
   - replace the `sunburstGrantOut()` call in `paymentPlanCastShapeOK` with a
     planner-local check that a battlefield face GRANTS sunburst to spells
     (`Keywords$ Sunburst` / `AddKeyword$ Sunburst` in an Animate/Pump/
     Continuous body), not that it merely mentions it (Engineered Explosives
     has its own `K:Sunburst`);
   - `paymentPlanHasTargetDependentModifier` (`:115`) strips `ValidTarget$` and
     so taxes every spell: decline only when the cast face's spell ability (or
     its chain) actually targets. An untargeted instant is not declined by an
     opponent's Syr Elenora or Icefall Regent.

Keep `TestPaymentPlanDeclinesEffectCreatedProduceManaReplacement` passing (an
unscoped effect-created replacement still declines).

## Evidence

- Census proof, `git show 6c0072d63:rules/autopay_fp_proofs_test.go`
  `TestAutopayV2InterferenceIsScopedToAffectedSources`: opponent's Mana Vault /
  City of Brass / Claustrophobia -> `{Plan:<nil> Reason:unsupported}` for an
  Island `{U}` cast (all three fail on main).
- Mirror proof, `git show 0da6e8e59:rules/payment_plan_mirror_findings_test.go`
  `TestPaymentPlanRespectsManaConvertStatics` (authored Dawn fixture + a G
  elf: a one-activation plan for `{G}` is offered; the real payment cannot
  spend G under the static). Mirror seed 3072 random, Sylvan Paradise at seq
  2070: `a_witness:pool_after`, then a manual window with NO PaymentFallback.
- Census kill-switch run (all 35,473 corpus faces placed one at a time on the
  opponent's battlefield, Island + `{1}` probe on seat 0): 400 faces decline
  every plan -- 346 through `paymentPlanManaInterference` (`replacement:Untap`
  156, `trigger:Taps` 113, `trigger:TapsForMana` 64, `replacement:ProduceMana`
  11, `replacement:LoseMana` 4) and 54 through cast-shape gates (cast-spend
  readers 21, faces that merely mention Sunburst 18, ValidTarget cost statics
  13, converge readers 2). 15 are repo-deck cards: Mana Vault, Grim Monolith,
  Basalt Monolith, City of Brass, Wild Growth, Crypt Ghast, Manabarbs,
  Badgermole Cub, Forsaken Monument, Virtue of Strength, Captain America
  Living Legend, C.A.M.P., T-45 Power Armor, Engineered Explosives, Syr
  Elenora. Research repo census: 9 of 29 decks carry a whole-plan-disabling
  card.
- Research probe P3: an opponent's City-of-Brass-shape or Mana-Vault-shape
  permanent makes seat 0's `{U}` plan `unsupported`.

## Files (symbols)

- `rules/payment_plan.go`: `paymentPlanManaInterference` (narrowed, same
  signature), new `paymentPlanGlobalManaEffect`, the per-source check inside
  the classifier/`paymentPlanUnitAlternatives`, `paymentPlanCastShapeOK`
  (sunburst), `paymentPlanHasTargetDependentModifier`, `PlanCastPayment`.
- Read-only: trigger/replacement matchers in `rules/trigmatch_tap.go`, `rules/trigger_match.go`,
  `rules/replacement.go`; `rules/stack.go` `paymentConv`; `rules/cast.go`
  `sunburstGrantOut` (do not modify).
- `rules/autopay_census_test.go`: keep compiling; its kill-switch section must
  still run.
- Tests: new `rules/payment_plan_interference_test.go`.

## Out of scope

- Modelling the extra mana of a matching trigger (Wild Growth's +G): deferred.
- Admitting last-resort sources (`aph-last-resort-plans`).
- The shared capture gates in `rules/cast.go`.

## Done means

- `go test ./rules -run 'TestPaymentPlanInterference|TestPaymentPlanDeclinesEffectCreatedProduceManaReplacement' -count=1 -v`
  passes, 0 SKIP:
  - opponent's Mana Vault / City of Brass / Claustrophobia / Manabarbs /
    Engineered Explosives / Syr Elenora (corpus): seat 0's Island plan for an
    untargeted `{U}` instant exists and is identical to the plan without them;
  - seat 0's own Wild Growth on Island A with an untouched Island B: the `{U}`
    plan uses B, never A;
  - seat 0's own City of Brass and Mana Vault: never in a plan (classified
    last resort `damage:1` / `no_untap`, asserted through the classifier), and
    other seat-0 sources still fund plans;
  - Mana Reflection on seat 0: seat 0 has no plan (every source deferred,
    detail names Mana Reflection); seat 1's plans are unaffected;
  - the Celestial Dawn fixture: no plan, `Detail` starts with
    `global_mana_effect`;
  - a targeted spell under an opponent's Syr Elenora still declines
    (`shape:target_dependent_cost`).
- Census kill-switch rerun (`GORGE_AUTOPAY_CENSUS=<dir> …`): `killswitch.md`
  rows for `replacement:Untap`, `replacement:LoseMana`, the Sunburst mention and
  the ValidTarget cost static report 0; `trigger:Taps`/`TapsForMana` from the
  opponent's side report 0.
- Mirror sweep (constructed,random 300 and commander 150): planned casts do
  not fall and equivalence does not drop; report the new counts.

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
