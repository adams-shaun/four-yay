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
| W3 | Resolution kernel: checkpoint + intent-tape re-execution | `resumePoint` cursors, `Resume*` riders, the 102-arm answer switches, ~220 `Ctx` cursor fields, the "state lost across a suspension" bug class | W1a + clone-fidelity fuzz; spike S3 must pass its exit criteria |
| W4 | Typed parameter IR | stringly `Params["X"]` reads interpreted separately on each path; the hand-ratcheted param census | W0 ratchet in place |
| W5 | Package extraction (the lasagna proper) | the "any of 2,162 methods can call any other" coupling | W1, and W3 for the resolution layer |
| W6 | Pipeline and process | duplicate tickets; feature work racing a refactor in the same subsystem | none |
| W7 | Set adoption and certification pipeline | per-set bespoke audit projects | W2 per-card files; W4 helps |

Recommended order: W0, W1a, W1b, W2 and W6 immediately and in parallel (they
are small and mechanical); the clone-fidelity fuzz, then spike S3; then W3 and W4 run as sequenced
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
| String literals on `case` lines | effects 1,333, rules 1,250 (W0 defines the exact ratchet metric) |
| `Params["Lit"]` reads (`rules`+`effects`) | **2,146** over **693 distinct keys**; the compiled `ParamKey` set (`cards/params.go`) has 112 keys, read through 52 `.Param(cards.PK…)` call sites |
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
(`resumeAnswerBinding`, 64 arms; `resumeAnswerBindingRest`, 42 arms), and
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
- Six suspension holders besides `e.resume` (`unlessPayment`, `cumulative`,
  `triggerCost`, `offStackMana`, `queuedPlays.cont`, `etbMove`), and
  engine-posed asks in 38 files that bypass `effects.Host.Ask` entirely.
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
 L7  rules (orchestration)  turn/step/priority, stack, SBAs, casting flow,
                            Host implementation, Clone      — package rules
 L6  rules/resolve          resolution kernel: checkpoint, intent tape,
                            ask/answer, deferred-ask ordering (W3)
 L5  rules/trigmatch        trigger matching and queueing  ┐
     rules/combat           attack/block legality          │ subsystem
     rules/pay              mana + payment planning        │ packages (W5)
 L4  rules/chars            characteristic computation      ┘ (layers 1-7)
 L3  botpolicy, effects     (unchanged; chars/trigmatch use effects' filter
                            and count evaluators: matchesSpec, CDA via Ctx)
 L2  rules/cost             Cost/CostPart vocabulary, cost parsing (leaf)
     per-SA facts record    typed params, may-ask, mana facts (W4)
 L1  events, state, decision                                 (unchanged)
 L0  cards IR, ParamKey set                                  (unchanged)
