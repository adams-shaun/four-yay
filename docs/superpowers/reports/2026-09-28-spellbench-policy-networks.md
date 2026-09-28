# SpellBench policy networks — survey and build plan (v1c, 2026-09-28)

Status: research and design for operator review. No code is specified to the
ticket level; the stages in P§5 are written so each can become one or two
tickets.

Companion to the SpellBench agent design
(`docs/superpowers/specs/2026-09-28-spellbench-agent-design.md`, cited
**D§n**) and its theory notes
(`docs/superpowers/reports/2026-09-28-spellbench-theory-and-references.md`,
cited **T§n**). This document does not repeat T§2-T§7 (PIMC, ISMCTS, deck
posterior, why distillation plateaued, rating statistics); it covers what
they leave out: the learned network, how to train it, and how it plugs into
the agent. `§n` alone is a section of the SpellBench v2 protocol
(`spec/SPELLBENCH_PROTOCOL_V2.md`, SpellBench `main` @ `971a1fb`). **P§n** is
a section of this document.

Sources read: gorge `wt/spellbench` @ `68257c459` (`internal/policynet`,
`internal/azmcts`, `internal/spellbench/v2agent`, `cmd/policytrain`, the
reports named inline); training outputs under `/mnt/sata/gorge-training/`;
the SpellBench clone at `/mnt/sata/gorge-training/spellbench` and the
mtg-kernel clone at `/mnt/sata/gorge-training/mtg-kernel` @ `2c5e72f`.
External papers are listed in P§7 with how each was checked. **INFERRED**
marks our own reasoning and **HYPOTHESIS** marks a claim that a stage below
is designed to test. No new measurements were run for this document; every
number has a source.

## P§0. Summary

1. **The network's job is to serve search, not to replace it.** Every
   gorge attempt to get strength from a search-free policy has matched the
   bot and no more (T§6, P§1). The one strong improvement operator we have
   measured is search (clairvoyant az25: 78% vs bot; L10 PIMC: +3.3pp
   held-out). The network should make search cheaper and better: a leaf
   value, a prior, and a belief over hidden cards.
2. **Train value first.** Value is the cheapest signal (every game labels
   every state), the one we already fit well (AUC 0.83 within matchup) and
   the one search consumes directly. Its weak spot is the early game
   (within-matchup AUC 0.61-0.64 at turns 1-3), which is where PIMC's edge
   lives.
3. **Two value heads, not one.** V_info reads the seat's view and is used at
   the root. V_world reads a full sampled world and is used only at the
   leaves of simulations that already run in a sampled world. That is fair
   (the world is a sample, not the truth), and it should be a lower-variance
   learned rollout (HYPOTHESIS, P§3.7).
4. **Take policy targets only from honest search.** Clairvoyant visit
   counts encode hidden information that a redacted-view network cannot
   express. pn12 measured this for argmax labels: even a network given the
   opponent's hand fit 2-4% of its own training overrides. Stage 1 of the AZ
   plan (clairvoyant loop) is still a useful pipeline proof. Its policy
   targets should not seed the honest network (P§4.3, open question O1).
5. **Use Gumbel root selection with completed-Q targets** instead of PUCT
   visit counts once the loop runs. Our budgets are 25-100 simulations,
   which is the regime Gumbel MuZero was designed for.
6. **One entity encoder, two front ends.** The network's input is an entity
   table built either from gorge's `view.View` (search, training) or from the
   v2 neutral observation (foreign engines, no shadow). A parity test
   requires the two to agree bit for bit on gorge-engine transcripts. The
   candidate head is a pointer-style scorer over the v2 candidate list, so
   one checkpoint serves every engine.
7. **Keep the network small and CPU-only.** Deployment latency is not the
   constraint: the v2 clock allows seconds per searched decision (P§5.4).
   The constraint is self-play throughput. The whole simulation costs 2.6 ms
   today, so a leaf evaluation has to stay well under 1 ms in pure Go. Both
   GPUs on this box are fully occupied by the vLLM seat server (P§4.4).
8. **PPO is last.** We have 80+ flat rounds, and mtg-kernel's own program
   plateaued the same way (P§1). We say what would have to change before
   trying it again (P§4.2).

Ranked recipes (detail in P§4.1):

| Rank | Recipe | Expected value for compute | Gate |
|---|---|---|---|
| 1 | Value pretraining (V_info, V_world) on cheap self-play + existing corpora | high: cheap, directly used as the search leaf | leaf beats heuristic leaf in honest search |
| 2 | BC of the gorge bot in the engine-neutral (v2) format | medium: gives foreign engines a bot-strength fallback (bot 1238 vs sb-heuristic 1080 in W1, D§3.1) | net within 50 Elo of bot |
| 3 | Honest expert iteration (AZ stage 2) with Gumbel targets | highest ceiling, highest cost; only after M1 shows honest search beats bot | AZ spec kill criteria |
| 4 | Belief head (opponent hand) for world weighting | medium, conditional on a large clairvoyant-honest gap | weighted worlds +2pp |
| 5 | PPO / population fine-tuning | low on our history | only for a policy already beyond bot |

## P§1. What is already known (the new facts only)

T§6 tabulates the distillation, ExIt, PPO and 17lands results. Four facts
matter here and are not in T§6:

- **The PPO continuation drifted back toward the bot.** pn14's best arm
  (mz, T=1, residual 0.5) was continued from round 10 to round 28. Eval
  stayed at 48.65-51.45% against a 50.95% control. The share of attackers
  decisions that differed from the bot fell from 49% to 29%, and greedy
  flips per round fell from 7.7% to 1.8%
  (`/mnt/sata/gorge-training/converge-0927/RESULT.md`). Read as INFERRED:
  sampled exploration from a bot-anchored policy mostly finds moves worse
  than the bot's, so the outcome gradient mostly says "go back".
- **The value head is weak exactly where search helps.** Within-matchup AUC
  of the own-corpus value head (`v-con`) is 0.642 at turns 1-3, 0.740 at
  4-6 and 0.882 at 7+ (`/mnt/sata/gorge-training/17lands-spike/score-all.txt`).
  L10's PIMC edge is early-game only: coverage 64.5% at t1-6, 4.8% at t13+
  (training-approaches summary §4). Semantic IR labels (`v-sem-con1k`) and
  17lands FDN data did not change the early buckets (0.609, 0.602 against
  0.610 for `v-con1k`).
- **mtg-kernel's RL program hit the same wall.** Its CP7 progress page
  (`docs/CP7_60_PERCENT_PROGRESS.md`, updated 2026-09-01) records cycle-3
  self-play at 44.7-49.3% against XMage's CP7. It then records a sequence of
  continuations that did not move the policy. Stronger training opponents,
  a structured residual, and policy-only updates each ended within 1pp of
  g896. After 256 updates the policy's greedy action had changed on only
  0.41% of contestable decisions. The value gradient was 18.7 times the
  policy gradient. Its next lever is explicit opponent-belief features
  (56 deck-counting features, step 12I). That is our P§3.8.
