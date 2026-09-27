# Auto-pay: run the A/B payment mirror inside cardfuzz and botbench (opt-in)

Suggested issue ID: `aph-mirror-in-fuzz-bench`
Priority: 3
Kind: `payment-plan`
Size: S
Lane: pipeline
Depends-On: aph-lazy-offers
Changes offers: no (tooling; default outputs unchanged)

## Goal

Make the A/B mirror a standing regression net rather than a separate tool run.
`internal/paymirror` (merged `edb14c94c`) checks, at each priority decision
where a seat submits `Intent{Payment}`, that a pre-submit clone floating every
witness step via the ordinary `activate` option and casting via the ordinary
option reaches an equivalent engine (reflective walk of `rules.Engine` plus
the event multiset, with documented exclusions). `paymirror.CheckLive(e, in,
answer, Options{…})` takes a live engine at a priority decision plus the
seat's planned intent and leaves the engine exactly where the planned cast
leaves it (verify the current signature in `internal/paymirror/paymirror.go`).

1. `cmd/cardfuzz`: `-autopay-mirror` (default off). In the drive loop, when an
   auto-pay seat's intent carries `Payment`, call the checker instead of
   `Submit`; write a failure record with `kind:"mirror"` and the report's
   verdict key as `sig` for every non-equivalent report. `-repro` of such a
   record re-runs the checker.
2. `cmd/botbench`: `-autopay-mirror` for any policy whose seats auto-pay
   (`bot-auto-pay`); print a verdict histogram at the end.
3. Both: off by default; outputs without the flag byte-identical to main
   (the check roughly doubles the cost of a planned cast: two clones plus a
   reflective diff).

## Evidence

- Mirror audit: the checker found every executor/planner defect of the audit
  from ordinary bot games (base 0d307f44c: 53,647 planned casts, 91.6%
  equivalent; main 7fb66ee5f: 52,169, 99.0%), at ~120 planned casts per second
  on 4 cores; the curated acceptance suite had none of them.
- `cmd/cardfuzz -autopay` (merged 6c711fece) already records `planrev`/`planfb`
  failures; the mirror adds state-equivalence.

## Files (symbols)

- `cmd/cardfuzz/main.go`, `cmd/cardfuzz/autopay.go` (flag, drive loop, record
  kind, `-repro`).
- `cmd/botbench/main.go` (flag, histogram).
- `internal/paymirror`: reuse only; no new checker logic.
- Tests: `cmd/cardfuzz` `TestAutopayMirrorFlag`; a botbench flag test.

## Out of scope

- Fixing findings; turning the check on by default.

## Done means

- `go test ./cmd/cardfuzz -run TestAutopayMirrorFlag -count=1 -v`: one
  fixed-seed auto-pay game on an authored basic-land fixture with
  `-autopay-mirror` records only `equivalent` verdicts; an injected
  perturbation (the `TestPayMirrorDetectsPerturbation` technique) yields a
  `kind:"mirror"` record.
- `go run ./cmd/cardfuzz -dir .cards -games 200 -batch 200 -workers 4 -seed 26092600 -autopay all -autopay-mirror -state <fresh> -failures <f> -stats <s>`
  completes; every `mirror` record's sig is listed in the report.
- Without the flag, `cardfuzz` state/failures/stats (bar timing fields) and
  `botbench -out json` are byte-identical to main for the same seeds.

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

