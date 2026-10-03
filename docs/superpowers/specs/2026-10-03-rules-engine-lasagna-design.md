# Rules engine: spaghetti to lasagna — design (2026-10-03)

Status: design draft for operator review. No code yet. Measured against
`main` at `49bd9af8d`. Every number below carries the command or data source
that produced it (Appendix A); re-measure before quoting, and treat anything
marked *hypothesis* as a claim a spike must confirm or kill.

This spec answers three operator questions:

1. **Make the spaghetti into lasagna.** Give `rules` internal layers with
   enforced boundaries instead of one 131k-line package.
2. **Why are recent changes so difficult?** Fix the root cause, not the
   symptom (hot files, resolver rounds, fix-on-fix).
3. **How do we make adopting and certifying all remaining sets smooth?**

The short answer to all three is the same: most of the per-change cost comes
from **hand-maintained parallel structures that must be edited in lockstep**,
and the worst of them is the way a resolution suspends for a player's answer.
Remove those classes, and the package split, the conflict rate and the
per-set cost all fall out of it.

## 0. Summary

| # | Workstream | Deletes (class, not instance) | Gate |
|---|---|---|---|
| W0 | Guardrails and shrink-only ratchets | regrowth of everything below; the size-split incentive | none, start now |
| W1 | Generated / checked parallel structures | clone omissions; Kind-table lockstep gate failures; N-site context plumbing; printed-vs-derived bugs | none, start now |
| W2 | Shared aggregate files to per-entry files | conflicts manufactured by heads prose and generated blocks | none, start now |
| W3 | Resolution kernel: answer-tape re-execution | `resumePoint` cursors, `Resume*` riders, the 102-arm answer switches, ~220 `Ctx` cursor fields, the "state lost across a suspension" bug class | spike S3 must pass its exit criteria |
| W4 | Typed parameter IR | stringly `Params["X"]` reads interpreted separately on each path; the hand-ratcheted param census | W0 ratchet in place |
| W5 | Package extraction (the lasagna proper) | the "any of 2,162 methods can call any other" coupling | W1, and W3 for the resolution layer |
| W6 | Pipeline and process | duplicate tickets; feature work racing a refactor in the same subsystem | none |
| W7 | Set adoption and certification pipeline | per-set bespoke audit projects | W2 per-card files; W4 helps |

Recommended order: W0, W1a, W2 and W6 immediately and in parallel (they are
small and mechanical); spike S3 next; then W3 and W4 run as sequenced
programs; W5 follows each enabling step; W7 runs alongside from day one
because it is mostly tooling outside `rules`.

## 1. Where we are (measured)

### 1.1 The outer architecture is sound

These hold and are mechanically enforced; nothing in this spec weakens them:

- One-way package chain `cards → state → decision → events → effects →
  botpolicy → rules → view → seat → replay → protocol → host`
  (`internal/archtest` `TestDependencyOrderHolds`).
- All mutation through `events.Apply`; a match is `(Config, intents)`;
  replay, undo, resume and search clones are one mechanism.
- Determinism (no clock, seeded PCG with draw counter, no order-visible map
  range), guarded by `TestHeads`, replay tests and `make sim -verify`.
- The information boundary: seats see only `view.View`.

### 1.2 Inside `rules` there are no layers

| Metric | Value |
|---|---|
| `rules` non-test / test LOC | 131,406 / 354,695 |
| `rules` non-test files | 295, **zero Go subpackages** |
| Methods on `*rules.Engine` | **2,162** (one receiver; any can call any) |
| Engine fields | ~30 top-level + **391** in 14 embedded sub-structs (`engineScratch` 89, `engineTriggerBatches` 59, `engineContinuation` 32, …) |
| Functions > 500 lines (`rules`+`effects`) | **26**; > 200 lines: 80; > 100 lines: 217 |
| Longest | `effEffect` 1,199 · `resumeAnswerBinding` 1,080 · `cloneWith` 969 · `definedSpec` 958 · `resumeResolution` 952 · `payCast` 932 · `effCopyPermanent` 878 · `battlefieldWalk` 861 · `checkFaceTriggers` 820 · `pushTrigger` 765 |
| `effects.Host` | **96 methods**, one implementation (`*rules.Engine`, spread over 26 files, 39 of them in `stack_helpers.go`), no `var _ effects.Host = (*Engine)(nil)` assertion |
| `effects.Ctx` | **293 named fields** + 2 embeds (≈326 reachable); 73 core, **220 per-primitive ask/resume cursors** in ~60 families |
| `resumePoint` | **90 fields**; `contFrame` 23 |
| String `case "..."` literals | effects 1,333, rules 1,250 |
| `Params["Lit"]` reads (`rules`+`effects`) | **2,146** over **693 distinct keys**; typed `ParamKey` vocabulary: 15 keys |
| Commits touching `rules/` in 7 days | 1,212 |

### 1.3 Why changes are hard: the evidence

Sources: `git log --numstat`, a blame sample, `.ds4/orchestrator/journal.jsonl`
(26,857 rows), 2,983 review verdicts, 1,681 gate logs,
`scripts/reward_collect.py flow`.

- **Fixes land on fresh code.** Blaming the lines 70 random fixes (3–10 days
  old) modified in `rules/`/`effects/`: 389 of 687 (57%) were under 72 hours
  old, median 46 h; 230 of those were written by another fix. 40 of the 70
  fixes rewrote code under 72 h old.
- **Commits are small** (median 3–4 files, 109–170 lines). The difficulty is
  not change size; it is the number of hidden places a change must reach.
