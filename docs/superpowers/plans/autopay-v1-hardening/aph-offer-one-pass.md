# Auto-pay: build every offer from one candidate walk and the pending Options

Suggested issue ID: `aph-offer-one-pass`
Priority: 2
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: aph-lazy-offers, aph-interference-scope
Changes offers: no (byte-identical actions, plans and IDs)

## Goal

After `aph-lazy-offers` only consumers pay for the builder, but for them it is
still N+2 legal-action walks per priority decision.
`PaymentActionsForPriority` (`rules/payment_plan.go:147-193`) runs
`legalActionsPriced(p, &PotentialMana(p))` for candidates, then `legalActions(p)`
again just to find `BaseOptionIndex` (a duplicate of the walk `askPriority`
already did), then per candidate `PlanCastPayment`, whose
`paymentPlanCastCandidate` (`:64-76`) runs ANOTHER full `legalActionsPriced`
against a 2^28 hypothetical pool for that one card.

1. `PaymentActionsForPriority` takes the pending decision's `Options` (or is
   given them by `EnsurePaymentActions` and the Submit-time build) instead of
   calling `legalActions` for `BaseOptionIndex`.
2. One candidate walk per build: run the huge-pool hypothetical walk ONCE for
   the player and derive the plain-cast candidate set from it; the
   `PotentialMana` walk's superset role is kept only if it filters anything the
   huge-pool walk does not (verify; if both are needed, run each once).
   Split `PlanCastPayment` into the public entry (unchanged behaviour: it still
   runs the candidate check for a single cast, as `ValidateCastPayment` and
   tests call it) and an internal `planCastPaymentChecked` the builder calls
   after its one walk.
3. Collect cost statics once per build (`collectCostStatics`) and pass them
   down instead of re-collecting per candidate, where the code already has a
   `…Using(statics, …)` variant.
4. Add `BenchmarkPriorityAskPaymentActions` in rules pinning the cost on a
   fixed mid-game board.

## Evidence

- Gaps audit pprof (constructed botbench, before lazy offers):
  `PaymentActionsForPriority` 117.84 s of 221.76 s; within it
  `paymentPlanCastCandidate` 18.31 s, hypothetical `legalActionsPriced`
  37.02 s, duplicate `legalActions` 35.19 s, `PotentialMana` 3.79 s, shape
  gate 4.52 s.
- Fuzz audit: publication was 53.6-58.6% of CPU in ordinary games; an
  auto-pay seat still pays all of it on every one of its priority decisions.

## Files (symbols)

- `rules/payment_plan.go`: `PaymentActionsForPriority` (signature may take the
  options), `PlanCastPayment` / new internal checked variant,
  `paymentPlanCastCandidate`.
- `rules/engine.go`: `EnsurePaymentActions` and the Submit-time build pass the
  pending options.
- `rules/autopay_census_test.go`: keep compiling (it calls
  `paymentPlanCastCandidate`).
- Tests: `rules/payment_plan_one_pass_test.go` (+ the benchmark).

## Out of scope

- Planner eligibility, ranking and search.

## Done means

- A rules test builds offers with the old path (keep a test-only reference
  builder or compare against recorded offers) and the new one on at least
  six fixture boards and 200 priority decisions of a fixed-seed auto-pay game:
  `reflect.DeepEqual` on every `[]PaymentAction`, IDs included.
- `go run ./cmd/cardfuzz -dir .cards -games 150 -batch 150 -workers 1 -seed 5550101 -autopay all -state <fresh> -failures <f> -stats <s>`:
  `stats`, `kinds`, `sigs` equal the pre-ticket run; `game_seconds` falls
  (quote both); a `-verify` run replays byte-identically.
- `go run ./cmd/botbench -a bot-auto-pay -b bot -pairs all -games 2 -seed 1 -workers 4 -out json`
  is byte-identical to the pre-ticket output; quote user CPU before/after.
- `go test ./rules -run 'PaymentPlan' -bench BenchmarkPriorityAskPaymentActions -count=1`
  runs; quote ns/op before/after.

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

