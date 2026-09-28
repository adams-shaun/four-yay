# SpellBench agent — theory and references (2026-09-28)

Companion to `docs/superpowers/specs/2026-09-28-spellbench-agent-design.md`
(cited as **D§n**). `§n` without a prefix is a section of the SpellBench v2
protocol (`spec/SPELLBENCH_PROTOCOL_V2.md`, SpellBench `main` @ `971a1fb`).
Every external claim below was read at the source listed in T§8 unless the
reference is marked **unverified**. INFERRED marks our own reasoning.

## T§1. The problem in one paragraph

A SpellBench v2 seat sees its own information state: public board, own hand,
counts, and name-level knowledge of hidden cards (§6, §6.7-6.8). The
opponent's hand, both libraries' order, and (when `opponent_decklist` is
`hidden`) the opponent's list are unknown. The agent has a perfect-information
simulator (gorge) but not the true state. The theory question is how to turn
a perfect-information simulator into good play in an imperfect-information
game, and how much each shortcut costs.

## T§2. Determinization and PIMC, and how they fail

**Perfect Information Monte Carlo (PIMC):** sample K complete states
("determinizations", "worlds") consistent with the information state, solve
or search each as if it were perfect information, and aggregate the root
action values across worlds.

**Frank & Basin (1998)** identified two errors PIMC makes *irrespective of the
number of worlds* (as summarised by Long et al. 2010, p. 134):

| Error | What happens | MTG example (INFERRED) |
|---|---|---|
| Strategy fusion | Search assumes it can pick a different continuation in each world, but the player cannot tell the worlds apart and must play one strategy across them. A root move that needs a later world-specific guess looks as good as a move that is safe in every world. | "Attack now, and next turn cast removal on whichever blocker they have" looks perfect in every world, although we will not know which threat they drew. |
| Non-locality | In imperfect-information games a node's value depends on parts of the tree outside its subtree (what the opponent's earlier choices reveal about the world). PIMC evaluates subtrees locally. | The opponent passed with two blue open; worlds where they hold Counterspell are more likely than uniform redeal says. |

**Long, Sturtevant, Buro & Furtak (2010)** explained why PIMC nevertheless
works in practice using three measurable tree properties:

- **Leaf correlation** (probability sibling terminals share a payoff): PIMC
  is at its worst when it is low, because it "believes that the critical
  decisions are going to come 'later'".
- **Bias** (how strongly the game favours one player): extreme bias helps
  PIMC, a small effect.
- **Disambiguation factor** (how fast information sets shrink with depth):
  low is good for PIMC's absolute score, but its *gain over random* grows
  with disambiguation.

INFERRED for Magic: hidden information disambiguates at a moderate rate (every
cast or played card reveals one; hands refill every turn), and leaf
correlation is moderate-to-high in the late game (boards decide games) but
lower early (a single held card flips the outcome). gorge's own L10 PIMC
seat is recorded as having its edge early-game only, with 4.8% late-game
coverage (operator memory index, entry "L10 teacher seat"; not re-measured
here).

**What gorge has measured about naive determinization:** a 1-ply search seat
over a single cheating determinization scored 16.5-23.5% against the plain
bot (`docs/superpowers/specs/2026-09-06-gorge-learned-policy-research.md`
§0/§5.3). The sampled-world PIMC teacher, by contrast, is +5.8pp ± 1.25
paired over 3,000 dev games and, as the L10 seat, 53.7% held-out vs 50.4%
control (training-approaches summary, TL;DR rows 3-4). INFERRED: the
difference is many worlds instead of one, worlds that are consistent with the
seat's observations rather than the true state, and a behaviour-consistent
sampler (T§5.4).

## T§3. ISMCTS and Cowling et al.'s MTG results

**ISMCTS** (Cowling, Powley & Whitehouse 2012) searches a tree of
*information sets* rather than states: each iteration samples a
determinization, descends only through actions legal in it, and credits a
node's statistics for availability (a child is scored only over the
iterations in which it was available). One tree shared across worlds
removes strategy fusion at the searching player's own nodes (the player must
commit to one action per information-set node). gorge's AZ design adopts the
open-loop, availability-counted variant for its stage 2 (AZ spec §2).

**Ensemble determinization for MTG** (Cowling, Ward & Powley 2012), read at
the source, measured on a simplified MTG with fixed decks:

| Finding | Quote / figure |
|---|---|
| More worlds beat deeper trees | With a 10,000-simulation budget the best number of determinizations was 20-100; at 100,000 simulations the best simulations-per-determinization "again lay in the range from 100 to 1000, suggesting that an increased simulation budget is best used in running additional determinizations rather than searching each determinization more deeply." |
| Weak stochastic rollouts beat strong deterministic ones | "Applying a fully deterministic rollout strategy ... provided a clearly inferior performance to utilising the reduced rules player which uses very limited domain knowledge, but incorporates some randomness ... despite the fact that the expert rules player is an intrinsically stronger player." Uniform-random rollouts were "very weak". |
| Binary decomposition of compound moves | Deciding "play this card or not" as a chain of yes/no nodes let partial decisions accumulate statistics; they propose the same for attacker/blocker subsets. |
| Parallel cost | 0.62 s per decision for one tree, 1.12 s for 50 trees in their implementation. |

Consequences for the design: K worlds with shallow search per world
(D§6); SpellBench's own decomposition of attacks and blocks into
inclusion substeps (§8) is the binary decomposition Cowling et al.
recommend, and gorge's AZ tree already searches attackers/blockers as
candidate sets (AZ spec §2 "Priors"). The rollout finding is why the AZ
design uses a value leaf and no bot rollouts: "search whose rollouts are the
baseline is capped at the baseline" (AZ spec, prior-evidence table, from the
mtgbld project).

## T§4. AlphaZero, and what hidden information changes

**AlphaZero** (Silver et al. 2018): MCTS with PUCT selection
`Q(a) + c·P(a)·√N / (1 + N(a))`, a network supplying the prior P and the leaf
value (no rollouts), trained on the search's visit distribution and the game
outcome, iterated by self-play.

What changes with hidden information (INFERRED synthesis, with the sound
alternatives named):

| Issue | Perfect information | Hidden information |
|---|---|---|
| Node | a state | an information set; the network must see only the seat's view (gorge's AZ design feeds the redacted view in both stages) |
| Simulation root | the true state | a sampled world; the sampler's distribution is part of the algorithm |
| Value target | outcome from the state | outcome averaged over the belief; a clairvoyant-trained value head is biased when deployed honestly (hence AZ spec stage 1 vs stage 2) |
| Soundness | PUCT converges to minimax | determinized PUCT does not converge to equilibrium (strategy fusion survives at opponent nodes); sound methods reason over public belief states: ReBeL (Brown et al. 2020), Student of Games (Schmid et al. 2023). Their cost in a game with Magic's state space is untested here. |

**MTG and CCG prior work.**

- **MageZero** (WillWroble/MageZero): AlphaZero-style MCTS with a transformer
  over sparse tokens, deck-local ("each deck as a self-contained
  environment"), a 16-deck Standard 2022-25 pool, UW Tempo 16% → 66% against
  minimax opponents. No determinization in the current approach (repository
  README). gorge's AZ spec records that it trains with
  `see_opponent_hand: true`; SpellBench's program notes say its shipped
  config "appears to feed the opponent's hand to the network"
  (`everyone-on-the-board.md`). The see-opponent-hand setting was not
  re-verified in MageZero's source for this report (**unverified**).
- **LOCM with unsound search** (Rubin, arXiv 2609.06816): PIMC-style search
  over sampled opponent worlds with policy and value networks. Network alone
  26.8%; with search +24.6 points; the strictest battle-phase configuration
  51.35% [50.37, 52.33] over 10,000 pre-registered games. The in-repo note
  (`docs/superpowers/plans/2026-09-19-learned-cast-profile.md`, reference
  pass 2) adds that the policy head was trained by cross-entropy on the
  teacher's chosen action.
- **Learning to Beat ByteRL** (Haluska & Schmid, arXiv 2404.16689): ByteRL,
  a strong LOCM agent, is highly exploitable. The in-repo note's figures (BC
  from 3.5M pairs ≈ 42% vs the teacher; PPO fine-tune past 50% in 100
  episodes, ≈ 75% by 500) were not re-verified here (**unverified**).
- gorge's own: stage 0 clairvoyant gen-0 AZ at 25 simulations beat `bot`
  78.0% [72.3, 83.7] over 200 games, 65.8 ms per searched decision
  (`/mnt/sata/gorge-training/az/stage0/az25.txt:11,17`); honest stage 2 is
  unmeasured.

## T§5. Bayesian opponent-deck inference

### T§5.1 Model

Let the candidate lists be D₁…D_m (a closed pool) with prior π(D). Each list D
has count k_c(D) of card name c and size N. Let the evidence be the multiset
R of the opponent's cards the seat has identified (cast, played, revealed,
discarded, or listed in `known` hand entries), r_c copies of c, n = Σ r_c.

