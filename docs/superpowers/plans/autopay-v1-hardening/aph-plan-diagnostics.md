# Auto-pay: surface planner outcomes, details, nodes and fallbacks (engine sink + botbench report)

Suggested issue ID: `aph-plan-diagnostics`
Priority: 2
Kind: `payment-plan`
Size: S
Lane: pipeline
Depends-On: aph-offer-one-pass, aph-interference-scope
Changes offers: no

## Goal

Spec §3.2 ("eligibility and fallback diagnostics must explain which of these
cases occurred") and §5/§10 (amended): measure outcomes. Today
`PaymentActionsForPriority` drops `got.Reason`/`got.Nodes`
(`rules/payment_plan.go:168-171`: `if got.Plan == nil { continue }`), and
fallbacks are counted nowhere; the audits each had to build their own census
to learn `planned=1429 … fallbacks=1`.

1. `rules.PaymentPlanStats` (exported struct of plain counters and
   `map[string]int` histograms): priority decisions built, actions offered,
   plans offered, per-cast outcomes by `Reason` and by `Detail` (the vocabulary
   set by `aph-cast-shape-gate`, `aph-producer-tiers`,
   `aph-interference-scope`), search nodes (sum and max), `search_limit` hits,
   planned submissions, fallbacks by reason (`cost_changed`,
   `source_changed`, `production_changed`, `choice_required`).
2. `(*Engine).SetPaymentPlanStats(*PaymentPlanStats)`; nil (the default) costs
   nothing. Record in the builder (every candidate outcome), at Submit (a
   planned submission) and at the one fallback site `paymentPlanFallback`
   (`rules/cast.go`). No event, no RNG, no effect on any offer. `Engine.Clone`
   does NOT carry the sink (spec §7: never share a pointer across engines).
   Map iteration must not reach any event or output order (sort keys when
   printing).
3. `cmd/botbench`: `-payment-stats` prints the aggregated stats after the run
   (sorted), for any policy; the default report and `-out json` of a run
   without the flag are byte-identical to main.
4. Never put reasons or details on the wire to another seat (they can reveal
   hand contents); they stay engine/tool side.

## Evidence

- `rules/payment_plan.go:168` discards `Reason`/`Nodes`; the 2026-09-25
  acceptance report has no offer-cost or fallback numbers; the gaps audit
  built `internal/bench/autopay_audit_test.go` (proof branch 124ed89fb) to get
  `planned=1429 completed=1232 reversed=195 … fallbacks=1 reasons=map[cost_changed:1]`.
- The fuzz audit added `cardfuzz -stats` counters for the same reason.

## Files (symbols)

- `rules/payment_plan.go`: `PaymentActionsForPriority` (record), new
  `PaymentPlanStats`, `SetPaymentPlanStats`.
- `rules/engine.go`: Submit (planned submission count); `rules/clone.go`
  (explicitly not cloned).
- `rules/cast.go`: `paymentPlanFallback` (one counter call).
- `cmd/botbench/main.go`: `-payment-stats`.
- Tests: `rules/payment_plan_stats_test.go`; a botbench flag test.

## Out of scope

- Publishing reasons to clients; web display of reasons.

## Done means

- `go test ./rules -run TestPaymentPlanStats -count=1 -v`: a board with one
  insufficient cast, one unsupported X spell (`cost:x`), one Spree spell
  (`shape:modal_cost`) and one plannable cast reports exactly those outcomes,
  node totals > 0, and one fallback after a forced post-offer cost raise;
  attaching the sink changes no event, head or offer (compare with a sinkless
  engine); a clone has no sink.
- `go run ./cmd/botbench -a bot-auto-pay -b bot -pairs all -games 2 -seed 1 -payment-stats`
  prints offered actions, the reason/detail histograms, planned submissions and
  fallback reasons; the same run without the flag and with `-out json` is
  byte-identical to main.

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