- **g115 is a pool-trained, search-free sampled policy.** It leads
  `pauper-kernel` at 1388 [1322, 1464] (320 games;
  `benchmarks/pauper-kernel/runs/2026-09-27/LEADERBOARD.md`).
  - What SpellBench publishes. `benchmarks/pauper-kernel/benchmark.json`
    calls it an "mtg-kernel policy network, Phase 1 lineage ... sampled, no
    search": lineage g, block 115, 32,400 updates. It was trained on 6 of the
    8 benchmark decks plus Terror, and never saw Spy or CawGates. a48 has
    8,800 updates and c12 4,000 (c12 saw all 8 decks). The kernel-models
    plan (`docs/design/2026-09-27-kernel-models-plan.md`) adds that it reads
    the kernel's V4 flat-action input and plays a seeded sample, not argmax.
  - What mtg-kernel's public code shows. The branches were read by a
    delegated web check, not by us: `lead/phase1-campaign-001-v1` and
    related. The network is "Net8" (`kernel-policy-value-net-8`). The V4
    widths are state 219, object 98, edge 41, action 195, action-reference
    25, with 20 object groups. The update is a "terminal-only
    REINFORCE/value trainer": REINFORCE with a value baseline on the game
    outcome, not PPO. It trains by self-play against current, historical
    and specialist opponents.
  - The repository's Python reference model (`python/mtg_kernel_rl/model.py`,
    `KernelPolicyValueNet`, read by us) has hidden width 64 and 16-wide card
    embeddings. It runs an object encoder, one round of edge messages over
    typed relations, and pooling per object group. An action encoder pools
    the object encodings each action references. A scorer over
    [state ‖ action] produces one logit per legal candidate, and a value head
    sits alongside. There is no attention.
  - Unverified: that Net8 is exactly this shape (inferred from the matching
    dimensions), and the campaign-002 hyperparameters, which are
    unpublished.
  - Naming caution: "Phase 1" in mtg-kernel's `ROADMAP.md` and CP7 page is
    a different thing.
  - **Its rating is not comparable to gorge's bot.** g115's 1388 is on the
    kernel engine; the bot's 1238 is gorge-native, with ports of the
    builtins (D§3.1).
  - **What it tells us.** A small entity/graph network with candidate
    scoring, trained by plain terminal REINFORCE on a closed pool at a scale
    of tens of thousands of updates, is the strongest entry today. Our PPO
    runs were 10-28 rounds of 2,000 games. The ingredients differ from ours
    in scale, pool-specific training and self-play opponents, not in the
    algorithm (INFERRED).

## P§2. Survey

Each entry says what the work is and what we take from it. Citations are in
P§7. Anything not checked at the source is marked there.

### P§2.1 Search with a learned evaluator

| Work | Idea | What we take |
|---|---|---|
| AlphaGo (Silver et al. 2016) | policy network from human games (BC), then RL; value network; MCTS with policy prior and mixed value/rollout leaf | BC warm start is a legitimate first stage; the value network was trained on self-play positions, one per game, to avoid overfitting correlated states |
| AlphaGo Zero / AlphaZero (Silver et al. 2017, 2018) | no human data; search visit distribution as policy target, outcome as value target; residual tower; PUCT | the loop itself (AZ spec §1); soft targets |
| MuZero (Schrittwieser et al. 2020) | learned dynamics model instead of a simulator | not needed: gorge is an exact, fast simulator. Its value target (n-step bootstrapped from search) is the root-value blending the AZ spec already uses |
| Gumbel MuZero (Danihelka et al. 2022) | root action selection by Gumbel-Top-k + sequential halving; policy target from "completed Q-values"; provable policy improvement even at very few simulations | **use it**: our budgets are 25-100 simulations and PUCT visit counts at that budget are noisy, prior-dominated targets |
| Sampled MuZero (Hubert et al. 2021) | search over a sampled subset of a large action space, with corrected targets | a principled version of azmcts's `Limit: 6` priority cap (`internal/azmcts/options.go:76`) |
| MCTS as regularized policy optimization (Grill et al. 2020) | visit counts approximate the solution of a KL-regularized policy optimization; the exact solution is a better target at low simulation counts | same conclusion as Gumbel: don't train on raw low-count visits |
| KataGo (Wu 2019) | playout-cap randomization (cheap searches for most moves, full searches for training rows), auxiliary targets (score, ownership) for sample efficiency | cheap/full search split for data generation; auxiliary heads (P§3.7) |

### P§2.2 Imperfect information

T§2-T§4 cover PIMC, strategy fusion, ISMCTS and the MTG determinization
results. Additions:

| Work | Idea | What we take |
|---|---|---|
| DeepStack (Moravčík et al. 2017), Pluribus (Brown & Sandholm 2019) | poker: depth-limited search over public belief states with learned counterfactual values (DeepStack); blueprint + real-time search (Pluribus) | sound, but built for small action sets and a common-knowledge public state; poker's belief is over 1,326 hands, ours over deals of a 60-card list |
| ReBeL (Brown et al. 2020), Student of Games (Schmid et al. 2023) | self-play RL + search on public belief states; unified perfect/imperfect-information algorithm | the principled endpoint. Cost in Magic untested (T§4). Not a first step |
| DeepNash (Perolat et al. 2022) | Stratego without search at test time: R-NaD (regularized Nash dynamics), a model-free game-theoretic RL method | evidence that a search-free policy *can* be strong under hidden information, at DeepMind scale; the regularization-to-a-reference idea is what our KL anchor approximates |
| Suphx (Li et al. 2020) | Mahjong: "oracle guiding", which trains first with hidden information in the input and then drops it out gradually; global reward prediction; run-time policy adaptation | the cleanest published use of privileged information in training: privileged input is annealed away, never distilled from a privileged teacher's argmax. Also a cheap test for us (P§4.3) |
| PerfectDou (Yang et al. 2022) | "perfect-training-imperfect-execution": the critic (value) sees all hands, the actor sees only its own | **the direct precedent for V_world** (P§3.7): privileged information in the value used for training/search, never in the policy's input |
| DouZero (Zha et al. 2021) | DouDizhu with Deep Monte-Carlo: the action is encoded as a feature vector and scored jointly with the state, over a variable legal-action list; no search | candidate-conditioned scoring works at scale for a card game with a combinatorial action set |

### P§2.3 Variable action sets and entity encoders

| Work | Idea | What we take |
|---|---|---|
| Pointer Networks (Vinyals et al. 2015) | output is an attention distribution over input elements | "choose one of these objects" heads: targets, blockers, cards |
| DRRN (He et al. 2016), Dulac-Arnold et al. 2015, Chandak et al. 2019 | embed each available action and score by an interaction with the state embedding (DRRN), or act in a learned action embedding space | our candidate head: embed each v2 candidate, score against the state |
| Deep Sets (Zaheer et al. 2017), Set Transformer (Lee et al. 2019) | permutation-invariant set functions: per-element encoder + pooling (Deep Sets); self-attention among elements (Set Transformer) | the board is a set of permanents; pn14's entity encoder is Deep Sets with sum+max pooling |
| AlphaStar (Vinyals et al. 2019) | transformer over units (entity encoder); "scatter connections" project unit embeddings back to their map positions; autoregressive action heads; pointer network selects units | entity encoder + pointer selection + autoregressive decomposition are exactly the v2 shapes (candidates reference objects; groups are autoregressive) |
| OpenAI Five (Berner et al. 2019) | unordered sets through a set-processing module, a single 4,096-unit LSTM; the primary action is a dot product between the LSTM output and embeddings of the currently available action ids; the target unit is chosen by attention over available units; "surgery" to keep training through architecture changes | dot-product scoring of available actions is exactly the variable-candidate head; checkpoint surgery is our `UpgradeEntity` warm start (`internal/policynet/entity.go:500`) |
| Action branching (Tavakoli et al. 2018) | factor a joint action into per-dimension heads with a shared trunk | the alternative to autoregression for attack/block assignments; we prefer autoregression because v2 already poses substeps in order |
| Invalid action masking (Huang & Ontañón 2022) | masking invalid logits is a valid policy gradient and is what makes large masked action spaces trainable | the v2 list is already legal-only; the lesson applies to `concede`, which R1 §1.3 says must be hard-masked |

### P§2.4 Card games