- **Pipeline:** ~550 merged tickets a week; 43–56% need more than one
  dispatch; 33–44% are reworked after review; merge_fix 15–23%
  (0.152 over the last 7 days, worst ticket 15 resolver rounds).
- **What reviewers reject** (1,275 MAJOR/CRITICAL findings): loud/silent
  degrade 12%, scope 12%, weak test 9%, ratchet/census bookkeeping 9%,
  **sibling path not updated 7%**, replay invariant 6%, **suspend/resume state
  lost 6%**, **clone omission 4%**.
- **What gates fail on:** lockstep checks lead —
  `TestCommittedProtocolTSIsFresh` 17, `TestTriggerEventInterestMapping` 15,
  `TestGoldens` 13, `TestDescribeCoversEveryKind` 9, param census ~25,
  `TestEveryKindHasAName` 5.
- **Tax files** (share of 403 feat/fix commits to `rules/`/`effects/` in 7
  days): `cast.go` 14%, ratchet tables 11%, `engine.go` 10%, resume files 9%,
  `effects/registry.go` 8%, `clone.go` 7%.
- **`clone.go`:** 228 commits in 30 days; 153 of them also add `engine*.go`
  lines — a new field means a hand-written clone line. Only `Cost` has a
  reflection completeness test (`TestCloneCostCoversEveryCostSlice`).
- **Suspension:** 449 commits in 30 days mention suspend/resume; 224 of the
  386 non-merge ones are fixes.
- **Context plumbing:** one layer-derived fact costs 14–19 files
  (EffectiveNames 18, DerivedTypes 16, StaticGoads 14, ParentTargets 14,
  `TriggerContext.Reflexive` 19); 59 commits in 30 days are about printed vs
  derived disagreement.
- **`rules/heads_test.go`:** 94 commits in 30 days, hashes moved in 88; 1,491
  lines guarding four hashes, with each re-pin prepending prose at the top of
  the map — a conflict by construction.

The trend is mixed: after the 2026-09-22 docs cleanup (TEST_HISTORY and
AGENTS.md diaries frozen) the fix share fell from 46–54% to 29% and merge_fix
to 15%. That proves aggregate-file taxes are removable. **The code-structure
taxes — clone, resume, context, ratchets — are flat to rising.**

## 2. Root causes

Symptoms are what the pipeline sees: hot files, resolver rounds, merge_fix,
fix-on-fix chains, the hot-file notes in AGENTS.md (15 of 34 name `rules/`
paths). Throttling dispatch, sequencing branches and writing hot-file notes
treat symptoms. The causes, ranked by evidence weight:

### RC1. Hand-defunctionalised continuations for mid-resolution decisions

When a resolution asks a player something, the engine does not keep a
continuation. It snapshots a 90-field record (`buildAskResume`,
`rules/resolution_ask.go:142-262`), and on Submit it rebuilds a fresh
`effects.Ctx` from that record (`rules/resolution.go:240-647`), binds the
answer through one of ~110 kind strings in two switches
(`resumeAnswerBinding`, 60 arms; `resumeAnswerBindingRest`, 42 arms), and
**re-runs the asking primitive from its first line**
(`effects.Resolve(e, ctx, rp.sa)`, `resolution.go:828`). Enclosing loops are
a linked chain of records (`rp.outer`).

Consequences:

- Every local an effect holds across an ask must be moved by hand through
  **three** layers — `decision.Decision` `Resume*` riders (31 fields), the
  `resumePoint` (90), the rebuilt `Ctx` (~55 `ctx.X = rp.y` assignments) —
  and then deep-copied in `cloneResume`. Missing any one is a bug that
  surfaces only for the card that suspends at that link (c21c390de
  ParentTarget, 2ce6971e2, e359a451c, cec70b036, ecb8a5f09).
- ~15 per-object Engine side tables (`moveCounterAsk`, `aorAsk`,
  `counterTypeAsk`, `copyAnswerTargets`, `oppPicksMid`, `castSubTargets`,
  `triggerContexts`, …) and ambient Engine globals saved/restored around
  re-entry (`fusedResolving`, `windowPaidX`, `applyingReplacement`,
  `replRedirect`, `resolutionCtx`) take part in resume but live outside the
  record.
- Seven parallel suspension mechanisms (`e.resume`, `unlessPayment`,
  `cumulative`, `triggerCost`, `offStackMana`, `queuedPlays.cont`, `etbMove`).
- 123 ask call sites in 94 effect functions across 54 files; 93 distinct
  `ResumeKind` literals.

This is the single largest reason "add a choice to an effect" is non-local.

### RC2. Parallel structures kept in sync by hand and policed by tests

Tests catch the omission after the fact; nothing generates the second copy.

- **Engine deep copy:** ~420 fields copied by hand in `cloneWith` (969 lines)
  and `cloneResume`.
- **Context construction:** 108 `Ctx{` literals, 22 `SpecContext{`, 19
  `TriggerContext{`; 18 sites copy 8+ fields by hand; `SpecContext` has a
  second construction path in the hot statics walk.
- **Derived-characteristic threading:** layer results reach effects as tables
  threaded through every context (EffectiveNames, DerivedTypes, StaticGoads),
  so effects read printed state by default and derived state only where a
  table was threaded.
- **Event-kind tables:** names, Describe, trigger interest and the TypeScript
  protocol are separate hand-kept lists — the top gate failures.

### RC3. Untyped parameter IR

