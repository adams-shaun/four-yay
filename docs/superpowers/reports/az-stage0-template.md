<!-- Copy to docs/superpowers/reports/<run date>-az-stage0.md and fill from
     the scripts/az-stage0.sh output directory. -->
# AZ stage 0: gen-0 clairvoyant MCTS vs bot (2026-09-2X)

Spec: docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md §3 (Stage 0).
Plan: docs/superpowers/plans/2026-09-27-alphazero-mcts.md (tickets 1–2).
Commit measured: <commit.txt: commit>. Corpus pin: <commit.txt: forge_ref>. Dirty paths: <n>.

## Setup

- Decks: uw-tempo vs mono-white-equipment, mono-blue-tempo, mono-black-aggro,
  mono-red-prowess, mono-green-stompy; seats traded every game.
- Eval block: seed 90,000,000; 40 games per pair = 200 games per arm.
- Arms: control (`-a bot -b bot`), az25 and az100 (`-a az -b bot -az-world
  clairvoyant -az-sims 25|100`), gen 0 (no checkpoint: uniform prior,
  searchprobe.LeafValue leaf), every other knob at its default (c 1.5, FPU 0.1,
  6 candidates, 1000 env steps, kinds priority/attackers/blockers/target).
- Resources: systemd-run --user --scope -p MemoryMax=5G, GOMEMLIMIT=1GiB,
  taskset -c 12,28, -workers 2; output /mnt/sata/gorge-training/az/stage0/.
- Command: scripts/az-stage0.sh (unmodified | overrides: ...). Knobs as run: <commit.txt: knobs>.

## Cost

| measure | az25 | az100 | source |
|---|---|---|---|
| ns per simulation, early position (turn ≥ 3) | <bench.txt Early ns/sim> | — | bench.txt |
| ns per simulation, late position (turn ≥ 9) | <bench.txt Late ns/sim> | — | bench.txt |
| B/op, allocs/op per searched decision (25 sims) early / late | | — | bench.txt |
| ms per searched decision, mean / p50 / p95 | | | azNN.txt "ms/searched decision" |
| ms per simulation in play | | | azNN.txt "ms/simulation mean" |
| simulations per searched decision | | | azNN.txt |
| env steps per simulation | | | azNN.txt |
| searched decisions per game | | | azNN.txt "searched N (x/game)" |
| games/h on 2 cores (control: <x>) | | | summary.txt |
| botbench peak RSS (KB) | | | azNN.time |
| test-binary peak RSS (KB): azmcts / botbench | | | rss-*.txt |

Does the event-log copy after Clone bind? Late ns/sim ÷ early ns/sim = <r>.
(Spec "Known cost risk": a persistent log prefix is in scope only if this binds.)

## Strength (200 games per arm; ±~7pp, a pipeline check, not a verdict)

| arm | A wins | B wins | draws | stalls | pooled A win rate (95% CI) | pairs A loses / wins / undecided |
|---|---|---|---|---|---|---|
| control (A = bot) | | | | | | |
| az25 (A = az) | | | | | | |
| az100 (A = az) | | | | | | |

Per pair, az100: paste the matrix table from az100.txt.

## Counters (spec §4: every fallback, never silent)

| counter | az25 | az100 |
|---|---|---|
| simulations / completed | | |
| chance-failures, panics, submit-errors | | |
| bad-worlds, no-world, all-failed | | |
| step-capped, terminal, expanded | | |
| unavailable, prior-fallbacks | | |
| skipped, feed-stopped | | |
| per kind asked / searched / overrides: priority | | |
| attackers | | |
| blockers | | |
| target | | |
| skipped by kind, payment / few-candidates / translate-error: priority | | |
| attackers | | |
| blockers | | |
| target | | |
| skipped priority by the bot's answer (payment / few-candidates / translate-error): cast | | |
| ability | | |
| pass | | |
| play_land | | |
| activate (the manual bot's mana tap before a cast) | | |
| payment | | |
| other | | |

Source: azNN.txt, the `searched by kind` and `  skipped ...` lines under
`counters:`. Mana-tap share of priority decisions = skipped-activate ÷ kind
priority asked: how often the search meets a cast only after the bot has
already floated its mana.

## Kill criterion (spec §3)

"If full-information search with no network cannot beat bot, the candidates or
the leaf are broken; diagnose before any training."

Operationalised (controller decision 2026-09-27; operator may override): KILL if no az arm's pooled
win-rate 95% CI lies entirely above the same-seed bot-vs-bot control arm's pooled
win rate.

Verdict: PASS | KILL (summary.txt prints the mechanical verdict line). <one sentence with the numbers>.

If KILL, diagnose before tickets 3–5, in this order:
1. Skipped share per kind and reason, and the skipped-priority rows by the
   bot's answer: are candidates being built at all, and how much of priority
   is mana taps (activate) the search never sees?
2. Override share: does the search ever leave the bot's answer?
3. Step-capped share of completed simulations: are leaves mostly capped mid-walk?
4. -az-kinds subsets (attackers alone, priority alone) on one pair: which kind loses?
5. -az-sims 400 on one pair: does strength rise with budget (tree) or not (leaf)?
6. The heuristic leaf's scale (searchprobe.LeafScore /20): value spread at decision points.

## Recommendation for tickets 3–5

- Simulation budget for generation, from games/h: <n> sims → <x> games/h →
  <h> hours per 100–200-game generation on 2 cores.
- Anything in the counters that must be fixed first.