| Work | What it did | Relevance |
|---|---|---|
| Ward & Cowling 2009 | Monte Carlo search for card selection in a simplified MTG | the first MTG MCTS result; rollouts plus a rule-based baseline |
| Cowling, Ward & Powley 2012 | ensemble determinization MCTS for MTG | T§3: many shallow worlds beat few deep ones; binary decomposition of compound moves |
| Whitehouse, Powley & Cowling 2011; Cowling, Powley & Whitehouse 2012 | determinization vs ISMCTS on Dou Di Zhu; the ISMCTS paper | T§3 |
| Zhang & Buro 2017 | Hearthstone: learned high-level rollout policies; bucketing chance-node outcomes | chance bucketing is how to keep draw nodes from exploding a tree; we avoid chance nodes entirely by sampling worlds |
| Świechowski et al. 2018 | Hearthstone: MCTS with supervised-learned state evaluation and move pruning | a small learned evaluator improves MCTS in a CCG; the pattern we follow |
| Xiao et al. 2023 | Hearthstone: end-to-end RL without search; the action is an autoregressive (type, target) pair under a legal-action mask; LSTM and shared card embeddings; V-trace + UPGO; beat a top-10 China-ladder streamer | autoregressive (what, then which) decomposition with masking works at scale in a CCG |
| Xi et al. 2023 (ByteRL), Haluska & Schmid 2024, Rubin 2026 | Legends of Code and Magic: one end-to-end policy for draft and battle, trained with optimistic smooth fictitious play (ByteRL won both COG 2022 tracks); ByteRL is highly exploitable; unsound search with policy/value nets (T§4) | search on top of a trained net added +24.6 points in LOCM; strong search-free agents are exploitable |
| Kowalski & Miernik 2023 | five-year summary of the LOCM competitions | where CCG agent competitions ended up |
| Legends of Runeterra | no peer-reviewed or arXiv RL paper found; Riot described a self-play RL agent used for balance testing (about 48% against top humans, up to 10 decks) in a 2022 Anyscale blog post | industrial evidence only |
| da Costa Cunha et al. 2026 | a Gymnasium MTG benchmark with a 478-action masked discrete space and masked-PPO baselines | the only recent MTG RL benchmark found; its fixed action space is the design we avoid |
| Forge AI | see P§2.8 | the production MTG AI our card corpus comes from |
| MageZero | AlphaZero-style MCTS + transformer over sparse tokens on an XMage fork; deck-local | T§4; rising curve but clairvoyant training |
| mtg-kernel / g115 | self-play population training over a fixed pool, terminal REINFORCE with a value baseline, object-graph policy/value net with candidate scoring | P§1 |
| Ward et al. 2021; Bertram et al. 2021 | MTG draft pick prediction from human draft data (Draftsim; contextual preference ranking); learned card embeddings | card-identity embeddings learn useful structure from pick data; relevant to open formats, not to closed pools |

### P§2.5 Imitation and iteration

- **Behaviour cloning** is supervised learning on a teacher's actions. It is
  capped at the teacher, and it degrades under distribution shift: the
  student's errors take it to states the teacher never showed it.
- **DAgger** (Ross et al. 2011) fixes that shift. The student plays, the
  teacher labels the states the *student* visits, and the student is
  retrained on the aggregate. gorge can do this for free: the bot can label
  any state. pn06's attackers net overrode 34% in play against 1% on
  holdout, which pn13 traced to an admission bug, not to states. DAgger is
  still the right default for any BC stage (P§4.1).
- **Expert iteration** (Anthony et al. 2017) alternates a search "expert"
  and a network "apprentice" that imitates it, and the apprentice then
  guides the next expert. AlphaZero is the same loop.
- **PPO** (Schulman et al. 2017) is a clipped policy-gradient update. It
  improves a policy only as far as outcome-weighted gradients separate good
  actions from bad, which is the part that failed here (P§4.2).

### P§2.6 Opponent modelling and deck inference

- **DRON** (He et al. 2016) conditions a Q-network on a learned
  representation of the opponent's behaviour. mtgbld measured the same
  lever as matchup conditioning (+27pp for mono-green, training summary
  §10), but the opponent-deck tag turned out to be noise there.
- **Hearthstone deck and card prediction.** Bursztein & Bursztein's 2014
  DEF CON talk predicted the opponent's cards from cards already seen.
  Dockhorn et al. 2018 mined card co-occurrences from replays to feed
  search. Eger & Sauma Chacón 2020 predicted deck archetypes. On closed
  pools, T§5's Bayesian posterior does the same job exactly, and the list is
  identified after a median of 1-2 revealed cards (T§5.3).
- **INFERRED consequence:** on `pauper-kernel` a learned deck head adds
  nothing over exact Bayes. The unknown that matters is the opponent's
  *hand* given what they did and did not do. That is non-locality (T§2).
  PIMC with uniform redeal ignores it, and a learned hand model can supply
  it (P§3.8).

### P§2.7 Card representation

Networks see a card either by an identity embedding (one learned vector
per name) or through features derived from the card's rules (text, or
gorge's compiled IR). Identity embeddings are free and precise for cards
seen often in training. Derived features are the only way to generalise to
a card never seen. gorge measured the choice once: IR semantic labels added
nothing to value AUC (0.805 both, T§6) on its own deck set, where every card
is common. That tells us nothing about open formats. Published work on
generalising to unseen cards grounds the card in its text:
- Cardsformer (Xia et al. 2023) grounds Hearthstone card text for unseen
  cards;
- Bertram et al. 2024 use generalised text/feature card representations for
  MTG drafting.

gorge's compiled IR is a more exact equivalent of that text. No verified
paper applies a transformer over cards to an MTG gameplay policy.
HYPOTHESIS: use both, with identity dropout during training, so the net
learns to fall back on IR features (P§3.3).

### P§2.8 Forge's AI

Checked by the delegated web pass against Card-Forge/forge `master` @
`2ccbbb0`, 2026-09-27. Paths are under `forge-ai/src/main/java/forge/ai/`.

- **Default: rules per ability API.** `AiController` enumerates playable
  spells and abilities and asks a handler for each one. `SpellApiToAi` maps
  each `ApiType` to a `SpellAbilityAi` subclass (`ability/`, about 150
  files). Each handler answers with an `AiPlayDecision` such as `WillPlay`,
  `WaitForMain2` or `CantAfford`. Card scripts steer it with `AILogic$`.
  Attacks and blocks live in `AiAttackController` and `AiBlockController`.
- **Optional simulation.** `AIOption.USE_FULL_SIMULATION` routes spell
  choice to `simulation/SpellAbilityPicker`, which copies the game
  (`GameCopier`), applies each candidate, resolves the stack and scores the
  result with a hand-written `GameStateEvaluator`. `SimulationController`
  recurses to depth 3 over the AI's own actions only, with no opponent
  model.
- **Hybrid mode** lets the rules decide, with a safety checker vetoing
  unsafe plays.

The copy keeps hidden information (`GameCopier.PRUNE_HIDDEN_INFO = false`),
so Forge's simulation is **clairvoyant**. It is a shallow, greedy,
cheating lookahead with a hand-tuned evaluator, and it involves no learning.
Our `bot` belongs to the same heuristic tradition. Forge offers nothing to
borrow on the learning side.

## P§3. Architecture for our net

### P§3.1 Requirements

| Requirement | Source | Consequence |
|---|---|---|
| Seat-visible input only at decision time | §13 F1-F3, D§10 | V_world may read hidden cards only of a *sampled* world inside search (P§3.7); never of the real game |
| Engine-neutral | D§1 goal: any v2 engine | the input can be built from the v2 observation alone |
| Variable candidates, up to 4,096 | §7.1 | pointer/candidate scoring, not a fixed output layer |
| Decision groups | §8 | autoregressive substeps, or a plan at substep 0 (search path) |
| Deterministic | §11.4 "fixed search budgets ... seeded from `agent_seed`"; gorge's culture | float32 with fixed summation order (policynet's existing discipline, `net.go` header) |
| CPU-only, 4 cores, 16 GB, no GPU at inference | §11.4 example `resources` | pure-Go inference |
| Fast enough for self-play generation | P§4.4 | leaf evaluation well under the 2.6 ms simulation cost |
| No third-party deps in the rules core | AGENTS.md | inference in pure Go; trainer language is open question O2 |

### P§3.2 What `internal/policynet` already has

