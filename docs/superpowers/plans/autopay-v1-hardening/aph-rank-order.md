# Auto-pay: rank plans by cost tier, then creatures, then sources (Arena/Forge order); numeric tie-break

Suggested issue ID: `aph-rank-order`
Priority: 2
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: aph-interference-scope
Changes offers: yes (different recommended witness on many boards)

## Goal

Spec §5 (amended) replaces the V1 rank key. Today `paymentPlanRank.less`
(`rules/payment_plan.go:557-571`) orders by (1) number of sources,
(2) creatures, (3) surplus, (4) flex = number of *alternatives* of each chosen
unit (`out[i].flex = len(out)`, `:442-444`, so an `Any` source counts 5), and
(5) `fmt.Sprint(p.Activations)` string compare, under which object ID 10
sorts before 9. Implement the amended tuple, lower first:

1. **Irreversible cost** -- sum of the consequence weights over the plan's
   steps (sacrifice-self non-creature 10, creature 20; doesn't-untap 25; life
   paid or damage taken 3 per point; return-to-hand 8). Named constants in
   `rules/payment_plan.go` next to the rank. In this ticket no last-resort
   step is publishable yet, so this key is 0 for every offered plan; implement
   and unit-test it on hand-built rank inputs so `aph-last-resort-plans` only
   has to feed it.
2. Creature sources activated.
3. Newly activated sources.
4. Surplus (`pool_after` total).
5. Flexibility consumed: sum over chosen sources of the number of DISTINCT
   mana types (W/U/B/R/G/C) the source's eligible alternatives produce.
6. Hand reserve: a zero placeholder field, filled by `aph-hand-reserve`.
7. Remainder diversity: more distinct colours still producible by the untapped
   NORMAL eligible sources the plan does not use (compute once per query from
   the units; the plan's chosen sources are removed).
8. Typed witness compare: step by step, (Source numeric, Ability.Kind, Face,
   Index, Intrinsic, Produces vector) then length; delete the string key.

`rankPaymentPlan` needs the unit set to compute keys 5 and 7; pass what it
needs from `planPaymentCost` without changing the search itself (the search
rewrite is `aph-rank-search`; the existing DFS still visits every complete
plan within budget on the test boards below).

## Evidence

- Research audit probes on authored cards (gorge bdb2b7e6d; planner files
  unchanged through main):
  - P1: `{2}{U}` with Island x3 + a land "T: add {C}{C}, 2 damage to you" ->
    V1 picks the damage land + Island (fewest sources). After
    `aph-producer-tiers` that land is last resort, so the plan is Island x3.
  - P8: `{2}{G}` with Forest x3 + an untapped non-sick artifact creature
    "T: add {C}{C}" -> V1 taps Forest + the creature; expected Forest x3
    (creatures before sources).
  - P6: `{1}{U}` with Island + a land printing "T: Any, 3 damage" BEFORE
    "T: add {C}" -> V1 uses the painful ability for generic; after tiers the
    painful ability is last resort and the `{C}` ability is used.
  - P5: `{1}{W}` with Forest, Island, Plains -> V1 taps Forest by an
    object-ID tie; that is `aph-hand-reserve`'s case, not this ticket's.
- Shared practice (research report): non-creatures before creatures (Forge
  +13 per attack/block ability, manabrew +26, XMage +2, Arena "QQ taps all
  non-creatures"); Arena "more likely to tap a creature than take damage from
  a pain land" (2022.20.0); "prefer to leave up a diverse spread of mana"
  (1.11.00, 2020-08-13); Arena weights "favor sacrificing two treasures before
  tapping Manavault or Grim Monolith. It will tap those before sacrificing
  three treasures" (2026.56.10).
- `fmt.Sprint` compare: `rules/payment_plan.go:573`, `:570`.

## Files (symbols)

- `rules/payment_plan.go`: `paymentPlanRank`, `less`, `rankPaymentPlan`,
  the flex assignment at the end of `paymentPlanUnitAlternatives`,
  `planPaymentCost` (only to pass rank inputs), new weight constants.
- Tests: new `rules/payment_plan_rank_test.go`.

## Out of scope

- The search algorithm (`aph-rank-search`); hand reserve (`aph-hand-reserve`);
  publishing last-resort plans (`aph-last-resort-plans`).

## Done means

- `go test ./rules -run 'TestPaymentPlanRank' -count=1 -v` passes, asserting
  exact activation lists on authored boards:
  1. `{2}{G}`, Forest x3 + non-sick artifact creature `T: {C}{C}` ->
     Forest, Forest, Forest; the creature stays untapped.
  2. `{1}{U}`, Island + land with `T: Any` + DealDamage-3 rider printed before
     `T: {C}` -> [Island U, land C].
  3. `{2}{U}`, Island x3 + `T: {C}{C}` DealDamage-2 land -> Island x3.
  4. `{1}{G}`, Forest, Forest, Forest, Island -> two Forests; the Island stays
     untapped (remainder diversity).
  5. PP-03 (Island/Swamp/Mountain vs Badlands for `{1}{U}{B}`) still prefers
     Mountain; an `Any` rock (Gilded Lotus shape) counts flex 5 and a
     Boros-typed dual 2.
  6. Tie-break: two identical Islands with object IDs 9 and 10 -> the plan
     uses 9.
  7. Cost key unit test on constructed rank inputs: 2 sacrifice-self
     non-creature (20) < one no_untap (25) < 3 sacrifices (30); damage 2 (6)
     < one sacrifice (10); any cost > 0 loses to cost 0 regardless of later
     keys.
  8. Re-running the query and cloning the engine yield identical plan IDs.
- Existing `TestPaymentPlan*` pass (update an assertion only where it pinned
  the old order, and name it in the commit message).
- Mirror sweep (constructed,random 300): equivalence does not drop.

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

