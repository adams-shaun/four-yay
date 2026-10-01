# Search benchmark replication on gorge (DraftZero experiment #3)

*2026-10-01. Our version of DraftZero's
[docs/016](https://github.com/danieljbrooks/draft-zero/blob/b1e0ba6863182a612a1a06fbac9754cbc979e018/docs/016-search-benchmark-results.md),
the results of the search benchmark that docs/012 proposed (upstream pinned at `b1e0ba68`, MIT).
Upstream ran it on XMage with MageZero's search. We ran the same protocol on gorge's rules engine
with gorge's search. Every main table puts upstream's numbers next to ours. Upstream's numbers are
its **offline** (heuristic-leaf) arms throughout: we have no network arm. Plan and deviations:
`docs/superpowers/plans/2026-09-30-searchbench-replication.md` (branch `wt/sbrep`). Code:
`wt/sbrep-fast` at `408fb9dcc`.*

> **Placeholders.** `is-mcts-b10000` (IS-MCTS at 10,000 simulations) was still running when this
> was written. Its cells read **TODO(is-mcts-b10000)**. `scripts/searchbench/report_tables.py`
> picks the file up when it lands and fills its analysis rows and contrasts.

## The question

Docs/016 asks how much tree search helps a Magic limited agent and which kind works best, and
whether a search has to see the opponent's hidden cards. It scores each method by **agreement with
top 17lands players** on 1,000 real FDN positions: whether to cast or hold a spell, attack, or
block. We asked whether the findings carry over to a different engine and search. The positions
come from the same public data and the same selection protocol. The engine is gorge (pure Go,
compiled from Forge's card scripts) and the search is gorge's `azmcts`. The leaf evaluator is a
heuristic, as in upstream's offline arms.

## What we found

Against upstream's offline results:

1. **Replicated: search does not need to see hidden cards.** At equal budget, no fair method
   (PIMC with 1 or 4 worlds, IS-MCTS) scores below clairvoyant MCTS on A_set or the balanced
   score. At 3,000
   simulations, PIMC-1 and IS-MCTS are **ahead** by 4 points of A_set (95% CIs exclude zero). That
   gain comes from holding spells more often. On the balanced score the gap is +2 to +3 points,
   which is not a detectable difference. The leak probes reproduce upstream's verdicts. Clairvoyant
   MCTS fails 5 of 6 pairs, including the canary. Every fair method returns bit-identical searches
   in both worlds of every pair (§3).
2. **Not replicated: more search did not help.** Upstream gained 6–7 points of A_set from 100 to
   3,000 simulations. Our curves are flat from 100 to 10,000 for every method and every score. No
   budget contrast is detectable either way. Our trees already reach 2–6 turns past the root, so
   upstream's explanation (the search sees less than one turn ahead) cannot be why. The likelier
   limits are the leaf evaluator and the scripted opponent inside the path. That is a hypothesis we
   have not tested (§4, §8.5).
3. **Replicated: the method matters little.** PIMC-4 against PIMC-1 and IS-MCTS against PIMC-1 show
   no detectable difference at 1,000 or 3,000 simulations. IS-MCTS costs 2.8–3.6× as much, close to
   upstream's 2.8–3.3×. PIMC with 1 world is again the practical choice.
4. **Partly replicated: the discount's effect on agreement.** No discount arm moves the balanced
   score, which matches upstream. On A_set, stronger discounts *raise* our agreement by 3–6 points.
   Upstream's moved the other way (−3.2 at 0.9 per ply). The rise is passivity: the arms hold far
   more often. Gorge counts about 15 engine decisions per searched edge, against upstream's 1.43,
   so a per-ply discount is roughly 10× stronger here (§5).
5. **Replicated: the searches are more active than top players.** They attack 55–63% of the time
   (humans 44%) and block 56–62% (humans 30%). They cast something on 53–70% of holds, where the
   humans held. Always passing scores 71.9% A_set, more than any search (upstream: 73.8%).
6. **Search against no search.** Gorge's bot with no search (our stand-in for upstream's rule
   heuristic) scores 46.3% A_set, close to upstream's heuristic at 46.9%. It scores 0.607 balanced,
   against upstream's 0.557. Search beats it by about 9 points of A_set. On the balanced score the
   gain is only +1.5 to +3.8 points, detectable for PIMC-4 at 100 alone. The bot agrees with humans
   on attacks better than every search: 0.708 balanced against 0.60–0.66.
7. **Not replicated: the disagreements are not near-ties.** Upstream's median Q gap to the human's
   option was 0.043. Ours is 0.08–0.16 (0.150 for PIMC-1 at 3,000), and a third to a half of our
   gaps exceed 0.15. Our search is confident when it disagrees.
8. **Compute.** A decision costs us roughly a tenth of upstream's worker-seconds at every budget
   (§1.4). E2 and E2b together took about 2.3 hours of wall time on 8 workers, before IS-MCTS at
   10,000. The engine and search got 2–4× faster during the replication, with byte-identical game
   records.

Not replicated, because we have no FDN value network free of leaks: everything about #2a's and
#2b's networks. That covers upstream's findings 3 and 4, §4.3, §7 and §9.

## How agreement is scored

The scoring is upstream's, ported to Go and checked against upstream's `analyze.py` and
`diagnose.py` on a fixture (`internal/searchbench/report_test.go`):

- **A_set** is macro agreement over the four decision types. On spell decisions, Pass also counts
  as a match when the human attacked. **Strict** drops that.
- **Balanced** is the mean balanced accuracy of three yes/no questions: cast or hold, attack or
  not, block or not. A constant answer scores 0.50. As upstream concluded, this is the score to
  trust.
- **CIs** are 95% game-cluster percentile bootstraps: 1,000 resamples, seed 0, resampling games as
  upstream's `bootstrap()` does. Paired contrasts resample the same games for both arms.

One deliberate difference: upstream's balanced score scores each resample through a dict, so a
game drawn *k* times counts once. That is not a bootstrap, and its CI comes out about **0.77× as
wide as it should be**. We keep the draw multiplicity, so our balanced CIs are honest and wider.
Upstream's balanced point estimates are unaffected.

## The results at a glance

Balanced score, ours (upstream offline), heuristic leaf, discount 0.99 per ply:

| Method | 100 | 300 | 1,000 | 3,000 | 10,000 |
|---|---|---|---|---|---|
| Clairvoyant MCTS (fails E1) | 0.609 (0.567) | 0.607 (0.591) | 0.602 (0.613) | 0.602 (0.627) | — |
| PIMC, 1 world | 0.630 (0.578) | 0.614 (0.589) | 0.617 (0.597) | 0.631 (0.620) | 0.633 (0.631) |
| PIMC, 4 worlds | **0.645** (0.576) | 0.630 (0.602) | 0.629 (0.603) | 0.615 (0.622) | — |
| IS-MCTS | 0.618 (0.591) | 0.621 (0.585) | 0.616 (0.614) | 0.622 (0.623) | TODO(is-mcts-b10000) (0.636) |

No search: gorge's bot scores 0.607 (upstream's rule heuristic: 0.557).

