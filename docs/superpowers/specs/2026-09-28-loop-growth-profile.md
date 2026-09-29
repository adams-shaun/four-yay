# Loop prototype per-iteration growth profile

## Summary

`TestLoopPrototypeMiner`'s per-iteration cost grows with N, but most of the
growth the ticket quoted is an artifact of the rules test binary's
verification instrumentation, not engine behaviour. Two independent causes,
measured:

1. **Test instrumentation (dominant at the quoted numbers).** The rules test
   binary runs every cache hit and fast path in verify mode
   (`rules/*_verify_test.go`). Those re-derivations scale with the game's
   accumulated state, so measured per-iteration time with verify on is
   **2.65 ms at N=20 and 49.0 ms at N=100** (≈18×), matching the ticket.
   With every verify flag off, the same loop is **0.466 ms at N=20 and
   1.111 ms at N=100** (≈2.4×). Verification is ~6× the engine cost at N=20
   and ~44× at N=100.

2. **A real engine growth mechanism.** Even verify-off, the per-priority
   offer walk's cost grows with N because **`Rakdos, the Muscle` adds one
   `UntilYourNextEndStep` `MayPlay` continuous effect per sacrifice**, and
   the loop sacrifices once per iteration. Within a single turn `e.continuous`
   grows to exactly N entries (measured: N=1→1, N=20→20, N=100→100), and the
   exile zone grows with N too. Every priority decision walks *all* of
   `e.active()`'s `MayPlay` effects and, for each, scans the graveyard/exile
   zones (`mayPlayLandIds`/`mayPlaySpellIds` → `effectGrantMatches` →
   `matchesSpec`), so per-decision cost is ≈ O(N effects × N exiled cards).

So the answer to "why does per-iteration cost grow with N" is: **mostly test
verify-mode re-derivation, and underneath it a linear accumulation of
`MayPlay` continuous effects (one per Rakdos sacrifice) that every priority
offer walk rescans per effect.** The loop-shortcut path repeats these lines
as ordinary intents, so at a live table the second cause is paid per
decision; the first is not (live tables do not run verify mode).

## Method

Corpus present at `.cards` (tests did not skip). Commands run with
`GOMAXPROCS=3 GOMEMLIMIT=1GiB` and `-p 1`. All profile files and logs are
under `.ds4/scratch/` (not committed).

A test-only knob was added (`rules/loops_prototype_test.go`):

- `GORGE_LOOP_MINER_N=<list>` selects the Miner subtest Ns (default
  `1,20,100`, byte-for-byte unchanged).