| Component | Where | Reusable? |
|---|---|---|
| Hand-derived forward/backward with finite-difference test; deterministic float32 | `net.go` | yes, the discipline and the test pattern |
| Shared hashed embedding table (16,384 rows × H=128 by default) | `policynet.go:34`, `cmd/policytrain/main.go:42` | partly: keep hashing as the fallback for unknown names; use an explicit vocabulary for the benchmark's `card_name_domain` |
| Per-option scorer: MLP over [state trunk ‖ option features] | `net.go`, `option.go` | yes: this *is* DRRN-style candidate scoring; the option encoder must be rewritten over v2 semantics |
| Entity encoder: per-card MLP (68 raw scalars + hashed rows → K), sum+max pooling in 4 groups, option gets its own and a related card's encoding | `entity.go` (pn14) | yes: the Deep Sets encoder P§3.4 starts from; extend groups and the raw layout |
| Value head on the state trunk | `net.go` (pn08) | yes; add V_world as a second model, not a second head (different input) |
| Value-only training | `valueonly.go`, `cmd/policytrain/valueonly.go` (pn17) | yes, stage S1 |
| PPO with behaviour log-prob, KL anchor, sampling seat | `ppo.go`, `onpolicy.go`, `seat/policynet.go:160` | keep for S5; not on the critical path |
| Subset scoring for attackers/blockers (per-option Bernoulli, `CandidateScore`) | `candidate.go`, `internal/searchseat/prior.go:155` | superseded on the v2 path by autoregressive substeps; keep for the gorge-native az prior |
| Diagnostic hidden-info feature sets that refuse to checkpoint | `features.go` (`mz-opphand`, `mz-oracle`) | the pattern for V_world, with a new rule: checkpointable, but loadable only as a sampled-world leaf |
| Residual bot prior | `net.go` `ResidualW` | **drop** for new models: pn13/pn14 showed it pins priority; the bot enters as candidate 0 in search instead (AZ spec §1) |
| Checkpoint schema with encoder hash | `checkpoint.go` | yes; new schema version for the v2 entity encoder |

What is missing is an input built from the v2 observation. Everything
today encodes gorge's `view.View` and `decision.Option`.

### P§3.3 Observation encoding: the entity table

One row per visible object, plus one row per player and one global row.
Built by one of two front ends (P§3.9):

| Row | Features |
|---|---|
| object (battlefield, hand, graveyard, exile, command, stack, `known` hidden cards) | zone and controller/owner relative to us; characteristics after continuous effects (§6.4: types, subtypes, colors, mana value, P/T, keywords); permanent state (tapped, sick, damage, counters, attached-to, attacking/blocking and whom, phased out); token/copy/face-down; stack entry kind, targets (as edges); `known` provenance (`how`); **in-group flags** (already picked in the current decision group, P§3.6) |
| card identity | vocabulary index for the name (from `card_name_domain`, §12.2), hashed fallback for anything outside it; **identity dropout** during training (HYPOTHESIS: p 0.2-0.5, tuned on a held-out-card split) |
| card IR features | from gorge's compiled IR when the name resolves: ability APIs, cost shape, trigger modes, static modes. Same family as the `mz-sem` spike, which showed no gain on a closed deck set; kept for open formats (P§2.7) |
| player (×2) | life, poison, counters, mana pool, lands played, hand and library counts, designations, mulligans |
| global | turn, step, active/priority seat, stack depth, decision kind and purpose |
| belief (optional) | from the exact deck posterior (T§5): posterior probability of each candidate list; expected remaining copies of each card name in the opponent's library+hand. mtg-kernel is testing the same idea (P§1) |

Relations (who attacks whom, who blocks whom, who is attached to what,
which stack entry targets what) are edges between object rows. mtg-kernel's
reference model runs one message-passing round over typed edges. pn14's
entity encoder has none and only gives an option its "related card".

### P§3.4 Encoder

Start with pn14's shape, widened:

1. Per-row MLP: `h_i = relu(W2 · relu(W1 · [raw_i ‖ E[name_i] ‖ IR_i]))`, width
   d = 32-64.
2. One edge round (optional, S1 ablation): `h_i += Σ_{j→i} relu(We · [h_j ‖ edge_type])`.
3. Pooling per group (mine/theirs × battlefield/hand/graveyard/stack/other):
   sum (scaled) and elementwise max, which pn14 already does for 4 groups.
4. State trunk: MLP over [pooled ‖ player rows ‖ global].

**Why not a transformer first.** A cost estimate (INFERRED arithmetic,
to be measured in S0) at about 60 rows. The per-row MLP at d=64 is about
0.75 M multiply-adds. One self-attention layer at d=64 (QKV, 60×60 scores,
output projection, a 2× FFN) adds about 2.4 M. Scalar pure Go sustains
about 1-2 GFLOP/s per core. That makes attention about 2.5-5 ms per
evaluation, against the 2.6 ms a whole simulation costs today
(`/mnt/sata/gorge-training/az/stage0/az25.txt`: 2.633 ms/simulation). It
would roughly double self-play cost. Add attention only if an S1 ablation
shows interaction effects the edge round misses (combat is the likely
case), and then restrict it to creatures on the battlefield.

### P§3.5 Candidate scoring

Each v2 candidate (§7.2, §7.3) becomes a vector:

- `kind` one-hot (6 priority + 24 choice kinds), with `method`, `purpose`,
  `cost`, `cost_kind`, `action`, `destination` and `event` one-hots from
  the §7.4 vocabularies;
- **pointer features**: the entity encodings of every object the semantic
  references (`source`, `target`, `card`, `candidate`, `attacker`,
  `blocker`, `item`, `affected`, `choice`), as pn14's `EntA`/`EntB` do.
  A player reference points to that player's row;
- scalars: `value`, `minimum`, `maximum`, `selected_count`,
  `mode_index/mode_count`, `amount/remaining`, `position/count`,
  `pay`/`keep`/`cast_it` bits;
- for `choose_name`: the named card's identity and IR features, as if it
  were an object row.

Score: `z_c = MLP([state ‖ cand_c])`, which is policynet's current form.
The cheaper alternative is `z_c = ⟨g(state), f(cand_c)⟩` (DRRN / OpenAI
Five). Its cost is linear in candidates and it matters for 4,096-candidate
`choose_name` decisions. Softmax over the list. `pass` needs no special
case: it is candidate 0 whenever legal (§7.1), and its vector is its kind.
**Hard rule carried from R1 §1.3:** a concede candidate, if an engine ever
offers one, is masked.

Mapping to gorge-native decisions: the same head scores gorge
`decision.Option`s once they are projected to v2 semantics. That is the
mapping D§6 already needs to answer v2 candidates from a gorge intent, run
in reverse.

### P§3.6 Multi-step decisions (groups, attacks, blocks)

