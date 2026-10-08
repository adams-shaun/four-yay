# Search-probe engine reset: pooled replay instead of clone

Status: design (approved for planning)
Date: 2026-10-07

## Problem

The hosted-root search seat (`cmd/botbench -a search -b bot -hosted-root`) is
allocation-bound in `internal/searchprobe`. An 8-game CPU+alloc profile of the
current tree attributes roughly 1.3 GB of `alloc_space` to "engine clone"
frames:

| frame | alloc (8 games) |
|---|---|
| `rules.(*Engine).cloneWith` | 0.87 GB cum |
| `state.(*Game).CloneIntoDirty` | 0.63 GB flat |
| `events.growEvents` | 0.49 GB flat |
| `rules.newEngineShell` | 0.24 GB |
| `rules.(*Engine).creatureTypeOptions` | 0.22 GB |

A common framing calls this "a clone per simulated world". That is not what the
code does. `searchprobe.Sample` (internal/searchprobe/sample.go:155) **rebuilds**
each attempt from `PublicGame.Decks` via
`rules.NewHypotheticalPlanned -> newWithRNG -> newEngineShell + genesisDeal`,
recycling only arrays through `rules.Spare` (sample.go:255,265,274). The clone
frames come from a different set of sites:

1. **Kept-world detach** — `w.Engine = w.Engine.Clone()` at
   internal/searchprobe/sample.go:568, once per returned world. Needed because
   `weightedIndices` may select the same proposal more than once, so each
   returned `World` must own an independent engine.
2. **Teacher rollouts** — `internal/searchprobe/teacher.go:238` parallel
   `worlds[k].Engine.Clone()` per (world, candidate); `:267` sequential
   `CloneInto(&sp)`.
3. **Genesis log growth** — `growEvents` as the freshly dealt log grows
   (0.49 GB), including the forked-growth path
   (`forkMinSlack=256`, events/log.go:437) for cloned logs.

`newEngineShell` itself is 0.24 GB — not the dominant term. The dominant term is
the deep clone (`CloneIntoDirty` + `cloneWith`) of an already-dealt engine.

## Goal

Remove the clone-per-world / clone-per-rollout allocations by giving the search
probe a **pool of reusable engines that are reset in place by re-running genesis
and the observed tape**, instead of deep-copying a live engine.

Output-identical is a hard requirement: the sampled worlds, the events, and the
RNG stream must be byte-for-byte what the current code produces for the same
seed and history. Performance improvement is best-effort, measured and reported.

## Non-goals

- Removing the per-attempt shell build or the genesis deal. The shell is cheap
  and `Spare` already recycles the arrays; the win here is clones.
- Changing `rules/genesis.go`'s `newEngineShell`/`genesisDeal` logic. `Reset`
  re-runs the existing code against an existing `*Engine`.
- Touching `internal/searchprobe/redeal.go` (contended by the live cpu-redeal
  editor) beyond what is unavoidable.
- Any change to search semantics, weights, or the exclusion/plan caches.

## Design

### C1 — `rules.Engine.Reset` (core mechanism)

Add an additive `rules` API beside `newEngineShell`:

```go
// Reset returns e, a previously spent engine, rebuilt in place to the exact
// state NewHypotheticalPlanned(cfg, prefix, planner) would produce. e's
// backing storage (G/Objs/zones, L, arena, pool fields) is reused; nothing
// observable about the rebuilt game differs from a fresh construction.
func (e *Engine) Reset(cfg Config, prefix []ChanceDraw, planner ShufflePlanner) (err error)
```

Implementation: `newEngineShell` currently constructs a fresh `*Engine` and
sets ~30 fields plus `adopt*`/`releasePools` adoptions. `Reset` factors the
field assignment into a shared `resetEngineFields(e, cfg, random)` called by
both `newEngineShell` and `Reset`, so the two cannot drift. `Reset` then
mirrors `NewHypotheticalPlanned` exactly: validate `prefix`; build
`newRNG(cfg.Seed)` with `chance = &chanceState{prefix: copy, planner,
shuffleOrdinals: map}`; call `newWithRNG(cfg, r, false)`'s body against `e`
(GameStart emit + `genesisDeal`). It does not call `enableLiveArena`; the
search caller keeps its existing `SetDecisionArena(true)` immediately after
construction (`sample.go:271`). `Reset` reuses arrays in place, so it is called
on an engine that still owns them: a pooled engine is **not** `Release`d
between attempts (`Release` nils `e.L.Events`/`e.G.Objs`, which would defeat
in-place reuse). `Release` stays for engines that are genuinely done.