```

Rules for the layering:

1. A lower layer never imports a higher one. `{rules/X, rules}` rows are
   redundant (Go forbids the cycle once `rules` imports X). What archtest must
   add is **ordering between sibling packages** — `rules/cost` imports none of
   `pay`/`chars`/`trigmatch`/`combat`/`resolve`; `chars` imports none of the
   L5 packages — and **prefix matching**: today the `{effects, rules}` row
   matches the exact path only, so `effects → rules/chars` would pass. The
   `forbidden` slice (`internal/archtest/arch_test.go:152-178`) already skips
   rows whose `from` package does not yet exist, so rows can land first.
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
The single exception is W3's park-and-continue paths (§7.2), migrated last as
a deliberate, re-pinned behaviour change.

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
- Make every census that walks `rules/*.go` recursive, so a W5 move into a
  subpackage cannot silently drop files and run vacuously green:
  `TestResumeStateOwnedOnlyByTheResolutionMachinery`
  (`internal/archtest/arch_test.go:355`), `rules/paramcensus_test.go:328`,
  `rules/choosefor_distinct_test.go:24`, `rules/potential_kinds_test.go:55`.
  The W0 ratchets themselves walk `rules/...`.

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

5. **Clone-fidelity fuzz:** over a cardfuzz corpus sample, clone at every
   intent boundary, continue both engines with the same intents, and compare
   event streams. A divergence is a clone or cache bug (a cold cache in the
   clone behaving differently from the warm original). This becomes a live
   correctness property under W3, so it must be green before S3.

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

- `effects.NewCtx(host, source, controller, sa, extra CtxInit)` (a value
  struct, not functional options, which allocate per call) and
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
  current layer epoch, backed by the existing derived memo. The pointer is
  valid until the next emit (the epoch changes); callers that hold
  characteristics across an emit copy the value. A debug build asserts the
  epoch on access. Effects and
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

## 7. W3 — Resolution kernel: checkpoint and answer-tape re-execution

Revised after an adversarial review of the first draft (§14). The first
draft snapshotted mid-`Advance`, served answers only at `effects.Host.Ask`,
and assumed every ask stops the chain; all three were wrong. This version
fixes each and states which paths are a deliberate behaviour change rather
than a pure refactor.

### 7.1 Design

The engine is deterministic and event-sourced, and `Clone` is guaranteed
correct only at an intent boundary (`rules/clone.go:62-64`). Build on exactly
that:

1. **Checkpoint at the intent boundary.** When the priority pass that begins
   a resolution is submitted, the kernel keeps `S0`: the engine as it stood at
   that decision (an ordinary `Clone`, taken where `Clone` is legal). `S0` is
   immutable after creation and shared by pointer — never recycled by
   `Spare`/`Release`, never copied when the live engine is cloned.
2. **The tape is the intents, not the answers.** Every decision posed while
   the resolution is in progress — effect asks through `Host.Ask`, **and** the
   engine-posed ones that bypass it (`askReplacementChoice`,
   commander-zone choice, `replacement_etb.go`, casts made during a
   resolution in `cast_asks.go`, mana activations, unless-cost and cumulative
   windows, `askOffStackMana`) — is answered by one or more intents, and all
   of them are already appended to `L.Intents`. The tape is simply
   `L.Intents[k0:]`, where `k0` is the checkpoint. A multi-intent answer (an
   unless-cost window with mana activations, a payment with triggered mana
   abilities) needs no special case.
3. **Asks become tape reads at the one choke point.** `e.ask` (the function
   all 38 asking files funnel through) consults the tape: if the next intent
   exists, it is consumed and returned as the answer; if not, the decision is
   posed. Effects and engine code no longer save locals — they ask and
   receive.
4. **On Submit**, the new intent is appended and the kernel re-runs from
   `S0`: a scratch engine cloned from `S0` re-executes the resolution from its
   first line, reading answers from the tape. It applies and appends events as
   normal (state and `Seq = len(Events)`, `events/log.go:110`, depend on it),
   and the kernel checks `scratch.L.Events[base:base+n]` against the recorded
   prefix byte for byte. `DecisionAsk`/`DecisionMade` are emitted at every ask
   served from the tape exactly as `Submit` emits them today. When the run
   reaches the next unanswered ask or finishes, the scratch engine is adopted
   and `L.Intents` is spliced from the live log.
5. **Unwinding at an unanswered stop-ask** is a sentinel panic recovered by
   the kernel (Go has no other non-local exit once the 47 `Suspended()` polls
   are gone). The unwound run's ambient Engine fields (`applyingReplacement`,
   `fusedResolving`, `windowPaidX`, `damaging`, `resolutionCtx`, …) are not
   trusted: every field tagged `reset` by W1a is zeroed on adopt, and the
   posed state is the clean state the view, legal-action walk and search
   clones read. This makes **W1a a hard prerequisite of W3**.
6. Deferred-ask ordering, `AskEmpty` silent skips, the CR 117.3b priority
   reset and the trailing `Priority` event become kernel properties,
   implemented once.

What this deletes: the per-primitive cursors in `resumePoint` (32 fields) and
its chain-shared Ctx state (23); the `Resume*` riders on `decision.Decision`
(31); `resumeAnswerBinding`/`Rest` (64 + 42 arms); the ~220 cursor fields on
`Ctx`; the nine `Suspend*`/`Suspended` Host methods and their 47 polls; most
of the ~15 per-object side tables; and, as the migration completes, the six
suspension holders outside `e.resume` (`unlessPayment`, `cumulative`,
`triggerCost`, `offStackMana`, `queuedPlays.cont`, `etbMove`). What remains is
`k0`, `S0` and the event-prefix length.

Why it removes the bug class: nothing crosses a suspension except intents
already in the log. A primitive's locals, the ParentTarget link, Remember
sets and LKI captured earlier in the chain are all recomputed by re-running
the same deterministic code from the same checkpoint with the same answers —
the property replay already relies on.

### 7.2 Ask shapes, and what is and is not a pure refactor

Not every ask stops the chain today:

| Shape | Today | Under W3 | Event order |
|---|---|---|---|
| **Stop-ask** (most effect asks) | resolution suspends; the asking primitive re-runs from its first line on resume | unwind; re-run from `S0` on Submit | **identical** — pure refactor |
| **Deferred second ask** (`resolution_ask.go:41-60`) | queued on `contChain` while the body continues | the tape run continues past the pending ask exactly as today and poses the deferred one in the same order | identical, if the kernel models "pending, continue" — S3 must prove it |
| **Park-and-continue**: replacement choice parks the event ("never emitted, never applied") and the chain keeps running (`replacement_choice.go:10-16, 254-262`); commander-zone park (`ask.go:24-35`) | post-park events are emitted **before** the answer | the answer is available at the point of the event on re-run, so the replaced event applies **in place** | **changes** |

The park paths are a deliberate behaviour change, not a refactor. Applying
the replacement at the point the event would happen is what CR 616.1 says;
the park is the approximation. They migrate last, in their own tickets, with
a golden re-pin and the CR argument in the commit message. Everything else
must be byte-identical.

### 7.3 Hidden information and search

Search builds hypothetical worlds at a decision: `CloneHypothetical` reseeds
the RNG (`chance.go:189`) and `searchprobe.RedealBase` re-deals hidden cards.
A world built while a resolution is suspended carries `S0`, which holds the
**true** hidden cards and RNG. Re-running from it in that world would leak
clairvoyant information into honest search — exactly what the archtest
clairvoyant boundary forbids. If `S0` were simply redealt, the prefix check
would panic wherever the prefix shuffled or drew.

Required design (S3 exit criterion): the hypothetical world applies **the same
redeal mapping to `S0`** as to the live engine (revealed cards stay fixed, as
the redealer already guarantees), and the RNG is reseeded **at the draw
counter the prefix ended on** rather than at `S0`, so prefix draws reproduce
and post-prefix draws are fresh.

### 7.4 Alternatives considered

- **Explicit step machine / CPS** (`func(h, c, sa, k *Cont) Step`): rewrites
  all 94 asking functions into resumable steps; every local becomes an
  explicit field again — the same hand defunctionalisation, better organised.
  It is the fallback if S3 fails on cost.
- **Goroutine coroutines:** a parked goroutine cannot be cloned, which breaks
  `Clone` at a decision — the operation MCTS/azmcts and searchbench depend
  on. Rejected.
- **Invertible events (undo log instead of a checkpoint):** every event kind
  would have to carry before-values; a large change to the frozen event
  vocabulary. Rejected for now.
- **Snapshot mid-`Advance` at resolution start** (the first draft): violates
  the Clone contract — the livelock watcher's observation state is reset by
  `cloneWith`, and in-flight `resolutionCtx`, trigger drains and the cast in
  flight can be aliased — and it misses engine-posed asks. Rejected.

### 7.5 Costs and hazards (*hypotheses* S3 must measure)

- **Checkpoint cost.** One Clone per resolution that poses any decision. A
  per-SA "may ask" bit would have to close over SVars (Charm `Choices$`,
  Repeat, `Execute$`, delayed triggers) and still could not predict
  board-dependent engine asks, so it is not used for correctness. The cheap
  path is to keep the Clone that the priority decision already permits and
  drop it when the resolution finishes without asking. Measure on the perf-sb
  gate, both mana modes.
- **Re-execution cost.** k decisions in one resolution cost O(k²) primitive
  runs (typical k is 1–3). Pathological loops (vote with 8 players, RepeatEach
  over many objects) need measuring; if needed, add intermediate checkpoints
  every m decisions — those are also intent boundaries, so the same rule
  applies.
- **Clone at a decision** shares `S0` by pointer, so search pays nothing extra
  per clone beyond §7.3's redeal.
- **Cold caches.** A scratch engine starts with cold caches (`rekeyVersion`,
  the potential-walk cache, the derived memo) while the live engine's are
  warm. A latent cache bug that is harmless today would surface as a prefix
  divergence. Mitigation: W1a's clone-fidelity fuzz (§5) runs before S3.
- **Divergence in production.** The prefix check catches nondeterminism by
  name. In a hosted match it must not crash the table: it records a feedback
  snapshot (the existing capture) and ends the match as a recorded engine
  fault. In tests and the dual-run it fails hard.
- **Observer hooks.** `ManaAbilityHook` and `paymentStats` are nil in a
  clone; the kernel suppresses them during the prefix and re-attaches them
  after it, so harness counts neither miss nor double-count.

### 7.6 Spike S3 (gate for W3)

Prerequisites: W1a steps 1–3 and the clone-fidelity fuzz green.

Implement the kernel behind a flag for spell resolutions, with three converted
families of different shapes: a single stop-ask (ChooseColor), a loop with
per-iteration asks (RepeatEach or Vote), and a nested chain with ParentTarget
(the c21c390de scenario). Include one engine-posed ask during a resolution (an
"as it enters" choice via `replacement_etb.go`) to prove the tape covers the
bypass paths.

Exit criteria, all required:

1. Byte-identical event streams versus the legacy kernel on `TestHeads`, the
   oracle scenarios for those families, a 2,000-game cardfuzz sample and the
   `cmd/repro` fixtures (dual-run harness, §7.7).
2. A hypothetical world built at a suspended resolution (redeal plus
   `CloneHypothetical`) re-runs without a prefix divergence and without
   reading true hidden cards (§7.3).
3. Perf-sb throughput regression ≤ 3% in both mana modes; Clone-at-decision
   cost measured.
4. Lines deleted for the converted families ≥ lines added to the kernel.

Kill criteria: (1) or (2) cannot be met without per-primitive special cases,
or (3) exceeds 10% after intermediate checkpoints. Then fall back to the
step-machine alternative, scoped the same way.

### 7.7 Migration (after S3 passes)

- The new kernel handles a resolution only when every asking site it can
  reach is tape-ready, decided at run time: an un-migrated site reached during
  a tape run aborts the tape run, restores the live engine and lets the legacy
  path handle that resolution. Because engine-posed asks are board-dependent,
  this is a run-time fallback, not a static eligibility rule.
- **Dual-run oracle:** in tests every resolution runs through both kernels on
  cloned engines and the event streams are compared. Every conversion batch
  must pass it on the corpus-wide cardfuzz sample and the oracle suite.
- Each batch deletes its `ResumeKind` arms, `Resume*` riders and Ctx cursors
  in the same commit, and lowers the W0 ratchets.
- Order: single stop-asks (the majority) → loops (Repeat, Vote, charm rest,
  villainous, generic choice) → deferred asks → the suspension holders
  (unless-pay, cumulative, triggerCost, offStackMana, queuedPlays, etbMove) →
  the park-and-continue paths, last, as a deliberate re-pinned behaviour
  change (§7.2).
- Old saved logs keep replaying for every path but the park paths; saved logs
  that cross a park path are re-recorded or marked pre-W3 in the feedback
  fixtures.
- The program runs as one sequenced lane (W6): no feature ticket in the
  resolution files while a batch is open.

## 8. W4 — Typed parameter IR

Two pieces of machinery already exist, and W4 extends them rather than adding
a third:

- **The compiled `ParamKey` set** (`cards/params.go:21+`): 112 keys stored as a
  mask plus a popcount-ranked value slice on the hidden `sa.ps`, read through
  `Param`/`ParamStr`/`HasParam` at 52 `.Param(cards.PK…)` call sites. The
  2,146 `Params["X"]` literal reads over 693 keys are what has not moved onto
  it yet.
- **The write-once slot** (`cards/slot.go:56-89`), which hangs downstream
  compiled facts on an IR node. There is **one** `ExtSlot` per SA, already
  claimed for every `AB` ability by `manaSAFacts`
  (`rules/compiled_text.go:411`); `Store` returns false silently on a second
  claim, and the slot is nil for SAs never bound by their face's derive.

Plan:

1. **One rules-owned per-SA facts record** replaces the single-purpose slot
   use: mana facts, W4's typed params and any other compiled per-SA fact live
   in one struct hung on the slot, so nothing competes for it. Binding is made
   total (every SA reachable at resolution is bound), with a test.
2. **Move literal reads onto `ParamKey`** in read-count order (ValidCard 95,
   Defined 82, ValidTgts 72, ValidPlayer 63, Cost 59, Choices 54, Optional
   43). Adding keys edits `cards/`, which moves `CompilerFingerprint`: that is
   a one-time IR recompile per worktree, not a blocker, so batch key additions
   into few commits.
3. **Per-API typed structs** (`ChangeZoneParams{Origin ZoneMask; Destination
   state.Zone; ChangeType Spec; Defined Ref; …}`) compiled once per SA from the
   `ParamKey` values into the facts record. The compiler for an API is the
   **only** reader of that API's params; offer, payment, planner and resolution
   all read the struct, so "sibling path not updated" stops being possible for
   parameters. Start with APIs whose params are read on more than one path
   (costs, targets, `Defined$`).
4. An unread param is detected by the compiler at load (loud degrade, existing
   contract), so the param census becomes **derived** from the compilers
   instead of a hand-ratcheted 3,843-line table.
5. Strings compile to masks/bitsets (invariant 12,
   `docs/agents/invariants.md:191`).

Ratchet: `stringParamReads` (W0) only falls.

## 9. W5 — Package extraction (the lasagna proper)

The mana/payment survey shows why order matters: that subsystem is a
**24.5k-line ring** (31 files) whose 238 Engine methods reach ~60 Engine
fields and ~127 other Engine methods, call `ask`, `emit`, the layers and the
trigger queue, and resume resolutions. `Cost` is used 632 times outside it.
Extracting it first would fail. Extract leaves and low-fan-in subsystems first:

| Step | Package | Why this order | Size |
|---|---|---|---|
| E1 | `rules/cost` | Leaf vocabulary every other subsystem uses: `Cost`, `CostPart`, `ParseCost` (80 call sites), `formatCost`, pip tables, the 30 `regexp.MustCompile` cost parsers (compile them to a table — perf rule) | small |
| E2 | utilities out of feature files | `controllerOf` (124 call sites in 47 files) lives in `trigger_match.go:915`; similar shared helpers move to `rules/query` or stay in `rules` but out of subsystem files | small |
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

### 9.1 E7 redesign: a leaf parameter package and a narrow pay session

Measured on `main` at 41dd9f57e (after E7 slices 1-10): `rules/pay` holds
~4.5k lines behind a 10-method `pay.Engine`; the ring still in `rules`
(`payment_plan*`, `cast_payment*`, `cast_payparts`, `announce_pay`,
`unless_payment`, `mana.go`, `mana_activation.go`) is 226 Engine methods and
~7.7k lines. Each further slice was stalling on the same two walls, so the
remaining work is a two-step redesign (operator decision, 2026-10-03).

**Step 1 -- leaf parameter package.** The compiled parameter records the
ring reads on nearly every path move out of `effects` into
`effects/params`, which imports only `cards`, `state` and `decision`
(archtest `paramsImports`): the mana production record (`ManaOf`), the
generic targeting tier (`TargetsOf`), the Defined tier (`DefinedOf`, `Ref`),
api:DealDamage's record and the `Combo` classifier. `effects` aliases every
name (types and constants by alias, functions by one-line forwarders), so no
caller churns; the two `Amount$` evaluations that need a `Host` become
`effects.ManaAmountNum`/`ManaAmountResolvedStrict`. `effects.SAFacts`
embeds `params.Facts` as its first field, so `params.LoadFacts` reads the
configured record through the slot without `effects` (the offset is pinned
by `TestFactsIsSAFactsPrefix`). `rules/pay` may import `effects/params`. The
parameter census scans `effects/params` in the effects namespace, so
attribution is unchanged; the codeshape compiler-file constants follow the
files.

**Step 2 -- the pay session.** Ring code takes a `*pay.Session` plus
`pay.Engine`, never `*Engine`. The session is plain data the engine owns
and clones: the ring-owned engine fields (the payment-plan query memo and
carriers, the payment stats, the mana-activation frames, the unless-payment
state) move into it, so they leave the Engine field list and gain one
`cloneForeignDeep` entry. The cast's payment slice of `pendingCast`, the
ask seam (one coarse `Ask(flow, d)` that sets `choosing` and poses `d`) and
log appends through `Emit` reach the ring through it. `pay.Engine` stays
under 20 methods (`payEngineMethods`); each slice lowers
`engineMethodCount` and moves its white-box tests with the code.

**Measured ceiling.** A fixed-point census over the ring's call graph
(every method whose callees, fields and `effects` references are all inside
the moved set or the seam) with step 1 done, every ring-owned field in the
session and a greedily chosen 19-method `pay.Engine` reaches **~2.4k of the
~7.7k lines** (about 30%); the curve is flat after the first five seam
methods (+270, +120, +60, +50, +50 lines). The other ~70% is blocked by
four families that are not payment at all: the derived-characteristics
queries (`Derived`, `hasKeywordH`, `objColors`, `derivedTypesOf`), the
activation gates the mana walk shares with every ability (`abilityRestricted`,
`activationConditionOK`, `activationLimitBlocked`, `grantedAbilities`,
`walkFaceFactsOf`), `effects.Ctx` count evaluation and nested mana-ability
resolution (`appendAvailableManaAbilitiesGate`, `resolveManaEffect`,
`answerNestedManaColor`), and the cost-static collection
(`collectCostStatics`, `potentialCostModsUsing`, `windowManaUnits`). So E7
lands what the session honestly frees -- the payment planner core, the
unless-payment reachability, the cast payment parts -- and the activation /
mana-walk half of `mana_activation.go` waits for E4 (`rules/chars` as a
read interface pay may take) and a W1d-style evaluation seam, not for a
wider `pay.Engine`.

## 10. W6 — Pipeline and process

- **Refactor lane.** W1–W5 tickets run in one lane, in sequence, with
  `Depends-On` chains. While a ticket in the lane holds a subsystem, the
  seeder does not dispatch feature tickets that touch that subsystem's files.
  agentctl has no such file-set lock today (only `Depends-On` and the
  serialized landing lane), and tickets do not declare a file footprint up
  front, so this needs new agentctl work and a daemon restart: a ticket field
  naming the locked path set, and an intake check against it. Until then the
  operator holds conflicting feature tickets by hand. This replaces hot-file
  notes with scheduling.
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
3. **Templates are three functions in one file** (`compliance/oraclegen/gen.go`,
   items at `:112`, `:198`, `:285`), now a hot file for three live worktrees;
   there is one global `Version` (`gen.go:25`) and no per-template
   versioning, so any template change stales every card.
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
- **W3 is the large bet.** It is gated by W1a, the clone-fidelity fuzz and
  S3 with explicit kill criteria and a defined fallback; its park-and-continue
  paths are a deliberate behaviour change with a planned re-pin (§7.2).
- **The lane lock (W6) is new agentctl work**; until it exists the operator
  sequences conflicting feature tickets by hand.
- **Perf.** W1a generated clone, W1c constructors and W4 typed params all sit
  on hot paths; each must pass the perf-sb gate. W4 should *improve* hot
  paths by replacing map lookups with struct fields.
- **Refactor vs feature contention.** The lane lock (W6) freezes a subsystem
  briefly; keep steps small so feature throughput dips only locally.
- **Partial migrations.** Two kernels, two Host shapes, two param paths
  coexist during W3/W4. Each has a ratchet that only falls and a dual-run or
  derived check, so the legacy half cannot regrow.

## 14. Review log

An adversarial review (2026-10-03) checked about 30 cited facts against the
code and attacked W3. Changes made in this revision:

- **W3 redesigned** (§7). The first draft snapshotted mid-`Advance` (violates
  the Clone contract, `clone.go:62-64`), served answers only at
  `effects.Host.Ask` (38 files pose engine asks that bypass it), and claimed
  byte-identical order for every ask (park-and-continue paths emit post-park
  events before the answer). Now: checkpoint at the intent boundary, tape =
  `L.Intents[k0:]`, tape reads at `e.ask`, sentinel-panic unwinding with W1a
  `reset` fields zeroed on adopt, park paths migrated last as a re-pinned
  behaviour change, and a hidden-information design for search (§7.3).
- **W3 now depends on W1a** and a clone-fidelity fuzz (§5 step 5).
- **W4 rebuilt on existing machinery**: the compiled `ParamKey` set has 112
  keys, not 15; one rules-owned per-SA facts record replaces competing claims
  on the single `ExtSlot`; editing `cards/` is a one-time recompile, not a
  blocker.
- **Archtest**: sibling ordering and prefix matching instead of redundant
  `{rules/X, rules}` rows; three more non-recursive census globs listed in W0.
- **W6** lane lock marked as new agentctl work.
- Corrected counts: 9 Suspend/Suspended Host methods; `resumeAnswerBinding`
  64 arms; `ParseCost` 80 call sites; `controllerOf` 124 sites in 47 files;
  six suspension holders besides `e.resume`; case-literal metric definition.

## Appendix A. Measurement commands

All run from the repo root against `main` at `49bd9af8d`, with `/usr/bin/grep`
(bare `grep` is ugrep and skips git-excluded paths) under `bash -c`.

- LOC per top-level dir: `find <dir> -name '*.go' ! -name '*_test.go' | xargs cat | wc -l`
- Engine methods: `cat $(ls rules/*.go | grep -v _test) | grep -cE '^func \(e \*Engine\)'`
- Function lengths: go/ast walk over `rules` and `effects` non-test files
  (FuncDecl end line − start line).
- Host methods: `awk 'NR>=37&&NR<=828&&/^\t[A-Z][A-Za-z0-9]*\(/' effects/registry.go | wc -l`
- `Params` reads: `grep -ohE 'Params\["[A-Za-z0-9_]+"\]' rules/*.go effects/*.go` (non-test) `| wc -l`, `| sort -u | wc -l`
- String literals on case lines: all `"…"` literals on lines matching `^\s*case ` over the same files (the W0 `cmd/codeshape` metric is the authoritative definition)
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
