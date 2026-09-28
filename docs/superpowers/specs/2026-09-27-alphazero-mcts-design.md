# AlphaZero-style MCTS for gorge's bot — design (2026-09-27)

Status: approved in conversation 2026-09-27 (approach A, sections 1–4). This
document is the written spec; the implementation plan follows it.

## Why, and what this must answer

Every attempt since 2026-09-06 to compress search into a learned policy has
reproduced the default `bot` and nothing more
(`docs/superpowers/reports/2026-09-24-training-approaches-summary.md`). The
only durable edge is decision-time search (PIMC, +3.3pp held-out as the L10
seat). MageZero (AlphaZero-style MCTS + network on an XMage fork) shows a
rising curve, 37% → ~64% over 17 generations, but it trains clairvoyant
(`see_opponent_hand: true`), at 36–50 games/h on 2 vCPU.

The ingredient gorge has never tried is AlphaZero's loop itself: a tree
search whose leaf is the network's value head, whose training targets are
the search's visit distribution (soft, not argmax labels), and whose value
target comes from its own games, iterated and gated.

The effort is staged, as the operator chose ("both, staged"):

1. **Stage 1 (clairvoyant):** does the AZ loop compound in gorge at all?
2. **Stage 2 (honest):** the same code with sampled worlds. What does hidden
   information cost, and does it beat L10?

The two stages differ **only** in where a simulation's world comes from. The
network always sees the searching seat's redacted view, so a checkpoint
trained in stage 1 is loadable, and deployable, in stage 2.

## Prior evidence this design answers to

| Evidence | Consequence here |
|---|---|
| mtgbld: PUCT over a weak prior lost 11–20pp; "search whose rollouts are the baseline is capped at the baseline" | No bot rollouts at the leaf; the value head is the leaf. Gen 0 must beat `bot` with a heuristic leaf before any training (stage 0 kill). |
| pn12: argmax teacher labels are near-ties plus world luck, unlearnable | Train on the visit distribution π, not the pick. |
| pn09 / mtgbld: value heads predict outcomes but don't rank moves | Measure value-head discrimination at decision points per turn bucket, not state log loss. |
| mtgbld: ungated iteration random-walks | Every generation is gated against a fixed eval block. |
| MageZero trains against a fixed minimax pool, not self-play | Train against a fixed `bot`. This also keeps `searchprobe.Sample`'s assumption (the opponent plays like `bot`) true in stage 2. |

## §1 Components

### `internal/azmcts` (new, pure search, no I/O)

```go
Search(src WorldSource, net *policynet.Model, opts Options) (Result, error)
// Result: Candidates []decision.Intent, Visits []int, Prior []float64,
//         RootValue float64, Choice int, Stats (fallback counters)
```

- **`WorldSource`** hands each simulation a fresh engine to walk:
  - `Clairvoyant`: `(*rules.Engine).Clone()` of the real engine
    (`rules/clone.go:51`). Stage 1 only; bench and training only.
  - `Sampled`: K worlds drawn once per decision by `searchprobe.Sample`
    (`internal/searchprobe/sample.go:158`), with the pn21 redeal
    (`RedealBase`, `internal/searchprobe/redeal.go:39`) as the fallback when
    the sampler starves. Worlds are used round-robin, each simulation cloning
    its world. Stage 2.
  - Nothing else in the tree touches hidden information.
- **Env step.** After the searching seat's intent is submitted, `bot`
  (`botpolicy.Decide`) answers every decision until the seat's next
  **searched** decision, game over, or the step cap. This covers all opponent
  decisions and the seat's own trivial kinds (choose, trigger_optional,
  trigger_order, modes, replacement, and mana taps through the auto-pay
  adapter). Hypothetical worlds use `SubmitHypothetical` /
  `AdvanceHypothetical` (`rules/chance.go:69,83`).
- **Searched kinds and candidates** (the searchseat enumerators, reused):
  - priority, when it offers more than pass/concede: the option list, capped by its `limit` argument
    (`searchprobe.Candidates`, `internal/searchprobe/rollout.go:17`);
  - attackers: `AttackCandidates` (`teacher.go:317`);
  - blockers: `BlockCandidates` (`teacher.go:414`);
  - single-choice target: `SingleTarget` / `TargetCandidates`
    (`teacher.go:501,512`).
  - A decision with one candidate is not searched.