If the identified cards were a uniformly random n-subset of the list, the
likelihood is multivariate hypergeometric:

```
P(R | D) = Π_c C(k_c(D), r_c) / C(N, n)        (0 if any r_c > k_c(D))
P(D | R) ∝ π(D) · P(R | D)
```

Caveats (INFERRED), each biasing in a known direction:

- **Selection.** Identified cards are not uniform: players cast what they
  can, lands appear early. What *is* uniform is the drawn set (opening hand
  plus draws, from a secretly shuffled library, §11.6); the revealed subset
  is a policy-dependent thinning of it. If the thinning depends on card
  identity but not on which list the card came from, the hypergeometric
  likelihood is correct up to a list-independent factor for pools of
  distinct archetypes; when two lists share a card at different counts, the
  bias is toward over-weighting lists with more copies of castable cards.
- **Negative evidence is ignored** ("never played a second land colour",
  "passed with mana open"). Ignoring it keeps the posterior honest but
  slower; adding it needs an opponent policy model (T§5.4).
- **Tutors and searches** reveal non-uniformly chosen cards; treat searched
  cards as evidence of membership (count ≥ 1) rather than as a uniform draw.
- **Mulligans** (London) put chosen cards on the bottom; the drawn set is
  still uniform up to the bottomed cards, which stay hidden.

### T§5.2 Open formats

For Limited and open Constructed the list is not in a closed pool.
Represent each archetype a (a colour pair for FDN Limited; a metagame
archetype for Standard) by a distribution over card counts, e.g. a
Dirichlet-multinomial fitted to observed lists (17lands decks for Limited;
a metagame database for Standard). The posterior is over a, then over counts
given a and R; worlds sample a whole list. SpellBench's fixed-deck
benchmarks also publish `card_name_domain`, the union of names in all entry
decks (§15), which truncates the support of any list prior.

### T§5.3 Identification speed, measured on the pauper-kernel pool

Method: for each of the eight rated `pauper-kernel` lists
(`internal/spellbench/decks/pauper-kernel/*.json`), 2,000 trials; shuffle
the list, reveal cards one at a time uniformly at random, and record the
number revealed when the true list's posterior (uniform prior over the
pool, hypergeometric likelihood) first reaches 0.95. Computed for this
report with a 40-line Python script (not committed).

| List | mean | median | p90 | max |
|---|---|---|---|---|
| Affinity | 1.94 | 2 | 3 | 8 |
| Burn | 1.75 | 1 | 3 | 9 |
| CawGates | 1.16 | 1 | 2 | 4 |
| Elves | 1.54 | 1 | 3 | 8 |
| Faeries | 1.43 | 1 | 2 | 4 |
| Rally | 1.71 | 1 | 3 | 8 |
| Spy | 1.41 | 1 | 2 | 6 |
| Wildfire | 2.02 | 2 | 4 | 9 |

Adding Terror as a ninth candidate changes the means by at most 0.24. The
uniform-reveal assumption overstates speed where early reveals are shared
basic lands, but even so the conclusion is robust: **on a closed pool of
distinct archetypes the list is known within the first turn or two; the
remaining hidden information is hand and library.** The posterior earns its
keep in open formats (T§5.2).

How to measure it in real games (for M3/M5): log, per game and turn, the
posterior probability of the true list and its log-loss; report the
distribution of the first turn at which P(true) ≥ 0.95, and a calibration
table (predicted vs realised frequency, binned). A posterior that is
confident and wrong is worse than a slow one, because worlds then exclude
the real state.

### T§5.4 Hands and libraries, and why the sampler matters

Given a list, the unknown hand and library cards are a uniform deal of the
list minus everything seen, with `known` claims pinned (§6.7). This is
gorge's redeal (`internal/searchprobe/redeal.go:39`). It is uniform with
respect to the opponent's *behaviour*: it ignores that the opponent chose not
to cast a card it would have cast. gorge's rejection sampler
(`searchprobe.Sample`) conditions on behaviour by replaying the game under a
model opponent and rejecting worlds whose replays diverge, which requires
every event burst (`internal/searchseat/searchseat.go:14-39`). This is the
non-locality problem of T§2 in concrete form: behaviour-conditioned worlds
are the partial remedy, and a SpellBench seat cannot use gorge's
implementation of it. A likelihood-weighted redeal (weight each world by the
modelled probability of the opponent actions inferable from consecutive
observations) is the substitute (INFERRED; D§5.2).

## T§6. Why distillation and behaviour cloning plateaued in gorge