`cards.SA.Params` is `map[string]string`. 2,146 literal reads over 693 keys.
The same parameter is interpreted separately on the offer, payment, planner
and resolution paths — the "sibling path not updated" finding (7%), e.g. a
cost modifier honoured on one of two payment paths. The param census
(`rules/paramcensus_test.go`, 3,843 lines, 189 commits in 30 days) exists
only because reads are stringly typed.

### RC4. Shared aggregate files every change appends to

Heads prose (above); README coverage block (92 "refresh card coverage table"
commits in 14 days); AGENTS.md (400 commits in 30 days). Already fixed and a
model for the rest: the oracle `known-divergent` fixtures are now one file per
card (`rules/testdata/oracle/<family>/known-divergent/<card>.json`).

### RC5. No seams inside `rules` (amplifier)

With 2,162 methods on one receiver and files split by size, two tickets in
different features routinely edit the same file. The steward metric makes it
worse: `oversized_files` (`scripts/reward_collect.py:1147-1200`, `n > 1500`)
and the `BIG_FILE_ALARM_LINES = 4000` steward-split ticket
(`scripts/seed_candidates.py:62`) reward **size splits**
(`resolution_answer_rest.go` is the second half of one switch moved verbatim;
`stack_helpers.go` is a 1,387-line grab-bag hosting 39 Host methods), which
scatter one concern across files without adding a boundary.

### RC6. Fine-grained parallel tickets (amplifier, not cause)

~550 merges a week with reviewers who cannot see sibling paths turns each
hidden edit site into a follow-up fix and a conflict. Duplicate work happens
(02b3bd302 and cacf057f5 share a subject). Throttling would treat a symptom;
RC1–RC3 shrink each ticket's blast radius, which is what lowers the rate.

## 3. Target architecture

Lasagna means **layers that import only downward**, each with a narrow
interface to the one below, enforced by archtest rather than convention.

```
 L6  rules (orchestration)  turn/step/priority, stack, SBAs, casting flow,
                            Host implementation, Clone      — package rules
 L5  rules/resolve          resolution kernel: answer tape, ask/answer,
                            deferred-ask ordering            (W3)
 L4  rules/trigmatch        trigger matching and queueing  ┐
     rules/combat           attack/block legality          │ subsystem
     rules/pay              mana + payment planning        │ packages (W5)
 L3  rules/chars            characteristic computation      ┘ (layers 1-7)
 L2  rules/cost             Cost/CostPart vocabulary, cost parsing (leaf)
     cards facts            typed per-API params on ExtSlot (W4)
 L1  events, state, decision                                 (unchanged)
 L0  cards IR                                                (unchanged)
```

Rules for the layering:

1. A lower layer never imports a higher one. Each new package gets archtest
   rows `{module+"/rules/X", module+"/rules"}` (the `forbidden` slice in
   `internal/archtest/arch_test.go:152-178` already skips rows whose `from`
   package does not yet exist, so the rows can land before the package).
2. A layer reaches game state through a **read-only view** plus an **emit
   function**; it never holds `*rules.Engine`. Mutation still goes through
   `events.Apply` only.
3. Subsystem packages own their state types; `rules` embeds them. The Clone
   of a subsystem's state is generated or checked (W1a), not hand-written in
   `rules/clone.go`.
4. Effects keep talking to the engine through `effects.Host`, split into role
   interfaces (W1d) so each effect declares what it touches.

What does **not** change: the outer chain, `events.Apply`, determinism, the
event stream. Every step in W1–W5 is a **pure refactor that must leave
`TestHeads`, `TestOracleAudit` and the `cmd/repro` fixtures byte-identical**
(§9, §13). That property is what makes a large refactor safe to hand to seats.

## 4. W0 — Guardrails and shrink-only ratchets

Start immediately. Each is a constant plus a test that fails on growth and
logs on shrink, copying `internal/testutil/agentsdoc_test.go`
(`knownApproximationRows`, `TestKnownApproximationsOnlyShrinks`).

| Ratchet | Measures | Starting value |
|---|---|---|
| `maxFuncLinesOver300` | count of non-test funcs in `rules`+`effects` over 300 lines (go/ast) | measure at landing |
| `engineMethodCount` | `func (e *Engine)` declarations | 2,162 |
| `hostMethodCount` | methods in `effects.Host` | 96 |
| `ctxFieldCount` | named fields in `effects.Ctx` (+ promoted) | 293 |
| `resumePointFieldCount` | fields in `resumePoint` | 90 |
| `stringParamReads` | `Params["…"]` literal reads in `rules`+`effects` | 2,146 |
| `stringCaseLiterals` | `case "…"` literals in `rules`+`effects` | 2,583 |

Also in W0:

- Add `var _ effects.Host = (*Engine)(nil)` in `rules`.
- **Replace the steward size metric.** `oversized_files` counts files over
  1,500 lines (tests included) and mints split tickets at 4,000. Replace it
  with: functions over 300 lines (non-test), plus the ratchets above. A
  steward-split ticket's done criterion becomes "moves a cohesive concern
  behind a named seam", never "drops a file under N lines". Forbid new
  size-only file names (`*_rest.go`, `*_helpers.go`, `*_misc.go`, `*_more.go`)
  with an archtest name lint that allow-lists the existing ones.
- Make `potential_kinds_test.go` name-agnostic (it finds a method by name, so
  any rename or move breaks it; already filed as a follow-up in the hot-file
  notes).
- Fix `TestResumeStateOwnedOnlyByTheResolutionMachinery` to glob
  `rules/...` recursively so a W5 move into a subpackage does not silently
  drop the census (`internal/archtest/arch_test.go:355`).

