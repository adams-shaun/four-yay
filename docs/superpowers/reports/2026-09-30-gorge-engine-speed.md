# gorge engine speed on draft-zero's docs/015 metrics (2026-09-30)

I re-measured gorge on the speed rows of draft-zero's engine comparison
(`docs/015-rules-engine-comparison.md`, §1 for the table and §6 for the
method). I measured gorge only. Forge, XMage and mtg-kernel were not run.

- **Harness:** `cmd/enginebench` in this repo. It builds unchanged at `a4af596`
  and at today's `main`.
- **Raw data:** `/mnt/sata/gorge-training/enginecmp/raw/` holds
  `results.jsonl`, `load.log`, `summary.txt` and CPU profiles in `../prof/`.

## Builds

| label | commit | what it is |
|---|---|---|
| upstream | `a4af596` on an M1 Pro | docs/015's published gorge column |
| a4af596 | `a4af59637` | the commit docs/015 measured, on this box |
| main | `e0496f062` (2026-09-30 17:10) | `main` when the runs started |
| bbmem base | `a8f1c2f25` | the base of the three bbmem branches (`wt/enginecmp`'s base) |
| bbmem-corpus | `9803b0655` | the subset corpus loader. It is built with `-tags enginebench_subset`, so the run opens only the workloads' cards |
| bbmem-collector | `1e27720d0` | "wip(searchprobe): typed compact frame boards" |
| bbmem-churn | `308f7e0b9` + a dirty-tree snapshot | the branch had no commits of its own. I took a snapshot of its uncommitted tree at 17:22 (28 files, patch sha256 `c0eaff2cf4f2…`, plus 3 untracked files) |

The three bbmem builds branch from `a8f1c2f25`. Compare them with
**bbmem base**, not with `main`: `main` is 13+ commits ahead of them.

## Table (docs/015 §1 layout; one core, medians of 5 repetitions)

FDN cells show pair A / pair B. Rates are per CPU second of a pinned
single-thread process (see Method).

| | upstream (M1 Pro) | a4af596 | **main** | bbmem base | bbmem-corpus | bbmem-collector | bbmem-churn |
|---|---:|---:|---:|---:|---:|---:|---:|
| Random play on FDN, turns/s | 731 / 1,444 | 805 / 1,350 | **745 / 1,042** | 818 / 1,087 | 819 / 996 | 824 / 1,098 | 837 / 1,167 |
| … games/s | – | 18.7 / 34.8 | **17.5 / 25.9** | 19.1 / 27.0 | 19.1 / 24.7 | 19.3 / 27.3 | 19.5 / 29.2 |
| … decisions/s | – | 21.3k / 35.9k | **19.7k / 27.8k** | 21.6k / 29.1k | 21.6k / 26.6k | 21.7k / 29.3k | 22.1k / 31.1k |
| Random play on Burn, turns/s | 2,703 | 2,394 | **1,874** | 2,163 | 2,053 | 2,011 | 1,923 |
| Built-in bot on FDN, games/s (manual taps) | 38 / 58 | 41.1 / 59.4 | **34.3 / 45.4** | 33.9 / 44.0 | 32.9 / 42.9 | 31.8 / 43.8 | 31.7 / 41.0 |
| Built-in bot on FDN, games/s (auto-pay) | – | 33.1 / 45.1 | **24.7 / 33.7** | 26.2 / 35.9 | 29.3 / 28.7 | 28.5 / 31.1 | 28.1 / 37.0 |
| Copying a mid-game state (GC excluded) | 14 µs, 100 KB | 13.0 µs, 103 KB | **12.7 µs, 113 KB** | 12.6 µs, 113 KB | 11.5 µs, 113 KB | 13.7 µs, 113 KB | 13.9 µs, 112 KB |
| … same copies with GC paid | – | 47.6 µs | **57.7 µs** | 61.3 µs | 26.4 µs | 60.1 µs | 58.8 µs |
| One search step (Clone + Submit, GC excluded) | 46 µs | 67.9 µs | **64.0 µs** | 70.7 µs | 79.8 µs | 81.1 µs | 66.7 µs |
| … pass-priority step only | – | 38.6 µs | **42.0 µs** | 40.9 µs | 54.0 µs | 49.9 µs | 50.8 µs |
| Old rollout sampler, rollouts/s | ~40 | 35.0 | **30.5** | 27.8 | 28.7 | 31.5 | 30.3 |
| azmcts + honest redeal, 100 sims, sims/s | (no tree then) | n/a | **338** | 321 | 304 | 294 | 332 |
| azmcts + honest redeal, 1,000 sims, sims/s | – | n/a | **171** | 172 | 169 | 192 | 195 |

Copy bytes: a clone allocates and retains the same number of bytes. Nothing
is shared with the parent, so the KB figure is both the retained size and
the bytes allocated per copy. A step allocates 271 KB at a4af596 and 283 KB
at main.

The live heap after the corpus loads is 143 MB on every build except
bbmem-corpus, where it is **4.3 MB**. That is why that branch's GC-paid copy
costs 26 µs instead of about 58 µs (below).

### Spread (IQR across the 5 repetitions, a4af596 → main)

| row | a4af596 median [IQR] | main median [IQR] |
|---|---|---|
| random A turns/s | 805 [707, 885] | 745 [633, 787] |
| random B turns/s | 1,350 [1,260, 1,520] | 1,042 [1,010, 1,110] |
| random Burn turns/s | 2,394 [2,360, 2,590] | 1,874 [1,830, 2,000] |
| bot A games/s | 41.1 [33.5, 42.9] | 34.3 [31.6, 36.3] |
| bot B games/s | 59.4 [52.8, 60.6] | 45.4 [39.7, 48.1] |
| auto-pay bot A / B games/s | 33.1 [31.6, 35.3] / 45.1 [42.2, 49.4] | 24.7 [22.3, 28.8] / 33.7 [26.5, 34.1] |
| clone µs (GC excluded) | 12.95 [12.1, 14.5] | 12.71 [12.1, 13.6] |
| step µs (GC excluded) | 67.9 [63.1, 69.4] | 64.0 [63.1, 77.0] |
| sampler rollouts/s | 35.0 [32.3, 37.8] | 30.5 [29.8, 31.9] |
| azmcts 100 / 1,000 sims/s | – | 338 [266, 346] / 171 [169, 173] |

`raw/summary.txt` has the full per-build table (`cmd/enginebench/analyze.py`).

## Machine factor

The factor is upstream ÷ ours at the same commit (`a4af596`), so above 1
means the M1 Pro was faster:

| row | factor |
|---|---:|
| random FDN A / B | 0.91 / 1.07 |
| random Burn | 1.13 |
| bot FDN A / B | 0.93 / 0.98 |
| clone | 0.92 |
| old sampler | 1.14 |
| **geometric mean** | **1.01** (range 0.91–1.14) |

The one-step row is left out: its definition is not reproducible (see
Caveats). Its ratio is 1.48 on the uniform-over-options step and 0.84 on a
pass step.

**This box's pinned core, under the load below, is about as fast as the M1
Pro core docs/015 used.** A row's spread across cells is the same size as
this box's run-to-run spread. So our numbers can be read directly against
docs/015's other columns, with about ±15% for machine and noise.

## Today's main against a4af596 (same box, so already machine-normalised)

| row | main ÷ a4af596 |
|---|---:|
| random FDN A / B turns/s | **0.93× / 0.77×** |
| random Burn turns/s | **0.78×** |
| bot FDN A / B games/s | **0.83× / 0.76×** |
| auto-pay bot A / B games/s | **0.75× / 0.75×** |
| clone, GC excluded | 1.02× (no change); bytes +10% (103 → 113 KB) |
| clone, GC paid | 0.83× |
| one step, uniform / pass | 1.06× / 0.92× |
| old sampler rollouts/s | 0.87× |
| azmcts | n/a (new since a4af596) |

**Game play is 7–25% slower on today's main than at the docs/015 pin.**
- The IQRs do not overlap for random B, random Burn, bot B or either
  auto-pay pair.
- Random games at main run 1.4 turns longer on B (38.8 → 40.2) and 0.3
  shorter on A and Burn: the engine changed, so the games differ. A
  per-turn rate already controls for game length.
- Clone cost is unchanged per copy, but a copy is 10% bigger.

A CPU profile of random Burn shows one new hot spot at main:
- `rules.buildAirbendExileIndex` takes **12.4% of the run**. It is reached
  from `legalWalk.exileCastsWalk` (commit `e1a015808`, "index airbend exile
  permission once per offer walk").
- It still scans the whole event log once per priority walk whenever an
  exile zone is non-empty.
- Making that index incremental would win back about half of the Burn gap.
- The bot-B slowdown is diffuse in the profile: `legalActionsWithWindow`,
  `Derived` and `checkStateBased` are each a bit heavier. Profiles are in
  `/mnt/sata/gorge-training/enginecmp/prof/`.

The bbmem builds against their base `a8f1c2f25`:

- **corpus (`9803b0655`):**
  - The live heap is 33× smaller (143 → 4.3 MB), which halves the
    GC-paid clone (61 → 26 µs).
  - Steady-state random and bot rates show no significant change; they
    are within noise.
  - The auto-pay bot is +12% on A and −20% on B, both inside or close to
    the noise.
  - The pass step is slower (41 → 54 µs, IQR [42, 56]). That is worth a
    second look; it may be faulting in cards Lookup does not hold.
- **collector (`1e27720d0`):**
  - azmcts at 1,000 sims is +12% (172 → 192 sims/s; IQRs [159, 181] vs
    [170, 198]).
  - The sampler is +13%.
  - The rest is within noise. The GC-paid step is slower (173 → 197 µs).
- **churn snapshot:**
  - azmcts at 1,000 sims is +13% (172 → 195, IQR [194, 214]).
  - azmcts at 100 sims is +4%. The bot is within noise.
  - Random Burn reads −11%, but its IQR [1,690, 2,210] spans the base.

## Method

**Workloads (docs/015 §6):**
- FDN pair A is `FDN_top_04956_UG` vs `FDN_top_20626_WG`. Pair B is
  `FDN_top_07961_WR` vs `FDN_top_02581_UR`. The decks come from
  draft-zero's `assets/sample/decks/`.
- Burn is mtg-kernel's list. `pauper_pool_v1.json` deck `Burn` is identical
  card for card to `internal/spellbench/decks/pauper-kernel/burn.json`.
- Seats swap every game, and game g uses engine seed 1+g.
- The settings are 20 life, constructed format, no mulligan round
  (`Config.Mulligans` 0), and the deterministic no-host starting player.

**Rows:**
- **Random play.**
  - At each decision the random player draws a choice count uniformly
    in [Min, Max], then that many distinct options uniformly.
  - At priority this is uniform over the offered options, mana taps
    included, so lands are tapped one at a time as docs/015 describes.
  - The `concede` option is excluded. Otherwise most games end on turn 1.
  - A rejected intent is redrawn. 0.04–0.08% of draws were redrawn, and
    the heuristic-bot fallback after 32 redraws never fired.
  - There were no stalls (caps were 300 turns and 200k intents) and no
    panics.
  - Turns come from `Game.Turn`, which counts both players.
  - Games averaged 43 (A), 39–40 (B) and 35 (Burn) turns. docs/015 gives
    36–43 for gorge.
  - Decisions average 26 per turn on FDN and 21 on Burn.
- **Built-in bot.**
  - The bot is `seat.NewBot` on both seats, driven by
    `internal/bench.PlayGame` (cmd/botbench's loop).
  - "Auto-pay" is the same bot with `EnableAutoPayMana()`, the
    payment-plan path.
- **Copy.**
  - The roots are FDN pair A, played by the bot from seed 2. Each root is
    the first choice point (an option other than pass, concede or a mana
    tap) of turns 5, 6, 7 and 8, with nonland permanents on both sides
    (3–6 permanents a side, 494–936 events in the log). The roots were
    identical at a4af596 and main.
  - Each root gets 5,000 timed `Engine.Clone()` calls. The four roots'
    means are averaged.
  - "GC excluded" runs batches of 250 with the collector off and a forced,
    untimed GC between batches.
  - "GC paid" runs the same 5,000 copies at the default GOGC.
  - Retained bytes are the HeapAlloc growth after GC with 500 copies
    kept alive.
  - Allocated bytes are the TotalAlloc delta ÷ copies.
- **One step.**
  - `root.Clone()` plus one `Submit`, at the same 4 roots, GC excluded,
    5,000 per root.
  - The Submit cycles uniformly through every option the root accepts
    (land plays, casts, mana taps, pass; 4–6 per root).
  - Each option is also timed alone. The "pass step only" row is the
    median of the pass options.
- **Search.**
  - Both seats are the bot, and seat 0 keeps a `searchseat.Feed`. Every
    seat-0 decision in turns 5–8 is handed to the search (at most 6
    searched roots per game, pairs A and B alternating). The bot's answer
    is played, so every search mode sees the same roots.
  - **azmcts:** `azmcts.NewSeat` with `DefaultSeatConfig()`,
    `World = redeal`, `Worlds = 0` (a fresh honest deal per simulation),
    no network (uniform prior, heuristic leaf), CPUCT 1.5, 6 candidates,
    and MaxSteps 1000.
  - sims/s is simulations ÷ CPU time of the decisions that built a tree.
    The time covers the whole `DecideSearch`, redeal preparation included.
    There were 45–50 such roots per run at 100 sims, and 8 at 1,000.
  - A simulation averaged 70 (100-sim) to 98 (1,000-sim) environment
    submits.
  - **Old sampler:** `searchseat.NewSearchBot(Defaults())` (8 worlds, 6
    candidates, attackers + cast, rollouts to game end). The rate is
    rollouts ÷ CPU time of the decisions the teacher actually covered.
  - Across all the decisions it was asked, including the ones that fell
    back without covering, the rate is 19.5 (a4af596) and 14.7 (main)
    rollouts/s.
  - The old sampler still exists at main.

**Timing:**
- Every run went through `heavy.sh`, as
  `taskset -c 9 env GOMAXPROCS=1`: logical cpu 9, whose SMT sibling is
  cpu 25 and could not be reserved.
- Rates are per **CPU second** (getrusage user+sys of the process). Wall
  time was recorded too and came within 0.5% of CPU time on every row, so
  the pinned process was rarely descheduled.
- Game rows ran for 10 CPU-seconds after 2 untimed warm-up games. Search
  rows ran for 15 s (sampler, az100) or 30 s (az1000) of CPU, bot play
  between roots included.
- Each repetition ran one row group with all six builds back to back. The
  build order rotated per repetition and per row, and 5 repetitions ran
  between 17:23 and 19:07.
- The toolchain was go1.26.3 linux/amd64. docs/015 used Go 1.27.1.

**Load:**
- The box is shared with other agents. 1-minute load average at each batch
  start: min 9.1, median 16.7, max 29.6 (of 32 logical cpus). Per-build
  medians were 15.7–16.1, so no build ran under systematically different
  load.
- Before the runs, every logical cpu was 50–90% busy. A pinned core's SMT
  sibling was therefore often busy, and that slows the pinned thread in a
  way CPU-time accounting cannot remove. The ±10–20% IQRs come from this.

## Caveats and what could not be reproduced exactly

- **docs/015's harnesses were never published**, so these choices are mine:
  - the random policy's handling of multi-choice decisions;
  - excluding concede;
  - the seed schedule;
  - the root positions;
  - which Submit a "search step" makes.

  Game lengths and the a4af596 numbers land on docs/015's within the
  machine noise, except the step row.
- **One search step is not reproducible as defined.**
  - "Clone + one Submit" depends on which Submit is made.
  - Uniform over the root's options is 68 µs at a4af596. A pass is 39 µs.
  - docs/015's 46 µs lies between them.
- **GC dominates copying in a process that holds the full corpus.** With
  the 143 MB corpus live, each collection marks it all, and the GC-paid
  copy is 3.7–4.9× the copy itself. docs/015's 14 µs matches our GC-free
  figure. 1,000 copies of 100 KB do not reach the first GC trigger, so it
  was probably GC-free by construction.
- **Search rates are not docs/015's "rollouts".**
  - An azmcts simulation is one tree walk of up to 1,000 environment
    steps (about 70–100 here) to a heuristic leaf.
  - An old-sampler rollout plays a sampled world to game end.
  - The two rates are not comparable to each other, nor to mtg-kernel's
    or XMage's sims.
- **The bbmem tips are in-flight work**, and the churn row is an
  uncommitted snapshot. Re-measure before quoting them.
- **Different builds play different games.** Engine changes alter random
  and bot trajectories, so the work per game is not identical across
  builds. Per-turn rates control for game length, not for game content.

## Rerunning

```sh
# build each commit: copies cmd/enginebench into a detached worktree and builds it
cmd/enginebench/scripts/build.sh <label> <worktree> [enginebench_az[,enginebench_subset]]
# one repetition of one row group, builds interleaved (edit BUILDS in the script)
cmd/enginebench/scripts/driver.sh <rep> random|bot|copy|search|az1000
python3 cmd/enginebench/analyze.py /mnt/sata/gorge-training/enginecmp/raw/results.jsonl
```

Build tags:
- `enginebench_az` links `internal/azmcts`, which does not exist at
  a4af596. Without the tag, `-row az` reports unavailable.
- `enginebench_subset` swaps the corpus open for `cards.OpenCorpusFor`
  (bbmem-corpus and later).

Everything else uses only APIs present at both a4af596 and main.