| Attempt | Measured | Source |
|---|---|---|
| Distil PIMC teacher into a per-option net (L9, pn01-06) | holdout matches the bot; in play 46-47% vs 50.1% control | training-approaches summary, row 5 |
| Clairvoyant oracle teacher → distil | oracle seat +11-19pp; distilled net flat (14.1-14.6% vs 14.3%) | row 6 |
| Expert iteration loop (pn11) | 45.7 / 44.9 / 45.4% vs 48.7% control over 3 generations | row 9 |
| On-policy PPO (pn13, mtgbld recipe) | flat within ±0.3pp over 10 rounds | row 11 |
| Data/feature/hidden-info grid (pn12) | override top-1 3-10% at every size; best in-play 51.2% vs 50.3% control | row 12 |
| PPO continuation (pn14 arm, rounds 11-28) | 51.0-51.45% vs control 50.95 ± 1.55 | memory note `az-mcts-program`; `/mnt/sata/gorge-training/converge-0927/RESULT.md` (not re-read) |
| 17lands FDN data for the value head | within-matchup AUC 0.805 (1k own games) vs 0.768 (+FDN); 0.815 (FDN + 1k×5 epochs); 0.833 (full own corpus); IR semantic labels (mz-sem) 0.805 = plain | `/mnt/sata/gorge-training/17lands-spike/score-all.txt:1,5,13,17,21` |

