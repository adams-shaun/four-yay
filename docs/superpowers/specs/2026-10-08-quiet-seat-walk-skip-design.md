# Quiet-seat proof: skip the priority walk when a seat provably can only pass

- **Date:** 2026-10-08
- **Base:** `main` at `58c014f2d`. Every `path:line` below was read at this SHA. Re-check a citation before editing near it: `rules/` moves daily.
- **Subject:** `rules.(*Engine).priorityOptions` (`rules/prio_memo.go:328`), the only posed-priority caller of the legal-action walk (`askPriority`, `rules/turn.go:1043-1066`).
- **Status:** design and binding step plan (§6). No production code changed. The measurements came from a throwaway probe (§7), archived at `/mnt/sata/gorge-training/enginebench/quietseat-plan/`.
- **Related:**
  - `2026-10-06-legal-walk-design.md`: S0–S4 have landed (S3 is `39f59ef78`, S4 is `eb31d3f1a`). This design does not replace or depend on S5 (§8).
  - `2026-10-08-engine-mark-rewind-design.md`: tabled. Clone is about 3% of azmcts CPU; Submit is the target.

## 0. Summary

**Where the time goes.** On the azmcts row, `Engine.Submit` is 83.5% of CPU. The posed-priority walk (`priorityOptions` → `legalActionsWalkWithWindow`) is **35.8% of all CPU**, the largest single subtree. The profile is at `/mnt/sata/gorge-training/enginebench/quietseat-plan/az.pprof`; the binary is beside it. It was made with `enginebench -row az -sims 100 -secs 45`, `GOMAXPROCS=2`.

**The opportunity.**
- 60–61% of posed priority windows return only `pass` + `concede`, yet the walk still runs in full there: 2.4–3.8 µs per window, depending on box load.
- The 10-06 design showed two dead ends:
  - reusing a whole walk's result needs a key nearly as costly as the walk;
  - per-object castability facts depend on effectively the whole state.
- This design proves something weaker and cheaper: **an over-approximating proof that a seat has nothing to offer.**
  - It never computes an option; it only shows that none can exist.
  - Any window it cannot prove runs the walk exactly as today, so the offered option lists cannot change.

**Measured ceiling** (probe, §7; 4.1M posed windows, enginebench pairs A/B):

| | |
|---|---:|
| windows the proof covers | **58.1%** of all windows, **84.0%** of pass-only windows |
| walk time inside proved windows | **37.7%** of walk time ≈ **13% of total CPU** |
| proofs where the walk offered a cast or a non-mana ability | **0** of 2.40M |
| proved windows that also listed bare mana taps | 6.6% of windows (§3.3 serves them) |
| naive proof cost (map and string reads, no caching) | ~0.95 µs/window, against 3.8 µs for the walk on the same run |

**Plan** (§6 is binding). Q2 is where the speedup lands.

| step | what | gate | ticket |
|---|---|---|---|
| Q1 | per-face quiet facts; the `seatQuiet(p)` proof; verify mode; `-quietstats`; bench tooling for the az row. **The proof is never consulted** | 0 verify mismatches over the §4.2 sweep; coverage within ±3 points of §7 | 1 |
| Q2 | `priorityOptions` serves proved windows: mana section + post-walk tail + pass/concede | digests byte-identical; az row ≥ +8% sims/s | 2 (after Q1) |
| Q3a | floor for graveyard/exile recast costs (Flashback etc.) | coverage up, 0 mismatches | 3 (after Q2) |
| Q3b | bounds for non-mana cost parts (Sac/Discard/SubCounter/…) | coverage up, 0 mismatches | 4 (after Q2) |
| Q3c | scope-narrowed AddAbility/MayPlay/grant statics | coverage up, 0 mismatches | 5 (after Q2) |
| Q4 | incremental per-seat counts | only if the Q2 proof costs more than 15% of the walk time it saves | not queued |

## 1. Census

### 1.1 Profile (azmcts row, 45 s, 2 cores, honest redeal)

| path | % of CPU |
|---|---:|
| `Submit` | 83.5 |
| ├ `Advance` → `priorityRound` → `askPriority` → `priorityOptions` | **35.8** |
| │ ├ `handWalk` | 22.1 (`offerCastable` 9.5, `castRestricted` 3.3, `spellTimingOK` 1.5) |
| │ └ `battlefieldWalk` | 10.5 (mana gates ~2.5, ability loop) |
| ├ `handle` → `handlePriority` | 23.1 |
| ├ `emit` (crosses both of the above) | 20.2 |
| ├ combat steps and asks | ~5.7 |
| └ `checkStateBased` | ~3.9 |

No function has more than 2.5% of CPU in its own body. The gain has to come from calls not made.

### 1.2 What the walk is made of