Exit: all ratchets green on `main`; the steward seed no longer mints
size-split tickets.

## 5. W1 — Generate or mechanically check every parallel structure

### W1a. Clone completeness (first, cheapest, highest yield)

1. Tag every field of `Engine`, its 14 embedded sub-structs, `resumePoint`
   and `contFrame` with `clone:"deep|share|reset|hook"`:
   - `deep` — the clone owns a copy (requires a known copier for the type);
   - `share` — immutable or corpus-owned (compiled `*cards.SA`, registries);
   - `reset` — scratch, zeroed in the clone (planner scratch, memo epochs);
   - `hook` — observer, deliberately not copied (`ManaAbilityHook`,
     `paymentStats`).
2. A reflection test walks the types recursively and fails on any untagged
   field.
3. A behavioural test fills every `deep` field with non-zero values by
   reflection, clones, mutates the original, and asserts the clone is
   unchanged — this catches an omitted copy line mechanically.
4. Optional second step: `cmd/genclone` generates `cloneWith` from the tags
   (`go generate`), so a new field needs a tag and nothing else. The generated
   code must match the hand code's allocation profile (Spare pooling,
   `clone_cycle_bench_test.go`); if it cannot, keep hand code plus the tests.

Deletes: the clone-omission class (4% of MAJOR findings; 228 `clone.go`
commits in 30 days). Done when `clone.go` changes only via `go generate` or
when the tests are the only thing a new field must satisfy.

### W1b. One event-kind descriptor table

`events` gets one table, indexed by `Kind`, that is the source of the name,
the Describe text and a trigger-interest **category enum** (an `events`-level
vocabulary, so `events` does not import `rules`). From it:

- `kindNames` is derived (deletes `TestEveryKindHasAName` failures);
- `rules`' trigger-interest mapping is derived from the category plus the
  few genuinely rules-specific overrides (deletes most
  `TestTriggerEventInterestMapping` failures);
- `cmd/gentypes` emits `protocol.ts` from it.

Append-only ordering is unchanged: the table is indexed by the existing
`Kind` constants.

### W1c. One context constructor

- `effects.NewCtx(host, source, controller, sa, opts…)` and
  `ctx.Child(sa)` / `ctx.ForTrigger(tc)` replace the 108 literals; 18 of them
  copy 8+ fields today.
- `SpecContext` is derived only from a `Ctx` (or a single
  `NewSpecContext`), and the hot statics walk uses the same constructor with
  a reusable buffer (perf rule: no allocation).
- Ratchet: count of `effects.Ctx{` / `SpecContext{` / `TriggerContext{`
  literals outside the constructors, shrink-only.

### W1d. One characteristics query and role-split Host

- Add `Host.Chars(id) *Chars` returning the layer-derived characteristics
  (name, type bitset, colour mask, keyword bitset, P/T, controller) for the
  current layer epoch, backed by the existing derived memo. Effects and
  filters read `Chars` by default; printed values only through an explicit
  `PrintedChars(id)`.
- Delete the threaded tables (EffectiveNames, DerivedTypes, StaticGoads, …)
  from `Ctx`/`SpecContext` as each consumer moves. Deletes the "printed vs
  derived" class (59 commits in 30 days) and the 14–19-file cost of adding a
  derived fact.
- Split `effects.Host` (96 methods) into role interfaces: Read (incl.
  `Chars`), Emit, Batch/provenance, RNG, Continuous, Restrictions,
  Replacements, Targeting, Triggers, Ledger (30 history queries), Conditions,
  Ask. `Host` remains as the union so the change is incremental; new effect
  helpers take the narrowest role. 97% of the 157 `eff*` functions already
  use five or fewer Host methods directly.
- Move the Host implementation out of `stack_helpers.go` into one file per
  role (`host_ledger.go`, …) — a concern split, not a size split.

## 6. W2 — Shared aggregates to per-entry files

