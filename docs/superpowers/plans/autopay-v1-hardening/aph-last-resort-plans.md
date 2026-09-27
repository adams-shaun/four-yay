# Auto-pay: last-resort plans (sacrifice-self, life, damage, doesn't-untap, return-to-hand) with disclosed consequences

Suggested issue ID: `aph-last-resort-plans`
Priority: 2
Kind: `payment-plan`
Size: L
Lane: pipeline
Depends-On: aph-rank-search, autopay-exec-harden
Changes offers: yes (new plans where no normal plan exists; wire field added; TS types regenerated)

## Goal

Operator decision 2 / spec §3.2, §4, §5, §6 (amended): a plan may use a
last-resort source only when no plan from normal sources exists; every such
step discloses its consequence in the witness; the plan ranks by the
Arena-calibrated weights; bots use such plans automatically; humans confirm
life payments in the client (`aph-last-resort-web`); a caretaker standing in
for a human never auto-selects a life-paying plan. After `aph-producer-tiers`
and `aph-interference-scope` these sources are already classified with their
consequence; this ticket makes them usable end to end.

1. **Wire** (`decision/payment_plan.go`): add `PaymentConsequence` and
   `PaymentActivation.Consequence *PaymentConsequence` exactly as spec §4
   (json `consequence,omitempty`; fields `sacrifice`, `life`, `damage`,
   `no_untap`, `return_to_hand`, all omitempty). `validateShape`: a present
   consequence must set at least one field; quantities bounded like the rest.
   `ClonePaymentPlan` deep-copies the pointer. Canonical encoding
   (`appendPaymentPlanCanonical`): append the trailer ONLY when some step has
   a consequence -- `string("consequences")`, then per activation
   `u32(flags)` (bit0 sacrifice, bit1 no_untap, bit2 return_to_hand),
   `u32(life)`, `u32(damage)` -- so every existing plan ID is byte-identical.
   Update the codec note
   `docs/superpowers/plans/cast-payment-plans/payment-plan-v1-codec.md` and the
   `PaymentPlanV1` comment ("frozen … must use a new version" is withdrawn by
   the amendment: additive, pinned by goldens). Regenerate TypeScript with
   `go run ./cmd/gentypes` (`web/src/protocol.ts`); `-check` must pass.
2. **Planner** (`rules/payment_plan.go`): extend `paymentPlanManaUnits` with a
   unit walk that also collects last-resort abilities (their costs are outside
   `manaFreeCost`, so `windowManaUnits` never lists them; walk
   `availableManaAbilitiesForWindow(p, id, false)` and keep abilities the
   classifier marks last resort; Treasure's `T Sac<1/CARDNAME/this token>` +
   `Produced$ Any` expands to five colour alternatives like `Any`; painland /
   Talisman / horizon-land `Combo` halves expand like Combo). The alternative
   carries `activation.Consequence`. Phase 2 of the search (built by
   `aph-rank-search`) runs only when phase 1 over normal classes returns
   `insufficient`; rank key 1 gets the weights (`aph-rank-order`'s constants;
   sacrifice weight 20 when the source is a creature, e.g. Eldrazi Spawn).
   Never offer a plan whose summed `life + damage` is >= the caster's current
   life. A `PayLife<N>` step requires the payer's life >= N at plan time
   (CR 119.4; the gate `manaAbilityPayablePool` should already enforce it --
   verify). `paymentPlanStepAlternative` compares `Consequence` too.
   `paymentWitness` copies consequences into the plan.
