# Auto-pay: build PaymentActions lazily, only for consumers (53-60% of engine CPU today)

Suggested issue ID: `aph-lazy-offers`
Priority: 1
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: none
Changes offers: no (the same bytes, built later; games without an auto-pay consumer are byte-identical)

## Goal

Spec §2/§5 (amended): the extension is built on demand. `Engine.ask` builds
`d.PaymentActions = e.PaymentActionsForPriority(d.Player, d.Seq)` for EVERY
priority decision of EVERY seat (`rules/engine.go:3416-3418`), although only
AutoMana human seats and auto-pay bots read it; every replay rebuilds it
through the same `ask`. Make publication lazy without changing replay,
determinism or any offered byte.

Design (from the gaps audit, proven safe: re-planning after the DecisionAsk
emit for 77,838 priority decisions of 174 auto-pay games differed 0 times):

1. `ask` stops calling the builder. Add `(*Engine).EnsurePaymentActions()
   []decision.PaymentAction`: if the pending decision is `KPriority` and not
   yet built, compute `PaymentActionsForPriority(d.Player, d.Seq)`, store it on
   the pending decision (with a "built" marker so an empty result is not
   rebuilt), and return it. A pure read: no event, no RNG, no Seq change.
   `Engine.Clone` copies the cached value and marker (`rules/clone.go`).
2. `Submit` (`engine.go:3514`): when `in.Payment != nil` and the pending
   priority decision is not built, build it first -- generalise the existing
   `e.replayPaymentPlans` branch to all engines and delete the
   `replayPaymentPlans` field and `EnablePaymentPlanReplay` (`engine.go:3648-3651`;
   `clone.go:51`) after moving its one caller (`replay/replay.go:235`). Replay therefore never depends on a
   live cache.
