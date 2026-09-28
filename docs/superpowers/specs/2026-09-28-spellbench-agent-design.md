# A gorge-brained SpellBench agent — design (2026-09-28)

Status: draft for operator review (workstream W3). No code is specified to
the task level here; the implementation plan follows approval.

Sources, read at: gorge worktree `wt/spellbench` @ `cc3c349d8`; SpellBench
clone `/mnt/sata/gorge-training/spellbench` `main` @ `971a1fb` (protocol text
`spec/SPELLBENCH_PROTOCOL_V1.md`, `spec/SPELLBENCH_PROTOCOL_V2.md`); the gorge
engine adapter plan `origin/gorge-adapter` @ `a69b3cc`
(`docs/design/2026-09-27-gorge-adapter-plan.md`, code through
`origin/gorge-adapter-g14` @ `62b17fa`). `§n` is a section of the v2
protocol (v1 when marked); `D§n` is a section of this document. Theory and references:
`docs/superpowers/reports/2026-09-28-spellbench-theory-and-references.md`
(cited below as **[T§n]**). Anything not read from code or a document is
marked INFERRED.

## 1. Goals and non-goals

**Goal.** One generic agent (or a small routed collection of checkpoints)
that plays Magic in SpellBench arenas on *any* v2 engine, using gorge as its
private simulator: it rebuilds a gorge state from the neutral observation,
samples the hidden information it cannot see, and searches with gorge.
Secondary goal: the upstream gorge changes that make this, and Jack's gorge
engine adapter, cheaper to build and maintain.

**Why this shape.** The one durable edge gorge has ever measured is
decision-time search over sampled worlds; every attempt to compress it into
a policy reproduced the bot and nothing more
(`docs/superpowers/reports/2026-09-24-training-approaches-summary.md` TL;DR:
PIMC seat L10 53.7% held-out vs 50.4% control; distillation, expert
iteration, PPO all flat; [T§6]). Other SpellBench entrants today are
search-free policy networks (g115, a48, c12 on `pauper-kernel`,
`benchmarks/pauper-kernel/benchmark.json` "sampled, no search"). Search is
therefore both our comparative advantage and what the literature says adds
the most on top of a network (LOCM: 26.8% net alone, 51.35% with search
[T§4]).

**Non-goals.**
- Not the engine side: Jack's adapter (`engines/gorge` in the SpellBench
  repo) owns making gorge a v2 environment. We align with it and propose
  upstream hooks (D§11) but do not fork or duplicate it.
- No clairvoyance anywhere a rated game can reach. SpellBench's fairness
  contract exists to exclude it (§13; `everyone-on-the-board.md`,
  "bots that need clairvoyance cannot enter").
- No search on protocol v1 (D§3.1 explains why).
- No multiplayer, Commander, BO3/sideboarding (v2 scope, §0 "Scope").
- No new training program. The agent consumes whatever the AZ-MCTS program
  (`docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md`) and the
  existing policynet produce.

## 2. Architecture at a glance

```
host ── game_start / choose / game_over ──▶ agent process (Go, one game)
                                              │
 (a) protocol front end ── v2 NDJSON, lenient reader, v1 fallback
 (b) shadow builder ────── neutral observation (or x_gorge_view_v1) ─▶ gorge *rules.Engine
        └─ fidelity check: re-project shadow → v2 observation, diff
 (c) belief ────────────── opponent-deck posterior + knowledge (§6.7 `known`)
        └─ world source: redeal hidden zones from sampled deck (searchprobe-style)
 (d) decision core ─────── azmcts.Search over K worlds (PUCT, ISMCTS availability)
        └─ leaf/prior: policynet value/policy, routed per world (f)
 (e) fallback ladder ───── gorge bot on shadow view → neutral heuristic → candidate 0
 (g) budgeter ──────────── fixed simulations per decision class; clock guard
 (h) fairness guard ────── no cross-game state, no timing reads, leak test
```

Every stage is deterministic given (`agent_seed`, the seat's message
stream). The only wall-clock read is the clock guard (D§9), which is counted
and reported when it fires.

## 3. (a) Protocol front end

### 3.1 v1: counts only, no search

v1's `state_summary` "is the entire neutral observation in v1: it contains
only public scalars and counts, never hidden card identities" (v1 §7.3):
life, hand/library/graveyard/battlefield counts, stack count, turn, step.
Object identities reach the agent only through candidate references
(v1 §5, §6). There is no list of either battlefield, no power/toughness,
tapped state, counters, attachments or stack contents. A simulator needs a
board; v1 does not carry one. Search on v1 is only possible through an
engine-specific extension that carries a board (mtg-kernel's `x_kernel_v5`,
v1 §9) — which would make the agent kernel-specific, and is out of scope.

Decision: the v1 path is a **candidate-semantics policy** (priorities over
`semantic.kind` and the referenced card names, informed by gorge's card IR
where a name resolves), sized as W1's minimal workup. Its purpose is a
leaderboard presence and a floor, not strength.

