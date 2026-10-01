# Gorge-native replication of DraftZero experiment #3 (search benchmark)

Date: 2026-09-30. Integration branch: `wt/sbrep` (based on `wt/gorge-searchbench`
at 03bb4df89). Upstream: danieljbrooks/draft-zero at `b1e0ba6863182a612a1a06fbac9754cbc979e018`
(MIT), docs/012 (protocol) and docs/016 (results). A pinned clone lives at
`/mnt/sata/gorge-training/searchbench/draft-zero`, with a venv at
`/mnt/sata/gorge-training/searchbench/venv`.

## Goal

Rerun docs/016's experiment on gorge: same public data, same item population
protocol (sb-v1), same four search methods and budgets, gorge's rules engine and
gorge's search. Then publish "our version" of docs/016: the same tables, with
every deviation from upstream stated.

## Audit of `wt/gorge-searchbench` (the earlier agent's branch)

Nothing on that branch is trusted without re-verification. Findings:

1. **Replay staging is unsound for this benchmark.**
   `internal/searchbench/{genesis,stage,turnstage,root_replay,belief}.go`.
   Each game is replayed from turn 1, with casts staged at the first legal
   priority window and every omitted decision (blocks, targets, triggers)
   answered by `seat.Bot`. Nothing checks the board against 17lands'
   end-of-turn snapshots, so board, life and hands drift from the human game.
2. **Hands are wrong.** `ObservedUserDrawPlan` orders the user's library by
   *future plays*, not by `user_turn_N_cards_drawn`. At a root the user's hand
   is not the human's hand.
3. **Honest worlds leak.** `NewGenesis`'s planner puts the opponent's
   *future* plays on top of its library in every world seed, so PIMC/IS-MCTS
   worlds deal the opponent the cards it will actually play. The filler for
   the opponent's deck is the *user's* deck. The world seed only permutes the
   remainder.
4. **Attribution bug.** `replay_source.go actionsAt` reads the opponent's
   instants from `oppo_turn_N_user_instants_sorceries_cast`, which records the
   USER's instants on the opponent's turn. It reads abilities the same way.
5. **IS-MCTS was not IS-MCTS.** `cmd/searchbench rootRun` gives IS-MCTS 4
   worlds, not 8. `newRootWorldSource` cycles a fixed pool with `sim % n` and
   never re-deals.
6. **Roots are not benchmark items.** `root-run` searched every recorded
   land/cast root. There were no spell/hold/attack/block items, and the only
   saved run (`/tmp/gorge-searchbench-root-run.jsonl`) has `skipped=1, sims=0`
   on every row.
7. **Reusable after re-review:** the sealed `Manifest` contract (`manifest.go`),
   `score.go` (balanced accuracy, macro agreement), `analyze.go` (result
   validation), `source.go` (row eligibility), and the azmcts `AutoPayment`
   candidate lift.

Items 1–6 are replaced, not patched (see W5).

## Approach

Reconstruction is the one place where "17lands row → position" is an
interpretation of the data rather than a property of an engine. Upstream's
reconstruction (`draftzero.gameplay.reconstruct`, `coach.plan_determinization`,
`belief`, `pairs`) emits an engine-neutral JSON contract, **StateSpec v1**
(`statespec.py`, FDN names identical in Forge). We therefore:

- **run upstream's pinned Python only as a data-preparation tool**. It gives
  the item's spec, labels, bridge request options, the item's `real` world and
  its 8 belief `worlds`. Using it keeps the positions, labels and belief
  samples identical to upstream's wherever gorge accepts the item, so that
  differences in results isolate engine and search, not data interpretation;
- **do everything that touches the engine in gorge**: materialise a StateSpec
  into a `rules.Engine` through `events.Apply`, advance to the item's decision,
  check legality, enumerate and canonicalise the options, match labels, search,
  and score;
- **port upstream's item-selection loop (`items.py cmd_build`) into Go** with
  gorge's legality checks standing where the XMage bridge's did. The result is
  **sb-v1-gorge**, a sealed manifest with upstream's quotas (test
  375/125/300/200, dev 110/40/90/60) and a rejection ledger.

A Go port of the reconstruction itself is possible follow-up work and is out
of scope here.

## Search semantics (gorge-native, stated deviations)