Each of our cells has a CI of about ±0.033. Every search cell overlaps every other. Upstream's
curves climb with budget and ours do not. By 3,000–10,000 simulations the two engines land in the
same place, 0.60–0.63.

---

## 1. What ran, and how it differs from upstream

| | Upstream (docs/016) | Ours |
|---|---|---|
| Engine | XMage via MageZero's bridge | gorge, Forge corpus at `95f04e8a` |
| Positions | sb-v1: 1,000 test + 300 dev, four types | sb-v1-gorge: the same protocol and quotas (§1.1) |
| Methods | clairvoyant MCTS; PIMC-1, PIMC-4; IS-MCTS | the same, in `azmcts` (§1.2) |
| Budgets | 100, 300, 1,000, 3,000; 10,000 for PIMC-1 and IS-MCTS | the same (IS-MCTS 10,000: TODO) |
| Leaf | `GameStateEvaluator3`, and #2a's network | `searchprobe.LeafValue` heuristic only. **No network arm** |
| Leak test (E1) | six pairs, 16 seeds per world, 3,000 simulations | the same pairs, seeds and budget (§3) |
| Discount sweep (E2b) | 1.0, 0.95, 0.9 per ply; matched per action and per turn | the same arms, matched to gorge's own trees (§5) |
| References (E0) | chance, rule heuristic, #2a and #2b policies | chance, always-pass, always-act, random, gorge's bot. No policies |
| Compute | pod-seconds on an RTX 3090 pod (31 cores) | core-seconds on a shared 16-core 9950X, 8 workers (§1.4) |

### 1.1 The decisions (sb-v1-gorge)

Reconstruction turns a 17lands row into a position, and that is an *interpretation* of the data.
We did not port it. We ran **upstream's pinned Python as a data-preparation tool**
(`scripts/searchbench/prep.py`). It produces each candidate's StateSpec, its labels, its `real`
world and its eight belief `worlds`. Everything that touches an engine runs in gorge. That covers
materialising the spec through `events.Apply`, playing the preLand and reaching the decision,
checking legality, canonicalising the options and matching labels. Upstream's selection loop
(`items.py cmd_build`) is ported to Go, with gorge's legality checks where XMage's bridge checks
were. The same games, splits, candidate order and quotas give positions, labels and belief samples
identical to upstream's wherever gorge accepts the candidate.

| Type | Test / dev, ours (upstream) | Options, mean | Chance | Human acted |
|---|---|---|---|---|
| Spell | 375 / 110 (375 / 110) | 3.98 (3.99) | 48.5% (48%) | cast, always. Pass also matches in 62% (61%) |
| Hold | 125 / 40 (125 / 40) | 3.54 (3.40) | 32.3% (33%) | never, by construction |
| Attack | 300 / 90 (300 / 90) | 2.00 (2.00) | 50.0% (50%) | attacked 44% (46%) |
| Block | 200 / 60 (200 / 60) | 2.53 (2.37) | 44.1% (44%) | blocked 30% (20%) |
| **All** | 1,000 from 558 games / 300 from 172 games | | macro 43.7% (43.7%) | do nothing on 43% (45%) |

- **Selection.** The quotas filled after scanning 1,186 of the 1,874 held-out top-player games
  (12,749 candidate evaluations, 325 s). Of the 11,449 rejected candidates, upstream's Python half
  refused 10,776, exactly as upstream's own build would. The largest groups were "the human cast
  something" (2,975 holds and 2,084 attacks), fidelity tier (4,449) and "the opponent did not
  attack" (1,062 blocks). Gorge refused the other 673:

  | Gorge refusal | Count |
  |---|---|
  | block: no block question was asked (`reached PRIORITY at DECLARE_BLOCKERS`) | 277 |
  | hold: a non-Pass option matched the human's recorded plays (`hold label`) | 175 |
  | block: the human's block is not an exact unique pairing in gorge's options | 84 |
  | hold: fewer than two options (`trivial`) | 78 |
  | attack: defender closed | 24 |
  | hold: nothing castable | 21 |
  | other (preLand not offered, label ambiguity, world observation mismatch) | 14 |

- **Not upstream's exact item set.** Upstream's decision files are git-ignored, so we cannot measure
  the overlap. Same protocol, same quotas and the same Python half mean the populations should match
  closely. Gorge's refusals shift them slightly. The clearest shift is in blocks: our humans blocked
  in 30% of block items against upstream's 20%.
