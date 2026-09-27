# Auto-pay: admit "Add {X} or {Y}" (Combo), recorded-colour (Chosen) and commander colour-identity producers

Suggested issue ID: `aph-combo-chosen-identity`
Priority: 1
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: aph-producer-tiers, autopay-exec-harden
Changes offers: yes (more PaymentActions and different witnesses: the largest coverage gap)

## Goal

Spec §3.2 has always required "printed color-choice tap abilities"; the
amendment makes the finite choice shapes normal production. V1 admits none of
them: `windowManaUnits` (`rules/mana_available.go`) drops them because
`cards.ProducedCounts` (`cards/mana_production.go:152`) marks them `any`, and
`paymentPlanManaUnits` (`rules/payment_plan.go:350`) only extends the census
with `Produced$ Any`. Add, for a NORMAL-tier ability (`aph-producer-tiers`):

- `Produced$ Combo <c1> <c2> …` with amount 1: one alternative per listed
  colour. Reuse `manaAbilityComboColours(ma, chosen)`
  (`rules/mana_activation.go:804`), which flattens Combo for the manual wheel
  and substitutes `Chosen`.
- `Produced$ Chosen` / `Combo … Chosen` / `ChosenColor`: the source's recorded
  as-enters colour via `e.chosenProducedColour(id)` (`:1830`) and
  `substituteChosenProduced` (`:1794`); no recorded colour -> no alternative
  for that ability (the fixed ability of a Thriving land stays).
- `Produced$ ColorIdentity` / `Combo ColorIdentity`
  (`isColourIdentityProduced`, `:2412`): one alternative per colour of
  `e.commanderIdentityColours(p)` (`:2427`); no commander or an empty identity
  -> no alternatives.
- `Combo Any` and any Combo with amount > 1 (an allocation) stay deferred.

`flex` for these sources counts their distinct producible types (the full
rank change is `aph-rank-order`; here, just give each alternative the same
unit-level value the existing code gives `Any`).

**Execution** (`rules/cast.go`, after `autopay-exec-harden`): today
`executePlannedManaActivation` rewrites `Produced` only when it is exactly
`Any` (`withProduced(ma, ma, color)`). Move the rewrite into the planner: each
`plannedManaActivation` carries the exact `*cards.SA` to execute (the original
for fixed production; a `withProduced(ma, ma, "<colour>")` copy for
Any/Combo/Chosen/ColorIdentity), built in `paymentPlanUnitAlternatives`, and
the executor resolves `step.exec` with `step.ma` as the original. No colour
prompt may appear. `paymentPlanStepReady`/`paymentPlanStepAlternative`
re-derive the alternatives at every step, so a commander identity or recorded
colour that changed since the offer yields `production_changed` with no extra
code; assert it.

Painland and Talisman coloured halves are Combo + a damage rider: they are
last resort (classified by `aph-producer-tiers`) and must still produce no
alternative until `aph-last-resort-plans`. Their `{C}` ability stays normal.
Path of Ancestry's `TriggersWhenSpent` rider keeps it deferred.

## Evidence

- Census proofs, `git show 6c0072d63:rules/autopay_fp_proofs_test.go`:
  `TestAutopayV2ComboChoiceIsPlanned` (Selesnya Guildgate for a `{W}` instant:
  `Reason: insufficient` today), `TestAutopayV2ChosenColourIsPlanned`
  (Thriving Isle with `ChosenColor = "R"` for `{R}`),
  `TestAutopayV2ColourIdentityIsPlanned` (Command Tower with a `{G}{W}`
  commander designated through `e.G.Players[0].Commanders`).
- Census counts (main 0d307f44c): `Combo` 401 abilities on 400 cards (345
  plain, 45 with a damage rider, 9 IsPresent-gated) -- 51 repo-deck cards in 11
  decks (check/fast/slow lands, Temples, Guildgates, snarls, Celestial
  Colonnade); `Chosen` 31 cards (Thriving lands); colour identity 5 cards,
  Command Tower in 9 and Arcane Signet in 7 of the 15 repo Commander decks.
  The manual path activates 398/401 and 31/31 and 5/5 fine.
- Research probe P2: `{W}{U}` with Plains + a `Combo W U` dual reports
  `insufficient` though manually payable.
- The hearthhull-worldseed-landfall deck has 20 battlefield mana sources, 8
  usable by V1; 11 of the other 12 are Combo lands.

## Files (symbols)

- `rules/payment_plan.go`: `paymentPlanManaUnits`,
  `paymentPlanUnitAlternatives`, `plannedManaActivation` (+`exec *cards.SA`),
  `paymentPlanStepAlternative` (unchanged matching rule: identity AND produces).
- `rules/cast.go`: `executePlannedManaActivation` (use `step.exec`; delete the
  Any-only rewrite).
- Read-only: `rules/mana_activation.go` `manaAbilityComboColours`,
  `substituteChosenProduced`, `chosenProducedColour`,
  `isColourIdentityProduced`, `commanderIdentityColours`, `withProduced`;
  `effects.ComboColours` (`effects/mana_produced.go:22`).
- Tests: new `rules/payment_plan_choice_sources_test.go`.

## Out of scope

- Combo Any / multi-unit allocations; last-resort halves; reflected production.
- Ranking beyond carrying a flex value (`aph-rank-order`).

## Done means

- `go test ./rules -run 'TestPaymentPlanChoiceSources' -count=1 -v` passes, 0 SKIP:
  - Selesnya Guildgate, `{W}` instant: one activation, `Produces[W] == 1`;
    Submit through the offered action leaves priority pending with no colour
    ask, the Guildgate tapped, the spell on the stack, pool empty.
  - Plains + authored `Produced$ Combo W U` land, `{W}{U}`: a plan tapping
    both, the dual producing U (backtracking over the dual still works).
  - Thriving Isle with `ChosenColor = "R"`: `{R}` plan `Produces[R] == 1`;
    with no recorded colour only its fixed `{U}` alternative exists.
  - Command Tower with a `{G}{W}` commander: `{G}` plan uses it; a `{U}`
    instant with only the Tower is `insufficient`; with `Commanders` empty the
    Tower has no alternatives; changing the commander identity after the offer
    makes the Tower step fall back `production_changed` before tapping.
  - Adarkar Wastes: its coloured ability is never planned; its `{C}` is.
  - `ValidateCastPayment` rejects a witness naming a colour outside the
    ability's set.
- Existing `TestPaymentPlan*` (including the dual-land and Produced$ Any
  execution tests) pass.
- Census: `classes.md` row `T: X or Y (Combo)` V1 structural >= 345 and the
  Thriving (`Chosen`) row 31; colour identity >= 3 (Command Tower, Arcane
  Signet, Hidden Hideout; Path of Ancestry stays out).
- Mirror sweep (`-formats constructed,random` 300 games and `-formats commander`
  150 games): planned casts rise versus the pre-ticket run and the equivalence
  rate does not fall; any new non-equivalent verdict is explained in the report.

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
