# Cast payment plans acceptance audit

This audit covers the implementation specified in
[`2026-09-24-cast-payment-plans.md`](../specs/2026-09-24-cast-payment-plans.md)
at source commit `6e1bcaaa0`, including the host witness-copy repair
`b84c97b48`. The local Forge corpus was present at pin
`95f04e8a04c8925fa97cb226fc3341cabcc90a53`. No Forge scripts, AGENTS.md
rows, or manual golden heads changed.

The formerly failing repeat-plan path is now covered by
`host.TestPaymentPlanHumanSeatSelectsAnOfferedPlanAndReplays`. It is a
View/Decision-only external seat: it receives a host-published decision,
submits the exact offered `PaymentSelection` through
`Registry.SubmitIntent`, selects two real empty-pool land-payment witnesses,
then uses the legacy `Choices` priority form. It completed and replayed both
fixed-seed games without a caretaker substitution:

| fixture | seed | events | replay head |
|---|---:|---:|---|
| two seats (`a`, `b`) | 20260924 | 17,111 | `4582c4b36985be2f` |
| four seats (`a`--`d`) | 20260925 | 81,880 | `f0ae466ea38aedcf` |

This closes the hosted planned-cast portion of PP-20. The adjacent legacy bot
uses only `Options`, and the external seat's ordinary post-plan priority answer
proves the selector is per-action rather than a game-wide mode.

## PP matrix

| PP | Automated evidence | Result |
|---|---|---|
| 01--02 | `rules.TestPaymentPlanPriorityAskPublishesAdditively`, `TestPaymentPlanActionsPreserveLegacyOptions` | pass |
| 03--05 | `rules.TestPaymentPlanBacktracksExclusiveSources`, `TestPaymentPlanDoesNotDoubleCountDualAndKeepsColorlessDistinct` | pass for exclusive-source backtracking, colorless and pool-only witnesses; preferred-source ranking needs its own focused assertion |
| 06--10 | focused `Test.*PaymentPlan` suite | planner/execution smoke pass; this audit did not add every shape-specific assertion |
| 11 | `decision.TestPaymentPlanValidateExclusiveSelector`, `TestPaymentPlanLegacyValidationUnchanged` | pass |
| 12--14 | `rules.TestPaymentPlanSubmitExecutesWitness`, `TestCR601TargetsPrecedeManaPayment`, `make conformance` | pass for normal payment and CR ordering; mutation/interruption cases remain in lower-layer coverage |
| 15--16 | `decision.TestPaymentPlanCopiesOwnWitness`, `TestPaymentPlanIdentityIsIndependentOfPresentation`; host replay lane | pass for copied selector and live replay; restart/undo/feedback are covered by their existing package regression gates, not a new payment-specific fixture |
| 17 | existing host/httpapi package regression in `make test` | pass; no new payment-specific HTTP test was needed because selector admission is `Decision.Validate` at the existing seat fence |
| 18--19 | `web/src/lib/seatpanel.payment.test.ts`, `SeatPanel.svelte.test.ts`; `npm run check` | Type check pass; test runner was unavailable before dependency installation, then lint baseline blocked a full web test run in this audit |
| 20 | `host.TestPaymentPlanHumanSeatSelectsAnOfferedPlanAndReplays` | pass: two-seat and multiplayer natural completion plus replay |
| 21 | repo-deck heads/replay ratchets, `make conformance`, `make sim` | pass; see commands below |

The V1 boundary stays intentionally narrow: ordinary front-face hand casts
with fixed generic/WUBRGC costs and exact simple tap producers. X, hybrid,
Phyrexian, snow, additional/alternative costs, target-dependent prices,
mana-sensitive riders, and complex/restricted/dynamic producers remain manual.
No bot policy promotion or planner-ranking change is implied by these smoke
games.

## Commands and results

```
go test ./host -run TestPaymentPlanHumanSeatSelectsAnOfferedPlanAndReplays -count=1 -v
# PASS: two-seat 2.31s; four-seat 7.18s

go test ./decision ./events ./rules ./view ./seat ./replay ./host ./host/httpapi ./cmd/repro -run 'Test.*PaymentPlan' -count=1
# PASS. events, view, seat, replay, host/httpapi and cmd/repro reported
# "[no tests to run]" for this name filter; they were not counted as focused coverage.

go test ./rules -run 'TestHeads|TestEveryRepoDeck|TestRepoDeckGamesReplayExactly' -count=1
# PASS, 3.327s; no golden update

make conformance
# PASS, 2.522s; TestCR70157DiscoverRandomBottomOrder skipped by its existing
# conformance exclusion.

make sim
# PASS: 20/20 four-seat fixed seeds replayed identically.

make test
# Runner stopped the command after 30 seconds while package output was still
# arriving. Every package reported before that cutoff passed; this is not a
# full-suite result and is not counted as one.

go run ./cmd/gentypes -check
git diff --check
# PASS

cd web && npm run check
# PASS: 0 errors, 0 warnings.
```

`make lint` installed the worktree's missing web dependencies and then failed
on the known unrelated ESLint baseline:

```
web/src/components/ManaPool.svelte:105:13
error  Each block should have a key  svelte/require-each-key
```

The installation printed Node 20 versus Vitest 5's Node >=22 engine warnings,
but completed successfully; `svelte-check` passed. This acceptance change did
not modify `ManaPool.svelte`, so it leaves that baseline intact. No other tool
error occurred.