Why (the summary's own diagnosis, plus the measured label statistics): the
teacher's better choices depend on hidden information a redacted view cannot
express; only 1-3% of labelled decisions override the bot; and the teacher's
candidate values inside one decision differ by a median of 1/16, one sampled
world at K = 16 (`2026-09-19-learned-cast-profile.md`, reference pass 2), so
the targets are near-ties plus world luck. Behaviour cloning of a bot is
capped at that bot by construction. LOCM's result points the same way: the
distilled network recovered part of the teacher, and search at decision time
still added +24.6 points (T§4). Hence the design keeps search at decision
time (D§6).

The brief's figure "1k × 5 epochs 0.820" for own data alone is not in
`score-all.txt` (which lists `v-con1k` 0.805, `v-con` 0.833,
`v-fdn-con1kx5` 0.815); treat it as **unverified**.

## T§7. Ratings and sample sizes

### T§7.1 How SpellBench computes ratings

From `python/spellbench/arena/ratings.py` (docstring and `_fit_score_pairs`)
and the leaderboard notes in `benchmarks/pauper-kernel/runs/2026-09-27/LEADERBOARD.md`:

- Bradley-Terry: P(i beats j) = s_i / (s_i + s_j). Fitted by the MM
  iteration s_i ← W_i / Σ_j n_ij / (s_i + s_j), where W_i is i's score
  (draws count half) and n_ij the games between i and j; renormalised each
  step so the anchor (`uniform`) is 1; converged when the max change in
  log s is below 1e-10.
- One virtual draw per rated matchup is added to the fit and every bootstrap
  refit (a shrinkage prior). Mirror matchups are excluded.
- Rating r = ln s; displayed Elo = r · 400 / ln 10 + 1000.
- 95% CI: percentile paired bootstrap over seat-swapped pairs within each
  matchup (2,000 replicates on the published run). Pair outcomes are stored
  as half-points 0-4.
- In v2, the two games of a pair have independent secrets (§11.6), so
  pairing balances seat but gives no common-random-number variance
  reduction on the deal.

### T§7.2 Power analysis

With p the win probability and Δ the Elo difference,
p = 1 / (1 + 10^(−Δ/400)), so dp/dΔ = p(1 − p) · ln 10 / 400. With no draws a
game's score has variance p(1 − p), so the standard error of Δ from n
head-to-head games is

```
SE(Δ) ≈ (400 / ln 10) / √(n · p(1 − p))      = 347.4 / √n at p = 0.5
```

| Goal (p ≈ 0.5) | Games |
|---|---|
| 95% CI half-width ±100 / ±50 / ±25 / ±10 Elo | 47 / 186 / 742 / 4,638 |
| detect Δ = 100 / 50 / 35 / 23 / 20 Elo at 80% power, two-sided α = 0.05, n = ((1.96 + 0.84) · 347.4 / Δ)² | 95 / 379 / 774 / 1,792 / 2,369 |

Conversions near 50%: 1pp ≈ 6.9 Elo; 3.3pp ≈ 23 Elo; 5pp ≈ 35; 10pp ≈ 70.
A 64-game matchup (the published benchmark's size) has SE ≈ 43 Elo.

### T§7.3 Empirical inflation on a real run

The leaderboard intervals are wider than the head-to-head formula because
the rating is relative to the anchor through a network of matchups, deck
slices differ, and the bootstrap resamples pairs. On the 2026-09-27
`pauper-kernel` run: g115 (320 games, 265 wins) has a CI width of 142.0
Elo against 100.9 predicted; heuristic (320 games, 143 wins) 109.8 against
76.6. Both ratios are 1.41-1.43, so leaderboard CIs need about 2× the games
the formula gives (1.41² ≈ 2).

## T§8. References

External (read at source for this report unless marked):

1. Frank, I. & Basin, D. (1998). Search in games with incomplete
   information: a case study using Bridge card play. *Artificial
   Intelligence* 100(1-2):87-123.
   https://www.sciencedirect.com/science/article/pii/S0004370297000829 —
   bibliographic data verified by search; the paper itself was read only
   through Long et al.'s summary (**unverified** at source).
2. Long, J., Sturtevant, N., Buro, M. & Furtak, T. (2010). Understanding the
   success of perfect information Monte Carlo sampling in game tree search.
   *AAAI* 24(1):134-140. https://ojs.aaai.org/index.php/AAAI/article/view/7562
   (PDF read).
3. Cowling, P. I., Powley, E. J. & Whitehouse, D. (2012). Information Set
   Monte Carlo Tree Search. *IEEE TCIAIG* 4(2):120-143.
   https://eprints.whiterose.ac.uk/id/eprint/75048/ — bibliographic data and
   abstract verified; algorithm details as commonly described (full text not
   re-read, **partially unverified**).
4. Cowling, P. I., Ward, C. D. & Powley, E. J. (2012). Ensemble
   determinization in Monte Carlo tree search for the imperfect information
   card game Magic: The Gathering. *IEEE TCIAIG*.
   https://eprints.whiterose.ac.uk/id/eprint/75050/ (PDF read; quotes in T§3).
5. Silver, D. et al. (2018). A general reinforcement learning algorithm that
   masters chess, shogi, and Go through self-play. *Science* 362:1140-1144.
   https://www.science.org/doi/10.1126/science.aar6404 (bibliographic data
   verified; PUCT form as used in gorge's AZ spec).
6. Brown, N., Bakhtin, A., Lerer, A. & Gong, Q. (2020). Combining deep
   reinforcement learning and search for imperfect-information games
   (ReBeL). https://arxiv.org/abs/2007.13544 (abstract).
7. Schmid, M. et al. (2023). Student of Games: a unified learning algorithm
   for both perfect and imperfect information games. *Science Advances* 9.
   https://arxiv.org/abs/2112.03178 (abstract).
8. Rubin, D. (2026). Unsound search with policy and value networks in
   Legends of Code and Magic. https://arxiv.org/abs/2609.06816 (abstract;
   figures in T§4).
9. Haluska, R. & Schmid, M. (2024). Learning to beat ByteRL: exploitability
   of collectible card game agents. https://arxiv.org/abs/2404.16689
   (abstract; BC/PPO figures **unverified**).
10. MageZero. https://github.com/WillWroble/MageZero (README read).
11. SpellBench. https://github.com/jackmaiorino/spellbench — protocol v1/v2,
    ratings code, `pauper-kernel` run 2026-09-27, program doc
    `docs/design/2026-09-27-everyone-on-the-board.md`, gorge adapter plan on
    `origin/gorge-adapter` (local clone `/mnt/sata/gorge-training/spellbench`).
12. mtg-kernel. Local clone `/mnt/sata/gorge-training/mtg-kernel` @ `2c5e72f`,
    `data/pauper_pool_v1.json` (MIT) — source of the pool lists.

In-repo (gorge, `wt/spellbench` @ `cc3c349d8` unless noted):

- `docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md` (AZ spec).
- `docs/superpowers/specs/2026-09-06-gorge-learned-policy-research.md` (R1).
- `docs/superpowers/reports/2026-09-24-training-approaches-summary.md`.
- `docs/superpowers/plans/2026-09-19-learned-cast-profile.md` (reference
  passes 1-2).
- `internal/searchprobe/sample.go:158`, `redeal.go:39`, `teacher.go:89`;
  `internal/searchseat/searchseat.go:14-39`; `internal/azmcts/search.go:15,86,154`,
  `world.go:25,39`; `rules/chance.go:48,189`; `rules/engine.go:219,2610`;
  `rules/layers.go:1458`.
- Branch `wt/spike-17lands-value` @ `d13cfbda5` (not merged) and its outputs
  under `/mnt/sata/gorge-training/17lands-spike/`.
- AZ stage 0 output `/mnt/sata/gorge-training/az/stage0/az25.txt`.