| Today | Change |
|---|---|
| `rules/heads_test.go`: 4 hashes + 1,491 lines of prose, prose prepended on each re-pin | `rules/testdata/heads/<seats>.txt`, one hash per file, no prose. Head-move rationale goes in the **commit message**. A `cmd/headdiff` (built on `replay`'s first-divergent-event code) prints the first differing event between `main` and the branch for each seat count, so a reviewer sees *what* changed instead of four hashes |
| README coverage block refreshed by commits (92 in 14 days) | Generated in CI and published (or a gitignored file); seats never commit it |
| Param census table hand-edited | Becomes derived once W4 lands; until then, one file per card like `known-divergent/` |
| AGENTS.md hot-file notes (each notes ticket appends prose) | Keep the generated table; move durable per-file notes to `scripts/hotfiles-notes.json` only (already the source) and stop mirroring prose into AGENTS.md |

Model: the oracle `known-divergent/` directories already did this.

## 7. W3 — Resolution kernel: answer-tape re-execution

### 7.1 Design

The engine is deterministic and event-sourced. Use that instead of hand
continuations:

1. **Before** resolving an object whose ability chain *may* ask (a per-SA bit
   computed once from the compiled catalog — the 94 asking primitives are
   known — and hung on `sa.ExtSlot()`), take a snapshot `S0` of the engine
   (Clone with Spare pooling).
2. Run the resolution normally. Events are applied and appended as today, so
   players see revealed cards and intermediate state before they choose —
   **the event order and information flow are unchanged**.
3. When a primitive needs an answer it does not have, it calls `Ask` as
   today. The kernel records the committed event count `n` and the answer
   tape `[a1 … ak]`, posts the decision (`DecisionAsk`, unchanged), and
   unwinds — no locals are saved anywhere.
4. On Submit, the kernel appends the answer to the tape, restores `S0` into a
   scratch engine and **re-runs the whole resolution from its first line**
   with the tape. Each `Ask` is served from the tape in order. The first `n`
   events are **compared**, not appended: each must equal the recorded event
   byte for byte, or the kernel panics with the first divergence (this is the
   determinism contract made explicit). After the prefix, events apply and
   append normally. When the run finishes or reaches the next unanswered ask,
   the scratch engine's state is adopted.
5. Deferred-ask ordering, `AskEmpty` silent skips, the CR 117.3b
   priority reset and the trailing `Priority` event are properties of the
   kernel, implemented once.

What this deletes: the per-primitive cursors in `resumePoint` (32) and its
chain-shared Ctx state (23); the `Resume*` riders on `decision.Decision`
(31); `resumeAnswerBinding`/`Rest` (102 arms); the ~220 cursor fields on
`Ctx`; the ten `Suspend*`/`Suspended` Host methods and the 47 `h.Suspended()`
polls; most of the ~15 side tables; and, eventually, the six parallel
suspension mechanisms. What remains of the record is identity (object, kind,
player, direct), the tape and `n`.

Why it removes the bug class: nothing crosses a suspension except the tape.
A primitive's locals, the ParentTarget link, Remember sets, LKI captured
earlier in the chain — all of it is recomputed by re-running the same
deterministic code on the same snapshot with the same answers.

Note that re-running the asking primitive from its first line is **already**
today's behaviour; this design makes it uniform (whole chain, from the
snapshot) instead of partial (one primitive, from a hand-rebuilt context with
consume-and-clear guards).

### 7.2 Alternatives considered

- **Explicit step machine / CPS** (`func(h, c, sa, k *Cont) Step`): rewrites
  all 94 asking functions into resumable steps; every local becomes an
  explicit field again — the same hand-defunctionalisation, better organised.
  Kept as the fallback if S3 fails on cost.
- **Goroutine coroutines:** a parked goroutine cannot be cloned, which breaks
  `Clone` at a decision — the operation MCTS/azmcts and searchbench depend
  on. Rejected.
- **Invertible events (undo log instead of snapshot):** would avoid the
  snapshot cost but requires every event kind to carry before-values; a large
  change to the frozen event vocabulary. Rejected for now.

### 7.3 Costs and hazards (*hypotheses* S3 must measure)

- **Snapshot cost.** One Clone per may-ask resolution. Mitigated by the
  may-ask bit (most resolutions never ask) and Spare pooling. Measure on the
  perf-sb gate (`/mnt/sata/gorge-training/perf-sb`), both mana modes.
- **Quadratic re-execution.** k asks in one resolution cost O(k²) primitive
  runs. Typical k is 1–3; pathological loops (vote with 8 players, RepeatEach
  over many objects) need measuring. If needed, add intermediate snapshots
  every m asks (checkpointing) — a kernel-local change.
- **Clone at a decision** must carry `S0` and the tape: one extra engine copy
  per clone *while a resolution is suspended*. Measure the effect on search
  throughput.
- **Effects that read state outside the engine** would break replay already;
  the prefix comparison turns any such leak into an immediate, named failure.
- **SA pointer identity** (`sa == e.resume.sa`, `repeatReported`) disappears
  with the frames; line-keyed matching (`charmModeTarget`) is unaffected.

### 7.4 Spike S3 (gate for W3)

Implement the kernel behind a flag for one resolution path (spells only, no
triggers) and convert three primitive families with different shapes: a
single ask (ChooseColor), a loop with per-iteration asks (RepeatEach or
Vote), and a nested chain with ParentTarget (the c21c390de scenario).

Exit criteria, all required:

1. Byte-identical event streams versus the legacy kernel on: `TestHeads`, the
   oracle scenarios for those families, a 2,000-game cardfuzz sample and the
   `cmd/repro` fixtures (dual-run harness, §7.5).
2. Perf-sb throughput regression ≤ 3% in both mana modes, and Clone-at-decision
   cost measured.
3. Lines deleted for the converted families ≥ lines added to the kernel.

Kill criteria: (1) cannot be met without per-primitive special cases, or (2)
exceeds 10% after checkpointing. Then fall back to the step-machine
alternative, scoped the same way.

### 7.5 Migration (after S3 passes)

- The new kernel handles a resolution only when **every** asking primitive in
  its chain is marked tape-ready (registry bit); otherwise the legacy path
  runs. This lets primitives convert in batches with both kernels live.
- **Dual-run oracle:** in tests, a resolution is run through both kernels on
  cloned engines and the event streams compared. Every conversion batch must
  pass it on the corpus-wide cardfuzz sample and the oracle suite.
- Each batch deletes its `ResumeKind` arms, `Resume*` riders and Ctx cursors
  in the same commit, and lowers the W0 ratchets.
- Order: single-ask primitives (the majority) → loops (Repeat, Vote, charm
  rest, villainous, generic choice) → replacement-body asks → the parallel
  mechanisms (unless-pay, cumulative, triggerCost, offStackMana, queuedPlays,
  etbMove).
- Old saved logs keep replaying because the event stream is identical by
  construction and verified by the dual run.
- The whole program runs as **one sequenced lane** (W6): no feature ticket in
  the resolution files while a batch is open.

## 8. W4 — Typed parameter IR

