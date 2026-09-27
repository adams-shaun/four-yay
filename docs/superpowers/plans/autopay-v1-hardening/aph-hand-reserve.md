# Auto-pay: hand-aware tie-break (keep the colours the rest of your own hand needs)

Suggested issue ID: `aph-hand-reserve`
Priority: 3
Kind: `payment-plan`
Size: S
Lane: pipeline
Depends-On: aph-last-resort-plans
Changes offers: yes (tie-broken plans change on boards with several equal-cost sources)

## Goal

Fill rank key 6 (spec §5 amended; `aph-rank-order` left a zero placeholder).
It sits after cost, creatures, sources, surplus and flexibility, and before
remainder diversity and the typed witness, so it only breaks ties.

1. Demand vector from the acting player's OWN hand (`e.G.Zone(state.ZHand, p)`),
   excluding the card being cast and lands: for each colour W/U/B/R/G, the
   largest number of that colour's pips on any single card's mana cost
   (parse with the rules cost parser; hybrid pips count for both colours;
   Phyrexian colour pips count for their colour; generic, X and colourless do
   not count).
2. Supply of a plan: for each colour, the number of untapped NORMAL eligible
   sources NOT used by the plan that can produce that colour.
3. Coverage per colour `min(supply, demand)`. Order colours by descending
   demand, ties in WUBRG order; compare coverage vectors lexicographically in
   that order; larger coverage ranks first.
4. Compute the demand once per query. Deterministic, no RNG, no event, no
   other seat's hidden zone. Nothing is added to the witness.

The search's branch-and-bound (`aph-rank-search`) prunes only on the monotone
prefix (keys 1-5); this key must not be used for pruning.

## Evidence

- Research probe P5: `{1}{W}` with Forest, Island, Plains -> V1 taps Forest by
  an object-ID tie, stranding a `{G}` card in hand.
- Forge `ComputerUtilMana.sortManaAbilities` (L161-201): among equal-score
  sources for generic, keep the colours most common in the rest of the hand
  ("increases chance AI can play a second spell"); Forge also uses the hand's
  most prominent colour for leftover combo mana.
- Arena patch notes: "floating mana that can be used to cast additional cards
  in hand" (0.16.00.00), "leaving open mana that can pay for more cards"
  (1.07.00.00), and its stated limit "Autotap does not know your plan"
  (2023.29.0) -- hence a tie-break, not a primary key.
- Privacy: the offer and plan are visible only to the acting seat (spec §7);
  the executed taps are public, as with any manual payment.

## Files (symbols)

- `rules/payment_plan.go`: the key-6 computation and its input to
  `rankPaymentPlan`; `planPaymentCost` passes the per-query demand.
- Tests: new `rules/payment_plan_hand_reserve_test.go`.

## Out of scope

- Full castability of each hand card; next-turn planning; bot tap heuristics
  (`botpolicy/tap.go`).

## Done means

- `go test ./rules -run 'TestPaymentPlanHandReserve' -count=1 -v`, battlefield
  order Forest, Island, Plains, casting `{1}{W}`:
  1. a `{G}` instant also in hand -> [Island U, Plains W]; Forest untapped;
  2. a `{U}` instant in hand instead -> [Forest G, Plains W];
  3. no other nonland card -> decided by remainder diversity then the typed
     witness; identical across repeated runs and clones;
  4. changing only the OPPONENT's hand leaves the plan ID unchanged;
  5. keys 1-5 still dominate: with a `{G}` card in hand and `{1}{W}` over
     Forest, Plains and a non-sick `{C}{C}` creature rock, the creature is
     never tapped.
- Existing `TestPaymentPlan*` and the search oracle test (now including key 6)
  pass.

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