- **Tiers.** T0/T1: spell 193/182, hold 54/71, attack 128/172, block 9/191.

### 1.2 The search code

The four methods run through `internal/searchbench` (`RunArm`) on `internal/azmcts`.
`searchbench run` is a parallel per-item runner, deterministic per seed. The deviations from
upstream's `BenchSearch`:

| | Upstream (XMage / MageZero BenchSearch) | Ours (gorge `azmcts`) |
|---|---|---|
| Tree | both players' decisions, micro decisions included | **the searching seat's searched decisions only** (priority, attackers, blockers, targets). The opponent and every unsearched ask are played by `botpolicy` *inside the path* |
| Root options | every playable action | Pass plus every canonical cast, activation and land play. A play that needs mana tapped first is one recorded **macro** edge, never a bare tap |
| Attack / block | sequential "attack with X?" / "what does X block?" | one joint attackers / blockers decision, **projected** onto the item's creature X |
| PUCT | c = 1 on values in [−1, 1]; unvisited Q = 0 | c = 0.5 on values in [0, 1], the same ratio; unvisited Q = 0.5, the same midpoint |
| Priors | uniform | uniform |
| Leaf | `GameStateEvaluator3` at priority decisions; micro decisions inherit their parent's score | `searchprobe.LeafValue` at the next searched decision |
| Chance in the tree arms | each node's state cached at first visit, so its chance outcome is drawn once | each simulation replays from the root with the tree's fixed chance seed, so every path is deterministic: the same one-outcome-per-node property |
| **Ply** (discount unit) | one tree decision: 1.43 per action, 16.2 per turn | **every engine decision on the path**, the bot's included: about 15 per searched edge and 28 per turn |
| Clairvoyant | one tree on `real` | one tree, each simulation a clone of `real` |
| PIMC-k | k trees, budget split, root merged by label | the same, merged by semantic key |
| IS-MCTS | one tree; each iteration picks one of 8 worlds and re-deals it | one availability-count tree; each simulation picks world *i* and re-deals it (`RedealSource` over world *i*'s decklists). No opponent nodes |
| Failed simulation | retried once without the re-deal | discarded and counted: 99.6–99.97% of each run's budget completed; no run fell back to the bot |
| Final choice | most visits | most visits |

**The discount-default bug.** Docs/012 sets the E2 default to 0.99 per ply. Our pilot
(`sb-v1/gorge2/runs`) and the first grid wave ran with `-discount` defaulting to 1, so they were
undiscounted. We caught it on 2026-09-30 at 21:35. `683b3cf69` makes 0.99 the default, and the
grid restarted with explicit `-discount 0.99 -discount-unit ply` (`grid/grid.sh`). The first wave's
undiscounted 1,000-simulation clairvoyant and PIMC-4 runs became E2b's 1.0 arm. A rerun with
`-discount 1.0` reproduced them exactly: 100% the same choices and every score difference 0.0
(`report_tables.py`, "rerun checks"). The pilot's own tables are undiscounted. Its curves are as
flat in budget as the grid's, so the budget finding does not depend on the bug:

| Pilot, undiscounted (A_set / balanced) | 100 | 300 | 1,000 |
|---|---|---|---|
| Clairvoyant MCTS | 54.8% / 0.634 | 54.4% / 0.634 | 52.8% / 0.622 |
| PIMC, 1 world | 56.1% / 0.632 | 54.3% / 0.624 | 53.8% / 0.615 |
| PIMC, 4 worlds | 56.9% / 0.662 | 53.7% / 0.625 | 54.1% / 0.632 |
| IS-MCTS | 57.2% / 0.627 | 57.4% / 0.636 | 56.1% / 0.624 |

### 1.3 The network

Not run. No FDN value checkpoint free of leaks exists for gorge, and upstream's checkpoint is not
gorge's model. Every arm is offline.

### 1.4 Compute

- **Machine.** One AMD 9950X (16 cores, 32 threads, 58 GB), shared with other agents and a vLLM
  server. Single readings swing ±20–35% with load.
- **Load.** 8 workers, one search each, GC off with a 1.5 GiB soft memory limit, each run under
  one 4 GB systemd scope.
- **Unit.** Core-seconds per decision: a run's wall-clock × 8 workers / 1,000 decisions, loading
  included (about 1 s). Upstream reports pod-seconds, wall-clock / 1,000 with 30 workers on a
  31-core pod. Upstream's pod-seconds × 30 is the comparable worker-seconds. SMT, a shared box and
  JVM against Go make that ratio an order of magnitude, not a measurement.

| Core-s / decision: ours (upstream worker-s ≈ pod-s × 30) | 100 | 300 | 1,000 | 3,000 | 10,000 |
|---|---|---|---|---|---|
| Clairvoyant MCTS | 0.089 (0.81) | 0.230 (2.4) | 1.00 (10.2) | 4.11 (42.6) | |
| PIMC, 1 world | 0.079 (0.78) | 0.216 (2.4) | 1.01 (10.2) | 4.40 (38.1) | 18.9 (196) |
| PIMC, 4 worlds | 0.102 (0.69) | 0.234 (2.1) | 0.85 (9.6) | 3.08 (35.4) | |
| IS-MCTS | 0.256 (2.7) | 0.775 (8.4) | 2.95 (31.2) | 12.2 (108) | TODO(is-mcts-b10000) (443) |

- **Per simulation our engine does more work.** Tree methods step the engine 15–33 decisions per
  simulation, because the bot plays the path out to the next searched decision. Upstream's tree
  methods step about 1.02. IS-MCTS steps 50–89 against upstream's 6.3–13.7. Per decision we are
  still about 10× cheaper.
- **IS-MCTS costs 2.8–3.6× PIMC-1** at 100–3,000 simulations (upstream 2.8–3.3×).
- **Totals.** The E2 grid without IS-MCTS at 10,000 took 1.8 h wall and 15.0 user core-hours. E2b
  took 0.6 h and 4.6 core-hours. E1 ran 768 searches at 3,000 simulations on 4 workers.
- **Perf work during the replication** (ledger: `/mnt/sata/gorge-training/perf-sb/RESULTS.md`).
  Merged engine and search optimisations, gated byte-identical on game records, made engine-only
  games about 3× cheaper in CPU and fixed-world search 3.6–4× cheaper (a node state cache among
  them). Between the pilot and grid binaries the cost of a decision fell 1.6× (IS-MCTS, 1,000
  simulations) to 7× (PIMC-1, 1,000). The load and the discount (the pilot ran undiscounted)
  differed between the two runs, so treat those as indicative.

## 2. E0: references

On the 1,000 test decisions.

| Reference | A_set, ours (upstream) | Strict | Balanced | Spell | Hold | Attack | Block |
|---|---|---|---|---|---|---|---|
| Chance (uniform over distinct options) | 43.7% (43.7%) | | | 48.5% (48.3%) | 32.3% (32.6%) | 50.0% (50.0%) | 44.1% (44.0%) |
| Always passive | 71.9% (73.8%) | 56.4% (58.6%) | 0.500 | 62.1% (61%) | 100% (100%) | 56.0% (54%) | 69.5% (80%) |
| Always active | 27.1% (27.8%) | 27.1% | 0.500 | 43.5% (51%) | 0% (0%) | 44.0% (46%) | 21.0% (14%) |
| Uniform random | 43.6% [40.5, 47.2] | 40.0% | 0.529 | 46.9% | 29.6% | 53.0% | 45.0% |
| No search: gorge's bot (upstream: rule heuristic) | 46.3% [43.5, 48.8] (46.9%) | 46.3% | 0.607 [0.577, 0.637] (0.557) | 60.5% | 0.8% | 71.3% | 52.5% |
| #2a / #2b network policies | not run (68.6% / 54.7%) | | | | | | |

- **The label bias is the same as upstream's.** Always passing beats every search. The 2-point gap
  to upstream is mostly blocks: our humans blocked more often (§1.1).
- **Our no-search reference is not upstream's rule heuristic.** It is gorge's redacted auto-pay
  bot, the same `botpolicy` that plays the opponent inside every search. Upstream's heuristic is
  "cast the biggest spell; attack when power ≥ the best blocker's toughness; block when the blocker
  kills and survives". The two land within a point on A_set. Ours is 0.05 higher balanced, because
  it attacks like a top player (attack-or-not 0.708). It casts on 99% of holds.
- **Not run:** a same-settings second-seed rerun (upstream's test–retest check). The
  undiscounted-vs-`-discount 1.0` rerun above checks determinism only.

## 3. E1: the hidden-information test

**Clairvoyant MCTS fails five of six pairs. PIMC-1, PIMC-4 and IS-MCTS pass every pair, with every
difference exactly zero.** This is upstream's verdict, reproduced. Setup: `searchbench leak`,
heuristic leaf, 3,000 simulations, 0.99 per ply, 16 seeds per world, seeds paired across the pair.
Upstream's `leak.py` builds the pairs and the fair methods' public-view worlds in Python
(`scripts/searchbench/leak_prep.py`), and gorge materialises and searches them. ΔQ is on upstream's
[−1, 1] scale.

| Pair | Clairvoyant MCTS, ours: worst option's ΔQ (X − Y), 95% CI; choices X / Y | Verdict | Upstream clairvoyant (offline) | Upstream verdict |
|---|---|---|---|---|
| counterspell (Refute + Island / Island ×2) | Cast Serra Angel −0.372 [−0.476, −0.268]; Pass 15 of 16 / Cast 15 of 16 | fails | −0.37 [−0.45, −0.29]; Pass 10 / Cast 16 | fails |
| counterspell, extreme (Refute ×3 / Island ×3) | −0.334 [−0.447, −0.222]; Pass 14 / Cast 14 | fails | −0.39 [−0.44, −0.34] | fails |
| cantrip (own next draw Llanowar Elves / Plains) | Pass +0.046 [+0.025, +0.067]; p = 0.51 | passes the rule | +0.073 [+0.067, +0.079] | passes the rule |
| cantrip, extreme (next five draws) | Cast Cathar Commando +0.211 [+0.170, +0.251] | fails | +0.086 [+0.070, +0.102] | fails |
| decklist (hidden hand from a list with four counterspells / none) | Cast Serra Angel −0.097 [−0.222, +0.028] | fails (CI leaves ±0.1) | −0.10 [−0.20, −0.00] | fails |
| canary (Counterspell ×2, outside FDN / Island ×2) | −0.267 [−0.393, −0.140]; Pass 13 / Cast 15; the canary reached 16 / 16 searches | fails | −0.31 [−0.41, −0.21]; 16 / 16 | fails |

| Fair methods, every pair | Ours | Upstream |
|---|---|---|
| PIMC-1, PIMC-4, IS-MCTS | ΔQ +0.000 [+0.000, +0.000], identical search in 16 of 16 seeds, p = 1, canary hits 0 / 0 | ΔQ 0.000, the same choice in all 16 seeds, no canary reached |

- **The rule** (docs/012 §2.5): every option's paired ΔQ has a 95% CI inside ±0.1, and the choices
  don't differ (permutation test, p > 0.01).
- **The numbers sit close to upstream's.** Our counterspell leak (−0.37) matches upstream's to two
  decimals. Our extreme cantrip leaks more (+0.21 against +0.09).
- **What the test does not cover.** Upstream's caveat applies: no known-card controls. The public
  view drops every hidden card, including ones the player legitimately knows. On our side, the
  probes are staged at their decision with no walk to reach it, so two world-builder paths are
  never exercised:
  - `p.pin`, which pins the cards the searching seat saw leave its library on the way to the root
    on top of every world's library;
  - `equalLibraries`, which trims every world's library to a common public size.

  Both run on every benchmark item. The leak test vouches for neither.

## 4. E2: method × budget

### 4.1 The grid

A_set, ours (upstream offline). Each of our cells has a 95% CI of about ±3.5 points. Paired
contrasts are tighter.

| Method | 100 | 300 | 1,000 | 3,000 | 10,000 |
|---|---|---|---|---|---|
| Clairvoyant MCTS (fails E1) | 53.8% (46.9%) | 53.9% (50.5%) | 51.8% (52.7%) | 51.6% (53.5%) | — |
| PIMC, 1 world | 56.7% (48.1%) | 54.9% (50.9%) | 54.6% (52.7%) | 55.5% (54.2%) | 54.1% (55.3%) |
| PIMC, 4 worlds | 56.2% (50.3%) | 56.9% (50.4%) | 56.9% (50.2%) | 54.3% (52.9%) | — |
| IS-MCTS | 56.6% (49.5%) | 57.0% (49.1%) | 55.6% (52.1%) | 55.6% (54.2%) | TODO(is-mcts-b10000) (55.4%) |

Balanced: see *The results at a glance*.

Paired differences (A − B, points, 95% CI over games; **bold** = CI excludes zero):

| Comparison | Ours, A_set | Ours, balanced ×100 | Same choice | Upstream offline, A_set |
|---|---|---|---|---|
| PIMC-1 − no search, 3,000 | **+9.2 (+5.3, +13.2)** | +2.4 (−1.8, +6.6) | 50.7% | |
| PIMC-4, 100 − no search | **+9.9 (+6.7, +13.3)** | **+3.8 (+0.5, +7.1)** | 64.0% | |
| IS-MCTS − no search, 3,000 | **+9.3 (+5.8, +12.9)** | +1.5 (−2.3, +5.1) | 53.2% | |
| PIMC-1 − clairvoyant, 3,000 | **+4.0 (+0.6, +7.4)** | +2.9 (−0.6, +6.3) | 66.5% | +0.6 (−2.2, +3.5) |
| IS-MCTS − clairvoyant, 3,000 | **+4.1 (+0.8, +7.0)** | +1.9 (−1.3, +5.1) | 69.9% | +0.7 (−2.0, +3.5) |
| PIMC-1 − clairvoyant, 1,000 | +2.8 (−0.2, +6.1) | +1.5 (−1.6, +4.7) | 69.2% | |
| PIMC-4 − PIMC-1, 1,000 | +2.3 (−0.2, +5.0) | +1.2 (−1.8, +3.9) | 78.6% | −2.5 (−5.2, +0.5) |
| PIMC-4 − PIMC-1, 3,000 | −1.3 (−4.2, +1.5) | −1.6 (−4.6, +1.4) | 77.6% | −1.2 (−4.0, +1.5) |
| IS-MCTS − PIMC-1, 1,000 | +1.0 (−1.8, +3.7) | −0.1 (−3.1, +3.0) | 74.2% | −0.6 (−3.1, +2.0) |
| IS-MCTS − PIMC-1, 3,000 | +0.1 (−2.8, +2.9) | −0.9 (−4.1, +2.1) | 72.9% | 0.0 (−3.0, +3.0) |
| IS-MCTS − PIMC-1, 10,000 | TODO(is-mcts-b10000) | | | +0.1 (−3.1, +2.9) |
| PIMC-1: 1,000 − 100 | −2.1 (−4.9, +0.6) | −1.3 (−4.0, +1.4) | 80.5% | |
| PIMC-1: 3,000 − 100 | −1.2 (−4.3, +2.0) | +0.0 (−3.1, +3.1) | 75.2% | **+6.1 (+2.3, +9.6)** |
| PIMC-1: 3,000 − 1,000 | +0.9 (−0.8, +2.7) | +1.3 (−0.1, +2.9) | 92.0% | +1.4 (−1.1, +4.2) |
| PIMC-1: 10,000 − 3,000 | −1.5 (−3.2, +0.1) | +0.2 (−1.5, +1.7) | 90.9% | +1.1 (−1.2, +3.6) |
| Clairvoyant: 3,000 − 100 | −2.3 (−5.3, +0.7) | −0.7 (−3.5, +2.2) | 74.5% | **+6.7 (+3.1, +10.6)** |
| IS-MCTS: 3,000 − 1,000 | −0.0 (−1.6, +1.5) | +0.5 (−1.0, +2.1) | 91.5% | +2.0 (0.0, +4.1) |
| IS-MCTS: 10,000 − 3,000 | TODO(is-mcts-b10000) | | | +1.2 (−0.6, +3.2) |

- **Search against no search.** Search wins on whether to cast (cast-or-hold +7.7 to +10.5,
  detectable) and on holds (+35 to +40 points of A_set). It loses on whether to attack
  (attack-or-not −7.5 to −8.0, detectable for PIMC-1 and IS-MCTS at 3,000). The two nearly cancel
  on the balanced score.
- **Fair against clairvoyant.** The fair methods' lead is on whether to cast. At 1,000 and 3,000,
  PIMC-1 − clairvoyant is +5.1 and +6.3 on cast-or-hold and +8.8 and +10.4 points on holds, all
  detectable: clairvoyant casts more on holds (70% against 59–62%). Our clairvoyant arm reads
  `real`, whose opponent hand is itself a belief sample, as upstream's is. We have not found the
  mechanism. One untested candidate: `real` keeps the searcher's own library order, so clairvoyant
  MCTS knows its next draws, and that may make casting now look safer.
- **Budget.** No contrast is detectable. PIMC-1 at 10,000 is slightly worse than at 3,000 on spells
  (−3.5 points, detectable) and on which spell (−3.7, detectable), and no better anywhere else.

### 4.2 By decision type

A_set by type, ours. Upstream's offline ranges are in the last row.

| Run | Spell | Hold | Attack | Block |
|---|---|---|---|---|
| Clairvoyant 100 / 300 / 1,000 / 3,000 | 66.7 / 67.2 / 64.8 / 63.5% | 34.4 / 33.6 / 29.6 / 30.4% | 61.3 / 61.0 / 60.0 / 61.3% | 53.0 / 54.0 / 53.0 / 51.0% |
| PIMC-1 100 / 300 / 1,000 / 3,000 / 10,000 | 68.3 / 68.5 / 67.2 / 65.6 / 62.1% | 42.4 / 37.6 / 38.4 / 40.8 / 36.8% | 63.7 / 61.3 / 60.3 / 61.7 / 62.3% | 52.5 / 52.0 / 52.5 / 54.0 / 55.0% |
| PIMC-4 100 / 300 / 1,000 / 3,000 | 64.8 / 70.4 / 72.0 / 69.9% | 39.2 / 39.2 / 38.4 / 31.2% | 64.7 / 64.3 / 64.3 / 62.0% | 56.0 / 53.5 / 53.0 / 54.0% |
| IS-MCTS 100 / 300 / 1,000 / 3,000 | 65.9 / 69.9 / 69.3 / 69.3% | 47.2 / 40.8 / 38.4 / 36.0% | 59.7 / 61.7 / 60.3 / 61.7% | 53.5 / 55.5 / 54.5 / 55.5% |
| No search | 60.5% | 0.8% | 71.3% | 52.5% |
| Chance | 48.5% | 32.3% | 50.0% | 44.1% |
| *Upstream, offline* | *64–69%, flat* | *14–29% at 100, rising to 34–41% at 3,000 (PIMC-4 19–32%)* | *53–64%, flat* | *46–62%, mostly 50–55%* |

- **Holds are where the two engines differ most.** Upstream's searches started below chance at 100
  simulations and learned to wait with budget. Ours already hold on 34–47% of hold decisions at 100,
  above chance, and do not improve. IS-MCTS drifts down from 47% to 36%.
- **Spells, attacks and blocks** sit in or slightly above upstream's ranges and are flat in budget,
  as upstream's were.

## 5. E2b: the backprop discount

**Matched discounts.** Measured on clairvoyant MCTS's undiscounted 1,000-simulation trees, gorge
counts 15.2 plies per searched edge and 28.3 per turn. So 0.95 per ply matches **0.4578 per
action** and **0.2340 per turn**. Upstream used 0.930 and 0.435, from 1.43 and 16.2. The same
nominal per-ply discount is therefore about 10× stronger per action here. Our default 0.99 per ply
is 0.86 per searched edge, about what upstream's 0.9-per-ply arm gives per action. A leaf 120 plies
deep is discounted to 0.99¹²⁰ ≈ 0.30.

Paired differences from the 0.99-per-ply default, 1,000 simulations, A_set points (balanced ×100):

| Arm | Clairvoyant, ours | Upstream clairvoyant, offline | PIMC-4, ours | Upstream PIMC-4, offline |
|---|---|---|---|---|
| 1.0 per ply | +1.0 (−2.2, +4.3); bal +1.9 (−1.1, +4.8) | −0.3 (−1.5, +0.9) | −2.8 (−5.8, +0.1); bal +0.3 (−2.3, +3.0) | −0.1 (−1.1, +0.7) |
| 0.95 per ply | **+3.1 (+0.3, +6.1)**; bal −1.2 (−4.4, +1.6) | −0.8 (−2.8, +1.3) | +2.0 (−0.6, +4.7); bal −0.5 (−2.8, +1.7) | −0.5 (−2.3, +1.3) |
| 0.9 per ply | **+6.2 (+2.8, +9.7)**; bal +0.4 (−3.0, +3.6) | **−3.2 (−5.6, −0.5)** | **+4.0 (+1.0, +7.2)**; bal +1.8 (−1.1, +4.8) | −1.2 (−3.5, +0.8) |
| per action, matched (ours 0.4578; upstream 0.930) | +0.5 (−3.1, +3.9); bal +1.3 (−2.0, +4.4) | +0.3 (−2.0, +2.9) | +0.7 (−2.5, +4.1); bal +1.8 (−1.0, +4.7) | +1.3 (−0.8, +3.3) |
| per turn, matched (ours 0.2340; upstream 0.435) | **+3.5 (+0.4, +6.9)**; bal −1.2 (−4.3, +1.8) | −0.2 (−3.0, +2.6) | +1.8 (−1.0, +4.9); bal −0.8 (−3.5, +2.1) | +2.0 (−0.4, +4.3) |

What moves with the arm (1,000 simulations):

| Arm | Clairvoyant: hold A_set / does nothing / median Q gap | PIMC-4: hold A_set / does nothing / median Q gap |
|---|---|---|
| 1.0 per ply | 24.0% / 32% / 0.455 | 30.4% / 30% / 0.253 |
| 0.99 per ply | 29.6% / 32% / 0.144 | 38.4% / 33% / 0.096 |
| 0.95 per ply | 44.8% / 38% / 0.008 | 49.6% / 40% / 0.021 |
| 0.9 per ply | 51.2% / 42% / 0.003 | 54.4% / 42% / 0.012 |
| per action | 35.2% / 31% / 0.004 | 44.8% / 35% / 0.009 |
| per turn | 42.4% / 41% / 0.013 | 48.0% / 42% / 0.028 |

- **The balanced score doesn't move.** No arm differs detectably from 0.99 per ply on the balanced
  score, for either method. That matches upstream: the discount doesn't matter for agreement.
- **A_set moves the opposite way to upstream's, through passivity.** Stronger per-ply and per-turn
  discounts make the search pass more: holds +10 to +22 points, do-nothing share +5.5 to +9 points
  (both detectable). Which-spell agreement falls 2.5–9 points (detectable for PIMC-4). Passing is right most of the time
  (§8.1), so A_set rises while the balanced score stays put.
- **The discount compresses the root's values.** The median Q gap falls from 0.14 at 0.99 to
  0.003–0.03 under the strong arms. The per-action arm compresses as much without adding
  passivity, so compression alone doesn't explain the shift. One reading is that per-ply and
  per-turn units also discount the bot-played stretches between the seat's decisions, which grow
  when it passes. We have not isolated this.
- **The unit's effect depends on the engine's ply.** Upstream's "the unit barely matters" holds
  here only for per action against per ply. Per turn behaves like a strong per-ply discount.
- **Not computed:** upstream's visit-share check on multi-step options.

## 6. Discussion

- **What carried over to a second engine and search.**
  - The fairness result: fair search loses nothing, and the leak test separates the methods
    cleanly, with nearly the same ΔQ on the counterspell pair.
  - The method ranking: PIMC-1 is as good as anything, PIMC-4 and IS-MCTS add nothing, and IS-MCTS
    costs about 3×.
  - The label bias: always passing scores highest.
  - The agents' over-activity.
  - Discount's irrelevance on the balanced score.
- **What did not carry over: the budget curve.** Upstream's strongest positive result was that more
  simulations steadily helped. On gorge they don't. Our search sees much further: 2–6 turns, against
  upstream's under one. So "deeper search" is not the missing ingredient here. Two candidates, both
  untested hypotheses:
  - the leaf heuristic (`searchprobe.LeafValue`) cannot rank the options the humans pick;
  - a fixed bot plays the opponent inside every path, so the search optimises against that bot
    rather than against a strong opponent.

  An arm with a learned leaf, or with the opponent searched instead of scripted, would separate the
  two.
- **Different engine and search, same plateau.** By 3,000–10,000 simulations both reach
  0.60–0.63 balanced. That is about where gorge's bot already is with no search (0.607), and below
  upstream's human-trained policy (0.646). As upstream concluded, the leaf value and the target of
  imitation look like the bottleneck, not the search method.
- **The benchmark's own limits** are upstream's, plus three of ours:
  - our item set is a near-replica, not upstream's set;
  - the joint attack/block projection and the bot-played opponent change what "the search's choice"
    means on combat items;
  - the per-ply discount is not comparable across engines without matching.

## 7. Follow-ups

Upstream's §7 is about its networks: #2b's human policy as priors, #2a's policy as priors, and
IS-MCTS and PIMC-1 at 10,000 simulations. We ran only the 10,000-simulation arms, reported in §4
(IS-MCTS: TODO(is-mcts-b10000)).

## 8. Why agreement is only about 55%

### 8.1 The labels reward doing nothing

| Decision | Top players, ours (upstream) |
|---|---|
| Attack | attacked with the creature in 44% (46%) |
| Block | didn't block with it in 70% (80%) |
| Hold | passed, by definition |
| Spell | Pass counts as a match in 62% (61%) |
| All | do nothing on 43% (45%) |

Always passive scores 71.9% A_set (73.8%) and always active 27.1% (27.8%), as in §2.

### 8.2 The searches are more active than top players

PIMC-1, 3,000 simulations. The other methods and budgets are within a few points.

| Decision | Top players, ours (upstream) | The search, ours (upstream) |
|---|---|---|
| Attack: attacks with the creature | 44% (46%) | 62% (66%) |
| Block: blocks with it | 30% (20%) | 60% (about 50%) |
| Hold: casts or activates something | 0% (0%) | 59% (64%) |
| Spell: one of the human's casts / another / passes | 100% cast | 52% / 27% / 21% (60% / 30% / 9%) |
| Attacks when the human didn't | | 50% (54%) |
| Blocks when the human didn't | | 50% (48%) |
| Does nothing, all decisions | 43% (45%) | 33% (25–32% across searches) |

This replicates. Our searches pass on spell decisions more than upstream's (21% against 9%) and
block more (60% against about 50%).

### 8.3 Act or not, then which

| Agent | Cast or hold (bal. acc.) | Which spell, both cast | Attack or not | Block or not | Which blocked, both block |
|---|---|---|---|---|---|
| Always passive | 0.50 (0.50) | — | 0.50 (0.50) | 0.50 (0.50) | — |
| No search: gorge bot | 0.503 | 60.7% | **0.708** | 0.610 | 73.8% |
| PIMC-1, 100 | 0.589 (0.57) | 66.8% (67.6%) | 0.651 (0.60) | 0.651 (0.56) | 76.0% (81%) |
| PIMC-1, 3,000 | 0.599 (0.63) | 65.9% (66.4%) | 0.633 (0.64) | 0.662 (0.59) | 76.0% (81%) |
| PIMC-1, 10,000 | 0.596 | 62.1% | 0.639 | 0.664 | 75.5% |
| IS-MCTS, 3,000 | 0.580 (0.64) | 70.3% (66.4%) | 0.628 (0.65) | 0.657 (0.58) | 81.6% (84%) |
| Clairvoyant, 3,000 | 0.536 (0.64) | 62.2% (64.3%) | 0.623 (0.63) | 0.647 (0.61) | 72.0% (78%) |
| #2b human policy (upstream only) | (0.66) | (67.5%) | (0.73) | (0.54) | (74%) |

- **The pattern differs by question.** Ours are lower than upstream's on whether to cast (0.54–0.60
  against 0.63–0.64 at 3,000) and higher on whether to block (0.65–0.66 against 0.58–0.61). Whether
  to attack and which spell are about equal.
- **Budget does not improve whether to act,** unlike upstream (0.57 to 0.63 on cast or hold).
  PIMC-1 goes 0.589 → 0.599 → 0.596 from 100 to 10,000.
- **Gorge's bot is the best attacker here**, 0.708, close to upstream's human-trained policy (0.73).
  On cast or hold it is no better than a constant: it casts whenever it can.
- The "which" columns are small samples: about 300 spell decisions where both cast, and about 50
  blocks where both blocked.

### 8.4 The disagreements are not near-ties

When the search disagrees with the human: the gap between the search's chosen option and the
human's best option, in the search's own values, on upstream's [−1, 1] scale.

| Run | n | Median gap | Under 0.02 | Under 0.05 | Over 0.15 | Human options' share of root visits |
|---|---|---|---|---|---|---|
| PIMC-1, 3,000, ours | 408 | 0.150 | 17% | 28% | 50% | 12% |
| PIMC-1, 3,000, upstream | | 0.043 | 32% | 54% | 19% | about 20% |
| All our 0.99 runs | 381–436 | 0.075–0.160 | 14–31% | 28–41% | 34–54% | 12–30% |

This doesn't replicate. Upstream's search usually could not tell the human's play from its own.
Ours usually can, and prefers its own by a clear margin. The stronger E2b discounts collapse the
gaps to near-ties (§5) without improving the balanced score. Near-ties, then, are not what limits
agreement.

### 8.5 How far the search looks

| Simulations | Ours, PIMC-1: leaf depth in plies / in searched edges / turns crossed per simulation | Upstream, PIMC-1: leaf depth in decisions / turns crossed |
|---|---|---|
| 100 | 65 / 4.4 / 2.3 | 5.7 / 0.26 |
| 1,000 | 120 / 7.9 / 4.3 | 11.1 / 0.69 |
| 3,000 | 144 / 9.7 / 5.2 | 14.4 / 0.95 |
| 10,000 | 165 / 11.3 / 5.9 | |

Our tree holds only the searching seat's decisions, and the bot plays everything between them. So
a path of 4–11 searched edges spans 2–6 turns. Upstream's typical leaf was within the current turn.
Its explanation that budget helps holds because their value lies past the horizon cannot hold here.
Our search reaches the later turns where holding pays off, and still does not hold more with budget.

### 8.6 The method matters less than the run-to-run spread

| Pair of runs | Same choice, ours (upstream) |
|---|---|
| Same settings, second seed | not run (97%) |
| PIMC-1 against clairvoyant, 3,000 | 66.5% (78%) |
| PIMC-1 against IS-MCTS, 3,000 | 72.9% (78%) |
| PIMC-1 against PIMC-4, 3,000 | 77.6% |
| PIMC-1 at 1,000 against 3,000 | 92.0% (83%) |
| PIMC-1 at 3,000 against 10,000 | 90.9% |
| Clairvoyant, 0.99 per ply against undiscounted, 1,000 | 76.0% |
| PIMC-1, 3,000, against no search (gorge bot) | 50.7% |

Our methods agree with each other less than upstream's (67–78% against 78–83%). Our budgets agree
more (91–92% against 83%): our trees have settled by 1,000 simulations. A change of discount
changes as many choices as a change of method. None of these changes moves agreement with the
humans.

### 8.7 What this means for the benchmark

Upstream's recommendations stand and are reinforced. A_set mostly measures passivity: here a
discount change raises it 6 points with no change in the balanced score. Use the act/which split or
the balanced score as the headline. A second engine adds one more point: the per-ply discount and
the depth diagnostics are engine-relative, so they have to be matched, as §5 does, before
comparing across engines.

## Appendix: reproducing

Data lives under `/mnt/sata/gorge-training/searchbench/` and is never committed. That covers
17lands rows, item stores, results and Forge scripts. Heavy commands run through `heavy.sh`, a
machine-wide flock inside a 4 GB systemd scope.

```bash
# items (sb-v1-gorge): upstream's pinned Python half + gorge's builder (inputs: `searchbench -h`)
searchbench build -out sb-v1/gorge2 -workers 3 -games <games.jsonl> -rows <rows.jsonl.gz> \
    -upstream draft-zero -python venv/bin/python -prep scripts/searchbench/prep.py
# E2 grid at 0.99 per ply (and IS-MCTS/PIMC-1 at 10,000); E2b arms in grid-e2b.log
bash sb-v1/gorge2/grid/grid.sh
#   one run: searchbench run -manifest manifest.json -store items.jsonl.gz -arm pimc-1 \
#            -sims 1000 -workers 8 -discount 0.99 -discount-unit ply -out pimc-1-b1000-d0.99.jsonl
# references
searchbench baselines -manifest manifest.json -out runs/baselines
searchbench run ... -arm no-search -sims 0 -out runs/no-search.jsonl
# E1
python scripts/searchbench/leak_prep.py --out leak/probes.json.gz --seeds 16
searchbench leak -probes leak/probes.json.gz -out leak -sims 3000 -seeds 16 -workers 4
# every table in this report (analysis only, no engine; ~5 s)
python3 scripts/searchbench/report_tables.py --out <dir>
# figures in docs/016's style (needs matplotlib in the searchbench venv)
venv/bin/python scripts/searchbench/plot.py --out <prefix> <dir>/e2.json
```

The manifest is `6b9f17f0…` (`sb-v1/gorge2/manifest.json`; Forge `95f04e8a`, compiler
`bfb98bf0…`, 17lands FDN PremierDraft replay data at SHA-256 `11ea7029…`).