- **Measured (W1, 2026-09-28, commit `50adc8999`).** gorge-native round
  robin on the 8 pauper-kernel decks as seat-swapped mirrors
  (`botbench -spellbench`, SpellBench's seed and schedule formulas, rated
  with SpellBench's own leaderboard code: anchored Bradley-Terry, paired
  bootstrap). Benchmark-shaped, 4 pairs per deck, 64 games per bot pair:

  | Bot | Elo | CI95 | W-L |
  |---|---|---|---|
  | az-clairvoyant-sims25 (reference, NOT a fair entry) | 1436 | [1361, 1522] | 229-27 |
  | bot (gorge default) | 1238 | [1178, 1308] | 174-82 |
  | sb-heuristic (port) | 1080 | [1019, 1140] | 122-134 |
  | sb-uniform (port, anchor) | 1000 | anchor | 96-160 |
  | sb-first (port) | 708 | [602, 797] | 19-237 |

  At 8 pairs: az 1443, bot 1256, sb-heuristic 1094, sb-first 671. The
  heuristic-over-uniform gap (+80 at 4 pairs, +94 at 8) overlaps
  mtg-kernel's published +102, but head to head it is weaker (59% vs 70%).
  The main cause is payment granularity: gorge's auto-pay lets the uniform
  anchor cast whatever it picks, while mtg-kernel offers mana taps as
  candidates. The `-manual` variants, which expose every mana ability,
  drop about 190 Elo each. gorge also poses about 770 decisions per game
  against mtg-kernel's about 218. 2688 games, 0 truncated, 0 halted,
  0 draws; 9m25s on 4 CPUs (96% of it az). `lethal-pressure` and the L10
  seat were not in this run. Rerun: `scripts/spellbench-rate.py` and the
  commit message of `50adc8999`.

### 3.2 v2: the real path

- **Hello.** `requires: {observation: [], extensions: []}` so the agent is
  admissible on every v2 engine; `extensions_accepted:
  ["x_gorge_view_v1"]` (informative, §10.1).
- **game_start.** Read `own_deck` (always full, §10.2), `opponent_deck`
  (`null` when `rules.opponent_decklist` is `hidden`, §12.2),
  `rules.card_name_domain`, `engine_profile` (which optional observation
  fields exist, which decisions are engine defaults, §7.6), `time_control`,
  `resources`, `agent_seed`.
- **choose.** Decode the canonical `seat_decision`, update the shadow and
  belief, decide, answer `{"selection": {"candidate_id": n}}`. Echo fields
  are optional (§10.3) and are sent, to catch our own mapping bugs.
- **Groups (§8).** A physical decision decomposed into substeps (attacks,
  blocks, fixed-count targets, arrangements) is decided **once**, at
  substep 0, as a gorge intent; later substeps are matched to that plan.
  This is the pattern Jack's adapter uses for its own Go agent (plan
  Task 26, `agent.Pick` / `agent.Plan`). A substep the plan cannot match
  goes to the fallback ladder for that substep only.
- **Errors.** Never answer `choose` with an error (it is a forfeit,
  §10.5); every internal failure resolves to a fallback answer.

**Measured (W2, 2026-09-28, commit `bf425c60d`).** `cmd/sbagent` +
`internal/spellbench/v2agent` play the v2 agent role (random, heuristic,
first) against the reference fake v2 engine through the reference host's
real seat drivers and live validator. The branch's `spellbench run` is still
the v1 runner, so games were driven through `host/game.py` `play_game` from
unmerged `protocol-v2-p23` plus `-p26` merged locally
(`cmd/sbagent/scripts/sbv2_harness.py`).

- **Parity:** all 927 games that seated a Go agent were replayed with the
  python twin in its place; every game digest (which chains every
  selection, §11.8) matched. heuristic/first reproduce the builtins exactly;
  random reuses uniform's SplitMix64 derivation.
- **Protocol:** 0 forfeits, 0 halts attributable to the agent, no unknown
  observation field logged (typed decode) even with every observation flag
  on. Go tests replay 7 recorded per-seat transcripts (70 decisions, all 30
  v2.0 kinds) plus 27 malformed/out-of-order request lines against the
  python answers.
- **Leaderboard** (fake scoring game, 1260 games, BT anchored uniform =
  1000): sbagent-heuristic 1338, heuristic 1336, uniform 1000,
  sbagent-random 953, sbagent-first 616, first 608 (twins 60/60 draws
  head to head; the random gap is seed noise).
- **Spec ambiguities found:** §2 line limit and the newline; §10.5 vs §9.8
  for valid-JSON non-object lines (the reference bot answers
  `malformed_json`, we answer `malformed_request`); §7.3 mixed-type fields;
  §10.1 hello mid-game; §10.6 stale.
- **Not yet measured:** shadow fidelity (needs the shadow, D§4) and parity
  with native gorge answers (needs Jack's gorge engine adapter).

### 3.3 Mana taps as candidates: plan lowering (measured)

Engines that pose mana abilities as candidates (mtg-kernel v1; any v2 engine
not declaring `mana_payment: engine_autopay`) require the agent to choose its
taps. The agent keeps deciding at intent level (pass, land, cast X, ability),
exactly as under auto-pay, and `internal/spellbench/payexec` lowers a chosen
cast into gorge's payment-plan witness: tap each planned source, answer its
colour ask, then cast, aborting on any divergence (pool check each step).
Taps never enter the search tree.

Measured (commit after `ddc66997c`, 8 pairs per deck, 2688 games): the
Planned variants sit inside their auto-pay twins' CIs (heuristic 1076 vs
1064, uniform 1012 vs 1000) and roughly 230 Elo above the naive `-manual`
variants (840, 788); head to head each planned bot is 65-63 against its
auto-pay twin. Aborts: 0.5-0.9% of lowerings, all on plans that sacrifice a
mana source whose trigger stacks before a sorcery-speed cast. Open: lowering
for activated abilities (gorge plans casts only), and ordering sacrifice
steps last.

## 4. (b) The shadow gorge state

The agent's core data structure is a `*rules.Engine` positioned at the
decision the host just posed, rebuilt from what the seat can see.

### 4.1 Inputs, in order of fidelity

| Source | When | What it adds |
|---|---|---|
| `x_gorge_view_v1` payload | engine is Jack's gorge adapter and the host enabled the extension | gorge's own seat `view.View` + the native `decision.Decision`, re-keyed to per-seat non-native ids (plan Task 24: JSON keys `view`, `decision`, `policy_facts`, `followups`, `ops`) |
| neutral observation (§6) | always | zones, characteristics after continuous effects, permanent state, stack, pending triggers (optional flag), `known` |
| the seat's own history | always | our own past answers; consecutive-observation diffs (what left the opponent's hand, what was cast) |