| | upstream (XMage/MageZero BenchSearch) | gorge (azmcts) |
|---|---|---|
| root | every playable action | Pass plus every canonical cast, activation and land play (the item's options): a payment action or offered option is one intent; a play reached only by tapping mana first (a mana-costed ability, a kicked cast, a scripted-prefix cast) is a recorded macro (the planner's witness, its script, or the exact manual-mana search) played as one edge; the bot's candidate is the play it taps for, never a tap |
| tree | both players' decisions, including micro decisions | the searching seat's searched decisions (priority, attackers, blockers, target); the opponent and unsearched asks are played by `botpolicy` |
| PUCT | c = 1 on values in [-1,1]; unvisited Q = 0 | c = 0.5 on values in [0,1] (the same ratio); unvisited Q = 0.5 (absolute) |
| priors | uniform (priors off) | uniform |
| leaf | `GameStateEvaluator3` at priority decisions | `searchprobe.LeafValue` at the next searched decision |
| discount | 0.99 per ply by default; E2b sweep | same arms: per ply = per engine decision on the path, per action = per searched edge, per turn |
| attack/block | sequential "attack with X?" / "what does X block?" | joint KAttackers/KBlockers, projected onto the item's creature X |
| final choice | most visits | most visits |
| clairvoyant | one tree on `real` | one tree, each simulation a clone of `real` |
| PIMC-k | k trees, split budget, merge root by label | same, merged by semantic key |
| IS-MCTS | one tree; each iteration picks one of 8 worlds and re-deals it | one availability-count tree; each simulation picks a world i and re-deals it (gorge `RedealSource` over world i's decklists) |

## Workstreams

The heavy-job rule applies to every stream. All heavy Go commands go through
`/mnt/sata/gorge-training/searchbench/heavy.sh <cmd>`, which takes a
machine-wide flock and a 4G systemd scope. Data and outputs live under
`/mnt/sata/gorge-training/searchbench/`, never in the repo, and no Forge
script text is committed.

- **W1 spec → engine (`sbrep-spec`).** A Go StateSpec v1 mirror with strict
  decoding, a rules-side staging API that builds an engine from a spec through
  events with no staging-triggered abilities, the enter modes the items use,
  preLand, reaching the item decision, canonical options per item kind,
  label matching, and fidelity refusals as reason codes.
- **W2 upstream prep service (`sbrep-prep`).** The Python selection order
  (games, split, per-game candidate order) and a per-candidate payload (spec,
  labels, real, worlds, opts, or a Python-side rejection). Pairs file built.
- **W3 search arms (`sbrep-search`).** azmcts knobs (absolute unvisited Q,
  discount units, candidate limit, per-root Q/visits/depth/turns-crossed
  export). The four arms over already-materialised world engines. IS-MCTS
  with 8 worlds and a re-deal.
- **W4 analysis (`sbrep-analysis`).** A_set (macro, strict), balanced and
  per-question scores, which-agreement, paired game-cluster bootstrap CIs,
  baselines, activity rates, run-vs-run agreement, Q-gap and depth
  diagnostics, plot data.
- **W5 builder + runner** (after W1 and W2). The Go port of the items.py
  loop, the sealed manifest, the item store, and `searchbench run`, a
  parallel per-item runner with timing at the command boundary. It removes
  the unsound replay code.
- **W6 leak probes** (after W1 and W3). Gorge versions of docs/016 §3's pairs,
  built as StateSpec fixtures.
- **W7 runs and report.** E0 references, E1 probes, the E2 grid (4 methods ×
  100/300/1k/3k, plus 10k for PIMC-1 and IS-MCTS), E2b discount, and a
  learned-value arm if a leak-free FDN value checkpoint exists. Then the
  report.

## 2026-09-30 21:35: discount correction

The pilot (sb-v1/gorge2/runs) and the first grid wave ran with the backup discount OFF (`-discount` defaulted to 1). docs/012 E2 uses 0.99 per ply. Every pilot table is therefore an undiscounted arm. Fix: 683b3cf69 on wt/sbrep-fast makes 0.99 the default, and the grid restarted with an explicit `-discount 0.99 -discount-unit ply` (grid/grid.sh). The undiscounted 1k clairvoyant/pimc-4 runs are E2b's 1.0 arm (grid/undiscounted, identical to the `-d1.0-ply` rows). E2b matched discounts for gorge: 0.95 per ply ≙ 0.4578 per searched edge and 0.2340 per turn. Gorge counts every engine decision on the path as a ply, about 15 per searched edge and 23 per turn; upstream counted 1.43 and 16.2.
