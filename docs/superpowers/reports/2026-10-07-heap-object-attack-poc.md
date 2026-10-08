# Heap-object attack — POC results (2026-10-07)

Spike branch `wt/heapattack` (off `wt/dzgorge-rebase` = main + the vendored `cmd/dzgorge`
driver). Goal: raise wall-clock games/s of the dzgorge self-play generator by cutting
heap-object CPU (allocation rate → mallocgc + GC + write barriers, plus the clone copy).
Contract: **bit-identical** events / RNG / chain head / visit records.

## Workload

The dzgorge push workload, exactly as profiled earlier, on the rebased (main) build:

```
play -cards .cards -decks <dzgorge-repro/inputs/decks> -pool <…/pool.tsv> -split train \
  -a 'az:sims=100:autopay:oppnodes:explore' -b <same> -seed 701 -mulligans 2 -mull-heuristic \
  -record-features entity -pairs {34|70} -workers 5
```

`GOMAXPROCS=5`, `-pprof` off. Paired A/B: baseline = `wt/dzgorge-rebase/bin/dzgorge`,
cut = `wt/heapattack/bin/dzgorge`.

## Bit-identical evidence

Every run printed the same corpus hash across baseline and cut:
- 34 pairs: `370fe249f618c93b…`
- 70 pairs: `129c9238651fd55c…`

Plus the engine guards pass on the cut: `TestCloneIntoIsInvisible`,
`TestSpareReuseIsInvisible`, `TestHeads$`, `TestCloneGenIsUpToDate`, `TestCloneInto`
(`go test ./rules`, capped, ok).

## Changes (POC, quick-and-dirty)

1. **Recycle the `Engine` struct** (`rules/genesis.go`, `rules/clone.go`).
   The single biggest flat allocation of the search was the `&Engine{…}` composite
   literal in `cloneWith`: **3.73 GB = 11.0 % of alloc_space** (`sizeof(Engine) ≈ 2 KB`
   × ~1.8 M clones). `Spare` gains an `engine *Engine`; `Release` hands the spent
   engine's struct over; `cloneWith` reuses it, zeroed (`*c = Engine{}`) so it is
   semantically a fresh allocation. Guard tests confirm invisibility.
2. **`paymentIntent` scratch** (`seat/bot.go`).
   Per actor-priority per simulation it did `d.Clone()` + an `Options` slice + three
   maps (`payable`, `candidateToOriginal`, `legacyOrdinary`) = **17.3 % cum of
   alloc_space**. A per-`Bot` reused `decision.Decision` + reused maps/slice replaces
   them. (`Bot` is single-goroutine; adds mutable scratch state — see caveats.)

3. **`manaMemberCarry` growth** (`rules/mana_member_carry.go`).
   `entryFor`/`touchObj` used `make(len = i+1)` on every new index, ignoring the
   spare capacity they had just allocated, so a carry regrew its slice one step
   at a time. A length-vs-cap check plus a zero of the newly exposed region cuts
   `entryFor` alloc_space ~4× (1.28 GB → 312 MB over 90 pairs). **Isolated wall
   effect ≈ 0** (cut2 median 6708 → cut3 6712, within noise): the churn was
   cheap memset/copy off the critical path, not GC pressure. Kept as alloc
   hygiene.

## Results (games/hour; HGC)

34 pairs, 3 reps (default GOGC):

| arm | reps | median | vs base |
|---|---|---:|---:|
| baseline | 4502.6, 4520.9, 4375.9 | **4502.6** | — |
| cut (struct + paymentIntent) | 4562.8, 4676.1, 4683.0 | **4676.1** | **+3.8 %** |

34 pairs, 2 reps:

| arm | median | vs base(default) |
|---|---:|---:|
| baseline GOGC=800 | 4847.9 | +7.7 % |
| **cut GOGC=800** | **4969.1** | **+10.4 %** |
| cut GOGC=400 | 4910.0 | +9.0 % |
| baseline GOGC=off, GOMEMLIMIT=6GiB | 4893.8 | +8.7 % |
| **cut GOGC=off, GOMEMLIMIT=6GiB** | **4991.7** | **+10.8 %** |

70 pairs (directional, high variance on the shared box):

| arm | reps |
|---|---|
| baseline default | 4343.6, 5563.0 |
| cut GOGC=off | 6426.8, 6192.6 |

## Read

- The GC/alloc thesis pays: the **GOGC knob alone is ~+8 %** (baseline default →
  GOGC=off/800), and the corpus hash is unchanged — GC settings never touch output.
- The two structural cuts are **bit-identical and worth ~+3.8 %** at default GOGC,
  stacking to ~+10.8 % with GOGC=off. Their marginal value shrinks as GC pressure
  falls (at GOGC=off the cuts add ~+2 %), so at least some of the win is the recycled
  struct simply removing work the GC knob also hides.
- A later, quieter-box batch (peers on 8091–8096) put baseline default at ~6536 gph
  and the struct+paymentIntent+mana-carry cut at ~6712 gph — the same ~+3–4 %
  structural delta, confirming the paired result is load-robust even though absolute
  gph is not comparable across sessions.
- Clone copy itself is only ~4.7 % CPU; the addressable prize is the ~19 %
  `mallocgc` + mark + write-barrier pool, which the struct cut shrinks.

## Caveats / hardening needed before landing

- `seat.Bot` scratch adds mutable state to a type documented as stateless for
  priority decisions and a potential concurrency hazard if a `Bot` is ever shared.
  Land only behind the search's per-`Bot` lifetime or move the scratch to a search
  scope.
- The `Engine`-struct reuse relies on `cloneEngineFields` covering every non-hand
  field; the guard tests pass, but the full suite (`TestCloneGenIsUpToDate`, golden
  heads, replay-exact, `make sim`) must run before any production landing.
- 70-pair numbers are noisy; re-measure on a quiet box with interleaved arms.

## Next (not done in the POC window)

- Clone residual arrays still allocate per clone (`Players` + 6 per-player slices
  ≈ 0.5 GB; zones backing ≈ 0.4 GB; `arena.ids/tgs/ctr`; `Stack`/`Entered`; two maps)
  — ~4.5 % of alloc_space; needs a `state` recycle-struct API.
- `resolve.Kernel.Submit` = 19 % of alloc_space, unbroken-down; profile then pool.
- `azmcts` per-simulation `engineEnv` / `botStreams` / `actorBot`; walk candidate
  slabs; `Point`/`keys` (retention-aware — tree keeps `Point`s).
