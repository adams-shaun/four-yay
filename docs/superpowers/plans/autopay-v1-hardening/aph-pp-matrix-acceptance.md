# Auto-pay hardening: integrated acceptance, PP-01..PP-28 matrix and sweep report

Suggested issue ID: `aph-pp-matrix-acceptance`
Priority: 3
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: aph-hand-reserve, aph-last-resort-web, aph-plan-diagnostics, aph-mirror-in-fuzz-bench, aph-potential-granted-intrinsics, aph-pp-coverage-port, cr302-6-sick-mana
Changes offers: no (tests and a report only)

## Goal

Close the amended acceptance matrix (spec §9, PP-01 to PP-28) and publish the
evidence the amended §10 asks for. Every row maps to at least one named,
passing, non-skipped test at the narrowest useful layer; the standing sweeps
are run at the wave's final main and their numbers are compared with the
audit baselines below.

1. Build the PP-to-test matrix from the landed wave: grep the `TestPaymentPlan*`
   tests (rules, decision, host, host/httpapi, cmd/repro, seat), the web
   suites, and the tickets' commit messages. Any row without a test gets one
   here (tests only; if a row cannot be met without an engine change, stop
   and report it as a finding, do not fix it here).
2. Run the sweeps at the final main and record: planned casts, equivalence
   rate and every non-equivalent verdict class (paymirror); planned,
   planned_reversed, fallback, planrev/planfb signatures, engine-failure rate
   by mode and replay verification (cardfuzz); census class table and
   findings sections (census); botbench CPU with and without auto-pay seats.
3. Write `docs/superpowers/reports/<date>-autopay-v1-hardening-acceptance.md`:
   the matrix, commands and results, corpus pin, source commit, the supported /
   last-resort / deferred shape table, search limits and observed node counts,
   offer cost, fallback counts by reason, the exact golden checks, and the
   audit-baseline comparison. No policy win-rate claims.

## Evidence (audit baselines to compare against)

- Mirror (2,400 games per sweep): base 0d307f44c 53,647 planned / 91.6%
  equivalent; main 7fb66ee5f 52,169 / 99.0%, with 297 mid-cast priority
  states, 114 casts aborted before payment, 43 `cost_changed` fallbacks, 627
  planned casts with side effects.
- Fuzz (seeds 26092600+k, 1,000-game chunks, `-verify`): auto-pay failures
  3.33% of `all` games and 1.23% of `mixed`; 731 of 105,164 planned casts
  reversed, 283 fell back; engine-failure rate equal across modes (0.18% /
  0.23% / 0.20%); 105,164 planned casts replayed byte-identically.
- Gaps census (174 games): planned 1,429, reversed 195, mismatch 239 (P0
  since fixed: 0 / 3), life lost 68 over 36 casts.
- Mana census: V1 admitted 1,158 / 1,980 sources (58%), 154 / 246 repo-deck
  battlefield source cards (63%); 400 kill-switch faces.
- Perf: eager publication 53-60% of engine CPU.

## Files (symbols)

- New tests only where a row lacks one (new files, `…_pp_matrix_…_test.go`).
- `docs/superpowers/reports/<date>-autopay-v1-hardening-acceptance.md`.

## Out of scope

- Engine or web fixes (report findings instead); bot policy refits (the bot
  wave); the deferred tier.

## Done means

- `go test ./decision ./events ./rules ./view ./seat ./replay ./host ./host/httpapi ./cmd/repro ./internal/paymirror/... ./cmd/cardfuzz ./cmd/botbench -run 'Test.*PaymentPlan|AutoPay|PayMirror|Autopay' -count=1 -v`
  passes with 0 SKIP (list the selected tests in the report).
- `go test ./rules -run 'TestHeads|TestEveryRepoDeck|TestRepoDeckGamesReplayExactly' -count=1`,
  `make conformance`, `make sim` (20/20 replay OK), `go run ./cmd/gentypes -check`
  and `git diff --check` pass with no golden update in this ticket.
- Web: the five payment suites and the full web suite pass under node 24;
  `svelte-check` 0 errors.
- Sweeps (spec §10): paymirror constructed,random 300 + commander 150;
  cardfuzz `-autopay all` and `-autopay mixed` 1,000 games each at seed
  26092600 with `-verify`; census rerun; `botbench -a bot -b bot` and
  `-a bot-auto-pay -b bot` `-pairs all -games 5 -seed 1` CPU. The report
  states each number next to its audit baseline and explains every remaining
  non-equivalent verdict or plan-contract failure by cause (a storied engine
  bug, a documented CR 601.2 ordering difference, or a new finding).

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