`view.View` (`view/view.go:86`) is still not engine state: it has no
continuous-effect records with durations (they live in the unexported
`rules.Engine.continuous`, `rules/engine.go:219`), no delayed triggers
(`state.Game.Delayed`, `state/game.go:379`), no zone-entry history
(`Entered`, `state/game.go:392`) and no per-turn counters
(`ResolvedThisTurn`, `state/game.go:325`). The neutral observation lacks the
same things plus everything gorge-specific. Both paths therefore need the
same reconstruction step; the extension only makes it more exact.

### 4.2 Reconstruction

1. Resolve every visible `card_name` through the gorge registry. Unknown or
   unsupported names become **proxies** (D§7).
2. Stage zones, battlefield permanents (tapped, damage, counters,
   attachments, summoning sickness, combat roles), stack entries (targets,
   modes, X), life, mana pool, turn, step, active/priority seat, lands
   played, monarch/initiative designations.
3. Recompute characteristics through gorge's layers and diff against the
   observation's `characteristics` (current values after continuous effects,
   §6.4). An unexplained difference becomes an explicit staged continuous
   effect: a P/T or keyword delta **until end of turn** unless a visible
   static source explains it (INFERRED heuristic: pump spells dominate
   unexplained deltas in the Pauper pool; measure it, D§12.3 K2).
4. Hidden zones get placeholder cards that the world source (D§5) replaces.
   The shadow's own hidden contents are never used for a decision except
   through sampled worlds.

Today step 2 needs package-internal access: the 17lands spike staged
replay snapshots by calling the unexported `(*rules.Engine).emit`
(`rules/engine.go:2610`) from a `_test.go` file inside package `rules`
(branch `wt/spike-17lands-value`, `rules/spike17l_replay_test.go:300-370`).
That proved staging works — 93.8% of recorded 17lands actions were legal in
the staged engine (25,139 of 26,805,
`/mnt/sata/gorge-training/17lands-spike/replay2000.kept.log`) — but it is not
a supportable API. D§11 H2 proposes one.

### 4.3 Fidelity check (re-project and diff)

After staging, project the shadow back into a v2 observation for our seat
and diff it field by field against the one received, matching objects by
(zone, owner, card_name, characteristics) since v2 ids are per-viewer HMACs
(§5.3). Also map the shadow's legal options to v2 semantics and compare with
the received candidate list.

| Metric | Definition | Use |
|---|---|---|
| observation match | fraction of fields equal after projection | gate for search (D§7) |
| candidate agreement | \|shadow ∩ engine\| / \|engine\| over candidate semantics | gate for search; divergence report |
| unmapped candidates | engine candidates with no shadow counterpart | always playable via fallback, never searched |

The projector should be the same code the gorge adapter uses to produce the
observation (`engines/gorge/internal/observe/project.go` on the adapter
branch), so a gorge-engine game diffs to zero by construction and any
residue is a real staging gap. It is package-internal to the adapter today;
D§11 H5 proposes moving it upstream.

## 5. (c) Hidden information: deck posterior and worlds

### 5.1 Opponent decklist posterior

Assume `opponent_decklist` is `hidden` (operator direction). When it is
`visible` the posterior is a point mass and nothing below changes.

- **Prior** by benchmark type:

| Setting | Prior over the opponent's list |
|---|---|
| rotating pool (`pauper-kernel`, `pauper-gorge`) | the published pool, weighted by the published pairing rule (INFERRED from the ledger: games are deck mirrors; see Q1) |
| fixed-deck benchmark (§15, reserved) | metagame database for the format, restricted to lists whose every name is in `rules.card_name_domain` (the union of all entry decks' names, §15) |
| FDN Limited (proposed) | per-colour-pair card-count model built from 17lands decks |
| Standard 2022-25 (proposed) | archetype lists (MageZero's 16-deck pool as a seed) |

- **Likelihood:** multivariate hypergeometric over the multiset of the
  opponent's cards the seat has seen leave hidden zones (cast, played,
  revealed, discarded, `known` hand entries). Lists that cannot contain the
  seen multiset get zero. Derivation, caveats and the identification-speed
  measurement are in [T§5]. Measured on the eight-deck `pauper-kernel` pool:
  the true list reaches 95% posterior after a median of **1** uniformly
  drawn card, p90 2-4, worst case 9 [T§5.3]. On closed pools the list is
  essentially free; the posterior matters for open formats.
- **Open formats** (Limited, Standard): the posterior is over (archetype,
  card counts), sampled as whole lists, never a single MAP list, so worlds
  keep the uncertainty.

### 5.2 Worlds

A world is (a list D drawn from the posterior; the opponent's unknown hand
cards and both libraries' unknown cards dealt uniformly from D and our list
minus everything seen; every `known` claim pinned). This is exactly the
construction of gorge's redeal fallback (`internal/searchprobe/redeal.go:39`
`RedealBase`: keep public state and hidden-zone sizes, pin known cards,
redeal the rest from the pool the seat can derive, re-seed future chance with
`CloneHypothetical`, `rules/chance.go:189`) and of SpellBench's reserved
noninterference probe, step 2 (§9.7). One sampler, three uses.