3. **Execution** (`rules/cast.go`, after `autopay-exec-harden`): steps run
   through the ordinary `resolveManaAbilityRefOriginal` path as today, with
   the planner-built `exec` SA (`aph-combo-chosen-identity`). Verify, and
   assert in tests, that:
   - `Sac<1/CARDNAME>` with the source as the only candidate is settled by
     `continueManaDiscard` (`rules/mana_activation.go:1180`) WITHOUT an ask
     (the "exactly N candidates" forced branch), and the Treasure's typed
     producer provenance survives;
   - `PayLife<N>` is paid by the ordinary cost settle (a `LifeChange` event),
     never by writing `Life`;
   - a DealDamage rider, City of Brass's Taps trigger, Mana Vault's untap
     replacement and Undiscovered Paradise's return rider behave exactly as for
     a manual activation (the Taps trigger goes on the stack after the cast's
     payment, like a manual activation in the 601.2g window);
   - `paymentPlanCheck` (exec-harden's per-step revalidation) re-derives the
     step's consequence (a change is `production_changed`) and, before the
     first and every later step, falls back `cost_changed` if the remaining
     steps' `life + damage` would now be lethal.
4. **Caretaker** (`host/match.go` ~490 builds caretakers with
   `NewBotPolicySeatWithAutoPayMana`; `host/bot_policy.go:48`): a caretaker
   for a human seat must not submit a plan with any `life > 0` step. Add a
   `seat.Bot` option (e.g. `SkipLifePlans()`), set it only for caretakers,
   and in `paymentIntent` treat such an action as having no plan (the policy
   then sees the legacy options). Hosted bots and tool seats keep taking
   `Plans[0]`. The bot-adapter wave also edits `seat/bot.go`; keep the change
   minimal and isolated.

## Evidence

- Census classes (main 0d307f44c, V1 admits 0 of each): `T + sacrifice self`
  51 abilities / 48 cards + 3 tokens and `sacrifice self (no T)` 29 / 25 + 4
  tokens -- Treasure has 373 corpus creators and appears via 6 repo decks;
  Lotus Petal in 4 decks; Eldrazi Scion in 3; `pay life` 40 (Mana Confluence,
  horizon lands, Fiery Islet; 3 repo decks); Ancient Tomb in 10 repo decks;
  Mana Vault / Grim Monolith / Basalt Monolith in 3 / 2 / 1; City of Brass 2.
- Research probes: P4 `{1}{W}` with Treasure + Plains -> V1 no plan (expected:
  a last-resort Treasure plan); with an Island added -> Plains + Island (the
  Treasure untouched). P7 a land that returns to hand was used silently.
- Arena (research report §2.5): life costs prompt a confirmation; "favor
  sacrificing two treasures before tapping Manavault or Grim Monolith. It will
  tap those before sacrificing three treasures" (2026.56.10); damage preferred
  over sacrificing a permanent (State of the Game DMU 2022-08-31). Forge's
  human path never auto-guesses a non-undoable ability; manabrew confirms a
  Treasure sacrifice.
- Fuzz: Ancient Tomb at 1 life killed its own caster (the reason for the
  lethal guard); mirror: Emrakul/eldrazi-stompy self-kills.

## Files (symbols)

- `decision/payment_plan.go`: `PaymentConsequence`, `PaymentActivation`,
  `ClonePaymentPlan`, `validateShape`, `appendPaymentPlanCanonical`,
  `PaymentPlanV1` comment. `web/src/protocol.ts` (generated only).
- `rules/payment_plan.go`: `paymentPlanManaUnits`,
  `paymentPlanUnitAlternatives`, `paymentPlanStepAlternative`,
  `paymentWitness`, `planPaymentCost` (phase 2 wiring, lethal guard),
  `rankPaymentPlan` (key 1 inputs), `ValidateCastPayment`.
- `rules/cast.go`: `paymentPlanCheck` (consequence + lethal revalidation).
- `seat/bot.go` (`paymentIntent`, new option), `host/match.go` (caretaker
  construction), `host/bot_policy.go` if the option is plumbed there.
- `docs/superpowers/plans/cast-payment-plans/payment-plan-v1-codec.md`.
- Tests: `decision/payment_plan_consequence_test.go`,
  `rules/payment_plan_last_resort_test.go`, a seat/host caretaker test.

## Out of scope

- Web disclosure and confirmation (`aph-last-resort-web`).
- The deferred tier: sacrificing or tapping another permanent, discard/exile/
  counter costs, mana-costed abilities, dynamic/reflected/restricted
  production, production-altering effects, riders other than the listed ones.
- Phyrexian/hybrid life elections for the spell's own cost.
- Bot policy changes beyond the caretaker option.

## Done means

- `go test ./decision -run 'TestPaymentPlan' -count=1 -v`: the existing
  identity golden (`TestPaymentPlanIdentityIsIndependentOfPresentation`) is
  unchanged; a new fixture with one `sacrifice` step and one `life:1` step pins
  its plan ID; a zero-valued consequence and an over-bound `life` reject; JSON
  round-trips; a clone does not alias the consequence.
- `go test ./rules -run 'TestPaymentPlanLastResort|TestPaymentPlanDecisionMadeGolden' -count=1 -v`
  (0 SKIP), each asserting exact activation lists and post-Submit state:
  1. `{1}{W}`, Plains + Treasure (corpus token or authored
     `Cost$ T Sac<1/CARDNAME> | Produced$ Any`): the only plan is
     [Plains W, Treasure <colour>] with `Consequence{Sacrifice:true}` on the
     Treasure; after Submit the Treasure has left the battlefield, Plains is
     tapped, the spell is on the stack, the pool is empty, and no decision
     other than priority was asked between Submit and the spell reaching the
     stack.
  2. Same board plus an Island: the plan is [Plains, Island]; the Treasure is
     untouched.
  3. `{2}`, Treasure x2 + authored Mana Vault shape (`T: {C}{C}{C}` + own
     `R:Event$ Untap … Card.Self`): both Treasures (20 < 25). `{3}`,
     Treasure x3 + the same: the Vault alone (25 < 30).
  4. Island + Mana Confluence, `{1}{U}`: both; the Confluence step has
     `life:1`; life 20 -> 19 through a `LifeChange`-class event. At 1 life the
     Confluence has no alternative and the cast is `insufficient`.
  5. `{2}` with only Ancient Tomb: plan with `damage:2` at 20 life; at 2 life
     no plan (lethal guard).
  6. City of Brass alone for `{R}`: `damage:1`; after Submit the Taps trigger
     is on the stack above the spell, as with a manual activation.
  7. Undiscovered Paradise alone for `{G}`: `return_to_hand`.
  8. A normal plan always wins: Forest + Llanowar Elves (non-sick) + Treasure
     for `{1}{G}` taps Forest and the Elves, not the Treasure.
  9. Changes between offer and execution fall back before any further tap:
     the Treasure removed by another effect -> `source_changed`; the caster's
     life lowered so the remaining `life + damage` is lethal -> `cost_changed`.
  10. `TestPaymentPlanDecisionMadeGolden` unchanged.
- Caretaker test: a caretaker `seat.Bot` with the option set, offered only a
  life-paying plan, returns a legacy intent; the same bot without the option
  submits the plan.
- `go run ./cmd/gentypes -check` passes.
- Sweeps: mirror (constructed,random 300; commander 150) -- report planned
  casts and equivalence; a last-resort step must be equivalent to the float
  route or the difference must be a documented CR 601.2 ordering case (spec
  amendment item 5). `cardfuzz -autopay all` 1,000 games (seed 26092600):
  no new `planrev`/`planfb` signature from a last-resort step; `-verify`
  replays byte-identically.

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

