# Auto-pay: rank-aware bounded search (unit classes, pips first, branch-and-bound); O(1) incarnation lookup

Suggested issue ID: `aph-rank-search`
Priority: 2
Kind: `payment-plan`
Size: M
Lane: pipeline
Depends-On: aph-rank-order
Changes offers: yes (wide boards get a plan, or a better-ranked one, instead of `search_limit`)

## Goal

Spec §5 (amended). `planPaymentCost` (`rules/payment_plan.go:293-343`) is a
DFS over units in battlefield order that explores the "skip" branch first
(`:325`) and treats thirty Mountains as thirty distinct units, so ordinary wide
boards exhaust the 65,536-node budget (`decision.MaxPaymentPlanSearchNodes`)
before any complete plan exists -- the cast gets NO plan -- or return a
worse-ranked plan found before the cutoff. Separately `paymentSourceZoneSeq`
(`:495-513`) scans the whole event log backwards for every alternative of every
unit on every call. Verified: a `TokenCreate` event carries no `Obj` (the ID is
assigned inside `events.Apply`), so the scan never matches a token's entry,
walks the whole log, and returns `GenesisZoneSeq` for every token.

1. **Classes**: group units whose alternative sets are identical (same
   ability identity shapes, produced vectors, tier, consequence, creature flag,
   flex) into a class; choose a count per class. Materialise the witness in a
   canonical order (battlefield order within a class, lowest object IDs
   first) so witnesses stay deterministic and minimal under key 8.
2. **Pips first**: satisfy coloured requirements by exact backtracking over
   alternatives (a dual assigned to one colour must not strand a later pip),
   then fill generic best-first from the cheapest remaining classes; confirm
   each complete candidate with the existing `Cost.resolveManaWith` settle
   (`rules/mana.go`), as today.
3. **Branch-and-bound** on the monotone prefix of the amended rank (cost,
   creatures, sources and flex never decrease as units are added): prune a
   branch whose prefix already loses to the best complete plan.
4. **Budget**: keep the node counter and the `search_limit` outcome exactly
   as specified (best complete plan found is returned deterministically with
   `search_limit` recorded; no complete plan -> `search_limit`).
5. **Two phases**: structure the search so it can run over normal classes only
   and, separately, over normal + last-resort classes (spec §5 phase 2);
   `aph-last-resort-plans` wires phase 2. Phase 1 behaviour is all that is
   observable in this ticket.
6. **Incarnation index**: build one map `ObjID -> latest zone-entry Seq` per
   query (one backward pass over `e.L.Events`) and have the planner read it.
   It must return exactly what `paymentSourceZoneSeq` returns today for every
   object, tokens included (`GenesisZoneSeq`); improving token incarnation
   identity is out of scope. Do not add an Apply-side field to `state`/
   `events`. `paymentSourceZoneSeq` keeps its signature for the executor.

## Evidence

- Gaps proof, `git show 124ed89fb:rules/payment_plan_audit_test.go`
  `TestPaymentPlanAuditSearchLimitDeterministicAndTimed`: a 12-generic spell
  over 30 Mountains + 6 duals -> `reason="search_limit" nodes=65536 plan=false`
  (6.29 ms per call, deterministic): a trivially payable board is reported
  unplannable.
- Research probe P10 (authored boards): 8 basics + 8 typed duals at `{6}{U}`
  -> `search_limit`, plan taps 1 dual; 10+10 at `{8}{U}` -> 3 duals; 12+12 at
  `{9}{U}` -> 4 duals. Enough basics existed each time for a 0-dual plan.
- pprof (constructed botbench, before lazy offers): the DFS 10.77 s and
  `paymentSourceZoneSeq` 1.48 s of 221.76 s.
- mtg-kernel `mana.rs` @5472539 (`solve`/`solve_pips`, `pay_generic`): exact
  pip backtracking then generic; its test `backtracking_is_required_for_modal_sources`
  shows why greedy fails.

## Files (symbols)

- `rules/payment_plan.go`: `planPaymentCost` (rewrite), a class builder,
  `paymentSourceZoneSeq` (index), `paymentPlanUnitAlternatives` only if the
  class key needs a field.
- Tests: new `rules/payment_plan_search_test.go` (with a brute-force oracle
  helper enumerating every subset x alternative on boards of <= 20 sources).

## Out of scope

- Ranking semantics (fixed by `aph-rank-order`); hand reserve; multiple plans.
- Publishing last-resort plans.

## Done means

- `go test ./rules -run 'TestPaymentPlanSearch' -count=1 -v` passes:
  1. 12 Islands + 12 typed Island-Mountain duals, `{9}{U}`: `Reason == ""`,
     exactly 10 activations, 0 duals, `Nodes < 5000`; same for 8+8 at `{6}{U}`
     and 10+10 at `{8}{U}`.
  2. 30 Mountains + 6 duals, 12 generic: a plan, `Nodes < 1000`.
  3. A 20-source mixed board (basics, typed duals, a Combo dual, a creature
     dork, an `Any` rock): the plan equals the oracle's best under the rank for
     at least 25 costs; repeated runs and clones give identical IDs.
  4. A dual-backtracking board (the existing
     `TestPaymentPlanBacktracksExclusiveSources` shape) still finds the plan.
  5. A budget-exhaustion board still returns `search_limit` deterministically
     (construct one that defeats the classes, e.g. many distinct-colour
     sources with an unpayable pip).
- A property test over a fixed-seed game's priority decisions: the index
  agrees with `paymentSourceZoneSeq` for every battlefield object, including
  tokens and objects that re-entered the battlefield.
- Existing `TestPaymentPlan*` pass.
- `go run ./cmd/botbench -a bot-auto-pay -b bot -pairs all -games 2 -seed 1 -workers 4 -cpuprofile <p>`:
  `planPaymentCost` + `paymentSourceZoneSeq` below 2% of samples (quote).

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