The section order is fixed (`rules/legal.go:179-187`):
1. `handWalk` (`rules/legal_walk_hand.go:11`)
2. `mayPlayLandWalk` (`rules/legal_walk_alt.go:10`)
3. `mayhemLandWalk` (`:30`)
4. `mayPlaySpellWalk` (`:60`)
5. `plotZoneWalk` (`rules/legal_plotzone_walk.go:14`)
6. `commandZoneWalk` (`rules/legal_walk_alt.go:223`)
7. `graveyardCastsWalk` (`:329`)
8. `exileCastsWalk` (`:621`)
9. `battlefieldWalk` (`rules/legal_walk_battlefield.go:18`). It contains:
   - the **mana section**: `:49-150`, recorded by `recordManaSection`, reused by `reuseManaSection` (`rules/walk_block_reuse.go:168-178`);
   - the **activated-ability loop** over battlefield, stack, graveyard, hand and exile, for every seat's zones (`:169`);
   - **granted abilities**, **station**, **room unlock**, **morph turn face up** and **specialize**.

Then come the post-walk filters (`rules/legal.go:206-212`): `filterNoManaCostCasts`, `filterInertHeldOut`, and `filterSplitSecondActions` when split second holds. Finally `pass` and `concede` are appended (`:213-224`).

**Why bare taps appear.** The mana section offers an `activate` for every untapped source the seat may activate, whether or not anything could spend the mana. So a window with nothing castable still lists bare taps when the seat has an untapped land. That is the 6.6% bare-mana class. Pass-only windows are the ones where the seat also has no untapped source.

## 2. The proof

### 2.1 Contract