- Do not change `cards/` for this: any edit there moves
  `CompilerFingerprint` and recompiles every IR cache. Use the existing
  **write-once `cards.ExtSlot`** pattern (already used by
  `rules/mana_safacts.go:166` and `rules/walk_face_facts.go:297`): each API
  gets a typed struct (`type ChangeZoneParams struct { Origin ZoneMask;
  Destination state.Zone; ChangeType Spec; Defined Ref; … }`) compiled once
  per SA on first use and hung on the slot.
- The compiler for each API is the **only** reader of that API's string
  params. Every path — offer, payment, planner, resolution — reads the typed
  struct, so "sibling path not updated" stops being possible for parameters.
- An unknown or unread param is detected by the compiler at load (loud
  degrade, existing contract), so the param census becomes **derived** from
  the compilers instead of a hand-ratcheted 3,843-line table.
- Strings compile to masks/bitsets (invariant 12, `docs/agents/invariants.md:191`).
- Order: by read count. Top keys today: ValidCard 95, Defined 82, ValidTgts
  72, ValidPlayer 63, Cost 59, Choices 54, Optional 43. Start with the APIs
  whose params are read on more than one path (costs, targets, Defined).
- Ratchet: `stringParamReads` (W0) only falls.

## 9. W5 — Package extraction (the lasagna proper)

The mana/payment survey shows why order matters: that subsystem is a
**24.5k-line ring** (31 files) whose 238 Engine methods reach ~60 Engine
fields and ~127 other Engine methods, call `ask`, `emit`, the layers and the
trigger queue, and resume resolutions. `Cost` is used 632 times outside it.
Extracting it first would fail. Extract leaves and low-fan-in subsystems first:

| Step | Package | Why this order | Size |
|---|---|---|---|
| E1 | `rules/cost` | Leaf vocabulary every other subsystem uses: `Cost`, `CostPart`, `ParseCost` (72 call sites), `formatCost`, pip tables, the 30 `regexp.MustCompile` cost parsers (compile them to a table — perf rule) | small |
| E2 | utilities out of feature files | `controllerOf` (39 callers) lives in `trigger_match.go:915`; similar shared helpers move to `rules/query` or stay in `rules` but out of subsystem files | small |
| E3 | `rules/trigmatch` | Small inbound surface (`checkTriggers` 7 sites, `putTriggersOnStack` 3); already has a registry seam (`trigMatcher`, 64 registrations, `trigger_match.go:2545-2574`). The matcher signature changes from `*Engine` to a read-only interface | ~11k |
| E4 | `rules/chars` | Read-only characteristic computation (`layers_derived`, `layers_types`, `layers_static`); the effect lifecycle (`layers_effect`) and caches stay in `rules`. Blocked today by the `Derived → matchesSpec → Derived` recursion and CDA evaluation through `effects.Ctx` — W1d's `Chars` query is the interface | ~6k |
| E5 | `rules/combat` | Legality predicates (`canAttack`/`canBlock`) are only called inside `combat.go`/`attack_cost.go`; they read `attackBlocked` and Chars | ~5k |
| E6 | `rules/resolve` | Only after W3; the kernel is small once the cursors are gone | — |
| E7 | `rules/pay` | Last: after E1 (cost vocabulary), W4 (typed cost params) and W3 (no resume state in payment). Needs a ~35-method interface today; the target is under 20 | ~24k |

Per step:

- A pure move plus interface introduction. `TestHeads`, `TestOracleAudit`
  and the repro fixtures byte-identical; perf-sb within noise.
- White-box tests move with the code (61 mana test files are white-box today;
  plan for that in E7).
- Archtest rows for the new package land first.
- One sequenced lane; the subsystem's files are frozen to feature tickets
  while the step is open (W6). A step is sized to land in one or two days so
  the freeze is short.

## 10. W6 — Pipeline and process