- `GORGE_LOOP_NO_VERIFY=1` turns the rules test binary's 20 verify flags off
  for the selected run, restoring them via `t.Cleanup` (registered before
  `loopBegin`'s cleanup so the fixture-boundary replay also runs verify-off).
  Default unset → no behaviour change. This knob is what separates cause 1
  from cause 2; without it, verify re-derivation dominates every profile.

Profiles were captured at matched `-count=10` for both N (so the per-process
setup, including corpus load, cancels in `pprof -diff_base`), and also at
higher counts for the N=100 sample volume:

```sh
GORGE_LOOP_NO_VERIFY=1 GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=10 \
  -run '^TestLoopPrototypeMiner/20$'  -cpuprofile=.ds4/scratch/m20c10.cpu  -memprofile=.ds4/scratch/m20c10.alloc  ./rules/
GORGE_LOOP_NO_VERIFY=1 GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=10 \
  -run '^TestLoopPrototypeMiner/100$' -cpuprofile=.ds4/scratch/clean100.cpu -memprofile=.ds4/scratch/clean100.alloc ./rules/
GORGE_LOOP_NO_VERIFY=1 GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -v \
  -run '^TestLoopPrototypeMiner/(1|20|100)$' ./rules/
go tool pprof -top -diff_base=.ds4/scratch/m20c10.cpu -nodecount=22 .ds4/scratch/clean100.cpu
go tool pprof -top -diff_base=.ds4/scratch/m20c10.alloc -alloc_space -nodecount=18 .ds4/scratch/clean100.alloc
```

Each run executes the N-iteration mining pass **and** `loopBegin`'s
fixture-boundary replay (a second full pass on a clone), so count is
`N × 2` iterations per test invocation.

### Per-iteration wall time

Verify off (`GORGE_LOOP_NO_VERIFY=1`), one invocation per N:

| N | ms/iteration |
|---|---:|
| 1 | 0.402 |
| 20 | 0.466 |
| 100 | 1.111 |

Verify on (the ticket's numbers, from the pre-existing report): N=20 2.653,
N=100 49.021.

### Growth in the verify-off profiles

`pprof -diff_base` (N=100 count=10 minus N=20 count=10; both have 10 setups,
so setup cancels). Values are the *extra* cost at N=100 relative to N=20
over the extra 800 iterations:

| Function | Flat Δ | Cum Δ |
|---|---:|---:|
| `rules.(*Engine).mayPlayLandIds.func1` | +50 ms | **+840 ms** |
| `rules.(*Engine).effectGrantMatches` | +210 ms | **+720 ms** |
| `rules.(*Engine).matchesSpec` | +90 ms | +490 ms |
| `rules.(*Engine).mayPlaySpellIds` | +110 ms | +400 ms |
| `rules.(*Engine).active` | +50 ms | +300 ms |
| `effects.compiledMatch` | +60 ms | +300 ms |
| `effects.compiledPositive` | +160 ms | +230 ms |
| `rules.(*Engine).activationUsedCount` | +80 ms | +80 ms |

Absolute cumulative CPU for the offer walk (both passes):

| | N=20 (400 iters) | N=100 (2000 iters) |
|---|---:|---:|
| `legalActionsWalkWithWindow` cum | 0.16 s | 1.57 s |
| per iteration | ≈0.40 ms | ≈0.79 ms |
| `mayPlayLandIds` cum | (below cutoff) | 0.86 s |
| `effectGrantMatches` cum | (below cutoff) | 0.72 s |
| `mayPlaySpellIds` cum | (below cutoff) | 0.40 s |

`effectGrantMatches` is reached **only** through `mayPlayLandIds.func1` and
`mayPlaySpellIds` (`-peek` shows 100% of its callers), so the growth is
entirely the may-play offer walk.

Allocations (`-alloc_space` diff, extra bytes at N=100 over N=20):

| Function | Flat Δ | Cum Δ |
|---|---:|---:|
| `state.(*Game).CloneInto` | +957 MB | +958 MB |
| `rules.(*Engine).active` | +558 MB | +562 MB |
| `rules.(*Engine).snapshotTriggerBoard` | +208 MB | +1168 MB |
| `rules.(*Engine).legalActionsWalkWithWindow` | +103 MB | +125 MB |
| `events.growEvents` | +78 MB | +78 MB |

`snapshotTriggerBoard` (`rules/trigger_match.go:869`) is a production path —
SBA/trigger checks call it per event and it does `e.G.Clone()`. A larger
game plus more events means bigger, more frequent clones.

## Growth mechanism, with code references

### 1. A `MayPlay` continuous effect accumulates per iteration

The board is `Rakdos, the Muscle`, `Phyrexian Altar`, `Forsaken Miner`
(`rules/loops_prototype_test.go:695`). Each iteration activates Phyrexian
Altar (sacrificing a creature) and drains the Miner's return. Rakdos'
sacrifice trigger (`.cards/cardsfolder/r/rakdos_the_muscle.txt`):

```
SVar:DBEffect:DB$ Effect | RememberObjects$ RememberedCard | StaticAbilities$ STPlay ... | Duration$ UntilYourNextEndStep
SVar:STPlay:Mode$ Continuous | MayPlay$ True | ... | Affected$ Card.IsRemembered | AffectedZone$ Exile
```

So every sacrifice exiles cards and installs one `ContinuousEffect{
MayPlay:true, Affects:"Card.IsRemembered", AffectedZone:"Exile",
UntilEOT/UntilYourNextEndStep}`. Measured `len(e.continuous)` at loop end:

| N | `e.continuous` | `objs` | `events` | `intents` | exile seat 1 | library seat 1 |
|---:|---:|---:|---:|---:|---:|---:|
| 1 | 1 | 165 | 89 | 13 | 1 | 72 |
| 20 | 20 | 203 | 887 | 184 | 20 | 53 |
| 100 | 100 | 363 | 4220 | 904 | 73 | 0 |

`e.continuous` is exactly N (one new MayPlay effect per sacrifice). It is
`UntilYourNextEndStep`, so it would be pruned at the end of the turn, but the
prototype loop never leaves the turn, so within one turn the list grows
linearly with the number of repetitions.

### 2. Every priority offer walk rescans all MayPlay effects and the zones

`rules/legal.go:71` `mayPlayLandIds` and `rules/legal.go:308`
`mayPlaySpellIds` both do:

```go
for _, ce := range e.active() {            // O(number of MayPlay effects)
    if !ce.MayPlay || ce.Controller != p { continue }
    ...
    for _, z := range []state.Zone{ZGraveyard, ZExile} {
        for _, q := range e.G.AliveFrom(0) {
            for _, id := range e.G.Zone(z, q) {   // O(zone size)
                ...
                if !e.effectGrantMatches(ce, id) { continue }   // matchesSpec
```

`e.active()` (`rules/layers.go:2303`) returns the growing list; per effect
the walk scans every graveyard/exile card and runs `effectGrantMatches`
(`rules/mayplay.go:883`) → `matchesSpec` (`rules/statics.go:465`) per card.
With N MayPlay effects and O(N) exiled cards, each priority decision is
≈ O(N²). In the Miner loop a priority decision is asked on every drain, so
this is the per-iteration cost.

### 3. Larger game → larger per-event trigger snapshots

`rules/trigger_match.go:869` `snapshotTriggerBoard` clones the whole game
(`e.G.Clone()`), and SBA/trigger checks call it per event. More iterations →
more events and a bigger object table → larger clone allocations (the
`CloneInto` +957 MB and `snapshotTriggerBoard` +1168 MB cumulative deltas).

## Proposed follow-up ticket

**Title:** Index `MayPlay` grants so the priority offer walk does not rescan every effect per zone card

**Done means:**
- Add an index from a player's may-play-eligible zones to the active
  `MayPlay` continuous effects that can cover them (or an equivalent
  pre-computed per-decision map), so `mayPlayLandIds`/`mayPlaySpellIds` visit
  each candidate card once rather than once per `MayPlay` effect.
- Prove equivalence: a digest/fixture test that the offer surfaces (option
  sets and their keys) are byte-identical before and after on the repo decks
  plus the Rakdos/Miner board, and no change to event streams, `TestHeads`,
  or deterministic replay.
- Measure the Miner line at N=20 and N=100 with `GORGE_LOOP_NO_VERIFY=1`:
  per-iteration cost at N=100 should fall toward the N=20 figure; no
  regression at N=20.
- Separately (smaller): avoid cloning the whole game per trigger check in
  the common "no trigger cares" case, or make `triggerSnapshot` copy-on-write
  for the fields trigger matching reads.

**Test:** targeted `GORGE_LOOP_NO_VERIFY=1 go test -p 1 -run '^TestLoopPrototypeMiner/(20|100)$' ./rules/` for the timing, plus an offer-surface
equivalence test and the rules behavioral goldens.

## Gates

Required prototype gate (verify flags at their default on):

```text
$ GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run '^TestLoopPrototype' ./rules/
EXIT=0
ok  	github.com/adams-shaun/gorge/rules	15.151s
```

Behaviour goldens:

```text
$ go test ./internal/archtest/
ok  	github.com/adams-shaun/gorge/internal/archtest	2.608s

$ go test -run TestConstructedDefaultIsByteIdentical ./cmd/botbench/
ok  	github.com/adams-shaun/gorge/cmd/botbench	0.666s
```

## Issues

- **Rakdos, the Muscle leaves one `UntilYourNextEndStep` `MayPlay`
  `ContinuousEffect` per sacrifice in `e.continuous`** for the rest of the
  turn. This is rules-correct (the duration is real), but nothing indexes it,
  so the offer walk is O(N²) in repeated-activation turns. This is the
  proposed follow-up ticket above; not fixed here (profile-only task).
- **The rules test binary's verify modes dominate these profiles.** They are
  the intended empirical checks, not defects, but any profiling of `rules`
  tests must disable them (`GORGE_LOOP_NO_VERIFY=1` here) or the numbers
  describe the verifier, not the engine.
- `snapshotTriggerBoard` clones the entire game per event-check
  (`rules/trigger_match.go:869`); on large boards this is a real allocation
  growth with event count. Named for the follow-up, not fixed.
- No head/ratchet files were changed; the test diff is additive and defaults
  to the historical N set and verify flags.