The `Spare` contract (rules/genesis.go:16-30) is the model and the precedent:
every field is overwritten or cleared before it is read, and the emitted event
stream carries no byte derived from backing-array capacity. `Reset` extends
that contract to the whole shell.

Risk: `newEngineShell` sets `G` internals (players, zones, stack, dirty tail),
`loop`, `compiledText`, `deckManifests`, `turnsTaken`, `manaExpended`,
`derivedMemo`, the `pool=` fields, watchers and `NameUniverse` refs. Factor-out
plus a diff test is the mitigation; a missed field is a replay bug, which the
test must catch.

### C2 — attempts draw from a per-worker pool

`internal/searchprobe` gains an engine pool (new file
`internal/searchprobe/engine_pool.go`, avoiding contended files). Each worker
owns one pool (`[]*rules.Engine`, matching the existing per-worker
`constraintPlanCache`/`rules.Spare` ownership at sample.go:488-490).

`runAttempt` (sample.go:256) draws an engine from the pool and `Reset`s it with
the attempt's `cfg`, `tape` and `proposal.plan`, replacing
`rules.NewHypotheticalPlanned(hcfg, tape, proposal.plan)` (sample.go:266). A
rejected attempt returns its engine to the pool instead of
`*spare = e.Release()` (sample.go:274). The draw pattern for each attempt
(seed, observer, proposal) is unchanged, so the RNG stream is unchanged.

### C3 — kept worlds transfer, no clone

Replace the per-world `Clone()` at sample.go:568. A returned `World` takes
ownership of the reset engine directly. Because `weightedIndices` can pick the
same proposal index more than once, a repeated selection draws an additional
pooled engine and replays that proposal's tape into it, yielding an independent
engine with the identical event stream and state. The pool grows on demand
(no fixed cap) and never blocks; its steady-state size is bounded by the live
worlds plus one engine per in-flight attempt per worker.

### C4 — teacher rollouts replay from the pool

`teacher.go` parallel (`:238`) and sequential (`:267`) rollout paths draw a
pooled engine and replay from the world's tape instead of `Clone()` /
`CloneInto(&sp)`. The sequential path already recycles via `Release`; the
parallel path is extended to a per-worker pool the same way.

### Phase order

C1 (Reset + test, no caller change) -> C2 (attempts via pool) -> C3 (world
transfer) -> C4 (teacher). Each phase lands independently and must keep the
whole suite and the output-identical check green.

## Verification

- **Output-identical (primary).** On the same seeds/history, botbench
  hosted-root `run.log` must match the pre-change run: `sampler: attempts /
  accepted / prefix-rejected`, `ESS weighting` lines, and `ms/asked decision`.
  Before/after binaries are run back to back on a pinned core.
- **Determinism.** `Reset` must not perturb the toss/shuffle draw order. A new
  `TestEngineResetIsInvisible` builds a game both ways
  (`NewHypotheticalPlanned` vs `Reset` of a spent engine) over the repo decks
  and diffs the entire event log plus selected `G` fields.
- Existing `TestSpareReuseIsInvisible` pins the array-reuse contract and must
  stay green.
- Full `scripts/postmerge_full.sh <worktree> <sha>` must end exit 0 / ALL GREEN
  before landing.

## Measurement

Profile with the established harness:

```
go build -o /tmp/botbench-<x> ./cmd/botbench
taskset -c 8 env GOMAXPROCS=1 GOMEMLIMIT=2GiB /tmp/botbench-<x> \
  -dir .cards -a search -b bot -hosted-root -games 8 -workers 1 -format constructed \
  -cpuprofile <f.cpu> -memprofile <f.mem> > <run.log> 2>&1
```

Report `alloc_space` total and the clone frames before/after, plus CPU, using
controlled back-to-back runs (load drift on this box is large). Do not quote
stale numbers.

## Contention / sequencing

- `rules/genesis.go` is a generated-hotspot file with a live cpu-redeal editor
  (`scripts/hotfiles-notes.json`, entry for `genesis.go`). C1's `Reset` and the
  `newEngineShell` factoring are the only necessary edits there: keep the hunk
  minimal, prefer a new `rules/reset.go` file, and land C1 **after cpu-redeal**.
- `internal/searchprobe/redeal.go` is cpu-redeal's live file; the pool lives in
  a new `engine_pool.go` and does not edit `redeal.go`.
- `state` and `events` have no live editors; `growEvents` tuning, if any, is a
  separate follow-up, not part of this spec.