- **Net-only path** (foreign engine, no shadow). Score each substep
  independently, and carry the group's earlier picks as in-group flags on
  the entity rows ("declared attacking in this group", "already chosen as
  target slot 0"). This is AlphaStar's autoregressive head without a
  recurrent state: the flags carry what the recurrence would. It costs one
  forward per substep. Attack groups are one decision per potential attacker
  (§7.5), so a 6-creature board costs 6 forwards, which is negligible.
- **Search path** (shadow available). Decide the whole group at substep 0
  as one gorge intent and match later substeps to the plan (D§3.2). The
  prior over candidate *sets* comes from the autoregressive product of
  substep probabilities. This replaces the per-option Bernoulli
  `CandidateScore` for the v2 path. Enumerating sets stays azmcts's job
  (`AttackCandidates`/`BlockCandidates`).
- Arrangements (2n-1 decisions) and order blocks use the same flags
  ("placed at position k").

### P§3.7 Value heads

| Head | Input | Trained on | Used at |
|---|---|---|---|
| V_info | the seat's entity table (P§3.3) | outcomes, blended with search root values (AZ spec §1 λ schedule) | the search root and the net-only fallback; also the baseline for any PPO |
| V_world | the entity table of a *full* state: both hands, library order to a fixed depth | outcomes of the same games, with the hidden zones as they really were | leaves of simulations that already run in a sampled world |
| auxiliary (optional) | shared trunk | turns remaining, final life difference | sample efficiency (KataGo-style); drop if it does not raise AUC |

V_world is legitimate for the same reason PIMC is. In a simulation the
"hidden" cards are the sample, drawn from the belief, not the real ones. It
is a learned replacement for a rollout of the environment policies from a
full state. Both seats in those rollouts play on their own views, so it
does not assume clairvoyant play. It inherits PIMC's strategy-fusion error
and adds none of its own. **HYPOTHESIS H-V:** V_world is markedly
more accurate than V_info in the early game, where V_info's within-matchup
AUC is 0.61-0.64 (P§1), because much of early-game uncertainty is the
opponent's hand. S1 measures it directly. Precedent: PerfectDou's
perfect-information critic (P§2.2).

Leak discipline: V_world checkpoints are a separate type. The agent may
call them only through the world source's leaf path, and the D§10 leak test
covers them (vary the real game's never-revealed hidden cards; answers
identical).

### P§3.8 Belief heads

- **Deck list:** exact Bayes (T§5), not learned. It is already near-certain
  after 1-2 reveals on closed pools. Learning it would only add error.
- **Opponent hand (HYPOTHESIS, S4):** a multi-label head
  `p(card name ∈ opponent hand | public history, list)`. It is trained
  supervised from self-play logs, where the true hand is known, which gives
  unlimited data. Its input needs the public history: which cards the
  opponent had mana for and did not cast. Summarise that as per-turn
  features (untapped mana at each of their priority passes, cards drawn)
  rather than a recurrent network. Uses:
  1. importance-weight redealt worlds (D§5.2's improvement path);
  2. feed the policy the belief (mtg-kernel's step-12I idea).
  Worth building only if the clairvoyant-vs-honest search gap is large
  (P§5, S4 gate). That gap is the most any belief can recover.

### P§3.9 Two front ends and parity

```
gorge view.View + decision ──(front end G)──┐
                                            ├─▶ entity table + candidate table ─▶ encoder/heads
v2 Observation + candidates ─(front end V)──┘
```

- Front end G: training data generation, azmcts priors and leaves. It must
  also produce V_world's full-state table.
- Front end V: the SpellBench agent's root decision and the net-only
  fallback (D§7 new tier "T1.5": net on the v2 observation when the shadow
  fails, above the neutral heuristic).
- **Parity test:** on gorge-engine transcripts (Jack's adapter with
  `x_gorge_view_v1`, or gorge's own projector once H5 lands), G(view) and
  V(observation) must produce byte-identical tables. It is the same idea as
  D§4.3's fidelity check, one level up. Any residue is a projection bug,
  and it would otherwise show up as a silent train/deploy skew.

### P§3.10 Size

At d=64 with a 16-wide identity embedding over a pool vocabulary of at
most a few thousand names, the model is about 0.1-0.3 M parameters,
dominated by the embedding and the first layers (INFERRED). Today's
policynet default is about 2.1 M, almost all hashed-table rows
(16,384 × 128). The mtg-kernel reference model is the same order as the
new one (hidden 64, card embedding 16). Checkpoints are single-digit MB and
load well inside `startup_ms` (300 s).

## P§4. Training recipes

### P§4.1 Ranked by expected value for compute

| # | Recipe | Signal | Cost on this box | Evidence | Expected value |
|---|---|---|---|---|---|
| 1 | **Value pretraining** (V_info, V_world) | every state labelled by its game's outcome; λ-blend with search root values once they exist | data: bot-vs-bot at 57 games/s on 2 vCPU (training summary §6); training: 30 K rows/s single-threaded at H=128 (R1 §3.3), so a 10 M-row epoch is about 6 min, ~1.7× that for entity models (pn14 train 13.2 s vs 7.8 s) | value fits outcomes well (T§6; AUC 0.833); mtgbld/pn09 warn that good AUC does not guarantee better move ranking | **high**, directly consumable as the leaf, measurable offline, pretrains the entity encoder pn14 said was never trained |
| 2 | **BC of the bot, v2 format, with DAgger** | the bot's answer at every decision (and at the student's own states) | same data pipeline as #1 | BC matches its teacher and no more (T§6); the bot is +158 Elo over sb-heuristic in W1 (D§3.1) | **medium**, capped at bot, but it is the best fallback for foreign engines and the prior for #3's generation 0 |
| 3 | **Honest ExIt / AZ stage 2 with Gumbel targets** | completed-Q policy targets from honest search over sampled worlds; values from outcomes + root value | roughly 10 min per 1,000-game generation at 100 simulations on 24 cores, about 2 h on 2 (INFERRED, P§4.4) | clairvoyant gen-0 search +28pp over bot; honest search unmeasured (M1 Q2); ExIt with a weak or confounded teacher was flat (pn11) | **highest ceiling**, gated on M1 |
| 4 | **Belief head** | supervised on true hands from self-play logs | cheap to train; cost is in the world-weighting integration | none yet in gorge; mtg-kernel testing belief features | **conditional** on the honest-vs-clairvoyant gap |
| 5 | **PPO / population self-play** | outcome advantage | 10 min per 20 K-game round at 22 cores (pn14) | 80+ flat rounds here; mtg-kernel plateau | **low** unless P§4.2's conditions change |
| – | 17lands human data | human actions and outcomes | staging solved (94% legal) | no value gain; hurt within-matchup AUC (T§6) | only for Limited, as a BC prior, if we ever enter Limited |

### P§4.2 Why PPO went flat, and what would change it

Measured causes (pn13, pn14, converge-0927):

1. **The update had nothing to amplify.** With the residual prior at 2,
   priority never deviated from the bot. With it at 0.5 and T=1 sampling,
   22-61% of decisions deviated, and almost all deviations were worse than
   the bot's move.
2. **The outcome signal is too weak for the eval noise.** 2,000 games per
   round carry about 20,000 decisions of advantage signal, against ±1.5pp
   eval noise (pn14 verdict 3). The value baseline is weakest early
   (P§1), so early-game advantages are mostly noise.
3. **Sampling explores in the wrong place.** Uniform temperature spreads
   exploration over trivial decisions as much as over the few that matter.
4. **The opponent was fixed.** A policy that beats `bot` only by small
   exploits has little to learn from 50% games.
5. (mtg-kernel, same shape) **the value gradient swamps the policy
   gradient** in a shared trunk: 18.7× in their audit.

What would have to change before PPO is worth another round (INFERRED):

- **A stronger improvement operator than the gradient.** Search supplies
  it. With search in the loop the target is "what search found", not
  "which sampled action happened to win". This is AZ, recipe #3.
- **A value baseline that is good early:** recipe #1, or V_world
  advantages computed in hindsight on full states, which is legitimate
  for a training-only baseline.
- **Exploration focused on contested decisions:** sample only where the
  search's improved policy and the prior disagree, and play greedy
  elsewhere (KataGo's cheap/full split, applied to exploration).
- **10× the games per round** (pn14's own recommendation: 20 K games per
  round, about 10 min at 22 cores).
- **Separate or stop-gradient value and policy trunks**, or at least
  measure the gradient ratio as mtg-kernel did.
- **Scale, if we try search-free RL at all.** The one search-free entry
  that clearly beats heuristics, g115, is plain terminal REINFORCE with a
  value baseline. Its differences from our runs are 32,400 updates, a
  self-play population and training on the benchmark pool (P§1). Our
  largest run was 28 rounds. Matching that scale is days of cores (P§4.4).
  So this is a question of budget, not of a missing algorithm (INFERRED),
  and it is why PPO stays ranked last and optional.

### P§4.3 Privileged information: where it is allowed

| Use | Allowed? | Why |
|---|---|---|
| Policy input at decision time | no | fairness contract (§13) |
| Policy *targets* from clairvoyant search | allowed, but **not recommended** | pn12: override labels from an oracle teacher are unlearnable from the seat's view (2-4% fit even with the hand as input); they teach the net to chase luck |
| Value input inside sampled-world search (V_world) | yes | the "hidden" cards are the sample (P§3.7) |
| Training-only baseline / critic | yes | never reaches a decision (PerfectDou) |
| Annealed privileged input (Suphx oracle guiding) | yes, as an experiment | the privileged features are dropped to zero before deployment; cheap to try within S1 on V_info |

This changes one thing in the AZ spec's staging: stage 1's clairvoyant
loop should be treated as a pipeline proof, not a source of
policy targets for the honest network (open question O1).

### P§4.4 Compute on this box

| Resource | Measured now | Note |
|---|---|---|
| CPU | AMD Ryzen 9 9950X, 16 cores / 32 threads (`lscpu`) | shared with other sessions; the memory rule is one heavy job at a time |
| RAM | 58 GB total, about 11 GB available at the time of writing (`free -g`) | memory rule: `systemd-run` scope, `MemoryMax=4G`, `GOMEMLIMIT=1GiB` |
| GPU | 2× NVIDIA RTX PRO 6000 Blackwell, 96 GB each (`nvidia-smi`) | **both at 99-100% utilization with 97 GB used by the vLLM seat server** (`VLLM::Worker_TP0/TP1`); unavailable for training unless the operator frees one |
| AZ budget | 2 cores / 5 GB (AZ spec §3, D§14 Q5) | every estimate below is quoted at 24 cores and at 2 |

Throughput anchors (all measured, sources inline):

- engine, bot vs bot: 57 games/s on 2 vCPU (training summary §6);
- policynet seat, sampled, mz: 118 K games/h on 2 vCPU (pn14);
- clairvoyant az25: 65.8 ms mean per searched decision, 38.5 searched
  decisions per game, 2.633 ms per simulation (az stage 0);
- policynet forward, H=128: 9.4-20.9 µs for 2-33 options (R1 §3.2);
- Go SGD, H=128: 29.8 K rows/s single-threaded, 168 K rows/s at 32
  workers (Hogwild, not reproducible) (R1 §3.3).

Estimates per stage (INFERRED from the anchors; S0 re-measures):

| Stage | Work | 24 cores | 2 cores |
|---|---|---|---|
| S1 data | 200 K bot-vs-bot games with full-state dumps (the dump cost is unmeasured; assume 10× the play cost) | ~1 h | ~10 h |
| S1 train | 10 M rows × 5 epochs, entity model, single-threaded deterministic | ~50 min (training is single-threaded either way) | same |
| S3 generation | 1,000 games × 38.5 searched decisions × 100 sims × (2.6 ms + ≤0.5 ms leaf) + world sampling (unmeasured) | ~10 min | ~2 h |
| S3 eval | 2,000 games per arm at the same cost (the 3pp detection size, D§12.2) | ~20 min | ~4 h |
| S3 loop | 10-20 gated generations | 5-10 h | 2-5 days |

Two conclusions follow. At 2 cores, S3 is a multi-day job per
configuration, which is question O4. The model does not need a GPU at
this size, and nothing in S0-S4 is blocked on one.

### P§4.5 Data volumes

- Value: hundreds of thousands of games are cheap (above). The binding
  quantity is *independent games*, not rows, because rows in one game share
  an outcome. pn08 put the effective sample of a small corpus at about 150
  games. AlphaGo sampled one position per game for the same reason.
  **Recommendation:** subsample a few rows per game per turn bucket and
  spend the budget on games.
- BC: ~164 real decisions per game in gorge (R1 §1.3; forced
  pass/concede rows dropped), so 100 K games give ~16 M decision rows.
- ExIt: ~38 searched decisions per game, so a 1,000-game generation gives
  ~38 K policy targets. MageZero used about 1,000 games per generation.

## P§5. Build plan

### P§5.1 Stages

Each stage names what it touches, what it reads out, and when it stops.
Stages S1 and S2 can run in parallel. S3 waits for M1's answer (D§12: does
honest search beat the bot?).

| Stage | Work | Touches | Readout | Kill criterion |
|---|---|---|---|---|
| **S0** entity table and parity | the entity/candidate table types; front end G (from `view.View`, extends `entity.go`); front end V (from `v2agent.Observation` and `Candidate`); corpus dumper rows (table, candidates, bot answer, outcome, optional full-state table, optional visits); forward-cost benchmark | `internal/policynet` (new feature set, e.g. `v2ent`, new checkpoint schema); `internal/spellbench/v2agent` (observation → table); `cmd/botbench` or a new dumper cmd | parity diff count on gorge-engine transcripts; µs per forward at d=32/64 | any parity residue that is not a known projector gap → fix before S1; forward > 0.5 ms at d=32 → redesign before S1 |
| **S1** value nets | train V_info and V_world, value-only (`policytrain -loss value` path, pn17); ablations: edge round on/off, identity dropout, belief features, Suphx-style annealed oracle input on V_info | `internal/policynet`, `cmd/policytrain` | within-matchup AUC and log loss per turn bucket (1-3, 4-6, 7+) on a held-out seed block, against `v-con` (0.642/0.740/0.882); then as the az leaf in honest search (M1 Q2 arm) vs the heuristic leaf (`searchprobe.LeafValue`) | V_world early AUC not ≥ V_info + 0.05 → drop V_world. The leaf arm not ≥ heuristic leaf + 2pp at 2,000 games → keep the heuristic leaf; S3 still possible with it |
| **S2** BC + DAgger, v2 format | the bot labels the student's own states; the student scores v2 candidates through front end V | `internal/policynet`, `cmd/policytrain`, `internal/spellbench/v2agent` (a `NetPolicy` implementing `Policy`, `policy.go:80`), `cmd/sbagent` (`-policy net -checkpoint`) | net vs bot, gorge-native via front end G (same weights), 2,000 games; then on the W2 v2 harness vs builtins | < 45% vs bot → do not ship as a fallback tier (the neutral heuristic stays T2) |
| **S3** honest ExIt | AZ tickets 3-5 (azgen corpus, visits/completed-Q loss, exitloop az, sampled world source with redeal) with Gumbel root selection as an option; prior from S2, leaf from S1 | `internal/azmcts` (Gumbel root, sampled `WorldSource`), `cmd/azgen`, `cmd/policytrain`, `cmd/exitloop` | the AZ spec's four readouts per generation; honest az vs `bot` and vs L10 | the AZ spec's: after 8 gated generations, not ≥ gen 0 + 3pp with CI clear → stop the loop; ship search with the S1 leaf and S2 prior |
| **S4** hand belief | multi-label hand head; importance-weighted worlds in the SpellBench world source | `internal/policynet` (new head), the D§5.2 world source | clairvoyant-vs-honest gap at equal simulations; weighted vs uniform redeal | only started if the gap is ≥ 5pp; stopped if weighting does not gain ≥ 2pp at 2,000 games |
| **S5** policy fine-tune (optional) | PPO or population self-play of the net-only policy | `ppo.go`, `onpolicy.go`, `exitloop -mode ppo` | net-only strength vs builtins and bot | only started if the net-only tier decides > 20% of decisions in M3/M4 runs; P§4.2's conditions apply |

### P§5.2 Plugging into the SpellBench v2 agent

- `v2agent.Policy` (`internal/spellbench/v2agent/policy.go:80`) is
  `GameStart / Choose(d) (int, error) / GameOver`. A `NetPolicy` builds the
  table through front end V, scores the candidates and returns argmax. It
  may instead sample with a SplitMix64 stream from `agent_seed`, which is
  how g115 plays.
- In the D§7 ladder the net enters twice: as the search prior/leaf inside
  T0, and as a new tier between T1 (bot on the shadow) and T2 (neutral
  heuristic) for decisions without a usable shadow. Whether it should
  outrank T1 is decided by S2's readout: bot-cloned net vs bot on the same
  states.
- Checkpoints load once at startup (`startup_ms` 300 s). Loading from
  outside gorge's module needs H1 (D§11).
- Determinism: fixed-order float32 forward, the policynet rule, so the same
  `agent_seed` and messages give the same answers (§11.4).

### P§5.3 Plugging into search

- `azmcts.Search(root, src, net, opts)` (`internal/azmcts/search.go:86`)
  takes a `*policynet.Model` and calls `priors` (`candidates.go:208`) and
  `leafValue` (`env.go:215`) on the seat's redacted view. The change is to
  make both take an interface (prior over candidates, value of a world
  state) so the S1/S2 models and the current policynet both fit.
- The leaf calls V_world on the simulation's own world. The root prior
  calls the policy head on the seat's view. Nothing else touches hidden
  information.
- Sampled worlds are D§5.2's redeal source (sibling workstream branch
  `wt/sb-m1-redeal`). The per-world specialist routing in D§8 is orthogonal:
  a specialist is a checkpoint chosen per world.

### P§5.4 Latency budget against the v2 clock

| Quantity | Value | Source |
|---|---|---|
| bank / increment / hard cap | 600 s / 2 s per decision / 60 s | §11.4 example |
| decisions a seat answers | a few hundred; gorge poses about 770 decisions per game in W1 (v1 path), and the kernel v1 ledgers show a mean of 268 steps and a p95 of 849, both seats combined | D§3.1, D§9 |
| searched decisions per seat per game | 38.5 (gorge-native, az stage 0) | az25.txt |
| rule | fixed simulation budgets; p99.9 game within 50% of the bank | D§9 |
| budget per searched decision at that rule | 300 s / ~40 ≈ 7.5 s | INFERRED |
| simulations that buys on 4 cores at 2.6 ms + 0.5 ms leaf | ~9,700 | INFERRED |
| net-only decision | one forward, under 1 ms | INFERRED from R1 §3.2 and P§3.4 |

**The clock is not the constraint for any network in this plan.** The
constraints are self-play throughput (P§4.4) and the cost of building the
shadow and sampling worlds per decision, which is unmeasured (M1/M2).
Deployment budgets should be set from measured strength per simulation,
not from the clock. That is also D§9's rule.

## P§6. Open questions for the operator

- **O1. Clairvoyant policy targets.** Accept that AZ stage 1 (clairvoyant)
  is a pipeline proof only, and that the honest network trains on
  honest-search targets (P§4.3)? Or run stage 1 as specified and measure
  whether its targets help the honest network?
- **O2. Trainer language.** R1 §3.5 chose a pure-Go, hand-derived,
  single-threaded trainer. The P§3 model is still small enough for that,
  but a message-passing round and autoregressive substeps make hand-derived
  backward passes costly to write and pin. Alternative: an offline PyTorch
  trainer outside the rules core, with pure-Go inference and a parity test
  (Go forward = trainer forward to 1e-6 on a pinned corpus). mtg-kernel
  trains in Torch on CPU with the determinism settings its model file
  configures.
- **O3. GPU.** Both GPUs are serving vLLM. Nothing in S0-S4 needs one. Is
  that the assumption for this program?
- **O4. Cores.** S3 at the current 2-core AZ budget is days per
  configuration (P§4.4). What allocation, and when (D§14 Q5)?
- **O5. Engine-neutral input as canonical.** Accept front end V plus the
  parity test (P§3.9) as the definition of the network's input, so that one
  checkpoint serves gorge search and foreign engines alike?
- **O6. Deck-local vs universal.** g115 is trained on the benchmark's own
  pool. mtgbld found universal models beat per-deck ones. Train on the
  pauper-kernel pool (bench-specific) or on gorge's broader deck set plus
  the pool?
- **O7. Opponents.** Search and training model the opponent as `bot`.
  Real SpellBench opponents are RL nets (g115, a48, c12) and builtins. Can
  we get the kernel bots as evaluation opponents (M4), even though their
  checkpoints are Codex-owned and read-only?
- **O8. v1.** Recommendation: no network on protocol v1. Its observation
  has no board (D§3.1), so leave v1 to the heuristic workstream
  (`wt/sb-v1b-heur`). Agree?
- **O9. Order.** Recommendation: S0 and S1 now, S2 in parallel, and S3
  only after M1 Q2 reports. Agree?

## P§7. References

How the references were checked: a delegated web pass on 2026-09-27 checked
every entry below against the arXiv API, Crossref or the publisher's or
proceedings' page (title, authors, venue, identifiers), plus the abstract or
the passage the one-line use relies on. Full texts were not read unless a
row says so. **Unverified** marks the details that pass could not confirm.
T§8 lists the references already in the theory notes: Frank & Basin, Long
et al., Cowling/Powley/Whitehouse ISMCTS, Cowling/Ward/Powley, AlphaZero,
ReBeL, Student of Games, Rubin, Haluska & Schmid, MageZero, SpellBench and
mtg-kernel. They are repeated here only where this document uses them for
something new.

### Search with learned evaluators

1. Silver, D. et al. (2016). Mastering the game of Go with deep neural
   networks and tree search. *Nature* 529:484-489. doi:10.1038/nature16961.
2. Silver, D. et al. (2017). Mastering the game of Go without human
   knowledge. *Nature* 550:354-359. doi:10.1038/nature24270.
3. Schrittwieser, J. et al. (2020). Mastering Atari, Go, chess and shogi by
   planning with a learned model (MuZero). *Nature* 588:604-609.
   arXiv:1911.08265.
4. Danihelka, I., Guez, A., Schrittwieser, J. & Silver, D. (2022). Policy
   improvement by planning with Gumbel. *ICLR 2022*.
   https://openreview.net/forum?id=bERaNdoegnO (no arXiv version).
5. Hubert, T., Schrittwieser, J., Antonoglou, I., Barekatain, M., Schmitt, S.
   & Silver, D. (2021). Learning and planning in complex action spaces
   (Sampled MuZero). *ICML 2021*, PMLR 139:4476-4486. arXiv:2104.06303.
6. Grill, J.-B., Altché, F., Tang, Y., Hubert, T., Valko, M., Antonoglou, I.
   & Munos, R. (2020). Monte-Carlo tree search as regularized policy
   optimization. *ICML 2020*. arXiv:2007.12509.
7. Wu, D. J. (2019). Accelerating self-play learning in Go (KataGo).
   arXiv:1902.10565 (AAAI-20 Reinforcement Learning in Games workshop).
8. Hessel, M. et al. (2021). Muesli: combining improvements in policy
   optimization. arXiv:2104.06159. Its ICML 2021 venue was recalled, not
   checked (**unverified**). Not used above; listed as the search-light
   alternative to MuZero.

### Imperfect information

9. Brown, N., Bakhtin, A., Lerer, A. & Gong, Q. (2020). Combining deep
   reinforcement learning and search for imperfect-information games
   (ReBeL). *NeurIPS 2020*. arXiv:2007.13544.
10. Schmid, M. et al. (2023). Student of Games: a unified learning algorithm
    for both perfect and imperfect information games. *Science Advances*
    9(46):eadg3256. arXiv:2112.03178 (v1 titled "Player of Games").
11. Perolat, J. et al. (2022). Mastering the game of Stratego with
    model-free multiagent reinforcement learning (DeepNash). *Science*
    378:990-996. arXiv:2206.15378. The R-NaD name was not re-checked in this
    pass.
12. Moravčík, M. et al. (2017). DeepStack: expert-level artificial
    intelligence in heads-up no-limit poker. *Science* 356:508-513.
    doi:10.1126/science.aam6960.
13. Brown, N. & Sandholm, T. (2019). Superhuman AI for multiplayer poker
    (Pluribus). *Science* 365:885-890. doi:10.1126/science.aay2400.
14. Li, J. et al. (2020). Suphx: mastering Mahjong with deep reinforcement
    learning. arXiv:2003.13590. The oracle-guiding dropout of perfect
    features was confirmed in the text.
15. Yang, G., Liu, M., Hong, W., Zhang, W., Fang, F., Zeng, G. & Lin, Y.
    (2022). PerfectDou: dominating DouDizhu with perfect information
    distillation. *NeurIPS 2022*. arXiv:2203.16406.
16. Zha, D. et al. (2021). DouZero: mastering DouDizhu with self-play deep
    reinforcement learning. *ICML 2021*. arXiv:2106.06135. State-action
    concatenated scoring was confirmed in the text.
17. Whitehouse, D., Powley, E. J. & Cowling, P. I. (2011). Determinization
    and information set Monte Carlo tree search for the card game Dou Di
    Zhu. *IEEE CIG 2011*, 87-94. doi:10.1109/CIG.2011.6031993.
18. Powley, E. J., Cowling, P. I. & Whitehouse, D. (2014). Information
    capture and reuse strategies in Monte Carlo tree search, with
    applications to games of hidden information. *Artificial Intelligence*
    217:92-116. doi:10.1016/j.artint.2014.08.002.

### Action spaces and entity encoders

19. Vinyals, O. et al. (2019). Grandmaster level in StarCraft II using
    multi-agent reinforcement learning (AlphaStar). *Nature* 575:350-354.
    doi:10.1038/s41586-019-1724-z. Checked in the text: self-attention over
    units, scatter connections, a pointer network, and autoregressive
    arguments. The layer sizes are **unverified**.
20. Berner, C. et al. (OpenAI) (2019). Dota 2 with large scale deep
    reinforcement learning. arXiv:1912.06680. Checked in the text:
    surgery, the 4,096-unit LSTM, a dot product with the available action
    ids, and attention over units. Per-unit max-pooling appears only in a
    figure (**unverified**).
21. Vinyals, O., Fortunato, M. & Jaitly, N. (2015). Pointer networks.
    *NeurIPS 2015*. arXiv:1506.03134.
22. Zaheer, M. et al. (2017). Deep sets. *NeurIPS 2017*. arXiv:1703.06114.
23. Lee, J. et al. (2019). Set Transformer. *ICML 2019*. arXiv:1810.00825.
24. He, J. et al. (2016). Deep reinforcement learning with a natural
    language action space (DRRN). *ACL 2016*. arXiv:1511.04636.
25. Dulac-Arnold, G. et al. (2015). Deep reinforcement learning in large
    discrete action spaces. arXiv:1512.07679.
26. Chandak, Y., Theocharous, G., Kostas, J., Jordan, S. & Thomas, P. (2019).
    Learning action representations for reinforcement learning. *ICML
    2019*. arXiv:1902.00183.
27. Tavakoli, A., Pardo, F. & Kormushev, P. (2018). Action branching
    architectures for deep reinforcement learning. *AAAI-18*, 4131-4138.
    arXiv:1711.08946.
28. Huang, S. & Ontañón, S. (2022). A closer look at invalid action masking
    in policy gradient algorithms. *FLAIRS-35*. arXiv:2006.14171.

### Imitation, iteration, policy gradient

29. Ross, S., Gordon, G. & Bagnell, J. A. (2011). A reduction of imitation
    learning and structured prediction to no-regret online learning
    (DAgger). *AISTATS 2011*. arXiv:1011.0686.
30. Anthony, T., Tian, Z. & Barber, D. (2017). Thinking fast and slow with
    deep learning and tree search (Expert Iteration). *NeurIPS 2017*.
    arXiv:1705.08439.
31. Schulman, J. et al. (2017). Proximal policy optimization algorithms.
    arXiv:1707.06347.

### Card games

32. Ward, C. D. & Cowling, P. I. (2009). Monte Carlo search applied to card
    selection in Magic: The Gathering. *IEEE CIG 2009*, 9-16.
    doi:10.1109/CIG.2009.5286501.
33. Cowling, P. I., Ward, C. D. & Powley, E. J. (2012). Ensemble
    determinization in Monte Carlo tree search for the imperfect information
    card game Magic: The Gathering. *IEEE TCIAIG* 4(4):241-257.
    doi:10.1109/TCIAIG.2012.2204883 (T§8 #4).
34. Zhang, S. & Buro, M. (2017). Improving Hearthstone AI by learning
    high-level rollout policies and bucketing chance node events. *IEEE CIG
    2017*, 309-316. doi:10.1109/CIG.2017.8080452.
35. Świechowski, M., Tajmajer, T. & Janusz, A. (2018). Improving Hearthstone
    AI by combining MCTS and supervised learning algorithms. *IEEE CIG
    2018*, 445-452. arXiv:1808.04794.
36. Xiao, C. et al. (2023). Mastering Strategy Card Game (Hearthstone) with
    improved techniques. *IEEE CoG 2023*. arXiv:2303.05197.
37. Xi, W. et al. (2023). Mastering Strategy Card Game (Legends of Code and
    Magic) via end-to-end policy and optimistic smooth fictitious play
    (ByteRL). arXiv:2303.04096.
38. Kowalski, J. & Miernik, R. (2023). Summarizing strategy card game AI
    competition. *IEEE CoG 2023*. arXiv:2305.11814.
39. Haluska, R. & Schmid, M. (2024). Learning to beat ByteRL: exploitability
    of collectible card game agents. arXiv:2404.16689 (T§8 #9).
40. Rubin, D. (2026). Unsound search with policy and value networks in
    Legends of Code and Magic. arXiv:2609.06816 (T§8 #8).
41. da Costa Cunha, et al. (2026). Causal RL for complex card games: a
    Magic The Gathering benchmark. arXiv:2605.06066. Only the abstract and
    the action-space summary were read.
42. Riot Games / Anyscale (2022). Riot Games and deep reinforcement learning
    in gaming (Legends of Runeterra balance agent). Blog post, not peer
    reviewed.
    https://www.anyscale.com/blog/riot-games-and-deep-reinforcement-learning-in-gaming
43. Xia, W. et al. (2023). Cardsformer: grounding language to learn a
    generalizable policy in Hearthstone. *ECAI 2023*, FAIA 372:2720-2727.
    doi:10.3233/FAIA230581. Checked bibliographically only; the
    architecture details are **unverified**.
44. Ward, H. N., Brooks, D. J., Troha, D., Mills, B. & Khakhalin, A. S.
    (2021). AI solutions for drafting in Magic: the Gathering. *IEEE CoG
    2021*. arXiv:2009.00655.
45. Bertram, T., Fürnkranz, J. & Müller, M. (2021). Predicting human card
    selection in Magic: The Gathering with contextual preference ranking.
    *IEEE CoG 2021*. arXiv:2105.11864.
46. Bertram, T. et al. (2024). Learning with generalised card
    representations for "Magic: The Gathering". *IEEE CoG 2024*.
    arXiv:2407.05879.
47. Forge. https://github.com/Card-Forge/forge, `forge-ai` module @
    `2ccbbb0`, 2026-09-27. Described in P§2.8.
48. mtg-kernel. https://github.com/jackmaiorino/mtg-kernel. Local clone
    @ `2c5e72f`: `README.md`, `docs/CP7_60_PERCENT_PROGRESS.md`,
    `python/mtg_kernel_rl/model.py`. Public branches
    `lead/phase1-campaign-001-v1` and `lead/phase1-v4-search-wrapper-v1`,
    covering Net8, the V4 widths and the REINFORCE trainer, were read by
    the delegated pass only.
49. SpellBench. https://github.com/jackmaiorino/spellbench. Local clone @
    `971a1fb`: the v2 protocol §7, §8 and §11.4,
    `benchmarks/pauper-kernel/benchmark.json` and the 2026-09-27 leaderboard,
    and `docs/design/2026-09-27-kernel-models-plan.md`.

### Opponent modelling and deck prediction

50. He, H., Boyd-Graber, J., Kwok, K. & Daumé III, H. (2016). Opponent
    modeling in deep reinforcement learning (DRON). *ICML 2016*.
    arXiv:1609.05559.
51. Dockhorn, A., Frick, M., Akkaya, Ü. & Kruse, R. (2018). Predicting
    opponent moves for improving Hearthstone AI. *IPMU 2018*, CCIS 854:
    621-632. doi:10.1007/978-3-319-91476-3_51.
52. Eger, M. & Sauma Chacón, P. (2020). Deck archetype prediction in
    Hearthstone. *FDG 2020*. doi:10.1145/3402942.3402959.
53. Bursztein, E. & Bursztein, C. (2014). I am a legend: hacking Hearthstone
    with machine learning. *DEF CON 22* talk, not peer reviewed.

### In-repo (gorge `wt/spellbench` @ `68257c459`)

- `docs/superpowers/specs/2026-09-28-spellbench-agent-design.md` (D),
  `docs/superpowers/reports/2026-09-28-spellbench-theory-and-references.md`
  (T), `docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md` (AZ
  spec), `docs/superpowers/specs/2026-09-06-gorge-learned-policy-research.md`
  (R1), `docs/superpowers/reports/2026-09-24-training-approaches-summary.md`,
  `docs/superpowers/reports/2026-09-24-pn14-explore-entity-outcome.md`.
- Code: `internal/policynet/{net.go,entity.go,features.go,candidate.go,valueonly.go,checkpoint.go,ppo.go,onpolicy.go}`,
  `internal/azmcts/{search.go,candidates.go,env.go,options.go,world.go}`,
  `internal/spellbench/v2agent/{policy.go,messages.go,observation.go}`,
  `internal/searchseat/prior.go`, `seat/policynet.go`, `cmd/policytrain`.
- Outputs: `/mnt/sata/gorge-training/az/stage0/az25.txt`,
  `/mnt/sata/gorge-training/17lands-spike/score-all.txt`,
  `/mnt/sata/gorge-training/converge-0927/RESULT.md`.
