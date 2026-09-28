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
mana source whose trigger stacks before a sorcery-speed cast.

Closed (branch `wt/sb-actions`): every one of those aborts was recoverable,
and the seat now never loses a chosen, payable action. payexec runs
last-resort steps after plain taps (Tinder Wall before Overgrown Battlement
shrank the defender count), hands a foreign ask (a trigger order) back to the
policy and resumes, and waits out a stack a sacrifice trigger filled (Eldrazi
Spawn into Writhing Chrysalis) instead of aborting. `rules.Engine.
PotentialPaymentPlans` gives each potential play the exact planner's verdict:
a witness for a printed activated ability's mana part (its own and its
cost's tap/sacrifice candidates withheld), and a proof when a play the
over-bound walk offers cannot be paid. An aborted lowering re-plans the same
play from the pool it holds; a refused answer is retried with the policy's
next choice. Measured on the same 768-game workup (4 sb seats, 8 pairs):
lowering aborts 59 -> 0; "legal actions lost" (chosen, planner-payable, not
taken) 0 in every seat; about 1,600-2,000 activated abilities per seat are
now lowered from a witness, and about 2,200-2,650 over-bound plays per seat
are dropped as proven unpayable instead of being tapped out for. The
remaining naive pursuit failures are plays no planner prices (a census with
an unpriceable source, X costs, flashback), not proven-legal plays. The v2
agent (`cmd/sbagent`) now answers every recoverable request instead of
erroring (a host forfeit): policy failure, unknown or stale game, missing
candidate ids, an echo that would not round-trip; `-stats` counts each.
On the v2 harness (`sbv2_harness.py tournament --pairs 2`, 84 games against
the python bots): 0 forfeits, 0 halts, empty agent stderr, twin parity 0
mismatches over 60 replays; sbagent-heuristic scored 90.0% (16-4-0), ahead
of the python heuristic at 82.5%.

### 3.4 v1b: sb-tactical (measured)

`sb-tactical` (`internal/spellbench/builtins/tactical*.go`, Policy
`Tactical`) is a hand-written scored heuristic built for the v2 observation
constraint: every answer is a function of the seat's projected `view.View`
(public battlefields, stack, graveyards, life, library and hand *sizes*; its
own hand and pool), the decision's options, and printed card facts its card
names resolve to in gorge's IR. It never reads the opponent's hand or any
library. It reuses gorge's seat-visible combat simulation
(`botpolicy.AttackSimDecide` over `seat.BoardFromView`) and falls back to
`botpolicy.Decide` on the same Board for asks it does not model (modes,
trigger order, searches).

- **Structure.** Feature extraction (`tactical_state.go`: derived P/T and
  keywords, projected damage per attack and turns-to-kill both ways, mana on
  hand) → one score per candidate (`scoreCand`), pass = 0, every weight in
  `TacticalWeights`. Card IR is labelled with an effect *direction*
  (`tactical_ir.go`: removal, damage, counter, pump, debuff, draw, ramp, mill,
  fog...), so targets are scored as "the change to the target controller's
  position, signed by whose it is" plus riders to the target's controller
  (`tactical_target.go`). That one rule points harm at the opponent and help
  at us, and also finds deliberate exceptions: Cleansing Wildfire on our own
  indestructible Bridge (no loss, we get the land and the card).