- **Refactor lane.** W1–W5 tickets run in one lane, in sequence, with
  `Depends-On` chains. While a ticket in the lane holds a subsystem, the
  seeder does not dispatch feature tickets that touch that subsystem's files
  (a file-set lock in agentctl's intake). This replaces hot-file notes with
  scheduling.
- **Tickets scoped by subsystem, not by card.** A card bug triggers a class
  census for its mechanism (existing practice); the fix ticket owns the
  mechanism across all paths.
- **Sibling-path brief.** When a ticket touches a param key, API or Host
  method, the brief lists every other read site (generated by grep today, by
  the W4 compilers later). This targets the 7% "sibling path not updated"
  findings.
- **Dedupe before dispatch.** Refuse a ticket whose subject matches an open
  or recently merged one.
- **Metrics on the reward dashboard** (§12) so the program is steered by
  measurement.

## 11. W7 — Set adoption and certification

Builds on
[`2026-10-02-xmage-compliance-oracle-design.md`](2026-10-02-xmage-compliance-oracle-design.md)
(levels A/B, the XMage oracle, the verdict store and gate). That spec stays
authoritative for the oracle itself; this section says what makes it scale
from one set to every set.

### 11.1 Where it stands (measured with `oraclediff status -set <S>`, ~1 s per set)

- Rollout X0–X8 landed; **FRA:A declared** (`compliance/declared.json`),
  0 of 285 outstanding. X9 (level B) not started
  (`compliance/gate/gate.go:123-124`); X10 (full sweep, seeder integration)
  not done — the seeder still emits "Oracle-audit the next N"
  (`scripts/seed_candidates.py:321`).
- 4,891 verdict rows, all level A: 4,638 agree, 156 diverge, 96 harness,
  1 xmage_wrong. Templates: cast-resolve 4,561, play-land 296, counter-spell 34.
- **The other 19 Standard sets: 508 of 5,331 entries outstanding (90.5%)** —
  150 untriaged divergences, 136 unsupported in gorge (kw:Bargain 20,
  kw:Craft 19, Teamwork 23, api:Endure 10), 97 template gaps (95 "no fixture
  gorge can cast"), 92 harness (55 share one driver message, "Can't find
  ability to activate command: Cast X"), 33 XMage does not implement.
- Mechanical cost is negligible: generating all 32,498 XMage card names took
  10.4 min; XMage replays a scenario in ~16 ms warm; ~7 s per set.
- **FRA took ~9 hours and ~25 commits.** ~90% of first-contact disagreements
  were harness/template problems; generated level A found 3 gorge bugs in 269
  cards, while the hand audit of the 16 cards XMage lacks found 7. **Level A
  is shallow** (cast and resolve only).

| Scope (inferred from XMage set type/date) | Sets | Cards | Scenario generated | Unsupported | Template gap |
|---|---|---|---|---|---|
| Standard | 20 | 5,141 | 4,891 | 136 | 102 |
| Pioneer | 77 | 16,942 | 15,865 | 641 | 381 |
| Modern | 114 | 22,792 | 21,174 | 993 | 546 |
| All | 589 | 32,498 | 29,683 | 1,528 | ~786 |

Across all XMage cards 1,528 are unsupported over 312 primitives; 1,402 are
blocked by exactly **one** primitive; 58 primitives each block 10+ cards
(api:Debuff 47, repl:Draw 43, kw:Bushido 35, kw:Rebound 34, kw:Shroud 30,
kw:Splice 30, Choose a Background 29, stat:CantTarget 29, kw:Soulshift 26,
MustBlock 48). Roughly 660 of the 2,387 non-playable corpus cards are
non-tournament (MakeCard, Planechase, Un-set stickers/contraptions, Draft).

### 11.2 Bottlenecks (the root causes, not the symptoms)

1. **Triage does not reuse.** A ruling is free text on one card's verdict row
   (`compliance/verdicts.go:35`); the spec's shape rulings
   (`compliance/rulings/<shape>.json`) were never built. Every divergence is
   triaged by hand, and the cluster files live off-repo on `/mnt/sata`.
2. **The gate is fragile to unrelated change.** `canon_sha` hashes the whole
   canonical snapshot (`gate.go:197-199`) and `scenario_sha` moves with any
   generator change (`gate.go:189-191`). One engine fix cycle staled 14 of 20
   Standard sets into a Java rerun. The spec's "freeze only the fields that
   changed" (§7) was replaced by a hash.
3. **Templates are three functions in one file** (`compliance/oraclegen/gen.go`
   `:112`, `:198`, `:285`), now a hot file for three live worktrees; there is
   no per-template versioning, so any template change stales every card.
4. **Findings do not become tickets.** No step turns `gorge_wrong`/`unsupported`
   rows into class-scoped tickets.
5. **Engine change cost** — every primitive or fix lands in `rules`; this is
   §2's RC1–RC5, and W1–W5 are the fix.
6. **Claims outside Standard are weak.** Without a printed list the gate falls
   back to the XMage manifest (`gate.go:140-144`), so cards XMage lacks drop
   out of the claim silently. Only the 20 Standard sets have printed lists.

### 11.3 Changes

| # | Change | Removes |
|---|---|---|
| C1 | **Commit the pass driver.** Move the std batch/write/summary scripts from `/mnt/sata/.../xmageoracle/` into `scripts/` behind `make compliance-pass SETS=…`; rerun only stale rows, under the heavy-job lock | X10 mechanical half; off-repo state |
| C2 | **Rulings by shape.** Key a ruling on `(template, field, normalised diff, IR api/mode)` in `compliance/rulings/<shape>.json`; `oraclediff diff` auto-classifies matches (1-in-10 sampled for human check); `oraclediff triage` writes committed `compliance/triage/<shape>.jsonl` clusters | per-card triage — Standard's 62 "choice" and 15 "class" divergences become 2 items |
| C3 | **Field-level freezing.** Replace `canon_sha` with the frozen fields the scenario asserts (spec §7 `oracleExpect` vocabulary); one file per template under `oraclegen/templates/` with its own version, so a template change stales only that template's cards | gate fragility; `gen.go` as a hot file |
| C4 | **Findings to tickets by class.** One `agentctl issue add` per shape or per primitive, carrying the affected cards and sets, each with a class-census ratchet (`fra_gap2_census_test.go` is the model). Retire `seed_candidates.py:321` | per-card fix tickets; duplicate fixes |
| C5 | **Primitive impact table.** `oraclediff gen` (or `forgec report`) emits primitive → cards → sets unlocked, ranked by declared-target formats. The 58 primitives blocking 10+ cards are the queue | guessing what to implement next |
| C6 | **Level B (X9).** Templates for activated abilities, trigger modes, attacks/blocks and statics; a fixture library for the 734 "no castable fixture" cards and Phyrexian mana. Make the meaning of level A explicit in the gate output | shallow certification |
| C7 | **Honest claims.** Printed lists for every tournament set; the gate refuses to declare a set without one (closes the `gate.go:140` fallback). Exclude non-tournament primitives from the "all sets" target | silent drop-outs |
| C8 | **Certification ratchet and dashboard.** Generated (not hand-committed, §6) per-set/per-format status with reason buckets from `status -all`; a test that declared sets and levels only grow and per-set outstanding counts only shrink, modelled on `knownUnsupported`; format roll-ups ("Standard:A" = all 20 declared) | regression and invisible progress |

### 11.4 New-set day-0 playbook

1. Bump `FORGE_REF` (as X0 did) and `XMAGE_REF`; `make xmage-oracle-setup`.
2. `make compliance-manifests` and the printed list for the new set.
3. `oraclediff gen` (~1 s per set); its skip output is the missing-primitive
   list → C5 table → C4 tickets, filed automatically.
4. `make compliance-pass SETS=<new>` (~7 s per set of XMage time).
5. Shape triage (C2): new divergences that match an existing ruling
   classify themselves; new shapes become one ticket each.
6. Hand scenarios for the cards XMage does not implement (FRA had 16).
7. Declare once outstanding reaches 0; the C8 ratchet locks it.

FRA showed steps 1–7 fit in a day once the harness is mature; C1–C3 are what
make that true for every set rather than the one the harness was tuned on.

### 11.5 Projection (*inference*, re-measure after C2)

Standard's diverge-plus-harness rate is ~5%, projecting ~1,400 triage items
across the 29.7k generated cards — tractable only with shape rulings (C2).
Pioneer level A is plausible in 4–6 weeks once C1–C4 land; the 58
high-impact primitives are ~3–4 weeks of focused seat work, cheaper per
primitive after W1/W4 because each primitive then lands as one new file with
typed params and no hand resume or clone plumbing. Level B multiplies scenario
count by ~3–5 but not triage effort, if shapes reuse.

### 11.6 How this ties to the engine work

Certification is where RC1–RC5 cost the most: each gorge_wrong verdict becomes
a fix ticket in `rules`, and a fix that must reach resume state, clone and
sibling paths by hand is the fix that comes back as the next divergence. The
order in §0 front-loads exactly the pieces (W1a clone checks, W3 tape kernel,
W4 typed params) that make a primitive a one-file change, which is what
"smooth adoption of all remaining sets" needs from the engine.

## 12. Measuring success

Tracked weekly; baselines from this spec:

| Metric | Baseline | Target |
|---|---|---|
| Fix-modified lines under 72 h old | 57% | < 25% |
| merge_fix rate (7 d) | 0.152 | < 0.05 |
| Review findings: clone omission + suspend/resume + sibling path | 17% of MAJOR | < 3% |
| Lockstep gate failures (Kind tables, census) per week | ~50 / 4.5 wk | ~0 |
| `clone.go` hand-edit commits / 30 d | 228 | 0 (generated) |
| Commits mentioning suspend/resume that are fixes / 30 d | 224 | < 20 |
| Files touched to add a layer-derived fact | 14–19 | 1–2 |
| `resumePoint` fields / `Ctx` fields / Host methods | 90 / 293 / 96 | ≤ 10 / ≤ 80 / union of ≤ 20-method roles |
| `rules` Engine methods | 2,162 | falling each W5 step |

## 13. Risks

- **Byte-identical replay is the safety net for everything.** It holds only
  with `.cards` present; a worktree without it runs vacuously green
  (AGENTS.md). Every lane ticket must show `TestHeads` actually ran.
- **W3 is the large bet.** It is gated by S3 with explicit kill criteria and
  a defined fallback.
- **Perf.** W1a generated clone, W1c constructors and W4 typed params all sit
  on hot paths; each must pass the perf-sb gate. W4 should *improve* hot
  paths by replacing map lookups with struct fields.
- **Refactor vs feature contention.** The lane lock (W6) freezes a subsystem
  briefly; keep steps small so feature throughput dips only locally.
- **Partial migrations.** Two kernels, two Host shapes, two param paths
  coexist during W3/W4. Each has a ratchet that only falls and a dual-run or
  derived check, so the legacy half cannot regrow.

## Appendix A. Measurement commands

All run from the repo root against `main` at `49bd9af8d`, with `/usr/bin/grep`
(bare `grep` is ugrep and skips git-excluded paths) under `bash -c`.

- LOC per top-level dir: `find <dir> -name '*.go' ! -name '*_test.go' | xargs cat | wc -l`
- Engine methods: `cat $(ls rules/*.go | grep -v _test) | grep -cE '^func \(e \*Engine\)'`
- Function lengths: go/ast walk over `rules` and `effects` non-test files
  (FuncDecl end line − start line).
- Host methods: `awk 'NR>=37&&NR<=828&&/^\t[A-Z][A-Za-z0-9]*\(/' effects/registry.go | wc -l`
- `Params` reads: `grep -ohE 'Params\["[A-Za-z0-9_]+"\]' rules/*.go effects/*.go` (non-test) `| wc -l`, `| sort -u | wc -l`
- String case literals: `grep -oE 'case "[A-Z][A-Za-z]+"'` over the same files
- Fix-on-fresh-lines: `git blame` of lines modified by 70 random `fix` commits
  (3–10 days old) in `rules/` and `effects/`; line age at the fix commit.
- Pipeline rounds: `.ds4/orchestrator/journal.jsonl`; verdicts:
  `.ds4/orchestrator/archive/*/verdict-*.md`; gate failures:
  `.ds4/orchestrator/gates/*/*/go-test-module.log`;
  `python3 scripts/reward_collect.py flow`.
- Resolution machinery: `rules/resolution_point.go:48-428` (fields),
  `rules/resolution_answer.go:29-1109`, `rules/resolution_answer_rest.go:22-749`
  (case arms), `grep -c 'Ask(' effects/*.go` (call sites).
- Mana ring: `ls rules | grep -iE 'mana|pay|cost' | grep -v _test.go | xargs wc -l`.