`(*Engine).seatQuiet(p state.PlayerID) bool`, evaluated at a posed priority window (§3.2's preconditions):

> **true ⇒** `legalActionsWalkWithWindow(p, nil, false, nil, true, …)` would produce no option other than the mana section's `activate` options. Pass and concede are not part of the claim.

`false` is always safe. The proof is a conjunction: it returns false as soon as any **blocker** (§2.3) fires. Blockers are deliberately coarse. Each one over-approximates a walk section's "may offer"; it never tries to decide whether the offer is real.

### 2.2 Inputs

**Per window:**
- **`sorceryOpen`:** `p == G.Active && G.Step.IsMain() && len(G.Stack) == 0`. This is the walk's sorcery-timing class. Re-check it against `w.sorcery`'s derivation in `legalActionsWalkWithWindow` before relying on it.
- **Mana ceiling:**
  - `Σ Players[p].Pool[i]` over all slots, plus `RestrictedMana` units (count them, or fail closed if the seat has any);
  - plus, for every object in `Zone(ZBattlefield, p)` with `Controller == p`, not `Tapped`, not `PhasedOut`, whose face has mana facts: the face's `manaMax` (§3.1);
  - if any counted source has `manaIndeterminate`, the ceiling is `+∞`;
  - summoning sickness is ignored: an over-count is safe;
  - mana abilities on non-battlefield objects (Spirit Guides in hand, Jack-o'-Lantern in the graveyard) and other seats' `Activator$` mana sources do **not** raise the ceiling in v1. If verify trips on one, add it (§4.3).
- **Board flags.** Read once per window. Prefer `walkBoardFacts` (`rules/legal_walk_skip.go:39-61`) and the walk's action-statics snapshot. Q1 may have to compute them outside a walk: see `boardFacts`' `activeDepth` caveat and `activeSummaryOf(e.active())`. Any one of these is a blocker:
  1. `hasGrants` or `addAbility`;
  2. any active `CastWithFlash` static (the S2 bit `flashBoard`);
  3. any active `MayPlay` grant (static or effect-registered), i.e. `len(e.mayPlaySpellIds(p)) != 0` (`rules/legal_mayplay.go:253`), or a may-play-land grant (`mayPlayLandWalk`'s source);
  4. any `ReduceCost` or `SetCost` cost static that is not inert for every card (reuse `costStaticsInertFor` from S1: a self-scoped static on another object is inert);
  5. `kwMaybe`, an active AddKeyword head in `grantedKWHeads` (`rules/legal_walk_skip.go:35`), or any active AddKeyword granting Flash.

  The split-second filter only removes options, so it is not a blocker.

### 2.3 Blockers per walk section

| walk section | blocker ("may offer") |
|---|---|
| `handWalk`, lands | a face with `isLand` in hand, `sorceryOpen`, and `Players[p].LandsPlayed < 1 + adjustLandPlays(p)` (`rules/legal_mayplay.go:424`). Also check `mayhemLandWalk`'s gate and `playLandForbidden`'s callers in `legal_walk_hand.go:69` for any other land path |
| `handWalk`, spells | a hand card with a face (front and every alternate face the walk probes: split, adventure, MDFC back, aftermath, room, omen) that is non-land, whose timing is open (`instantSpeed` or `sorceryOpen`), and where `castOpen` holds or `castFloor ≤ ceiling` |
| `handWalk`, hand abilities | a hand card whose face has `abZones` bit `ZHand` (cycling, channel, ninjutsu) and the §2.4 ability test passes |
| `mayPlayLandWalk`, `mayPlaySpellWalk`, `mayhemLandWalk` | board flag 3, plus `mayhemLandWalk`'s own trigger (discarded-this-turn). Read its gate and add a per-seat check |
| `plotZoneWalk`, `exileCastsWalk` | an object in `Zone(ZExile, p)` with any of: `PlottedTurn != 0`, `SuspendGranted`, `CastFlags&FlagForetold != 0`, a prepared copy (`IsCopy && PreparedSource != 0`), `Card.AlternateMode == "Adventure"`, or `exileCastKW` (Warp/Foretell/Plot/Suspend). Also: any airbend-recast candidate (the incremental airbend index, `rules/airbend.go`) |
| `commandZoneWalk` | any object in `Zone(ZCommand, p)` |
| `graveyardCastsWalk` | an object in `Zone(ZGraveyard, p)` whose face has `recastKW` (Flashback, Escape, Unearth, Disturb, Jump-start, Retrace, Embalm, Eternalize, Scavenge, Encore, Harmonize, Mayhem, Aftermath). Q3a puts a floor on this |
| `battlefieldWalk`, ability loop | for each zone z in {Battlefield, Stack, Graveyard, Hand, Exile} and each seat's `Zone(z, q)`, every object `o` with face facts: let `mask = abZones` if `controllerOf(o) == p`, else `abZonesActivator`. If `mask` has bit z, apply the §2.4 ability test. **Any stack-zone hit is a blocker outright** |
| `battlefieldWalk`, granted abilities | board flag 1 (`hasGrants`) |
| `battlefieldWalk`, station / unlock / face up / specialize | an own battlefield object that is face-down, a Room with a locked door, or a face carrying Station or Specialize. Read each section's gate and add the cheapest exact-or-coarser bit |
| `battlefieldWalk`, mana section | **never a blocker.** §3.3 serves it |

**Fail closed.** An object whose face has no facts (`walkFaceFactsOf` nil), a merged pile (`len(MergedCards) != 0`) or a `FaceDown` object counts as a blocker in every section that would visit it.

### 2.4 The ability test (all zones)

Per face, per zone z, §3.1 precomputes an `abQuiet[z]` summary over that face's non-mana AB abilities with `ActivationZone` admitting z:

```go
type abQuietZone struct {
    any        bool  // some ability admits z
    nonMana    bool  // some admitting ability's cost has a part the bound cannot price
    floorAny   int32 // min mana floor over admitting abilities with no {T}
    floorTap   int32 // min mana floor over admitting abilities that need {T}
    hasAny     bool  // floorAny is set
    hasTap     bool  // floorTap is set
    sorcAny    bool  // every no-{T} admitting ability is SorcerySpeed$ True
    sorcTap    bool  // every {T} admitting ability is SorcerySpeed$ True
}
```

**Blocker** when `any` and one of:
1. `nonMana`;
2. `hasAny` and `floorAny ≤ ceiling` and (`!sorcAny` or `sorceryOpen`);
3. `hasTap` and the object is untapped and `floorTap ≤ ceiling` and (`!sorcTap` or `sorceryOpen`).

**Mana floor of a compiled cost** (`pay.CompiledCost` / `cost.Cost`, `rules/cost/cost.go:130`):
- floor = `Generic + Σ Colored + len(Hybrid) + len(Twobrid) + Snow`, with `X = 0`.
- **`nonMana` is set** when any of these is non-zero or non-empty: `Life`, `Phyrexian`, `HybridPhyrexian`, `XMin`, `Waterbend`/`WaterbendX`, `Untap`, `Sac`, `Discard`, `SubCounter`, `AddCounter`, `Exile`, `ExileFromTop`, `Reveal`, `RevealOrChoose`, `RevealChosen`, `Behold`, `TapPermanent`, or **any `cost.Cost` field not listed here**. Q1 adds a test that fails when `cost.Cost` gains a field the classifier doesn't name (reflect over the struct fields against an explicit allowlist).

**Q1 deliberately ignores these:** `Activator$` restrictions beyond the mask, `PlayerTurn$`, activation limits, `IsPresent$` / `CheckSVar$` / `Condition*` gates, summoning sickness, and target availability. Each of them can only make an ability unavailable, so leaving them out stays sound and costs only coverage.

### 2.5 The spell test

**Per face** (§3.1): `castFloor`, `castOpen`, `instantSpeed`, `isLand`.
- `instantSpeed` = Instant type, or printed `Flash`. A conditional flash such as `MayFlashSac` or Teamwork is **castOpen**. Read `spellTimingOK` and `teamworkFlashOffer` to get the list exactly.
- `castFloor` = the face's mana value with X = 0 (`Face.Cmc()`). If the face prints a self `ReduceCost` static, it is the coloured pip count instead, since generic reductions cannot reduce pips. A `ReduceCost` that names colours or whose amount is not generic-only makes the face **castOpen**.
- **`castOpen` is set** when any of these holds:
  - `walkFaceFacts.altCosts` is non-empty (the alternative-cost family);
  - any of these keywords: Convoke, Delve, Improvise, Affinity, Emerge, Evoke, Surge, Spectacle, Prowl, Madness, Ninjutsu, Dash, Bargain, Offspring, Blitz, Sneak, WebSlinging, Kicker (only when a kicker could reduce or replace the cost);
  - a self `AlternativeCost` / `SetCost` static;
  - Phyrexian or hybrid-Phyrexian pips;
  - a no-mana-cost face;
  - any face the walk prices through `alternativeCosts`, `blitzCosts`, `sneakCosts`, `webSlingingCosts`, `teamworkOffer`, `hasCastConspire` or `hasCastOffspring`.

  The rule for anything unclassified: **when in doubt, castOpen.** Q1's job is a classifier that is exact for the plain case and open for everything else.

**Blocker** when the face is non-land, its timing is open, and (`castOpen` or `castFloor ≤ ceiling`).

## 3. Mechanism

### 3.1 Per-face quiet facts (Q1)

Add a `quiet quietFaceFacts` block to `walkFaceFacts` (`rules/walk_face_facts.go:24`). Build it in the same constructor as the existing facts (`compiledText.faceFacts`), under the same `currentFor` guards (`abFirst`/`abLen`, `kwFirst`/`kwLen`), so a replaced ability or keyword list misses and recomputes:

```go
type quietFaceFacts struct {
    isLand, instantSpeed, castOpen bool
    castFloor                      int32
    recastKW, exileCastKW          bool
    manaMax                        int32 // max units one activation of any printed mana ability yields
    manaIndeterminate              bool
    abQuiet                        [quietZones]abQuietZone // Battlefield, Stack, Graveyard, Hand, Exile
}
```

- `manaMax` comes from `Face.ManaProduction()` (`cards/mana_production.go:30,243`): the sum of `Colour[]`. A non-zero `Any` with an empty `Colour` counts 1; `Indeterminate` sets `manaIndeterminate`. A face that prints two or more mana abilities takes the **sum** over abilities, which overcounts. Verify that `ManaProduction` is per-ability-max rather than a sum before choosing.
- Costs are classified from the compiled cost the walk already uses (`compiledCostOf`, `rules/compiled_text.go:598`). No `Params` map or string is read at proof time.

The proof then reads per-object live fields only:
- `Tapped`, `PhasedOut`, `Zone`, `Controller`, `Owner`, `FaceDown`, `MergedCards`;
- `PlottedTurn`, `SuspendGranted`, `CastFlags`, `IsCopy`, `PreparedSource`;
- `Card.AlternateMode`, `Card.Faces` (for hand cards' alternate faces).

**Zero allocation, no maps.**

### 3.2 Hook (Q2)

In `priorityOptions` (`rules/prio_memo.go:328`), before the memo:

```go
if !quietOff && window == nil && e.quietUsable() && e.seatQuiet(p) {
    quietHits.Add(1) // only under the stats flag; see Q1
    return e.quietOptions(p)
}
```

`quietUsable` is `prioMemoUsable`'s precondition list (`rules/prio_memo.go:114`) minus the memo-only items:
- the engine fields: no `hostAsking`, no `cast`, not `Suspended()`, no `offStackMana`, no `resolvingObj`, no `applyingReplacement`, `activeDepth == 0`, no tape watching;
- and **the recorder is not armed**: `!(e.potentialFullDemand && e.WalkRecDemand)` (`rules/legal.go:176-177`).

An empty `inertHeldOut` is **not** required, because the tail filter is applied to the quiet result too.

### 3.3 Serving the mana section (Q2)

`quietOptions(p)` must return exactly what the walk would:
1. **The mana section.** Extract `battlefieldWalk`'s mana block (`rules/legal_walk_battlefield.go:38-150`: `boardFacts`, `walkClassesCatchUp`, the mana loop, `recordManaSection`) into `func (w *legalWalk) manaSection()`. `battlefieldWalk` must call the extracted function, so the walk itself goes through it and there is one copy. `quietOptions` builds a `legalWalk` the way `legalActionsWalkWithWindow` does (`rules/legal.go:125-177`), with `forAsk = true` and the result going into the decision arena, and calls only `manaSection()`.
2. **The post-walk tail.** Factor `rules/legal.go:206-224` (filters, then `pass`, then `concede`, then the `Index` reassignment and the arena handling) into one function that both paths call.
3. **The per-walk diagnostics** the walk maintains: `legalActionWalks`, the high-water marks, anything `notePriorityWalk` (`rules/potential_walk_cache.go:383`) reads after the ask. Read `legalActionsWalkWithWindow` top to bottom and mirror each side effect, or show it doesn't matter for a `forAsk` priority result.

**Byte-identity is the acceptance test.** `TestLegalWalkDigest` with the quiet path on must equal main.

### 3.4 Flags

- **`quietOff`:** `os.Getenv("GORGE_QUIET_SKIP") == "0"` once Q2 flips the default. Before that, the path is on only when `GORGE_QUIET_SKIP=1`. This is the prio memo's flag pattern (`rules/prio_memo.go:81`).
- **`quietVerify`:** `derivedMemoVerifyFlag != ""` (`rules/derivedmemo.go:166`; set by `make enginebench-verify` and the rules test binary), or `GORGE_QUIET_VERIFY=1`.
- **`quietStats`:** counters (proved, pass-only, bare-mana, sole blocker, ns) behind a link-time flag `-X github.com/adams-shaun/gorge/rules.quietStatsFlag=1`, so the default build pays one predictable branch. This is S0's pattern.

## 4. Correctness

### 4.1 Verify mode

With `quietVerify` set, **every** call to `seatQuiet(p)` that returns true also runs the full walk:
- in Q1, always, since nothing is consulted;
- in Q2, on every served window.

It compares the walk's non-mana options with the proof's claim. In Q2 it also compares the whole list with `quietOptions`, field by field, including `Index`, `Kind`, `Label`, `Obj` and `Cost`. On a mismatch it panics with: `rules: quiet proof wrong for seat %d (turn %d step %v stack %d): walk offered %s %q obj %d (%s)`, ending with the card name.

### 4.2 Required sweep (Q1 gate, re-run in Q2)

All of these, under the CAP in §6.0, with verify on:
1. `TestLegalWalkDigest` (`rules/legal_walk_bench_test.go:114-120`, opt-in via `LEGAL_WALK_DIGEST=`). It plays every repo deck; with the rules test binary, verify is on.
2. `TestHeads$` and the golden replay tests.
3. `make enginebench-verify REV=.` (rows sampler, random A/B, bot A). With Q1's `BUILD_ARGS` support, also `ROWS="-row az -sims 100"` and `BUILD_ARGS="-tags enginebench_az"`.
4. `make sim` (`mtgsim -seats 4 -games 20 -verify`; four seats exercise multiplayer `Activator$` and grants).
5. A new `TestQuietProofCorpusSweep`. It builds one battlefield, hand and graveyard object per corpus card face that has facts. For every seat and every timing class, it runs `seatQuiet` and the full walk, and asserts the contract. It needs per-test budget care: shard it by card index, or sample deterministically with a seed. It must fit the 2 GB / 1 min test budget.

### 4.3 When verify trips

Each mismatch either becomes a new blocker or tightens an existing one, and gets a row in `rules/quiet_proof_test.go` naming the card that tripped it.

**Never** weaken verify, and never special-case a card name in production code.

### 4.4 Section-coverage guard

`TestQuietCoversWalkSections` parses `rules/legal.go` (go/ast) and lists the method calls in `legalActionsWalkWithWindow` and `battlefieldWalk`. It asserts that each one appears in a `quietCoveredSections` table in `rules/quiet_proof.go`, and fails on any unlisted section. A future walk section therefore cannot silently bypass the proof.

## 5. Targets

| metric | target | measured by |
|---|---|---|
| proof cost | ≤ 300 ns/window (Q1), on the az row | `-quietstats` ns |
| coverage | ≥ 80% of pass-only (Q1/Q2); ≥ 92% (after Q3) | `-quietstats` |
| soundness | 0 verify mismatches over §4.2 | verify |
| offered options | byte-identical (`TestLegalWalkDigest` vs main) | digest |
| azmcts throughput | ≥ +8% sims/s (Q2), interleaved A/B, REPS ≥ 3 | `make enginebench-pair` with the az row |
| other rows | not slower than noise (random A/B, bot A, sampler) | `make enginebench-pair` default rows |

## 6. Step plan (binding)

### 6.0 Shared rules for every step

**CAP:**
```sh
systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m
```
- Focused tests only: never `go test ./...`, never `-count=1`.
- Always also run `./view`:
  ```sh
  <CAP> -run 'TestPotentialActionsGatedToViewerSeat|TestDecisionOption|TestLiveViewPreservesPriority' ./view
  ```

**Digest:**
```sh
LEGAL_WALK_DIGEST=/tmp/<id>-base.txt <CAP> -run 'TestLegalWalkDigest$' ./rules   # in a main worktree
LEGAL_WALK_DIGEST=/tmp/<id>-cand.txt <CAP> -run 'TestLegalWalkDigest$' ./rules   # on the branch
cmp /tmp/<id>-base.txt /tmp/<id>-cand.txt
```
Run it once with `GORGE_QUIET_SKIP=1` and once without.

**Benches:**
- `make enginebench-verify|profile|pair`. Do **not** set `HEAVY_LOCK`; enginebench uses its own default lock.
- The az row needs `-tags enginebench_az`.

**Invariants:**
- Offered option lists stay byte-identical.
- Heads (`rules/testdata/heads/*.txt`) are unchanged.
- Determinism.
- No maps and no allocation on the proof path.
- Every skip has a verify arm.

**`rules/` files are hot.** New logic goes in new files: `rules/quiet_proof.go`, `rules/quiet_facts.go`, `rules/quiet_serve.go`, `rules/quiet_stats.go`. Edits to existing files are limited to the hook lines named per step.

**Commits:** no Co-Authored-By or attribution trailers.

### Q1. Quiet facts, the proof, verify, stats — never consulted

- **What:**
  1. `rules/quiet_facts.go`:
     - `quietFaceFacts` and `abQuietZone` (§3.1, §2.4);
     - the cost classifier `quietCostFloor(c *cost.Cost) (floor int32, tap, nonMana bool)`;
     - the spell classifier `(§2.5)`.
     
     Wire the build into `walkFaceFacts` construction. Allowed edit: the facts constructor in `rules/walk_face_facts.go` / `rules/compiled_text.go`, adding one field and one call.
  2. `rules/quiet_proof.go`:
     - `seatQuiet(p) bool`;
     - `quietBlocker(p) quietBlockerID`, the first blocker hit (an enum, for stats; same logic, so implement `seatQuiet` as `quietBlocker(p) == qbNone`);
     - the board-flag reader;
     - `quietCoveredSections`.
  3. `rules/quiet_stats.go`:
     - `quietStatsFlag` (link-time);
     - atomic counters: windows, proved, proved-pass-only, proved-bare-mana, pass-only-unproved by sole blocker, proof ns, walk ns;
     - an exported `QuietStats()` snapshot.
  4. The verify arm in `priorityOptions`:
     - **Allowed edit:** `rules/prio_memo.go`, at most 6 lines at the top of `priorityOptions`. When `quietVerify || quietStatsFlag != ""`, run `quietBlocker(p)` alongside the normal path and compare with the normal path's result (do not run a second walk; the normal path's result is the walk).
     - The normal path's return value is unchanged.
  5. `cmd/enginebench/quietstats.go`:
     - a `-quietstats` flag, registered in `cmd/enginebench/main.go` beside `-walkstats` (`:110`), flag line only;
     - it builds with the stats ldflag (document that `-quietstats` needs `-ldflags "-X …quietStatsFlag=1"`, or have it error when the flag isn't linked);
     - it prints the §7-style tables at exit on every row, including az (`cmd/enginebench/search.go:runSearch`; one hook line).
  6. `scripts/enginebench-build.sh` callers: let `scripts/enginebench-pair.sh` and `scripts/enginebench-verify.sh` pass `BUILD_ARGS` (env) through to `enginebench-build.sh`, so `BUILD_ARGS="-tags enginebench_az" ROWS="-row az -sims 100"` works. The build-cache suffix already hashes the args.
  7. Tests:
     - `rules/quiet_proof_test.go`: one table row per blocker in §2.3/§2.4/§2.5, each asserting the proof is true in a quiet fixture and false with the one ingredient added. Include Plains-only (proof true, bare mana in walk), a Spirit Guide in hand, an instant with Flash at cost 1 against 1 untapped land, a flashback card, a plotted card, a foretold card, a cycling card in hand, an equipment with Equip {1}, an activated ability with Sac cost, `adjustLandPlays`, and a may-play grant from a resolved effect.
     - `TestQuietCostClassifierCoversCostFields` (the reflect allowlist).
     - `TestQuietCoversWalkSections` (§4.4).
     - `TestQuietProofCorpusSweep` (§4.2 item 5).
- **Why exact:** nothing consumes the proof yet, so behaviour is unchanged by construction. The verify arm is a pure read.
- **Files:** new `rules/quiet_facts.go`, `rules/quiet_proof.go`, `rules/quiet_stats.go`, `rules/quiet_proof_test.go`, `cmd/enginebench/quietstats.go`. Hook lines in `rules/prio_memo.go`, `rules/walk_face_facts.go` (or wherever the facts constructor lives), `cmd/enginebench/main.go` and `cmd/enginebench/search.go`. `BUILD_ARGS` in `scripts/enginebench-{pair,verify}.sh`.
- **Done means:**
  - Focused tests green under CAP:
    ```sh
    <CAP> -run 'TestQuiet|TestHeads$|TestLegalWalk|TestGhaltaSelfReductionFromHand|TestPotentialWalkSharedAcrossReaders' ./rules
    ```
    plus `./view`, plus `go vet ./rules ./cmd/enginebench`.
  - The digest is byte-identical against main.
  - `make enginebench-verify REV=.` is clean, and so is `BUILD_ARGS="-tags enginebench_az" ROWS="-row az -sims 100" make enginebench-verify REV=.`.
  - `make sim` is clean.
  - A `-quietstats` az run (≥ 30 s) reports coverage within ±3 points of §7 v3 (84% of pass-only) **or** explains the difference, and proof ns ≤ 300 at the run's load. Paste the table in the report.
- **Gate:** none on throughput (nothing is consumed). The verify sweep is the gate.
- **Kill:** if the verify sweep produces a mismatch class that can't be closed without making the proof a second walk, i.e. the coverage after fixing it falls below 60% of pass-only. Report the class and stop.

### Q2. Serve proved windows

- **Depends on:** Q1 merged.
- **What:**
  1. Extract `manaSection()` and the post-walk tail (§3.3) with **no behaviour change**. This is the only permitted edit to `rules/legal_walk_battlefield.go` and `rules/legal.go`: move code into named functions and call them from the original sites. Prove it with the digest before step 2.
  2. `rules/quiet_serve.go`: `quietUsable`, `quietOptions` (§3.2, §3.3).
  3. Hook in `priorityOptions` (§3.2), behind `GORGE_QUIET_SKIP=1`.
  4. Verify (§4.1): under `quietVerify`, a served window also runs the walk and compares the **whole** list.
  5. After the gates pass, **flip the default** to on (`GORGE_QUIET_SKIP=0` disables) in the same PR, as a separate commit.
- **Why exact:** a proved window's walk output is the mana section plus the tail (contract §2.1 plus §1.2); `quietOptions` runs the same code for both parts, and verify checks the whole list on every served window in the test binary and in `enginebench-verify`.
- **Files:** `rules/legal_walk_battlefield.go` and `rules/legal.go` (extraction only), new `rules/quiet_serve.go`, hook lines in `rules/prio_memo.go`, tests in `rules/quiet_serve_test.go`.
- **Done means:**
  - Q1's focused set green, plus `-run 'TestQuietServe'`.
  - The digest is byte-identical against main with the quiet path on **and** off.
  - `make enginebench-verify` is clean on the default rows and the az row.
  - `make sim` is clean.
  - Paired bench:
    ```sh
    BUILD_ARGS="-tags enginebench_az" ROWS="-row az -sims 100" REPS=3 make enginebench-pair BASE=main CAND=.
    ```
    shows ≥ +8% sims/s median. `make enginebench-pair BASE=main CAND=. REPS=3` on the default rows shows no regression beyond noise.
  - `make enginebench-profile` on the az row shows `priorityOptions` cum down by ≥ 25% of its base share. Report base and candidate cum.
- **Gate:** the az-row pair above. The box is shared; if load is above 20, rerun or report both runs.
- **Kill:** az gain < +4% median over two pair runs, or any digest difference that is not an extraction bug.

### Q3a. Floor for recast costs

- **Depends on:** Q2 merged.
- **What:** replace the coarse `recastKW` blocker with a per-face `recastFloor` and `recastOpen`. Flashback, Escape, Unearth and the rest take their cost from the keyword parameter; reuse the compiled parse the walk uses (`altCosts` / `altCastMode.faceCost`, or the graveyard walk's own cost reader). Escape's exile-N part, Delve-like parts and similar make it **open**. The blocker fires when the recast's timing is open and (`recastOpen` or `recastFloor ≤ ceiling`).
- **Done means:** Q1's tests plus new rows (Flashback above the ceiling: quiet; at the ceiling: blocked; Escape: blocked). Digest identical, verify clean, `-quietstats` shows the graveyard Flashback sole-blocker share dropping. Report before and after.
- **Kill:** sole-blocker share before the change < 3% on the az row.

### Q3b. Non-mana cost bounds

- **Depends on:** Q2 merged.
- **What:** turn `nonMana` from a blocker into bounded checks. Per cost part kind, a necessary condition the proof can evaluate cheaply:
  - `Sac` needs ≥ N permanents p controls matching the part's type. A type mask is fine; a bare count of p's battlefield is the coarse version.
  - `Discard` needs ≥ N cards in hand (excluding the source, if it's in hand).
  - `SubCounter` needs the counter on the source.
  - `Life` needs ≥ N life.
  - `Exile`-from-graveyard needs ≥ N cards in p's graveyard.
  - `TapPermanent` needs ≥ N untapped matching permanents.

  Anything else stays `nonMana`. Store the parts per face in `abQuietZone` as a small fixed array; overflow is `nonMana`.
- **Done means:** a test row per part kind, both satisfiable and not. Digest identical, verify clean, `-quietstats` before and after.
- **Kill:** "battlefield ability with a non-mana cost" sole-blocker share < 5% before the change.

### Q3c. Scope-narrowed grant statics

- **Depends on:** Q2 merged.
- **What:** board flags 1, 3 and 5 become per-seat. A `Continuous` `AddAbility$`/`AddKeyword$`/`MayPlay$` static whose `Affected$` cannot match any object p controls or owns in the relevant zones is not a blocker for p. Reuse the matcher the grants walk uses; when it can't decide cheaply, stay blocked.
- **Done means:** test rows (opponent's anthem with AddKeyword Flying: not a blocker for p; an AddAbility granting a tap ability to p's creatures: a blocker). Digest identical, verify clean, `-quietstats` before and after.
- **Kill:** sole-blocker share < 3% before the change.

### Q4. Incremental per-seat counts (not queued)

Only if Q2's `-quietstats` shows proof ns > 15% of the walk ns saved. If so, per-seat counts (open-timing hand floors, ability minimum floors by zone, recast and exile markers) are updated in `walkClassTouch` (`rules/walk_objclass.go:359`) from the per-object touch that `staticZonesCatchUp` (`rules/static_zoneskip.go:191`) already runs, and carried by clone like `walkObjCls`.

## 7. Probe evidence

- **Source:** `/mnt/sata/gorge-training/enginebench/quietseat-plan/probe-src/zz_walkprobe.go`, plus `hooks.diff`, applied to a `git archive 58c014f2d` copy.
- **What it does:** at every posed window it runs the real walk and a naive proof side by side and tallies the outcomes. It reads `Params` maps and strings, so its ns figures are pessimistic for the proof. It is a measurement aid, not a template for production code.
- **Command:** run from the repo root:
  ```sh
  systemd-run --user --scope -q -p MemoryMax=3G env GOMAXPROCS=2 enginebench-probe -row az -sims 100 -secs 45
  ```
- **Load:** about 23 on the first run, lower later. The percentages were stable across runs; the ns were not.

| probe revision | windows | pass-only | proved (% all / % pass-only) | walk-time share proved | unsound (non-mana) |
|---|---:|---:|---:|---:|---:|
| v1: hand-ability blocker ignored activation zone | 2.80M | 60.5% | 3.1% / 4.6% | 1.7% | 0 |
| v2: zone-aware abilities with cost floors | 2.64M | 60.4% | 52.6% / 76.8% | 32.7% | 0 |
| v3: land plays remaining, exile markers, may-play grants | 4.13M | 61.4% | 58.1% / 84.0% | 37.7% | 0 |

**v3 sole blockers** on unproved pass-only windows:

| blocker | share |
|---|---:|
| battlefield ability, non-mana cost | 39.4% |
| AddAbility/MayPlay static | 13.2% |
| graveyard Flashback | 12.9% |
| affordable battlefield ability | 12.2% |
| affordable instant | 11.1% |
| ReduceCost static | 2.7% |
| exile Flashback | 1.8% |
| affordable sorcery | 1.6% |

All 271k v3 "unsound" proofs offered only bare `activate`s, mostly basic lands. That is §1.2's bare-mana class, which §3.3 serves.

**Naive proof cost:** 947 ns/window against 3,777 ns/window for the walk in proved windows (25 s run).

## 8. Not this design

- **S5, timing-only whole-walk replay** (10-06 §S5): it replays a list across StepChanges. It is orthogonal and stays gated there.
- **azmcts `SkipPass`** (`rules/skip_pass.go`): a search-only policy that auto-passes windows without proof, which changes the decision stream. Quiet serving changes nothing offered and runs in live play too.
- **The prio memo** (`rules/prio_memo.go`): off by default; 12% hit rate, net neutral. The quiet hook sits before it and does not depend on it.

## 9. Risks

| risk | mitigation |
|---|---|
| a section or grant the blocker table misses, so a card is wrongly suppressed | verify on every proof (§4.1); the corpus sweep (§4.2.5); fail-closed defaults; Q1 lands unconsulted |
| a walk section added later with no blocker | `TestQuietCoversWalkSections` (§4.4) |
| a `cost.Cost` field added later is read as free | `TestQuietCostClassifierCoversCostFields` |
| the extraction changes the walk | digest before and after Q2 step 1, as its own commit |
| recorder-armed hosts lose PotentialActions reuse | Q2 serves only with the recorder unarmed |
| proof ns eats the win on big boards | `-quietstats` ns; Q4 is the fallback |
| face facts read stale after a face edit | the same `currentFor` guards as the existing facts; verify recomputes |