- **Action identity.** Candidates are semantic actions
  (`Collector.Actions` / `Collector.Match`, `internal/searchprobe/action.go:39,76`),
  stable across sampled worlds, so one tree is shared across worlds.
- **Leaf.** `policynet.Model.Value` (`internal/policynet/net.go:212`) on the
  searching seat's *redacted* view of that world; 1 or 0 at game over. Gen 0
  (no network) uses the frozen heuristic `searchprobe.LeafValue`
  (`teacher.go:89`) and a uniform prior.

### Seat `az` in `cmd/botbench`

`-a az -az-sims N -az-world clairvoyant|sampled -checkpoint <gpol>`. It
follows the L10 searchseat route: the driver feeds the seat the engine and
history (the `BoardSeat`-style type assertion in `internal/bench.PlayGame`).
`-az-world clairvoyant` is refused anywhere a non-bench opponent could be
seated (host / gorged).

### `cmd/azgen` (new)

Plays the `az` seat against `bot` over a seed block and writes a visit
corpus, JSONL, one row per searched decision:

- the redacted state features (the checkpoint's feature set) and option
  features, in the form `policytrain` already reads;
- `visits` per candidate and the resulting π;
- `root_value`;
- the game outcome, filled at game end.

### `cmd/policytrain -loss visits` (new loss mode)

- Policy: soft cross-entropy against π over the candidate options. Attackers
  and blockers candidates are subsets; their π target is spread onto the
  per-option Bernoulli form `candidateScore` already scores
  (`internal/searchseat/prior.go:155`).
- Value: target `λ·outcome + (1−λ)·root_value`, λ from a per-generation
  schedule starting at 0.95 (MageZero's `td_discount` schedule:
  0.95, 0.92, 0.85, then 0.70).
- The residual bot prior is 0 for this mode. The bot's answer enters through
  candidate 0 and the tie-break, not through a pinned logit (pn13/pn14:
  residual 2 pins the policy).

### `cmd/exitloop -mode az` (new mode)

Control → per generation: `azgen` → `policytrain -loss visits` over a
sliding window of the last 5 generations' corpora → evals (§3) → gate.

## §2 Search algorithm

- **Single perspective.** Tree nodes are the searching seat's searched
  decisions only; the opponent is part of the environment, so values are
  always the seat's win probability and there is no sign flip.
- **Selection (PUCT):** `argmax_a Q(a) + c · P(a) · sqrt(N_avail) / (1 + N(a))`,
  `c = 1.5` (flag). An unvisited child's Q is the parent's Q − 0.1
  (first-play urgency, flag).
- **Simulation:** clone the world, walk the tree by submitting intents (the
  env answers everything between), expand exactly one new node, evaluate the
  leaf, back up. No rollouts.
- **Priors:** priority and target, softmax of the policynet option scores;
  attackers and blockers, the `candidateScore` log-likelihood per candidate,
  softmaxed across candidates; gen 0, uniform. Candidate 0 is always the
  bot's answer and wins ties.
- **Tree across worlds (stage 2):** nodes are keyed by the path of the
  seat's semantic actions (open loop). A child is scored only in worlds where
  it is legal; `N_avail` counts the simulations in which it was available
  (the ISMCTS availability rule). In stage 1 a clairvoyant clone is
  deterministic along a path, so this reduces to an ordinary tree.
- **Budget:** `-az-sims`, default 100 for the brief tests (MageZero uses 300).
  A per-simulation cap on env steps guards against livelock. No wall-clock
  limits (the engine forbids nondeterminism).
- **Exploration, generation only:** Dirichlet noise on the root prior
  (α 0.3, ε 0.25); moves sampled ∝ visits (τ = 1) on turns 1–4, argmax after.
  Eval is argmax, no noise.
- **Determinism:** the per-decision RNG is seeded from (game seed, decision
  index); simulations run sequentially within a decision; cores go to
  parallel games. The same seed and checkpoint produce a byte-identical
  choice and corpus.
- **Out of v1:** tree reuse between consecutive decisions, parallel
  simulations, opponent nodes / self-play (approach C, deferred until stage 1
  shows a rising curve).

### Known cost risk

`Clone` is ~50 µs, but the first append after a clone copies the whole event
log (`events/log.go` `Log.Clone` shares cap-limited slices), about 385 KB by
turn 11 (`docs/superpowers/reports/2026-09-17-trigger-pruning.md`). The
design clones once per **simulation**, never per node, and stage 0 measures
the real per-simulation cost. A persistent log prefix is a possible engine
optimisation, out of scope here unless stage 0 shows it binding.

## §3 Stages, budget, gates, kill criteria

Deck set for every stage: uw-tempo against the five mono decks (the pn11–pn14
and MageZero-comparison set). Eval block: seed 90,000,000, seats traded.

### Stage 0: measure (after ticket 2)

- Cost per simulation and per searched decision; games/h at 25 and 100
  simulations, 2 cores.
- Gen-0 clairvoyant MCTS (no network, heuristic leaf) against `bot`,
  200 games per arm.
- **Kill:** if full-information search with no network cannot beat `bot`,
  the candidates or the leaf are broken; diagnose before any training.
- The numbers come back to the operator before tickets 3–5 are built.

### Stage 1: clairvoyant AZ loop

Per generation, four readouts:

1. the `az` seat (argmax, same sims) against `bot` on the eval block. This is
   the MageZero-curve analogue and, like theirs, it is clairvoyant;
2. the network alone (`-a policynet`) against `bot`: honest, cheap, and it
   shows whether the network absorbs anything;
3. value-head discrimination at decision points per turn bucket (1–6, 7–12,
   13+);
4. KL between consecutive generations' visit distributions on a fixed probe
   set of states (the convergence readout).

- **Gate:** a generation's checkpoint becomes the incumbent only if readout 1
  is ≥ the incumbent's; every checkpoint is still evaluated.
- **Budget now:** 2 cores, 5 GB. Runs at this size are pipeline proofs
  (about 3 generations × 100–200 games); 200-game evals are ±7pp, so they are
  not a verdict. The verdict run is sized from stage-0 throughput on
  resources the operator makes available later.
- **Kill:** after 8 gated generations, readout 1 is not ≥ gen 0 + 3pp with
  the CI clear of gen 0. Then AZ does not compound here even with full
  information, and stage 2 is not run.

### Stage 2: honest

- The same loop with `-az-world sampled`.
- Compared against the stage-1 curve (the price of hidden information) and
  against L10 (53.7% held-out).
- **Success:** beats L10 on the held-out block (seed 1,000,000, 400 games per
  pair) at comparable latency (L10: mean 664 ms, p95 2.2 s per asked
  decision). Promotion to the default bot is the operator's decision.

### Resource rules (every run and every brief)

- `systemd-run --user --scope -q -p MemoryMax=<cap>`, `GOMEMLIMIT=1GiB`,
  `taskset` to the granted cores.
- Data, corpora, checkpoints and `GOTMPDIR` under
  `/mnt/sata/gorge-training/az/`.
- One heavy job at a time machine-wide.

## §4 Testing and failure handling

### Tests

- **Search core:** PUCT selection, first-play urgency, availability counts
  and backup, against a small fake-env interface (clone, pending, submit,
  over, winner), so the arithmetic is testable without the engine; plus one
  integration test on real decks from the test corpus.
- **Determinism:** same seed and checkpoint → byte-identical choice and
  corpus row.
- **Leak test (stage 2):** varying the real game's never-revealed hidden
  cards leaves the `az` seat's choice unchanged (the pn21 test pattern).
- **Clairvoyant refusal:** the clairvoyant source errors outside bench and
  training.
- **Opt-in only:** default `bot`, `TestHeads` (`rules/heads_test.go`) and
  existing botbench output are byte-unchanged.
- **Memory:** a per-decision allocation benchmark, and the peak RSS of every
  new test binary measured (`go test -exec` with `/usr/bin/time -f %M`). A
  multi-GB test binary is a bug.

### Failure handling (every fallback counted and printed, never silent)

| Failure | Handling |
|---|---|
| Chance failure in a hypothetical world | Discard that simulation; count it. |
| Candidate cannot be mapped into a sampled world | Child unavailable in that world. |
| Every simulation failed, or no world could be sampled or redealt | Play the bot's answer; count it. |
| Step cap reached | Evaluate the leaf where the walk stopped. |

## Tickets

1. `internal/azmcts` core, the clairvoyant source, the gen-0 heuristic leaf,
   and the core tests.
2. The `az` seat in botbench; then stage 0's measurements and a short
   report. Checkpoint with the operator before 3–5.
3. `cmd/azgen` corpus, `policytrain -loss visits`, and the TD-blended value
   target.
4. `exitloop -mode az` and its per-generation summary table.
5. The sampled world source, ISMCTS availability, and the leak test.

The stage 1 and stage 2 loop runs are controller work, not tickets.