What we give up, stated plainly: gorge's rejection sampler
(`searchprobe.Sample`, `internal/searchprobe/sample.go:158`) replays the
whole game and so can reject worlds inconsistent with the opponent's
*behaviour* (epoch constraints). It needs every event burst at every
player's decision, which a seat never gets; gorge already rejected
"reconstruct history from successive views" on measurement
(`internal/searchseat/searchseat.go:36`: "without the event bursts there are
no epoch constraints, so Sample would draw from the wrong world
distribution and the teacher's measured edge would not survive"). A
SpellBench seat is in exactly that position. So:

- v1 of the world source is **uniform redeal** with known cards pinned.
- The measured L10 edge was obtained with the rejection sampler; whether it
  survives redeal-only worlds is unmeasured and is milestone M1's first
  question (D§12).
- Improvement path, not in v1: importance-weight redealt worlds by the
  likelihood of the opponent actions inferable from consecutive observations
  (e.g. passing with open mana), using the opponent model the value net
  implies. INFERRED; needs its own measurement.

## 6. (d) Decision core

**Choice: `azmcts.Search` with a SpellBench world source**, with the L10
PIMC teacher as the baseline arm.

- `azmcts.Search(root Root, src WorldSource, net *policynet.Model, opts)`
  (`internal/azmcts/search.go:86`) already has the right seams: `Root` holds
  the engine, the pending decision, the bot's answer as candidate 0 and a
  collector (`search.go:15-32`); `WorldSource.World(sim)` hands each
  simulation a world (`internal/azmcts/world.go:25`). The shadow engine is
  `Root.Engine`; the D§5.2 sampler is a new `WorldSource`.
- Tree: the searching seat's own decisions only, opponent played by the
  environment (`bot`), PUCT with `c = 1.5`, open-loop nodes keyed by the
  seat's semantic actions, the ISMCTS availability rule across worlds
  (AZ spec §2). Leaf: value head; prior: policynet option scores; gen 0 has
  a heuristic leaf (`searchprobe.LeafValue`,
  `internal/searchprobe/teacher.go:89`) and uniform prior.
- Why not plain PIMC as the core: PIMC averages per-world best responses,
  which is strategy fusion [T§2]; the availability-keyed shared tree is the
  ISMCTS fix and costs nothing extra here. Why not full ISMCTS with opponent
  nodes: the AZ spec defers opponent nodes until stage 1 shows a rising
  curve; we inherit that.
- Measured so far: clairvoyant gen-0 `az` at 25 simulations beat `bot` 78.0%
  [72.3, 83.7] over 200 games vs a 50.0% same-seed control, at 65.8 ms mean,
  144.2 ms p95 per searched decision
  (`/mnt/sata/gorge-training/az/stage0/az25.txt:11,17`). This is an expected upper
  bound: honest worlds are unmeasured.
- Multi-world budget split: Cowling et al. found MTG strength peaks at many
  determinizations with shallow trees (best 20-100 determinizations for a
  10k budget; extra budget better spent on more worlds) [T§3]. Default: K
  worlds round-robin with K ≥ 16 (INFERRED starting value; tune in M1).

**Mapping the answer back.** The chosen gorge intent is matched to the v2
candidates (and later substeps) by semantics: kind, source object (by our
observation-to-shadow object map), targets, modes. Unmatched → fallback
ladder; counted.

## 7. (e) Fallback ladder

| Tier | Used when | Policy |
|---|---|---|
| T0 search | fidelity gates pass; no proxy on stack or in our hand; ≥ 2 mapped candidates | D§6 |
| T1 gorge bot on the shadow | shadow built but a gate failed | `seat.NewBot(seed)` (`seat/bot.go:76`) on the shadow's projected view |
| T2 neutral heuristic | no usable shadow | priorities over candidate semantics (the v1 policy of D§3.1) |
| T3 first candidate | anything else | candidate 0 (`pass` when legal, §7.1) |

Gates (starting values, INFERRED, tuned in M1/M2): observation match =
100% on our own zones and ≥ 98% overall; candidate agreement ≥ 95%.
**Proxies:** an unsupported opponent card on the battlefield is staged as a
vanilla object with its observed characteristics and keywords (gorge
compiles keywords); search is allowed. A proxy on the stack or in our own
hand disables T0 for that decision. Every tier choice is counted per game
and reported with the result, never silent (same rule as AZ spec §4).

## 8. (f) The collection: routing checkpoints

A **router at checkpoint level** (mixture of experts where each expert is a
whole policynet checkpoint):

- Key: (format, own list or archetype, opponent list or archetype).
- **Routing is per world, not per game.** Each sampled world carries a
  concrete opponent list; its leaf is evaluated by the specialist for (own,
  that list) when one exists, else the generic checkpoint. The world mixture
  therefore weights specialists by the posterior automatically, and a
  near-certain posterior degenerates to one specialist.
- **Admission:** a specialist enters the table only after beating the
  generic checkpoint in its own matchup by a margin whose CI clears zero
  (K5, D§12.3). Otherwise it is dropped; the generic is the default.
- Evidence to date is against expecting much: gorge's value head reaches
  within-matchup AUC 0.805 from 1k own games and 0.833 from the full own
  corpus, while adding 17lands FDN data lowered it (0.768)
  (`/mnt/sata/gorge-training/17lands-spike/score-all.txt:1,5,13`). MageZero
  is one agent per deck [T§4]. Specialists are an experiment, not an
  assumption.
- Resources: 16 GB per agent (§11.4 example) leaves room for many small
  policynet checkpoints; load at startup (`startup_ms`), never mid-game.

## 9. (g) Time management

- **Clock (§11.4):** per-seat bank (example 600 s) plus increment (2 s) per
  decision, hard cap per decision (60 s); exceeding either is a `timeout`
  forfeit. Declared resources per agent: 4 CPUs, 16 GB, no GPU (example).
- **Load:** on `pauper-kernel` v1 ledgers (mixed bots incl. random ones) a
  game has mean 268 and p95 849 steps across both seats (max 1,507
  physical decisions)
  (computed from `benchmarks/pauper-kernel/runs/2026-09-27/matches.jsonl`);
  v2 decomposes more, so a seat can face several hundred decisions, most
  with one or two candidates.
- **Rule: fixed budgets, not seconds.** The spec itself says reproducibility
  requires "fixed search budgets (simulations, not seconds) seeded from
  `agent_seed`" (§11.4). Simulations per decision are a function of the
  decision class (priority with ≥ 2 mapped candidates, attack, block,
  target) and nothing else. Per-decision RNG =
  `azmcts.DecisionSeed(agent_seed, seat_step)`
  (`internal/azmcts/search.go:154`). Trivial decisions (one candidate, or a
  plan-matched substep) cost no search.
- **Sizing:** pick budgets offline so that the p99.9 game uses ≤ 50% of the
  bank on the declared 4 CPUs, parallelising across worlds (ensemble
  determinization parallelises trivially; merge visit counts in world order
  for determinism).
- **Clock guard:** if `clock.remaining_ms` falls below a floor (INFERRED
  default: 30 × increment), drop to T1 for the rest of the game. It reads
  wall-clock-derived input, so a game where it fires is not reproducible;
  it is counted and must fire in 0 of M2's games (K4).

## 10. (h) Fairness compliance

F1-F4 (§13) bind engines and the host; the agent's corresponding duties:

| Clause | Agent duty | How it is checked |
|---|---|---|
| F1 perspective | decisions depend only on this seat's messages in this game; no state outlives a game (§11.7) | process per game; no writable cache; leak test below |
| F2 never send | never try to recover hidden facts from ids, digests or text; use `x_gorge_view_v1` only as declared with `native_ids: false` | code review; the extension path must pass the same leak test |
| F3 decision shape | do not treat anomalies in decision shape as evidence about hidden state | the belief update consumes only observation fields and `known` |
| F4 extensions | require no extension; accept only audited/declared ones | `requires.extensions: []` |
| residual 1, timing (§13) | never read inter-decision wall time as evidence | only the clock guard reads the clock, and only our own bank |

**Leak test** (the AZ spec's stage-2 pattern): replay a real game twice,
varying only hidden cards never revealed to our seat; every answer must be
identical. **Clairvoyance ban:** the agent binary does not link the
clairvoyant world source (`azmcts.AllowClairvoyant`,
`internal/azmcts/world.go:39`, is never called).

## 11. Engine side: gorge, Jack's adapter, and upstream hooks

**Relationship.** Jack's adapter is the environment; it imports gorge
unmodified at `26257e0e` through `rules.NewHypotheticalPlanned`
(`rules/chance.go:48`) and wraps `seat.NewBot` / `seat.NewLethalPressureBot`
as agents (plan Tasks 8, 26). It carries `x_gorge_view_v1` with
`native_ids: false` (plan, "Declared engine profile"), departing from Annex
B's `native_ids: true`, which makes the extension usable in rated runs
without the §14 audit. Our agent is a separate v2 entry that can run on that
engine or any other. Two consequences:

- **Pin skew.** `26257e0e` is 584 commits behind this worktree's HEAD. An
  agent built at gorge HEAD simulating an engine at the pin is a rules
  divergence even within gorge; candidate agreement (D§4.3) measures it.
- **Two entries, not one.** Rate `gorge-search` (neutral observation only)
  and, optionally, `gorge-search-xview` (extension enabled). Only the neutral
  one is comparable across engines.

**Proposed upstream changes** (each a separate gorge ticket; none is needed
for the adapter to ship):

| # | Change | Why |
|---|---|---|
| H1 | Public checkpoint loader and scorer: a non-`internal` package re-exporting `policynet.LoadCheckpointFile` / `LoadScorerFile` (`internal/policynet/checkpoint.go:445`, `scorer.go:97`) plus a `seat.Seat` around it | Jack's plan leaves the PolicyNet bot out "because `internal/policynet` cannot be imported from outside gorge"; our agent needs the same to host checkpoints outside gorge's module |
| H2 | State-staging API: `rules.Stage(cfg, Snapshot)` building a hypothetical engine at a given public-plus-determinized state through logged events (zones, permanents, stack, turn/step, designations, and continuous effects via `AddContinuous`, `rules/layers.go:1458`) | replaces the spike's package-internal `emit`; keeps AGENTS.md's "all mutation through `events.Apply`"; the engine is hypothetical like `CloneHypothetical` (not replayable from Config) |
| H3 | Public world sampler: export the redeal with an explicit per-player card pool (a deck-list draw from our posterior) instead of `PublicGame.Decks` being one declared list (`internal/searchprobe/sample.go:24`) | lets hidden-list worlds reuse gorge's pinned-knowledge dealing; the same code can implement §9.7 `probe_resample` in the adapter |
| H4 | Public search: export `azmcts.Search`, `Root`, `WorldSource` | the agent lives outside gorge's `internal` tree if it ships in the SpellBench repo or as a standalone module |
| H5 | Upstream a v2 observation projector and the §6.7 `known` tracker (adapter `internal/observe`, MIT) as a public gorge package | adapter and agent then share one projection; the D§4.3 diff is exact on gorge engines |
| H6 | A botbench mode that forces redeal-only worlds for the L10 seat | measures, gorge-native, what the SpellBench observation constraint costs (M1 Q1) |

H2 and H3 are the load-bearing ones; H1 unblocks the most people.

### 11.1 v2 on gorge (reverse adapter)

Operator direction (2026-09-28): host a v2 environment backed by gorge and
run our agent "in reverse" against it, so the agent plays full games on
real gorge rules through the reference host, with every decision checked by
the host's live validator and the agent's reconstruction compared against
engine truth. Branch `wt/sb-v2-reverse`.

**Path taken: our own server, not Jack's adapter.** The newest adapter
branch, `origin/gorge-adapter-g14` @ `62b17fa`, implements plan Tasks 1-14
(framing, canonical JSON, secrets, v2 types, request decoding, catalog, game
construction, the validator subset, identity, observation, the mapping
framework and priority decisions). Mana abilities (T15), combat (T16),
targets and costs (T17), selections (T18), ordering and arrangement (T19),
simple choices (T20), resolution payments (T21), the game loop (T22) and the
server binary (T23) do not exist yet, so the module cannot host one game;
building it against this worktree would have given a library, not a
server. We wrote a minimal environment server inside gorge instead:
`internal/spellbench/v2engine` (mapping in its package doc) and
`cmd/sbv2engine`, serving the nine pauper-kernel catalog decks (and
decklist decks). It is a harness for agents, not a candidate rated engine
(deviations below).

| gorge decision | v2 wire decisions |
|---|---|
| `KPriority` | one priority decision: `pass` first, `play_land`, `cast_spell` (method from the option mode), `activate_mana_ability` (every mana ability; `mana_payment: null`), `activate_ability`, `special_action`; `concede` never. A kicked/buyback/entwined twin of a plain cast is the plain `cast_spell` plus an `optional_cost` decision |
| `KAttackers` / `KBlockers` | `declare_attack` / `declare_block` groups, one substep per creature; each candidate is kept only if a greedy completion passes gorge's `Decision.Validate` |
| `KTarget` | `choose_target` (fixed-count group) or `choose_target` + `finish_target_selection` (one group per decision) |
| `KChoose` object picks | `select_object` / `choose_cost_target` (sacrifice, return, tap costs); library cards appear in `known` with fresh ids (`searching`, `looked_at`) in (name, id) order |
| `KChoose` values | `choose_number` (X), `choose_color` (mana), `choose_boolean`, `choose_name` (card type) |
| `KModes` | `choose_spell_mode`; unless-pay -> `optional_cost unless_payment` |
| `KTriggerOrder` / `KTriggerOptional` | `order_pick triggers` (n-1 picks) / `choose_boolean optional_trigger` |
| `KArrange` | the 2n-1 arrangement group (`arrange_card`, `order_pick arrangement`) |
| anything else | one enumerated `choose_option` over every complete answer gorge accepts (§7.1) |

Declared: `mulligan: ["none"]`, `starting_player: ["host_assigned"]` (gorge's
CR 103.1 choice answered for the named seat), observation flags `keywords`
and `full_name` only (`known_cards` false), `replacement_order` and
`combat_damage_assignment` `engine_order`, no extensions, no probe.

**Setup** (reference host = spellbench `protocol-v2` plus the unmerged game
loop `protocol-v2-p23` and `-p26`, checked out at
`/mnt/sata/gorge-training/spellbench-v2-harness`; venv `sbvenv2`):

```sh
W=/mnt/sata/gorge-training/spellbench-work/v2rev
go build -o $W/bin/sbv2engine ./cmd/sbv2engine
go build -o $W/bin/sbagent ./cmd/sbagent
go build -o $W/bin/sbv2shadow ./cmd/sbv2shadow
PATH=/mnt/sata/gorge-training/sbvenv2/bin:$PATH python cmd/sbv2engine/scripts/sbv2_reverse.py play \
  --engine $W/bin/sbv2engine --dir $PWD/.cards --sbagent $W/bin/sbagent --out $W/r1 \
  --bots uniform,heuristic,sbagent-random,sbagent-heuristic --pairs 4 --workers 3 --truth
python cmd/sbv2engine/scripts/sbv2_reverse.py report --out $W/r1
$W/bin/sbv2shadow -truth $W/r1/truth-w0.jsonl.gz,$W/r1/truth-w1.jsonl.gz,$W/r1/truth-w2.jsonl.gz -belief-dir $W/r1/belief
```

The schedule is the benchmark's shape (every bot pair, seat-swapped pairs,
pair p on deck `pool[p % 8]` in both seats). `--truth` turns on the engine's
test-mode side channel (one gzip JSON line per posed decision: gorge's own
`view.Project` seat view, plus the exact own-library and opponent hand and
library contents) and each Go agent's `-belief-out` log; nothing on it
reaches an agent. Throughput: about 2.7 s per game with three workers
(1,330 wire decisions per game) against 0.1 s for the same games in-process.

**Games (all measured 2026-09-28).**

| Run | Bots | Games | Result |
|---|---|---|---|
| smoke | heuristic, sbagent-heuristic, uniform, sbagent-random; Burn, Faeries | 8 | 8 natural |
| R0 | uniform, heuristic, sbagent-random, sbagent-heuristic; 1 pair | 96 (+72 of an aborted twin run) | all natural |
| before | same, with `sbagent` built at `a4898664a` | 96 | all natural; outcome and step count identical game for game to R0's current agent |
| R1 | same 4 bots, 4 pairs | 384 | all natural |
| R2 | + first, sbagent-first; 1 pair | 240 | all natural |

896 host-driven games, 1,174,855 wire decisions; the Go agents answered
511,532 of them (R1 254,498, R2 154,622; the before binary does not count
its own).

**Protocol errors, before and after.** Counted at the host (forfeits by
cause, halts, validator violations), at the engine (error frames by code,
retransmissions, gorge refusals of an assembled answer, legal options not
offered, source-less degradations) and at the agent (error frames sent,
policy fallbacks). Every count is 0 in every run, before and after: no
error frame, no rejected selection (`invalid_selection`), no timeout, no
resync (`expected_step_mismatch`) or retransmission, no validator violation,
no halt, no truncation, no gorge refusal, no dropped legal option. The one
latent loss path on our side -- a policy failure answered `internal_error`,
which the host turns into an `agent_error` forfeit -- is now answered with
candidate 0 and counted (`sbagent-stats` on stderr); it never fired. Choice
decisions posed as the enumerated fallback: 0.4% (R1: 2,216 of 509,841;
mostly gorge's "Add C / Pay 1: Add any color" ability pick, trigger-cost
pay/decline, madness exile/graveyard).

**Shadow-state check.** The agent's reconstruction is the decoded
observation plus a decklist-arithmetic belief (`v2agent.Belief`: own
library = own list minus own cards seen; opponent hand + library = its list
minus its cards seen). Joined on (game, seat, seat_step) with the truth
channel; mismatch rate by field:

| Field | R1a (91,248 decisions, first build) | R1b + R2 (317,872, final) |
|---|---|---|
| turn, phase, active seat | 0 | 0 |
| life, hand/library/graveyard counts, mana pool (both seats) | 0 | 0 |
| hand / battlefield / graveyard / exile objects: presence, name, controller, owner | 0 | 0 |
| battlefield: tapped, P/T, damage, counters, summoning sick, attacking, token, keywords | 0 | 0 |
| stack: presence, controller, owner | 0 | 0 |
| stack: name | 0.50% | 0.010% (R1b), 0 (R2) |
| hidden: own library multiset exact | 5.68% | 0 |
| hidden: opponent hand + library multiset exact | 5.21% | 0 |
| own library size consistent with `library_count` | 0 | 0 |

The three first-build mismatches were real defects, all fixed: (1) the
catalog named multi-face cards by the front face ("The Modern Age") while
the battlefield showed the back ("Vector Glider"), so the agent could not
match the object to its decklist entry; decklists now use Oracle full names
("The Modern Age // Vector Glider", §4.4), the engine declares `full_name`,
and the belief matches either face. (2) An ability whose source moved to a
hidden zone after activation (Lembas's gain-life once its shuffle trigger
resolved) lost its name; it now keeps the name the viewer already saw. (3)
Ninjutsu activated from the other seat's hand was nameless; activation
reveals the card (CR 602.2a). Caveat: the truth side is gorge's own seat
projection, so this validates our projection and the agent's decode and
belief against gorge, not against an independent rules engine.

**Heuristic parity (exact).** The python heuristic's Go port playing through
this server replays the in-process port of the same bot (`sb-heuristic-manual`,
`internal/spellbench/builtins`) intent for intent in 15 of 16 games (2 seeds
x 8 decks, same gorge seed and starting seat; `TestHeuristicParityWithInProcess`).
The one divergence is spec-mandated: §7.1 orders candidates that reference
hidden cards by (name, id), so a scry-2 "keep both" orders Humbling Elder
above Island where the in-process bot keeps gorge's order.

**Ratings: protocol vs in-process.** Pooled by policy (python builtin and Go
twin together; each pair of twins is statistically the same bot:
heuristic vs sbagent-heuristic 42-38, first vs sbagent-first 8-8):

| Head to head | Protocol (R1 + R2) | In-process (`botbench -spellbench`, 64 pairs) |
|---|---|---|
| heuristic vs uniform | 55.3% (177-143, n 320), +37 Elo | 51.9% (531-493, n 1,024, `-manual`), +13 Elo |
| heuristic vs first | 62.5% (40-24, n 64) | 65.7% (673-351) |
| uniform vs first | 75.0% (48-16, n 64) | 76.0% (778-246) |

All three agree within noise (the largest gap, 3.4pp on heuristic vs
uniform, is 1.1 standard errors). Expected: the heuristic plays the same
games; the uniform bot's candidate lists differ only where the protocol
decomposes differently (the kicker follow-up, the completable-candidate
filter, name-sorted hidden candidates), which changes its draw
probabilities, not its strength class. On gorge's manual surface the
heuristic is barely above uniform, as D§3.1 measured (its "first mana
ability" preference taps lands for nothing).

**Deviations of this server** (why it is a harness, not a rated engine):
one gorge RNG stream for both seats (§11.6 asks per-seat streams; the
adapter's `NewHypotheticalPlanned` planner solves it); `known_cards` false;
`mulligan: none` only; a cast-time choice for an ability being activated
names the permanent as `source` (gorge has no stack object until payment;
the adapter's `ResolveSource` makes the same choice); no probe; the
seat-private arrangement-order and search results are exact but the order
of a pile-B remainder follows gorge's `Rest` convention unverified.

**Notes for Jack's adapter** (for the operator to pass on; no contact made):

- It cannot host a game yet (Tasks 15-23 missing); the pin `26257e0e` is
  hundreds of commits behind gorge HEAD.
- Multi-face names: the pauper-kernel catalog as mtg-kernel publishes it
  names "The Modern Age" and "Sagu Wildling"; §4.4 wants "A // B". The
  adapter's Task 7 already pins full names (good), but then the same
  benchmark deck gets a different `deck_id` on the two engines.
- `KChoose` vocabularies seen on the pauper pool that Annex B does not map:
  a choice between two mana abilities after activation ("Add C" / "Pay 1:
  Add any color", 1 in 250 decisions), `trigger_cost_pay`/`_decline`,
  `graveyard`/`top`, `pay_R`/`pay_G`, `division`, `hand_move`, `returncost`,
  `tapcost`, and `KReplacement` `madness_exile`/`madness_graveyard`. The
  adapter returns `ErrUnmapped` (a halt) for an unknown cast mode; an
  enumerated `choose_option` fallback kept every one of our games alive.
- Ability names on the stack after the source leaves (Lembas) and ninjutsu
  from a hand, above: the observation projector needs both rules.
- Every decision of gorge's that carried `Source: 0` resolved to a source
  through the last-priority-action rule (0 degradations in 896 games), so
  the adapter's `MustSource` halt should not fire on this pool.
- Throughput: about 30x the in-process cost with the python host; the
  adapter's `x_gorge_view_v1` payload (about 14 KB per decision) adds to it.

## 12. Benching plan

### 12.1 Stages

| M | Where | Question | Result slot |
|---|---|---|---|
| M0 | v1 gorge-native (W1) | where do gorge bots and ports of the builtins land on the pauper-kernel decks? | done (D§3.1): bot +238, clairvoyant az +436 over the uniform port |
| M1 | gorge-native botbench | (Q1) L10 with redeal-only worlds vs full sampler; (Q2) honest az25/az100 with the D§5.2 world source vs `bot` | — |
| M2 | v2 mocked backend (W2), then v2 on gorge in reverse (D§11.1) | protocol correctness; shadow fidelity; parity with native answers; leaderboard incl. our agent | protocol + builtin parity done (D§3.2, 927/927 digests); on real gorge 896 games with 0 protocol errors, belief-level shadow 0 mismatches, heuristic intent parity 15/16 (D§11.1); staged-shadow (D§4) fidelity open |
| M3 | v2 on Jack's adapter, `pauper-gorge` (5 decks: Wildfire, Rally, Spy, Burn, CawGates; plan Task 7) | strength vs `gorge-bot`, `gorge-lethal-pressure`, builtins; neutral vs xview entry | — |
| M4 | v2 on mtg-kernel (Annex A bridge), `pauper-kernel` | cross-engine: fidelity and candidate agreement on a foreign engine; rating vs g115 (1388), a48, c12, heuristic (1102) | — |
| M5 | fixed-deck / hidden-list benchmark (§15, when enabled) | deck inference pays off | — |

Coverage note: gorge fully supports 6 of the 9 kernel catalog decks
(missing primitives: Black Mage's Rod kw:Job select in Affinity, Avenging
Hunter api:TakeInitiative in Elves, Saiba Cryptomancer kw:Backup in Faeries;
commit `ab42bea5d`). On M4 those decks run with proxies (D§7) or are excluded.

### 12.2 Sample sizes

SpellBench fits anchored Bradley-Terry on seat-swapped pairs with a paired
bootstrap (`python/spellbench/arena/ratings.py`; [T§7]). Head-to-head, near
50%, one game carries 347 Elo of standard deviation (400/ln 10 / 0.5), so:

| Target | Games (pairs = half) |
|---|---|
| detect +100 Elo (≈ +14pp), 80% power, α 0.05 | 95 |
| detect +50 Elo (≈ +7pp) | 379 |
| detect +23 Elo (≈ +3.3pp, L10's edge) | 1,792 |
| ±50 Elo 95% CI on a leaderboard rating | ≈ 364 (186 games × 1.41², the CI inflation measured on the 2026-09-27 multi-opponent fit; [T§7.3]) |

The published benchmark plays 64 games per matchup (8 decks × 4 pairs × 2
seats), a ±85 Elo interval per matchup: it can rank us against the builtins,
not certify a 3pp gain. Internal decisions are made on our own larger runs.

### 12.3 Kill criteria

| K | Condition | Consequence |
|---|---|---|
| K1 | M1 Q2: honest search does not beat `bot` by ≥ 3pp at ≥ 2,000 games (CI clear of 0) | ship T1 (gorge bot on the shadow) as the agent; search stays in research |
| K2 | M2: < 95% of decisions pass the fidelity gates on the kernel pool | stop; fix staging (H2) before any search on a foreign engine |
| K3 | M4: candidate agreement < 90% on kernel | search off on that engine; publish a divergence report to both engines |
| K4 | any `timeout` forfeit in M3/M4 runs | halve budgets and re-size |
| K5 | a specialist does not beat the generic in its matchup, CI clear of 0 | drop the specialist |

### 12.4 Milestone order

H1 and H2 first (they gate everything), then M1 (cheap, gorge-native, answers
the biggest unknown), M2 with W2's harness, M3 when adapter Tasks 26-28 land,
M4 after the kernel v2 bridge exists, M5 when §15 ships.

## 13. Risks

| Risk | Why it bites | Mitigation |
|---|---|---|
| Rules divergence between engines | gorge simulates, the engine adjudicates; a card gorge implements differently makes search plan for a game that is not being played | candidate agreement gate; per-engine divergence reports; pin skew (D§11) measured |
| Observation → state fidelity for continuous effects | the observation gives post-layer characteristics, not effects or durations (§6.4); a pump misread as permanent (or the reverse) mis-values combat | staged until-EOT deltas; observation-match gate; H2 includes durations |
| Hidden history | delayed triggers, "this turn" counts, zone-entry history are not observable; cards reading them mis-simulate | enumerate such cards per pool from the IR; fallback when one is live (INFERRED coverage) |
| Card coverage | 6/9 kernel decks; FDN/Standard coverage unmeasured | proxies; per-benchmark coverage report before entry |
| Redeal-only worlds lose L10's edge | the edge was measured with behaviour-consistent worlds | M1 Q1 measures it first; importance weighting as the follow-up |
| Clock | Go search at hundreds of decisions per game | fixed budgets sized to 50% bank; clock guard; K4 |
| Opponent model mismatch | the environment plays opponents as `bot`; real opponents (RL nets, kernel bots) differ | measure per-opponent in M3/M4; the AZ loop trains against `bot`, same bias |
| Adapter moves under us | adapter at Task 14 of 30 | align on the published plan; re-verify at Task 26 |

## 14. Open questions for the operator

- **Q1.** Rotating-pool benchmarks are deck mirrors and send the list as
  `visible` (§12.2). Should the agent's prior use the published pairing
  rule (mirror) when the list is hidden, or ignore it as a benchmark
  artefact?
- **Q2.** Enter the `x_gorge_view_v1` variant at all? It is fair but not
  comparable across engines, and Jack's host policy (plan, P5) enables the
  extension only for entries that *require* it; our agent only accepts it.
- **Q3.** Where the agent lives: in gorge (needs H1/H4 only for outside
  consumers) or as a SpellBench-repo module like the adapter (needs H1-H5).
- **Q4.** Offer H2/H3/H5 to Jack as shared infrastructure (one sampler that
  also implements `probe_resample`)?
- **Q5.** Resources for M1 Q2 at 2,000+ games per arm (current AZ budget is
  2 cores / 5 GB).