- **Idea groups** (each switchable, the ablation arms):
  1. *EarlyGame* — land first, ramp and draw weighted up in the first four
     own turns, removal bonus for key pieces (mana creatures, engines,
     evasive threats).
  2. *Timing* — instant-speed plays have windows: counters only at a foreign
     spell, combat answers in combat, everything else at the opponent's end
     step; outside its window a play loses `max(Hold|WaitEOT, 0.6·value)`;
     proactive casts pay `KeepUp` for tapping below a held answer.
  3. *Race* — burn face-vs-creature by the race state, an alpha strike when
     the team is lethal through the best blocks (or when a never-blocking
     opponent is out-raced), otherwise the combat simulation; blocks chump
     only when the hit is lethal, drop chumps when we are ahead and the hit
     is safe, and double-block a big attacker that two free blockers kill
     profitably.
  4. *Archetype* (`sb-tactical-arch`; off in plain `sb-tactical`) — read
     the opponent's mana flavours from public cards
     (`tactical_archetype.go`: printed mana costs and projected mana
     production, never card names) and accumulate eight IR-derived features
     over their public cards: curve and speed (creature mana values and cheap
     early bodies), burn to face (`effDamage` targeting a player, `effDrain`),
     counterspells (`effCounter`), removal density (`effRemoval`), mana
     creatures and recurring engines (`manaSource` on a creature, `engine`),
     artifacts and affinity, token makers (`effToken`) and small bodies,
     evasion, card flow (draw/select/flashback/recursion) and self-mill. The
     features start from colour priors (R aggro/burn, U tempo/control, G
     ramp/midrange, W go-wide, B removal/drain) and produce a normalised
     score over eight archetypes — aggro, burn, tempo, control, engine/combo,
     ramp, go-wide, midrange — updated after every newly public card, blended
     rather than switched on one top archetype. The role ("who's the
     beatdown") comes from the Race group's clocks (`myClock` vs `oppClock`)
     and re-evaluates every turn. The counter-rows are multiplicative
     modulators of the existing weights, each behind its own ablation switch
     (`ArchBurn`, `ArchCounter`, `ArchEngine`, `ArchControl`, `ArchWide`,
     `ArchTempo`): against burn/aggro our life is worth more (`ArchLife`) and
     we chump earlier (`ArchBlock`); with open counter mana or a counter
     already seen we hold the best threat and bait with the lesser spell
     (`ArchBait`) and keep up (`KeepUp`, `HoldReactive`); against an
     engine/ramp deck removal on mana creatures and engines is worth more
     (`KeyPiece`); against control we value card advantage up and do not
     overextend (`ArchOverextend`); against go-wide we devalue one-for-one
     removal; against tempo/fliers removal on evasive threats is worth more.
     An unknown opponent leaves every weight exactly 1.0×.
- **Also fixed while measuring** (each found in a decision trace,
  `botbench -spellbench-trace <dir>`): pursuits of unaffordable plays
  (colour-aware `canAfford`), card flow at the wrong time (treasure/cycling in
  upkeep), cost-aware sacrifice and discard choices, {X} spells tapped out
  before casting, a counter *trigger* with no target, deck-out races (no
  extra draws with a short library), hand-size overflow, milling ourselves.

**Measured (commit `fd8c30e39`).** Benchmark-shaped round robin, 8 pairs per
deck (128 games per pair of bots), rated with `scripts/spellbench-rate.py`;
0 truncated, 0 halted, 0 refused-answer fallbacks for any sb-tactical seat.

| Bot | pauper-kernel, benchmark seed 20260926 | held-out seed 99991 | held-out decks: FDN Limited, 4 pairs |
|---|---|---|---|
| sb-tactical | **1507** [1465, 1555] 527-113 | **1538** [1477, 1607] 348-36 | **1444** [1392, 1504] 318-66 |
| sb-tactical-planned | 1509 [1465, 1561] 528-112 | — | — |
| bot | 1242 [1204, 1284] 330-310 | 1277 [1226, 1331] 228-156 | 1315 [1268, 1373] 249-135 |
| sb-heuristic-planned | 1094 [1057, 1132] | — | — |
| sb-heuristic | 1078 [1041, 1116] 194-446 | 1073 [1021, 1125] 115-269 | 1114 [1073, 1161] 131-253 |
| sb-uniform (anchor) | 1000 | 1000 | 1000 |

Head to head sb-tactical vs bot: 110-18 (benchmark seed), 106-22 (seed
99991), 87-41 (FDN); vs sb-heuristic 116-12, 116-12, 111-17. The edge over
`bot` roughly halves on decks it was never looked at (+130 Elo on FDN vs
+260 on pauper-kernel). Mean wall time per game (8 workers): about 50 ms
against sb-heuristic, 80 ms against bot (bot vs sb-heuristic is 50 ms).
Pursuits failed 15% (684/4430) against sb-heuristic's 35%.

**Ablation** (same run, all nine arms in one field): full 1506 [1473, 1543];
no-EarlyGame 1522 [1487, 1560]; no-Timing 1511 [1476, 1548]; no-Race 1449
[1415, 1487]. Only the Race group is a measured gain (+57); the early-game
and timing groups are neutral within the CI. Every arm keeps the shared
fixes above, which is where most of the strength is.

**Tuning honesty.** Three runs on the benchmark seed (the first cut at
1359, then 1401) drove the first round of fixes, including the Spy
investigation; everything after that used dev seeds 777-781 and 5000+
(about 25 runs, 128-512 games per pair). One weight change came from those
runs: `PostCombat` 20 → 0, measured +4-5 points of win rate. A 42-iteration
SPSA over 30 weights (log-space, seeds 5000-5041) moved no weight by more
than 6% and was discarded, so the landscape near the defaults is flat and
the weights are hand-set. Seed 99991 and the FDN catalog were each run once,
after the code was frozen.

**Card-specific knowledge**, and whether it generalises: `Count$Valid`,
`Count$ValidGraveyard` and `Count$Metalcraft` amounts (Timberwatch Elf,
Priest of Titania, Galvanic Blast, Lotleth Giant) use a small generic
Forge-grammar evaluator. Spellstutter Sprite's "mana value X or less" reads
the card's own SVar X. Consumables (clue, food, treasure, Lotus Petal) are
derived from "sacrifice this" abilities, not names. A fog is any
`Prevent$ True` replacement effect. The Forge AI hint `SVar:PlayMain1` is
honoured. Three rules are generic in form but were written for a specific
card or opponent: the mill value assumes "mill until a land" (Balustrade
Spy; it over-values fixed-count mills); the dig amounts (a third of the
cards when filtered, half for "all of a type") were set by eye on Lead the
Stampede and Winding Way; and the never-blocks opponent model exploits
sb-heuristic's fixed no-block rule. No card is named in the code.

**Known weaknesses.** The Spy combo is not played: self-mill into Dread
Return with Lotleth Giant needs plan knowledge, and the seat deliberately
never mills itself. Spy is its weakest deck (10-6 against bot on the benchmark seed, against 13-3 to 16-0 elsewhere). There
is no mulligan logic (the runner poses none). Plan choice is moot because
gorge offers one plan per cast. The early-game and timing weights are
unvalidated. On FDN the lead over `bot` is smaller, as expected from
pauper-driven fixes.

Rerun (worktree root, a built `botbench`, heavy-job wrapper per the
operator rules):

1. `botbench -dir .cards -workers 8 -spellbench sb-uniform,sb-heuristic,bot,sb-heuristic-planned,sb-tactical,sb-tactical-planned,sb-tactical-noearly,sb-tactical-notiming,sb-tactical-norace -spellbench-pairs 8 -spellbench-out <dir>`
2. Rate with `scripts/spellbench-rate.py --anchor sb-uniform --out <dir>/rate <dir>`, adding `--bots sb-uniform,sb-heuristic,bot,sb-heuristic-planned,sb-tactical,sb-tactical-planned` for the main table.
3. For the held-out runs, add `-spellbench-base-seed 99991` (and `--base-seed 99991` when rating), or use `-spellbench-catalog fdn -spellbench-pairs 4` (and `--format fdn-limited-bo1` when rating).

Data: `/mnt/sata/gorge-training/spellbench-work/v1b/{final-bench,heldout-seed,heldout-fdn}`.

### 3.5 Arena on mtg-kernel (v1, measured)

The public pauper-kernel leaderboard (g115 1388, a48 1238, c12 1226,
heuristic 1102, uniform 1000, first 867;
`spellbench/benchmarks/pauper-kernel/runs/2026-09-27`) is rated on
mtg-kernel through protocol v1. This subsection puts gorge's bots on that
engine, in that benchmark's shape, rated by SpellBench's own code.

**What is and is not obtainable.**

- *The engine bridge is private.* Jack's `agent_bridge_v1` (engine
  `source_revision` `1ca8eb40` in the published manifest) is not in the
  public `jackmaiorino/mtg-kernel` (main @ `2c5e72f`, none of its 145 refs
  mention it, and `1ca8eb40` is not fetchable: "not our ref"). We rebuilt
  it (below).