3. The derived-memo tail: `ask` records `recordDerivedMemoTail(d)` after the
   builder today (`engine.go:3419-3426`, `rules/derivedmemo.go:112`). Keep the
   record at the end of `ask`; make `EnsurePaymentActions` safe to call after
   it (it must not break `BeginDerivedReads`' resume of the ask-time read).
4. Consumers opt in (grep `Pending()` and `PaymentActions` to be sure the list
   is complete):
   - `host/match.go` `projectNext` (under `m.mu`, before `d.Clone()`): ensure
     when the deciding seat is a `*HumanSeat` on an `AutoMana` table, or a
     seat implementing a new `seat.PaymentPlanConsumer` interface
     (`WantsPaymentActions() bool`; `*seat.Bot` returns its `autoPayMana`).
     Caretakers are `*seat.Bot` built by `NewBotPolicySeatWithAutoPayMana`
     (`host/bot_policy.go:48`, `host/match.go:490`).
   - `host/viewat.go` (`ViewAtSeat`, ~line 83) and `host/feedback.go`
     (`SnapshotForFeedback`, ~line 257): ensure for an AutoMana table's
     acting human seat before cloning (both already strip the extension when
     `!AutoMana`); `host/action.go` preflight reads the parked copy that
     `projectNext` built.
   - `internal/bench/bench.go` `PlayGame` loop (~line 200) for seats that
     implement the consumer interface (botbench `bot-auto-pay`,
     `cmd/botbench/main.go:150`).
   - `cmd/cardfuzz` auto-pay seats (`cmd/cardfuzz/autopay.go`, which reads
     `d.PaymentActions` at ~222/234 and in its `-dump-at` printer ~496).
   - `internal/paymirror` (`driver.go` ~226 before the seat decides;
     `paymirror.go` clones and `comparePending` ~1089: ensure on both sides or
     compare built-ness consistently so a verdict does not change).
5. Do not change `rules/payment_plan.go` in this ticket beyond what the move
   requires (the one-pass builder is `aph-offer-one-pass`).

## Evidence

- Gaps audit (overlay build removing only the ask-time call, same seeds,
  `-workers 4`): constructed `botbench -a bot -b bot -pairs all -games 5 -seed 1`
  (406 pairs / 2,030 games) user CPU 221.22 s -> 96.63 s (-56.3%), 109.0 ->
  47.6 ms/game, `-out json` byte-identical; commander (105 pairs / 525 games)
  112.29 s -> 44.89 s (-60.0%), identical. pprof: `PaymentActionsForPriority`
  117.84 s = 53.1% of 221.76 s (`PlanCastPayment` 41.50, hypothetical
  `legalActionsPriced` 37.02, duplicate `legalActions` 35.19, `PotentialMana`
  3.79, `paymentSourceZoneSeq` 1.48).
- Fuzz audit A/B (`-autopay off`, 150 games, 1 worker): 43.6 s / 46.8 s with
  publication vs 19.6 s / 19.9 s without, state files identical; profiles of
  ordinary games put the builder at 53.6-58.6% of samples.
- Consumers today: only humans on AutoMana tables and bots with auto-pay
  (`host/match.go:374-379` already strips the extension for a human on a
  non-AutoMana table).

## Files (symbols)

- `rules/engine.go`: `ask`, `Submit`, `EnablePaymentPlanReplay` (remove;
  and its call in `replay/replay.go:235`);
  new `EnsurePaymentActions`. `rules/clone.go` (cached value + marker, drop
  `replayPaymentPlans`). `rules/derivedmemo.go` only if the tail needs it.
- `seat/`: new `PaymentPlanConsumer` interface; `seat/bot.go` implements it
  (one small method; the bot-adapter wave also edits this file -- keep the
  change minimal).
- `host/match.go` `projectNext`; `host/viewat.go`; `host/feedback.go`.
- `internal/bench/bench.go` `PlayGame`; `cmd/cardfuzz/autopay.go` (and the
  drive loop that hands decisions to seats); `internal/paymirror/driver.go`,
  `internal/paymirror/paymirror.go`.
- Tests: `rules/payment_plan_lazy_test.go` (ensure equals the former eager
  value on the fixture boards; a Submit with Payment on an unbuilt decision
  builds and accepts; Clone after ensure carries it; a clone before ensure
  builds the same bytes), a host test that a non-AutoMana human and a plain bot
  never trigger the builder (count through a test hook), and keep
  `TestPaymentPlanHumanSeatSelectsAnOfferedPlanAndReplays`,
  `TestAutoManaDisabledKeepsHumanPriorityOnTheLegacyWire` and the cmd/repro
  planned-replay test green.

## Out of scope

- Planner semantics, the one-pass candidate walk (`aph-offer-one-pass`),
  search changes.
- Changing what an AutoMana human seat receives.

## Done means

- `go test ./rules ./host ./host/httpapi ./seat ./internal/bench ./internal/paymirror/... ./cmd/repro ./cmd/cardfuzz ./cmd/botbench -run 'PaymentPlan|AutoPayMana|AutoMana|Lazy|PayMirror|Autopay' -count=1` passes, 0 SKIP.
- `go test ./rules -run 'TestHeads|TestEveryRepoDeck|TestRepoDeckGamesReplayExactly|DerivedMemo' -count=1` passes without golden updates.
- `go run ./cmd/botbench -a bot -b bot -pairs all -games 5 -seed 1 -workers 4 -out json`
  is byte-identical to main's output (`cmp`), and its user CPU drops by at
  least 45% against main on the same box (quote both `time` lines);
  `-a bot-auto-pay -b bot -pairs all -games 2 -seed 1 -out json` is
  byte-identical to main's.
- `go run ./cmd/cardfuzz -dir .cards -games 150 -batch 150 -workers 1 -seed 5550101 -autopay all -state <fresh> -failures <f> -stats <s>`:
  the `stats` object (`planned`, `planned_reversed`, `fallback`,
  `priority_with_plan`, …), `kinds` and `sigs` equal main's for the same
  command (only `seconds`/`game_seconds` may differ); the same command with
  `-autopay off` is state-file identical to main's and its `game_seconds`
  drops by at least 45%.
- `go run ./cmd/paymirror -dir .cards -games 20 -seed 1000 -seats 2 -formats constructed -workers 4`
  summary equals main's.

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