- *g115 (and a48, c12) are not runnable.* The benchmark's own summary says
  "the mtg-kernel engine build and the model checkpoints are private".
  Running g115 needs four things, none public: the checkpoint
  (`${G115_CHECKPOINT}`, model-state sha256 `8139016c…`), its scorer
  config (`${G115_SCORER_CONFIG}`), the native `spellbench_scorer_v1`
  scorer binary (absent from every public mtg-kernel ref we fetched,
  including `lead/cp7-scorer-v5*` and `lead/phase1-*`), and the bridge's
  `--x-kernel-flat-v4` extension (the Phase 1 model input, part of the
  private bridge contract). Only the public client
  (`spellbench/integrations/mtg_kernel/kernel_flat_bot.py`) exists. The
  published ledger is the only g115 evidence we can use (below).
- *sb-tactical (D§3.4, merged on spellbench-prep) cannot play here as is.*
  It is a gorge-native seat policy: it reads gorge's `view.CardView` and
  `state` ids, so on a foreign engine it needs the shadow state of D§4.
  The mtg-kernel analogue is `v1agent.Tactical` (below); the two share
  the approach (score candidates against the visible board) but no code.

**Setup (local only; nothing pushed to any external repo).**

- *Engine bridge rebuild,* `scripts/spellbench-arena/agent_bridge_v1.rs`:
  one Rust bin over the public in-process `RlEpisodeSessionV1` (policy
  surface V5, legal scan answers only). The kernel's `ActionSemanticV1`
  maps one-to-one onto the v1 candidate kinds (the aggregate `declare_*`
  and `ambiguous` actions never reach a V5 session; if one did, the bridge
  would halt the game rather than guess). `state_summary` is projected from
  `ObservationV5`; every decision carries `x_kernel_v5`
  (`observation_json`, `legal_actions_json`) as spec §9 says the reference
  bridge does (`--no-x-kernel-v5` omits it). A refused step or an
  unmappable action becomes a `halted` terminal, never a guessed move. It
  passes SpellBench's engine-conformance test on all eight decks (16/16).
  Known differences from the published build: card DB `64c82a261e078f1a`
  vs `064a7c989255ab3c` (card fixes merged since, e.g. PR #109's goad,
  menace and linked-exile fixes), and our own `game_seed` → kernel seed
  mapping (legacy reset, `env_seed = game_seed`), so no individual
  published game replays; distributions are what compare.
- *Build.* A user-local Rust 1.98 was already in `~/.rustup`; the repo pins
  1.94.1, overridden with `RUSTUP_TOOLCHAIN`. With thin LTO off the
  release build takes 38 s:

      W=/mnt/sata/gorge-training/spellbench-work/arena/mtg-kernel
      git -C /mnt/sata/gorge-training/mtg-kernel worktree add -b sb-arena-bridge $W HEAD
      cp scripts/spellbench-arena/agent_bridge_v1.rs $W/mtg-kernel/src/bin/
      cd $W && RUSTUP_TOOLCHAIN=1.98-x86_64-unknown-linux-gnu CARGO_BUILD_JOBS=6 \
        CARGO_PROFILE_RELEASE_CODEGEN_UNITS=16 CARGO_PROFILE_RELEASE_LTO=false \
        cargo build --release --locked --bin agent_bridge_v1
      SPELLBENCH_ENGINE_BIN=$W/target/release/agent_bridge_v1 \
        SPELLBENCH_ENGINE_DECKS=Wildfire,Rally,Affinity,Elves,Spy,Burn,CawGates,Faeries \
        pytest spellbench/python/tests/test_engine_conformance.py

- *Agent,* `cmd/sbv1agent` over `internal/spellbench/v1agent`: the v1 agent
  role (hello, game_start, choose, game_over; single-entry idempotent
  retry; the closed error-code set) for any Go `Policy`
  (`GameStart`, `Choose(*Decision) int`, `GameOver`). A policy panic or
  out-of-range answer never reaches the wire (a wire error forfeits the
  game): the heuristic answers instead and the agent counts it; `-stats
  FILE` appends one JSON line of counters per game. Policies: `uniform`,
  `heuristic`, `first` (the python builtins ported choice for choice) and
  `tactical`.
- *Arena,* SpellBench's own runner and rating code: `spellbench run` on a
  config from `scripts/spellbench-arena/mkcfg.py` (the benchmark's deck
  pool, seat-swapped pairs per deck, caps, timeouts, 2000 bootstrap
  replicates, uniform anchor), e.g.

      python3 scripts/spellbench-arena/mkcfg.py pub.json RUN_DIR 4 20260926 \
        builtin:uniform builtin:heuristic builtin:first go:sbv1-tactical:tactical
      spellbench run pub.json          # every arena run under heavy2.lock

  `scripts/spellbench-arena/rate.py` re-rates ledgers with
  `leaderboard.build_leaderboard` when `spellbench run` refuses its final
  write: a lopsided matchup (e.g. 125-3) gives the exact sign-test p-value
  a denominator above 2^53, which the canonical-JSON writer rejects (an
  upstream bug; the ratings are unaffected). `wl.py` and `view.sh`
  summarise a ledger and a `-trace` file.

**Correctness checks.**

- *Builtins reproduced.* uniform/heuristic/first on the rebuilt bridge in
  the benchmark shape (4 pairs per deck, base seed 20260926): heuristic
  1112.7 [1037, 1197] vs the published 1101.6; first 810 vs 867;
  heuristic vs uniform 45-19, the published count exactly. Within noise.
- *Go ports exact.* The same 192-game config with the three Go ports
  (subprocesses named like the builtins) gave 192/192 games identical to
  the python bots (outcome, step count and decision count per game id).

**Tactical.** v1's neutral observation is counts only, but on mtg-kernel
every decision carries `x_kernel_v5`: the acting seat's own ObservationV5
(both battlefields with effective power, toughness and keywords, tapped
and sick state, damage, counters and attachments; graveyards; exile; the
stack with targets; combat; its own hand; the pending effect's purpose;
its dungeon room), observer-relative and licensed by spec §9 (g115's
entry reads a kernel extension too). `NewBoard` reads it; without it the
policy falls back to the wire references (and to the heuristic for
combat). Card knowledge: `cardfacts.json`, generated from gorge's
compiled IR for every catalog card (`TestCardFactsMatchIR` regenerates and
diffs it; derived facts only: types, mana value, P/T, keyword heads,
effect APIs, damage, target class, polarity); `kernelcards.json` (the
kernel card DB, MIT: card ids for stack and hidden objects, costs, mana
production); `hints.go` (roles, timing and target polarity for the pool).
Every candidate is scored and the best taken:

- lands first, tapped lands when no spell needs the mana this turn,
  colours the hand lacks; creatures and sorcery-speed spells in main phase
  only; flash creatures on the opponent's attack or end step; counters
  only against an opposing spell worth it; burn at a creature it kills or
  face (burn decks, or when in reach); removal only at a creature worth it;
  X (posed by the kernel as a `choose_option` index) at maximum;
- targets by polarity (harmful at the opponent, beneficial at us);
  searches and reveals by card value; scry and Brainstorm by the pending
  effect's `purpose` (`card_selection` bottoms the unwanted, a hand
  `library_order` puts back the worst); Undercity rooms by the kernel's
  room order (Forge, Trap, Catacombs) and room-aware target polarity;
- attacks and blocks planned once per scan group (the kernel poses them as
  include/exclude substeps) and every substep answered from the plan:
  attack when no blocker eats the attacker for free, all-in when lethal
  gets through, hold back what the crack-back (evasion-aware) needs unless
  we win the race; blocks that kill and survive, double blocks that kill
  a big attacker for at most one blocker, free blocks, even trades, chumps
  when life gets low or the hit is lethal, all judged against the pump an
  untapped Timberwatch Elf or Basilisk Gate could add; an in-game model
  of the opponent (it passed up at least two chances to block) lets
  attacks ignore its blockers. No state crosses games.

**Results (2026-09-28).** Benchmark shape: the eight-deck pool, 4
seat-swapped mirror pairs per deck, base seed 20260926 (the published
run's), workers 8, our bridge with `x_kernel_v5`. Final build `tac9` =
`sbv1agent` at the head of `wt/sb-arena` (md5 `451f5f7f…`), run
`runs/final`:

| Bot | Elo | CI95 | W-L |
|---|---|---|---|
| sbv1-tactical (tac9) | 1430 | [1362, 1521] | 176-16 |
| heuristic | 1141 | [1070, 1218] | 109-83 |
| uniform (anchor) | 1000 | anchor | 70-122 |
| first | 841 | [756, 917] | 29-163 |

Head to head: tactical beats heuristic 50-14, uniform 64-0, first 62-2.
The previous build on the same seeds (`tac7`, run `runs/pub2`, pool also
holding `tac6`): 1444 [1383, 1513], heuristic 57-7, uniform 62-2, first
61-3; tac7 vs tac6 31-33, and tac7 vs tac9 64-64 at 8 pairs (`runs/dev9`,
where tac9 went 107-21 / 124-4 / 124-4 against heuristic / uniform /
first). The two builds are one policy within noise.

*Against g115, on the public scale.* No g115 game can be played (above),
so the comparison is through the common opponents. Against the three
builtins on the same seed schedule, g115 went 173-19 (57-7 heuristic,
55-9 uniform, 61-3 first) and sbv1-tactical 176-16 (tac7: 180-12). One
Bradley-Terry fit over the published ledger plus `runs/final`
(`rate.py`, builtins shared by bot id): **sbv1-tactical 1426 [1360,
1509], g115 1399 [1335, 1476]**, a48 1248, c12 1237, heuristic 1133,
first 841 (tac7 in the same fit: 1441 vs g115 1400). The CIs overlap; this
is not a head-to-head result, and the two sides of it ran on different
kernel builds (card DB `064a7c98…` vs `64c82a26…`). Per deck against the
builtins: g115 is stronger with Affinity (24-0 vs 20-4) and Elves (24-0 vs
21-3); tactical with CawGates (23-1 vs 15-9) and Spy (20-4 vs 18-6); the
other four decks are even (23-1 each).

**Held-out seed (pool tuning check).** `hints.go` and several branches of
`tactical.go` were written against these eight decks, and the published
seed 20260926 was used for `pub1`, `pub2` and `final`. So the final
configuration was run once more, frozen at the rebased tip (`b741760e5`,
binary md5 `3d891db2…`), on seed 99991 (never used before), same 4 pairs,
same roster (`runs/heldout`):

| | seed 20260926 (`final`) | seed 99991 (`heldout`) |
|---|---|---|
| sbv1-tactical Elo | 1430 [1362, 1521] | 1412 [1327, 1522] |
| W-L | 176-16 | 176-16 |
| vs heuristic | 50-14 | 55-9 |
| vs uniform | 64-0 | 61-3 |
| vs first | 62-2 | 60-4 |
| heuristic Elo | 1141 | 1102 |

Per deck against the builtins the held-out run ranges from 20-4 (Elves,
Spy) to 24-0 (Burn, Wildfire), the same spread as on the published seed.
So the result does not depend on the seed. That is **not** a test of
card-pool tuning, though: the held-out games use the same eight decks,
and every nonland card in them is named in `hints.go`.

*How much is hand-written.* `hints.go` has 106 entries, one per card
name. Together they cover all 93 distinct nonland cards of the eight
decks, plus 11 of the 19 lands (tapped lands and the gates), and two
tokens (Blood, Eldrazi Spawn). Fields set: `role` 94, `ab` (activated
ability use) 25, `pol` (target polarity) 17, `bonus` 13, `tapped` 8,
`dmg` 7, `flash` 3, `sacCost` 3, `selfLand` 1. 18 entries are only
`role: RoleCreature`, which the kernel card types already say. On top of the
table, `tactical.go` names 37 cards in its own branches (for example the
Spy combo, Fireblast's alt cost, the Undercity rooms, Timberwatch and
Basilisk Gate pumps, the counterspell conditions, Lotus Petal), and
`aggro()` names four decks. What comes from card data rather than names:
all numbers (P/T, mana value, keywords, damage marked, counters: kernel
observation), types and costs (`kernelcards.json`), and target polarity
for any card without a `pol` or role (`cardfacts.json`: the IR's harmful
flag). *A card with no hint* is played as follows:

- a creature: cast at sorcery speed (or at instant speed if the IR or the
  kernel says flash), valued from its observed P/T and keywords;
- a noncreature spell: scored 20 (below lands, creatures and every
  hinted role), so it is cast at sorcery speed, or any time if it is an
  instant, whenever nothing better is offered;
- its targets: from the IR's harmful flag;
- an activated ability: never used (the `ab` default is "never").
  Mana abilities are handled separately, so this is not a mana problem.

A generic-strength measurement needs decks the hints never saw (the
kernel also ships a `Terror` list: 9 of its nonland cards have no hint).
That has not been run.

**Protocol errors, rejections, timeouts.** Across every arena ledger kept under
`runs/` (5,184 games, 1,541 tactical agent processes with `-stats`,
187,339 decisions; an earlier revision said 5,568 games, an overcount):
0 forfeits, 0 timeouts, 0 truncated games and 0 refused steps; the agent
answered no request with an error and needed 0 fallbacks, with
`x_kernel_v5` present on every decision. 7 games ended `halted`, none of
them caused by a tactical move, from two mtg-kernel bugs:

- 5 games, all Faeries mirrors: `InvalidEffectContinuation` on the
  resolution of a ninjutsu activated in declare blockers
  (`dev2 m0000p0007g1` seed 1677312375586864, heuristic vs uniform:
  uniform activated Moon-Circuit Hacker's ninjutsu at step 176;
  `dev6 m0001p0023g0` seed 8790084201229895, uniform vs tac5; three more
  in `dev2`/`dev3`). The tactical policy ninjutsus at most once per
  combat and never returns a creature that entered this turn, which
  removed the halts its own play caused.
- 2 games, one CawGates pair of heuristic vs first (`heldout
  m0003p0014g0/g1`, seed 4016374969801364): the legal scan offers
  casting Journey to Nowhere with no creature on either battlefield; the
  cast is taken and the next decision has zero legal actions, which the
  bridge fails closed on (`fail_closed: nonterminal decision produced zero
  legal actions`). Tactical casts removal only at a creature worth it.

Both replay exactly with `scripts/spellbench-arena/replay.py SEED DECK DECK
GAME_ID P0 P1 -1 /dev/null`. Nothing was reported upstream. (The
published ledger's 7 halted games are a different, bridge-side cause:
`flat_v4_lockstep:selection_not_in_training_list`, all Elves.)

**Gaps and what would raise the kernel rating most.**

- *A real g115 measurement.* Everything above is transitive. The single
  most useful next step is a g115 head-to-head: that needs Jack's scorer,
  checkpoint and bridge (or him running our `sbv1agent` in his arena; it
  needs only the v1 wire and `x_kernel_v5`).
- *Play strength.* Tactical is one-ply. The losses that remain against the
  builtins are mostly mana screw (the kernel has no mulligan), races
  lost by a turn, and pump tricks after blocks. The biggest levers are
  (1) combat lookahead (simulate the declared combat with the pumps and
  instants the opponent can afford), (2) the Elves and Affinity lines g115
  plays better (Priest of Titania mana for big turns, affinity
  sequencing), (3) search: the kernel's session has `snapshot_v5` /
  `restore_v5`, and both decklists are public in `game_start`, so a
  determinized search over sampled hands is possible with an in-process
  kernel (the agent cannot query the engine; it would need a kernel state
  builder from `ObservationV5`, which the public API lacks).
- *gorge-native vs mtg-kernel.* Gorge-native (W1): sb-heuristic 1080,
  gorge `bot` 1238. On mtg-kernel the python heuristic rates 1100-1140 and
  tactical 1430-1445. The engines differ in decision granularity (kernel:
  ~200-500 decisions a game, mana auto-paid for casts, X as an option
  index, combat as include/exclude scans) and in hidden-information
  surface (the kernel extension shows the whole public board; v1's neutral
  observation shows counts only).

**Rerun.**

    W=/mnt/sata/gorge-training/spellbench-work/arena
    go build -o $W/bin/sbv1agent ./cmd/sbv1agent
    go test ./internal/spellbench/v1agent/ ./cmd/sbv1agent/
    python3 scripts/spellbench-arena/mkcfg.py final.json $W/runs/final 4 20260926 \
      builtin:uniform builtin:heuristic builtin:first \
      go:sbv1-tactical:tactical:-stats,$W/runs/final.stats
    spellbench run final.json
    python3 scripts/spellbench-arena/rate.py joint.md \
      /mnt/sata/gorge-training/spellbench/benchmarks/pauper-kernel/runs/2026-09-27 $W/runs/final

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
which the host turns into an `agent_error` forfeit -- was closed by the
agent's recovery paths (`v2agent.Stats`, `sbagent -stats`: the heuristic
fallback answers a failed policy); these runs predate that merge and used a
candidate-0 answer, which never fired either. Choice
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
| M1 | gorge-native botbench | (Q1) L10 with redeal-only worlds vs full sampler; (Q2) honest az25/az100 with the D§5.2 world source vs `bot` | Q2 done (D§12.5): K1 passes, az25 +14.7pp, az100 +20.5pp over `bot`; Q1 open |
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

**Policy networks (v1c).** The network this agent's search consumes (D§6
prior and leaf, D§7 fallback, D§8 specialists) is surveyed and planned in
`docs/superpowers/reports/2026-09-28-spellbench-policy-networks.md`. In
short, the network serves search and does not replace it. Value is trained
first, as two heads: a seat-view value for the root and a full-world value
that is used only at the leaves of sampled worlds. The input is an entity
table with two front ends: gorge's view, and the v2 neutral observation,
which must agree bit for bit on gorge-engine transcripts. Candidates are
scored pointer-style over the v2 list. Policy targets come only from honest
search, with Gumbel root selection. Stages S0-S5 carry kill criteria. S3
(honest expert iteration) waits for M1 Q2. Its open questions (O1-O9) sit
alongside D§14.

### 12.5 M1 result (Q2, measured 2026-09-28)

**Verdict: K1 PASSES.** Honest search beats `bot` by far more than 3pp at
more than 2,000 games, CI clear of 0: az25 on redeal worlds wins **64.7%
[63.2, 66.2]** over 4,000 games against a same-seed `bot`-vs-`bot` control
of 50.0% [48.4, 51.5], a gain of **+14.7pp [12.6, 16.8]** (about +106 Elo).
az100 gains **+20.5pp [18.0, 23.0]** (70.5%, 2,000 games). Search stays on
the critical path; T1 (the gorge bot on the shadow) remains the fallback.

**World source.** `azmcts.RedealSource` (`internal/azmcts/redeal.go`,
`-az-world redeal` or policy `az-redeal` in botbench and `-spellbench`) is
the D§5.2 v1 sampler with the list posterior a point mass: every simulation
walks a `searchprobe.Redealer` deal (`internal/searchprobe/redeal.go`, the
pn21 `RedealBase` redeal, now exported as prepare-once / deal-many). Public
state, zone sizes, the seat's own hand and every card the known-card
projection pins stay put. Each player's other hidden cards (the seat's own
library order, the opponent's hand and library) are dealt uniformly from the
pool the seat can derive: the declared list minus every card it has seen
outside the hidden zones. Future chance is re-seeded, per-world seeds are
SplitMix64 of the per-decision seed, and every change is a Secret event on
the world's own log. A pool the list cannot account for (a face-down card, a
wrong list) refuses: the refusal is counted (`redeal-refused`) and the bot's
answer is played, never a clairvoyant world. The leak test
(`TestRedealWorldsIgnoreTheRealHiddenCards`) swaps the opponent's real hand
cards with library cards of other names and reverses both libraries, then
checks that every seed deals name-identical worlds. `-az-worlds K` deals K
worlds round-robin, each simulation with fresh chance.

**Setting.** Main table: stage 0's matrix (`uw-tempo` against the five mono
decks, seat-alternating, 5 pairs), chunked at 100 games per pair per chunk,
seeds `91000000 + 1000·chunk` (50 per pair for the 100-simulation arms).
The seat is given the true lists (the benchmark's `visible` case), and its
known-card projection comes from the full gorge observation feed.
`bot` is both the opponent and the search's environment model, which
flatters every search arm equally; the round robin below also covers
non-`bot` opponents. Gen 0 throughout: uniform prior, heuristic leaf, no
network.

| Arm | Games | Win % [CI95] | vs control | ms/searched decision mean (p95) | games/h (8 workers) |
|---|---|---|---|---|---|
| `bot` vs `bot` (control) | 4,000 | 50.0 [48.4, 51.5] | — | — | ~550k |
| az25, redeal | 4,000 | 64.7 [63.2, 66.2] | +14.7 [12.6, 16.8] | 45 (107) | 15.6k |
| az25, clairvoyant | 4,000 | 75.4 [74.1, 76.8] | +25.4 | 46 (112) | 14.6k |
| az100, redeal | 2,000 | 70.5 [68.5, 72.5] | +20.5 [18.0, 23.0] | 216-278 uncontended (380-550) | 2.5k |
| az100, clairvoyant | 1,000 | 81.1 [78.7, 83.5] | +31.1 | 230-261 (474-574) | 3.1k |
| az25, redeal, K=1 world | 1,000 | 63.7 [60.7, 66.7] | +13.7 | 40 (90) | 18k |
| az100, redeal, K=4 worlds | 500 | 68.8 [64.7, 72.9] | +18.8 | 192-219 (367-414) | 2.7k |

- **Cost of honesty:** 10.7pp [8.7, 12.7] at 25 simulations and
  10.6pp [7.5, 13.7] at 100. It does not shrink with budget: both sources
  gain about 6pp from 4x the simulations (redeal +5.8 [3.3, 8.3]).
- **Worlds per decision:** no measurable strategy-fusion effect at gen 0.
  One deal per decision (K=1) is within noise of a fresh deal per
  simulation (63.7 vs 65.0 on the same seeds, 1,000 games), and K=4 at 100
  simulations equals the fresh-deal arm on the same 500 seeds (344 wins
  each). The §6 K ≥ 16 default is therefore not measured to matter, and
  cheaper K is free. Resolving this needs a network prior and value.
- **Cost:** redeal is not measurably dearer than clairvoyant. The deal
  (clone, secret moves, projection check, one capture) costs about as much
  as the clone it replaces. ms/simulation is 1.8-1.9 at 25 and 2.2-2.8 at
  100, because deeper trees take more env steps (35 vs 50). The
  100-simulation timings shared the box with other jobs (load up to 45 on
  32 cores), so their ranges are per uncontended chunk. Peak RSS was
  0.9 GB per 8-worker run.
- **Fallbacks:** 0 refusals and 0 failed deals in every matrix arm except
  one decision (25 simulations lost to the empty-library bug below). On the
  pauper-kernel round robin the first build refused 1.6% of searched
  decisions (an embalmed Sacred Cat token counted as a seen deck card) and
  failed deals once a library was empty (a nil zone compared with
  `reflect.DeepEqual`). Both were fixed in `b87ab3d4c` with a regression
  test. After the fix: 2 refusals in 15k decisions ("observed frame refers
  to an unpinned hidden card").

**SpellBench-shaped round robin** (8 pauper-kernel decks as mirrors, 4 pairs
per deck, 64 games per matchup, 640 games, rated with
`scripts/spellbench-rate.py`, anchor sb-uniform; `b87ab3d4c`):

| Bot | Elo | CI95 | W-L |
|---|---|---|---|
| az-clairvoyant-sims25 (reference, NOT fair) | 1444 | [1377, 1521] | 201-55 |
| **az-redeal-sims25** | **1382** | [1320, 1455] | 180-76 |
| bot | 1264 | [1205, 1330] | 136-120 |
| sb-heuristic | 1114 | [1056, 1177] | 80-176 |
| sb-uniform (anchor) | 1000 | — | 43-213 |

Head to head, az-redeal beats bot 43-21 and sb-heuristic 55-9, and loses to
clairvoyant az 25-39. That is +118 Elo over `bot` on the benchmark's own
fit, against +180 for the clairvoyant reference.

**Not answered here:** Q1 (L10 on redeal-only worlds vs its rejection
sampler) and hidden-list worlds (list posterior not a point mass).

**Rerun** (worktree `wt/sb-m1-redeal`; outputs and the chunk runner are in
`/mnt/sata/gorge-training/spellbench-work/m1/`: `run_chunk.sh`,
`aggregate.py`, one directory per arm):

```sh
go build -o $M1/botbench ./cmd/botbench
P=uw-tempo:mono-white-equipment,uw-tempo:mono-blue-tempo,uw-tempo:mono-black-aggro,uw-tempo:mono-red-prowess,uw-tempo:mono-green-stompy
# chunk c (0..7): 500 games at seed 91000000+1000c
$M1/botbench -pairs $P -games 100 -seed $((91000000+1000*c)) -workers 8 -a az-redeal -b bot -az-sims 25
$M1/botbench ... -a bot -b bot                                           # control
$M1/botbench ... -a az -b bot -az-world clairvoyant -az-sims 25          # clairvoyant
$M1/botbench ... -games 50 -a az-redeal -b bot -az-sims 100 [-az-worlds 4]
$M1/botbench -spellbench az,az-redeal,bot,sb-heuristic,sb-uniform -az-world clairvoyant \
    -az-sims 25 -spellbench-pairs 4 -spellbench-out $M1/sb-rr2 -workers 8
scripts/spellbench-rate.py --anchor sb-uniform --out $M1/sb-rr2-rated $M1/sb-rr2
```

Every heavy command ran under `flock heavy.lock systemd-run --scope -p
MemoryMax=4G env GOMEMLIMIT=2GiB GOMAXPROCS=8`. The matrix arms ran on
`666cc2720`, which has the two bugs above. They never hit the Sacred Cat
case (the matrix decks have no embalm), so their numbers stand.

### 12.6 sb-search: sb-tactical under honest determinized search (measured 2026-09-28)

**Verdict: search on top of sb-tactical is a clear win.** `sb-search-fast`
beats the champion heuristic `sb-tactical` **321-191 (62.7%, about +90
Elo)** over 512 seat-swapped mirror games (dev seed 777, 32 pairs x 8
decks), winning on every deck, and holds on the held-out seed 99991:
**315-197 (61.5%, +82)**. On the FDN catalog (4 pairs, 128 games) it is
74-54 (57.8%). Searching the attack declaration as well
(`sb-search-fast-atk`) adds more: **336-176 (65.6%, +112)** at seed 777 and
**329-183 (64.3%, +102)** held out; under common random numbers the games
the two arms split went 19-4 (seed 777) and 23-9 (99991) to the attack arm.
The cheapest arm, `sb-search-lite-atk` (4 worlds, 2-turn horizon), keeps
almost all of it -- **328-184 (64.1%)** -- at a 0.22 s mean searched
decision. Four heuristic decorators on sb-tactical had all measured
neutral; this is the first idea that moves it.

**Design** (`internal/spellbench/sbsearch`, registered in
`cmd/botbench/spellbench_registry.go`). The seat wraps an sb-tactical
`builtins.Seat` and is a `searchseat.SearchSeat`, so `bench.PlayGame`
hands it the live engine and its observation feed; `UnwrapSeat` exposes the
sb-tactical seat so the runner's planner hand-off, refused-answer retry and
stats reach it unchanged.

1. *Candidates.* At a priority decision `builtins.Seat.TacticalPriority`
   returns sb-tactical's scored candidate list exactly as its next `Decide`
   would rank it (no pursuit or plan lowering in progress, at least two
   candidates). The searched set is sb-tactical's pick, the next best by
   score up to `TopK` = 3, plus pass. A land drop is never searched
   (sb-tactical always plays it first; the next priority decision searches
   the spells). With `Attack`, a KAttackers decision compares sb-tactical's
   declaration with no attack and the alpha strike
   (`builtins.AttackAlternatives`).
2. *Worlds.* W worlds per decision, each a `searchprobe.Redealer` deal from
   the seat's feed (its History and the incremental known-card projection,
   `azmcts.KnownTracker`) and the declared game: in the mirror pool the
   opponent's list is the seat's own (the spec's `visible` case). The real
   engine is read for public state and zone sizes only; a refused redeal
   plays sb-tactical's pick.
3. *Rollouts.* Every candidate is applied in every world through a rollout
   copy of the seat (`RolloutClone`, then `ForcePriority`, so a pursuit or a
   lowered payment plays exactly as sb-tactical plays it) and the game is
   rolled on with sb-tactical on BOTH sides -- to game end, or to `Horizon`
   turns after the root's turn, scored by the frozen material leaf
   (`searchprobe.LeafValue` on the seat's own view). All candidates of one
   world share its deal, chance seed and rollout seeds (common random
   numbers). Rollout seats share one profile cache, kept apart from the
   real seat's.
4. *Choice.* Mean value over the worlds every candidate completed; the best
   candidate is played only if it beats sb-tactical's own pick by more than
   `Margin` = 0.05. A failed world (deal refused, rollout panic, a candidate
   not offered, every fallback answer refused) is dropped for all
   candidates; no valid world, or any panic in the search, plays
   sb-tactical's pick. The real seat's own state only ever sees the forced
   pick (`ForcePriority`), which `Decide` clears.
5. *Determinism.* Worlds, chance and rollout seats derive from
   `azmcts.DecisionSeed(seat seed, decision seq)`; everything iterates in
   slice order; the package reads no clock (`Millis` is injected for the
   cost report only). Unit tests: W=0 plays the sb-tactical game event for
   event (`TestZeroWorldsIsTactical`), a forced rollout failure at every
   searched decision plays the sb-tactical game (`TestForcedErrorFalls
   BackToTactical`), and the same seed repeats the same searched game
   (`TestSearchIsDeterministic`).

`sb-tactical` itself changed in one place: its reanimate value recursed
without end (a stack overflow, fatal) when a creature whose own ETB
reanimates sat in the graveyard. sb-search's rollouts reached it on the
FDN catalog; a nested call is now worth 0, and every other state scores as
before (the pauper-kernel runs never reached it).

**Measured** (vs `sb-tactical`, pauper-kernel, seat-swapped mirrors; ms are
wall per SEARCHED decision on a box at load 14-20, so real latency is
lower; about 85 searched decisions per game per seat):

| variant | W | horizon | games | result | ms mean / p50 / p90 / p99 | overrides |
|---|---|---|---|---|---|---|
| sb-search-fast | 8 | 3 turns + leaf | 512 (seed 777) | **321-191, 62.7%** | 502 / 343 / 1064 / 2519 | 4.8% |
| sb-search-fast | 8 | 3 turns + leaf | 512 (seed 99991) | **315-197, 61.5%** | 470 / 325 / 1017 / 2291 | 4.9% |
| sb-search-fast | 8 | 3 turns + leaf | 128 (FDN) | 74-54, 57.8% | 204 / 168 / 362 / 702 | 6.6% |
| sb-search | 16 | game end | 256 (seed 777) | 147-109, 57.4% | 4172 / 2443 / 10630 / 21889 | 13.5% |
| sb-search-fast-atk | 8 | 3 turns + leaf, attacks searched | 512 (seed 777) | **336-176, 65.6%** | 519 / 355 / 1097 / 2550 | 4.8% |
| sb-search-fast-atk | 8 | 3 turns + leaf, attacks searched | 512 (seed 99991) | **329-183, 64.3%** | 520 / 357 / 1131 / 2450 | 5.1% |
| sb-search-lite | 4 | 2 turns + leaf | 512 (seed 777) | **318-194, 62.1%** | 187 / 133 / 382 / 915 | 5.1% |
| sb-search-lite-atk | 4 | 2 turns + leaf, attacks searched | 512 (seed 777) | **328-184, 64.1%** | 219 / 152 / 448 / 1108 | 5.2% |

Per deck, seed 777, sb-search-fast: Wildfire 44-20, Rally 42-22, Affinity
39-25, Elves 41-23, Spy 39-25, Burn 38-26, CawGates 39-25, Faeries 39-25.
Held-out 99991: Wildfire 36-28, Rally 38-26, Affinity 43-21, Elves 42-22,
Spy 34-30, Burn 36-28, CawGates 45-19, Faeries 41-23.

Game-end rollouts are not better: on the same 128 pairs, `sb-search`
(W=16 to game end) went 147-109 where `sb-search-fast` went 160-96, at 8x
the cost. The full-game values are noisier (a 0/1 outcome per world), so
the same 0.05 margin lets twice as many overrides through; the short
horizon plus material leaf is both cheaper and a better estimator here.

**Time.** SpellBench's per-decision limit is about 1 s. `sb-search-fast`'s
median searched decision is ~0.33 s and its p90 ~1.0 s under load; its
tail (p99 2.3-2.5 s) comes from the long-game decks (CawGates, Wildfire),
where each rollout plays more decisions. `sb-search-lite-atk` (W=4,
horizon 2) is the budget that stays under the limit -- mean 0.22 s, p90
0.45 s, p99 1.1 s under load -- for 64.1%, so it is the recommended
SpellBench arm. A deployed seat still wants a hard per-decision cap (stop
dealing worlds once the budget is spent); a clock cut makes play depend on
time, so it belongs in the live seat only, not in the reproducible bench.

Existing policies are untouched: the sb-gauntlet smoke digest golden
passes, and a 96-game sb-tactical / sb-tactical-noearly / bot round robin
(seed 777, 2 pairs) is games.jsonl-identical (wall time dropped) between
the base commit `ee26ea630` and this branch.

```sh
W=/mnt/sata/gorge-training/spellbench-work/search-tac
$W/botbench -dir .cards -spellbench sb-tactical,sb-search-fast -spellbench-pairs 32 \
    -spellbench-base-seed 777 -spellbench-out $W/h2h-fast-p32 -workers 8
# the other arms swap the policy name (sb-search, -fast-atk, -lite, -lite-atk);
# held-out: -spellbench-base-seed 99991; FDN: -spellbench-catalog fdn -spellbench-pairs 4
```

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

## 15. Experiment framework

One standard way to add a SpellBench policy idea and measure it. Three
parts, all landed (sb-gauntlet workstream):

**Registry** (`internal/spellbench/registry`). A named policy registry:
`name -> factory(seed uint64, mana builtins.ManaMode) seat.Seat`. The
sb-* builtins are registered by the package itself (`sb.go`); `bot` and
`az` are registered by `cmd/botbench` (they need the command's hosted-policy
and azmcts wiring); `botbench -spellbench` resolves every name through it,
and an unknown name fails with the registered names listed. Registering a
new policy is one file plus one `Register` call from `init()`.

**Decorators.** A policy spec may be `base+dec1+dec2` (e.g. `bot+lethal`).
A decorator is `func(inner seat.Seat, seed uint64) seat.Seat`, registered
by name in the same registry; its seat may answer a decision itself or
delegate it to `inner`. The spec string is the policy's name in the ledger,
so the ledger shows the composed name. The first decorator is
`passguard`: it delegates everything, replacing a pass pick with the first
non-pass candidate only when the decision offers exactly one non-pass
candidate that is a land play (the wrapper keeps the wrapped seat's
BoardSeat-ness, so `bot+passguard` still answers from the board, and the
inner is consulted exactly once per decision, so a seed-streamed inner
draws the same numbers bare or wrapped). A delegating decorator implements
the registry's `Unwrapper` contract (`UnwrapSeat`), so the runner's
refused-answer fallback and stats collection reach the seat underneath: a
decorated `sb-*` spec keeps the builtin's fallback exactly as the bare
name has it. And `sbDisplayName` marks the spec's BASE, so an `az`-composed
spec keeps the clairvoyant ledger marker (`az+passguard` is recorded as
`az-clairvoyant-sims<N>+passguard`) and rate.py's name tag (a leading
`az-`) still classifies it as a search agent.

**Gauntlet** (`scripts/sb-gauntlet.sh <spec>[,<spec>...] [pairs] [decks]`).
Rates candidate specs against a fixed reference set: `sb-uniform` (the Elo
anchor), `sb-heuristic` and `bot`, plus every spec in
`/mnt/sata/gorge-training/spellbench-work/gauntlet/champions.txt` when it
exists. Candidates play the references through `-spellbench-with <spec>`
(full round robin's seeds and indices kept); reference-vs-reference games
are cached under `/mnt/sata/gorge-training/spellbench-work/gauntlet/ref/<key>/`,
keyed by the `git rev-parse HEAD:` tree hashes of `rules effects cards
decision botpolicy internal/spellbench cmd/botbench` plus pairs and decks,
so any engine or policy change invalidates the cache, as does any change
to the champions file's content (a new champion line must get its own
ref-vs-ref games, not ride a cache built without it). Every relevant ledger
is rated together with `scripts/spellbench-rate.py`
(`/mnt/sata/gorge-training/sbvenv/bin` provides the interpreter); the run
prints a per-candidate table (spec, Elo, CI95, W-L, head-to-head vs each
reference) and appends one JSON row per candidate to
`.../gauntlet/results.jsonl` (`spec, elo, ci_lo, ci_hi, wins, losses,
pairs, decks, git_head, key, ts`). Exit code 0 unless the run fails. Every
botbench invocation runs under
`flock -o .../heavy.lock systemd-run --user --scope -q -p MemoryMax=4G env
GOMEMLIMIT=2GiB GOMAXPROCS=8` with `-workers 8` (SB_GAUNTLET_WORKERS
overrides the worker count for a smoke-sized run).
