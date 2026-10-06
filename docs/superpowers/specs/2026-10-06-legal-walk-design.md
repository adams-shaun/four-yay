# Priority legal-action walk: design and implementation plan

- **Date:** 2026-10-06
- **Base:** `main` at `7cc443283b60e99d0bbcbb19a25e89264ff9d42e`. Every `path:line` below was read at this SHA. `main` moved to `0b62ac588` while this was written; `git diff 7cc443283 0b62ac588 -- rules/ cmd/enginebench internal/bench` is empty, so the citations hold there too.
- **Subject:** `rules.(*Engine).legalActionsWithWindow` (`rules/legal.go:57`) and its body `legalActionsWalkWithWindow` (`rules/legal.go:125`). `askPriority` (`rules/turn.go:1039-1058`) is the only posed-priority caller (`rules/turn.go:1051`).
- **Status:** design and plan only. No production code changed. The measurements came from throwaway builds that were never committed (§9).
- **Notes:** `/home/sadams/projects/gorge/.worktrees/legalwalk-plan/.ds4/legalwalk-plan-notes.md` (append-only log of this planning session).

## 0. Summary

**The walk is about 4.5 µs per priority ask, and the cost is spread thin.**
- The walk is 24–32% of CPU on the random and bot enginebench rows and 17% on the sampler row.
- Inside the walk, the hand section is ~55%, the battlefield section 25–34%, and the result allocation ~5–6%.
- Below that level, no single gate is more than ~20% of the walk. Most are 2–10%.

**Whole-walk reuse across decisions does not pay with a cheap key.** Measured over ~920k random-row asks and ~690k bot-row asks:
- **Ceiling:** 56% (random) and 73% (bot) of a seat's consecutive priority walks produce a byte-identical option list.
- **Cheap key:** **0%** of those consecutive walks have only bookkeeping events (`Priority`/`DecisionAsk`/`DecisionMade`) between them. That is the condition the engine's existing inert-suffix machinery (`layerInertSince`) can prove. Every same-seat transition carries either a `StepChange` or a real action.
- **Timing-only transitions:** the largest class a whole-walk memo could serve soundly is "only bookkeeping plus `StepChange`, with the sorcery-speed bit unchanged". It is 10–11% of transitions (all identical). Serving it needs a timing-sensitivity classification of every object the walk visits. So option (a) is the last step (S5), gated and likely killed.

**The largest root cause found is a global pessimisation, not a missing cache.**
- **What happens:** the walk's cheap mana-floor refusal (`offerFloorRefuses`, `rules/legal_walk.go:112-133`) never fires on any bench row. Scratch counters show it bailing on 100% of 1.04M offers on random A.
- **Why:** it bails whenever any Raise/Reduce/SetCost static exists anywhere on the board. Both bench pairs carry a self-scoped one (`ValidCard$ Card.Self | EffectZone$ All`): Luminous Rebuke in both pairs, plus Tolarian Terror in pair B. Such a static is collected from every zone, the library included (`rules/layers_static.go:58-61`), so one copy in either library defeats the floor for every card of both players for the whole game.
- **How common:** 302 corpus cost-static lines have this shape, half of all cost statics.
- **Prototype result:** treating a self-scoped static on another object as absent for the floor made the floor refuse 68–72% of offers, with **0 mismatches** over 1.30M full recompositions. The repo-deck offer digest (`TestLegalWalkDigest`) was byte-identical to base. Walk CPU per ask fell 5% (random A), 13% (random B) and 10% (bot A).

**Recommendation.** Five ordered, independently landable steps (S0–S4), plus one gated step (S5) that is expected to be killed:

| step | what it does | expected walk-CPU cut (per row) |
|---|---|---|
| S0 | a committed measurement harness, `enginebench -walkstats` | n/a |
| S1 | the self-scoped cost-static fix | 5–13% |
| S2 | hoist the CastWithFlash lookup out of the per-card timing gate | 2–4% |
| S3 | O(1) validity for the cross-walk board-statics scan | ≤ 5–6% |
| S4 | carry per-object mana-ability membership across priority walks, gated on S0 numbers | ≤ 4–12% |
| S5 | timing-only whole-walk replay | ≤ ~10% |

- **Per-object castability facts with dependency epochs (option b in full) is rejected.** A hand card's offer reads the pool, timing, every cost and restriction static, target availability over the whole board, and non-mana cost availability. Its dependency closure is effectively the whole state, so no key is cheaper than recomputing.
- **Lazy payment feasibility (option e) is rejected.** It changes what is offered.
- **Realistic total:** S1–S4 together, about 15–35% of the walk, i.e. **~4–10% of total CPU**. That is stated as a range because S3 and S4 are upper bounds read off profiles, not prototypes.

## 1. Census

### 1.1 Method

The profiles are the enginebench runs the brief named:
- `/mnt/sata/gorge-training/enginebench/prof/20261006T140106/` (14:01)
- `/mnt/sata/gorge-training/enginebench/prof/20261006T151957/` (15:19, used below unless noted)

The binaries carry no VCS stamp (`go version -m` prints no `vcs.revision`), so the 15:19 binary is INFERRED to be `main` as of 15:19. Commands:

```sh
cd /mnt/sata/gorge-training/enginebench/prof/20261006T151957
go tool pprof -top -cum -nodecount=0 enginebench random-A.cpu
go tool pprof -peek 'rules.\(\*Engine\).legalActionsWalkWithWindow$' enginebench random-A.cpu
go tool pprof -source_path=/home/sadams/projects/gorge -trim_path=github.com/adams-shaun/gorge \
  -list 'rules.\(\*legalWalk\).handWalk$' enginebench random-A.cpu
```

How to read the numbers:
- `cum` values are global. A function also called outside the walk (e.g. `candidatesForLimitInto`) is counted there too. Where it matters, the walk-only share comes from `-peek` on the walk.
- Total samples per row are 15.09 s (random-A), 14.99 s (random-B), 14.99 s (bot-A) and 14.95 s (sampler).

### 1.2 Cost per walk phase

Seconds of CPU, with % of the walk in parentheses. Every row below was read from `-peek` on the walk or on the named section.

| phase | random-A | random-B | bot-A | sampler |
|---|---|---|---|---|
| **walk total** (% of all CPU) | **4.17 (27.6%)** | **4.78 (31.9%)** | **3.65 (24.4%)** | **2.60 (17.4%)** |
| hand section (`handWalk`) | 2.31 (55%) | 2.37 (50%) | 1.98 (54%) | 1.55 (60%) |
| battlefield section (`battlefieldWalk`) | 1.06 (25%) | 1.63 (34%) | 1.19 (33%) | 0.81 (31%) |
| result allocation (`arenaOptions` → `makeslice`) | 0.24 (6%) | 0.22 (5%) | 0.20 (5%) | arena on |
| may-play spells (`mayPlaySpellWalk`) | 0.17 | 0.15 | 0.12 | 0.13 |
| graveyard casts (`graveyardCastsWalk`) | 0.11 | 0.11 | 0.03 | ≤0.02 |
| command zone / exile casts / plot / mayhem / may-play land | ≤0.02 each | ≤0.02 each | ≤0.02 each | plot 0.04, rest ≤0.01 |
| post-walk filters (no-mana-cost, inert hold-out, split second) | ≤0.01 each | ≤0.01 each | ≤0.01 each | — |

Cross-cutting gates, as cum (they are called from both sections):

| gate | random-A | random-B | bot-A | sampler |
|---|---|---|---|---|
| `offerCastableUsing` (cost composition + mana feasibility) | 0.78 | 0.84 | 0.99 | 0.80 |
| └ `costPotentialTargets` (target census for ValidTarget$ retry) | 0.23 | 0.10 | 0.26 | 0.18 |
| └ `costModifiersCompose` | 0.22 | 0.31 | 0.35 | 0.35 |
| └ `manaFeasiblePricedP` | 0.15 | 0.26 | 0.14 | 0.21 |
| └ `potentialCostModsUsing` | 0.10 | 0.08 | 0.32 | 0.14 |
| `castRestricted` (its first call pays the board-statics collection) | 0.45 | 0.42 | 0.30 | 0.27 |
| └ `collectActionStatics` → `boardStaticsWalk` | 0.39 | 0.33 | 0.29 | 0.28 |
| └└ `gatherBoardScan` + `boardScanMatches` | 0.22 | 0.22 | 0.15 | 0.14 |
| `spellTimingOK` | 0.27 | 0.20 | 0.13 | — |
| └ `castWithFlash` (activeStatics + withSelfStatics per card) | 0.17 | 0.09 | <0.08 | — |
| mana gate in the walk (`appendAvailableManaAbilities`) | 0.18 | 0.59 | 0.22 | 0.20 |
| `manaWalkEmpty` | 0.13 | 0.12 | 0.08 | 0.07 |
| `candidatesForLimitInto` (targets) | 0.39 | 0.23 | 0.43 | 0.27 |
| `castTargetsAvailable` | 0.14 | 0.09 | 0.15 | — |

The categories the brief named that are not in the walk on these rows:

- **Mana abilities:** the `activateManaFor` that the brief listed (4.9%) is **not part of the walk**. It is reached from `activateMana` → `resolveManaAbilityInteractive`, i.e. activating a mana ability the random player chose (`-peek 'activateManaFor$'`: callers `activateMana` 100%). The walk's own mana-ability cost is the mana-gate row in the table above.
- **Payment-plan search and potential walks:** each is below pprof's 0.5% node fraction on all four rows. No row projects `PotentialActions`, and `potentialWalkOf` is reached only from `PotentialActions`, `PotentialPaymentPlans` and `paymentActionsForPriority` (`rules/potential_walk_cache.go:155`; callers per the census in notes). Live hosts and botbench seats that project PotentialActions do pay it, and they benefit from S1 too: the potential walk runs the same `offerCastable`.

### 1.3 Inside the hand section (random-A, `-list handWalk`)

| line (`rules/legal_walk_hand.go`) | what | cum |
|---|---|---|
| 330 | ordinary front-face cast: `offerCastable(...) && targetsAvailable()` | 820 ms |
| 173 | `w.castRestricted(p, id)`. The first call per walk fills `actionStatics` (the board scan). | 450 ms |
| 254 | `!e.spellTimingOK(p, id, f, sorcery)` | 270 ms |
| 311 | `castOfferBase` | 130 ms |
| 69 | modal-land play gate (`playLandForbidden`, `adjustLandPlays`) | 90 ms |
| 219 | foretell keyword-param read | 60 ms |
| 256 | `teamworkFlashOffer` | 50 ms |
| remaining alternative-cost families | ≤ 40 ms each | |

### 1.4 Inside the battlefield section

- **random-B:** the mana loop is the cost, 0.59 s (`rules/legal_walk_battlefield.go:103`). Inside the gate (`rules/mana_activation.go:366`):
  - `grantedAbilities` 0.19 s
  - `abilityLoss` 0.16 s
  - AddAbility carriers 0.08 s

  These are per-object reads of board-level facts.
- **Elsewhere in the section:** the ability loop's flat cost is 0.26–0.33 s. It visits every seat's battlefield, graveyard, hand and exile, `rules/legal_walk_battlefield.go:142-200`. On top of that, `unlockRoomFaceCost` costs 0.05–0.12 s.

### 1.5 How often a seat's consecutive walks see the same state

**Method.** The throwaway observer (§9.1) runs at every posed priority decision. For each one it compares the JSON of the offered `Options` (including `Index`) with the same seat's previous priority decision. It also classifies the kinds of the events logged in between.

```sh
# scratch build of cmd/enginebench + measure.go (§9.1)
systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB \
  timeout 120 ./walkmeasure -row random -pair A -secs 15
```

| | random A | bot A |
|---|---|---|
| priority asks | 924,307 | 691,187 |
| mean options / hand size / own permanents | 3.90 / 6.31 / 8.87 | 4.13 / 3.48 / 6.98 |
| asks offering only pass + concede | 54.6% | 50.3% |
| same-seat transitions with an **identical** option list | **56.3%** | **73.1%** |
| transitions with only `{DecisionAsk, DecisionMade, Priority}` in between | **0.0%** | **0.0%** |
| ... only those plus `{StepChange, ManaClear, Note}` | 22.9% (78.8% identical) | 22.0% (86.2% identical) |
| ... and the same sorcery-speed bit | 11.1% (100% identical) | 10.0% (100% identical) |

The ten largest event-kind signatures. Each cell gives the share of transitions, then the share of those whose option list is identical.

| signature | random A | bot A |
|---|---|---|
| `{tap, mana_add, priority, ask, made}` (a mana ability was activated) | 29.3%, 1.3% | 13.1%, 1.2% |
| `{step, priority, ask, made}` | 15.6%, 83.3% | 18.0%, 83.1% |
| `{step, mana_clear, ...}` | 7.2%, 69.3% | 4.1%, 99.9% |
| `{step, end_combat_reset, ...}` | 6.5%, 85.0% | 7.8%, 100% |
| `{draw, step, mana_clear, ...}` | 4.5%, 91.6% | — |
| `{draw, step, ...}` | 1.0%, 100% | 7.7%, 100% |
| `{step, declare_attackers, ...}` | 4.2%, 100% | 4.5%, 100% |
| `{untap, step, turn, ...}` | 3.3%, 14.7% | 6.6%, 51.0% |
| `{move_zone, land_played, ...}` | 2.1%, 0.1% | 2.8%, 0.0% |
| `{move_zone, stack_resolve, ...}` | 1.4%, 81.6% | 3.9%, 96.4% |

What this means:
1. **No transition is inert.** The engine's existing whole-walk caches (`priorityWalkTail`, `potentialWalkCache`, `rules/potential_walk_cache.go:68-110, 366-410`) are keyed per posed decision (ask serial plus pending pointer), so nothing reuses a walk across decisions today. A cross-decision memo has to tolerate at least `StepChange`.
2. **Activating a mana ability changes nearly everything.** The `{tap, mana_add}` class is the largest on the random row, and almost no option list in it survives: the pool feeds every priced gate. Per-block reuse that keeps pool-priced blocks would not serve the hand section here, because almost every hand block is priced.
3. **Identical output is common, but proving it is the hard part.** The 56–73% identical rate is an upper bound for any reuse scheme, not a target. Most of it lies in transitions that change state the walk could read: the step, the turn, combat flags, a hidden draw.

### 1.6 Gate counters (scratch instrumentation)

Counters were added in a throwaway worktree (§9.2) to `offerCastable`, `offerFloorRefuses`, `spellTimingOK` and the walk entry, counting posed walks only.

| | random A | random B | bot A |
|---|---|---|---|
| walks (posed, real pool, full) | 634,283 | 284,369 (5 s) | 456,956 |
| walks at sorcery speed | 15.2% | 14.7% | 26.6% |
| walks with p's floating pool empty | 68.1% | 68.6% | 80.8% |
| `offerCastable` calls per walk | 1.65 | 1.96 | 1.73 |
| offers priced with an empty pool | 67.8% | 70.3% | 62.9% |
| offers that succeed | 9.9% | 8.8% | 13.7% |
| **floor refusals (`offerFloorRefuses` true)** | **0** | **0** | **0** |
| floor bail on cost statics present | **100%** of offers | **100%** | **100%** |
| ... of which a reduce static present / `validTarget` | 100% / 100% | 100% / 100% | 100% / 100% |
| cost-static sources seen | Luminous Rebuke | Luminous Rebuke, Tolarian Terror | Luminous Rebuke |
| `spellTimingOK` calls per walk / false | 5.6 / 75% | 5.3 / 67% | 2.6 / 51% |

The two cards, from the corpus:

```
Luminous Rebuke    S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ 3 | EffectZone$ All | ValidTarget$ Creature.tapped
Tolarian Terror    (pair B) a self-scoped "costs {1} less for each instant and sorcery card in your graveyard"
```

Corpus census, measured with `/usr/bin/grep -rhE '^S:Mode\$ (ReduceCost|RaiseCost|SetCost)' .cards/cardsfolder`:

| | count |
|---|---|
| card files carrying a cost static | 598 |
| cost-static lines with `ValidCard$ Card.Self` | 305 |
| ... of which also `EffectZone$ All` | 302 |
| all cost-static lines with `EffectZone$ All` | 306 |

**Why this matters.**
- `statics_costscan.go:15-18` documents that an `EffectZone$ All | ValidCard$ Card.Self` static is live from hand, library, command zone and stack. So the walk's snapshot holds one Rebuke view per copy in any zone.
- `offerFloorRefuses` then returns false for every card, because `len(statics.reduce) != 0 || statics.validTarget` (`rules/legal_walk.go:113`).
- Every offer therefore composes the modifier set (`costModifiersCompose`, `rules/statics_costmods.go:281`) and runs the mana feasibility.
- When that fails, it also enters the potential-target retry whenever `statics.validTarget` holds and the retry is not provably futile (`rules/mana.go:252-264`). `costPotentialTargets` allocates its result (`rules/mana.go:376`).

### 1.7 Prototype of the fix

The throwaway prototype (§9.2) added `selfOnlyOther(statics, id)`: true when every raise/reduce/set member has `ValidCard$` exactly `Card.Self` and `Source != id`. The floor bail at `rules/legal_walk.go:113` was skipped when it held.

Agreement check: `WALKPROTO_CHECK=1` recomposed every floor refusal with `offerCastableUsing`. This is the same check `walkSkipVerify` makes at `rules/legal_walk.go:77`.

| | random A (6 s) | random B (6 s) | bot A (6 s) |
|---|---|---|---|
| offers | 613,281 | 750,305 | 511,956 |
| floor refusals | 418,251 (68.2%) | 543,173 (72.4%) | 342,690 (66.9%) |
| refusals the full composition would have offered | **0** | **0** | **0** |

CPU, from 8 s capped runs at the same seed, with and without the prototype (walk cum ÷ posed walks):

| | random A | random B | bot A |
|---|---|---|---|
| µs per walk, base → prototype | 4.50 → 4.26 (−5%) | 4.74 → 4.14 (−13%) | 5.14 → 4.61 (−10%) |
| `offerCastableUsing` cum, base → prototype | 0.43 → 0.35 s | 0.38 → 0.24 s | 0.56 → 0.44 s |

The per-run profile resolution is ±0.02 s on ~2 s, so these are ±1–2 points.

**Repo-deck byte identity.** `TestLegalWalkDigest` was run with `WALKPROTO=1` in the prototype worktree and compared with the base digest, using `cmp`.
- **Result:** the two files are **identical**: 43 games over every repo deck, 2-seat, 4-seat and Commander, covering each game's Options, PotentialActions and PaymentActions.
- **Verify was live:** that run used the rules test binary, where `walkSkipVerify` is on (`rules/legalskip_verify_test.go:6`), so every prototype floor refusal was also recomposed in-line by the `rules/legal_walk.go:77` arm, and none panicked.
- **Cost:** 22.3 s test time; 1:49 wall including the test-binary build.

## 2. Root causes

What a walk recomputes that depends only on (card, layer epoch, pool, timing window, restrictions):

- **R1. Global pessimisation by self-scoped cost statics** (§1.6).
  - The snapshot of cost statics is board-wide, but half of the corpus's cost statics apply only to their own source's cast.
  - The floor refusal and the `validTarget` retry flag treat any member as possibly applicable to every card.
  - The floor is a sound cheap refusal that exists for exactly the empty-pool case, which covers 63–70% of offers. It is defeated by a card sitting in a library.
- **R2. Per-walk recollection of the board-statics snapshot** (`castRestricted`'s first get).
  - The cross-walk memo exists (`boardStaticsWalk`, `rules/walkcache.go:139`).
  - Its exact cross-walk hit (`walkCrossHit`, `rules/walkcache.go:117-123`) needs an unchanged `activeBuildSeq`, which any non-inert event defeats. §1.5 shows every transition has one.
  - The fallback (`boardScanMatches`, `rules/static_scan_reuse.go:198-209`) re-gathers and compares every summarized zone list of every seat, libraries included (`staticSourceIDs`' `slices.Equal(s.ids, cur)`, `rules/static_zoneskip.go:242`), on every walk.
  - That costs 0.22 s on random-A, ~5% of the walk.
- **R3. Per-card flash-grant lookup.**
  - `spellTimingOK` (`rules/statics_cast.go:768-776`) calls `castWithFlash` for every off-timing nonland card.
  - `castWithFlash`'s fast path (`rules/statics_cast.go:418-425`) still does an `activeStatics("CastWithFlash")` lookup and a `withSelfStatics` merge per card.
  - The board half is one fact per walk. The self half is a per-face constant.
- **R4. Per-object recomputation of mana-ability membership** (`appendAvailableManaAbilitiesGate`, `rules/mana_activation.go:366-740`).
  - Most of each answer is fixed between two of a seat's walks: a land's printed mana abilities, its activator, its zone gate, the board's grants and losses.
  - But it is recomputed for every object every walk, including the board-level probes inside it (granted abilities, ability loss, AddAbility carriers).
  - It already has board-fact skips (`board.hasGrants`, `rules/mana_activation.go:715`) and the per-object class skip (`manaHot`, `rules/walk_objclass.go:57-80`). What remains is the cost for the mana-hot objects.
- **R5. No cross-decision memo, and a cheap one is impossible.** §1.5: every transition carries `StepChange` or an action. The walk reads the step through many paths:
  - `sorcerySpeed`, `rules/legal.go:47-49`, which the walk computes once into `w.sorcery`;
  - `activationPhasesOK`;
  - Condition$/CheckSVar$ gates;
  - foretell, sneak and may-flash timing;
  - and ~70 `.Step` read sites across `rules` and `effects`.
- **R6. Result allocation** (`arenaOptions` → `make`, `rules/decision_arena.go:150`, 5–6% of the walk). On a non-arena engine every posed walk allocates its exact-size result (`rules/legal.go:242-244`). The concurrent "decision arena for live play" ticket owns this; nothing below touches it.

## 3. Options weighed

| option | hit / payoff (measured or bounded) | soundness argument | cost / risk | verdict |
|---|---|---|---|---|
| **(a) whole-walk memo keyed by a state epoch** | cheap key (bookkeeping-only suffix, `layerInertSince`, `rules/layercache.go:65`): **0%** of transitions. Timing-tolerant key (only `StepChange` added, same sorcery bit, timing-cold board): ≤ 10–11% of walks, ≈ 2.5–3.5% of total CPU | needs every step read the walk can reach to be either funnelled (compile-enforced accessor) or covered by a conservative per-object "timing-hot" class with a verify mode | a new object-class bit plus a board timing summary, an event-class table, and a result path that must also feed the arena tail (`rules/legal.go:225-265`) | **S5, gated; expected kill** |
| **(b) per-object castability/activatability facts with dependency epochs** | the identical-output ceiling (56–73%) is the bound, but the hand section's blocks are almost all pool-priced (§1.5 point 2) | a hand card's offer reads the pool, timing, cost statics, CantBeCast statics, the target census over every object, sacrifice/discard candidates, surge/delve counters and commander tax. No dependency key is narrower than "the state" | very high: a dependency model per gate | **rejected in full**. Only the narrow piece for mana membership survives as S4 |
| **(c) incremental board-statics index** | ≤ 0.22–0.26 s on random-A (5–6% of the walk) | the catch-up pattern already exists (`staticZonesCatchUp`, `rules/static_zoneskip.go:191-224`; `staticsProbeCatchUp`, `rules/layer4types.go:938`) | medium: zone reorders that name no object must be classified | **S3** |
| **(d) cheaper timing/restriction checks via compiled masks or hoisting** | flash hoist 2–4% of the walk. The floor fix (R1) is the same idea applied to cost statics: 5–13% (prototype) | pure hoisting of per-walk constants and an exact applicability pre-filter. Existing verify flags cover both | low | **S1, S2** |
| **(e) lazy payment feasibility (offer first, prove payability later)** | would remove most of `offerCastableUsing` | changes WHAT is offered, which breaks invariant 1 (§4). It recreates the offer–reverse–re-offer loop `rules/legal_walk.go:135-144` documents, and leans on the CR 733.1 illegal-cast reversal for normal play. AGENTS.md lists I-7 (targets before payment) and the CR 733.1 reversal as fixed and asserted; this would turn the reversal into the common path | high | **rejected** |

## 4. Invariants every step keeps

1. **Byte-identical offers.** Every posed priority decision's `Options` keeps the same order, contents and `Index`. So does every potential, casts-only and huge-pool walk (`rules/payment_plan.go:189`, `rules/potential_plan.go:471`) that shares the body. Labels, `Mode`, `AltCostIndex` and `Cost` markers are included.
2. **Heads unchanged.** The walk is a pure read: no event is emitted and no `state.Game` field is written. `TestHeads` (`rules/heads_test.go:45`) and `make sim` replays are untouched.
3. **Determinism.**
   - No `map` range that can reach an option or an event.
   - Every new per-engine cache is keyed on event-log position or counters, never on wall clock or pointer order.
   - A cache keyed on a log cursor detects a rewound log (`n < ep`) and drops everything, as `staticZonesCatchUp` does (`rules/static_zoneskip.go:205-212`).
4. **Hot-path shape** (operator rule: no maps, bitsets and dense slices, zero alloc).
   - New per-object state is a dense `ObjID`-indexed slice, as `walkObjCls` is (`rules/walk_objclass.go:296-326`).
   - Per-walk facts are bitfields on `legalWalk` or `walkBoardFacts` (`rules/legal_walk_skip.go:17-31`).
   - Nothing allocates per walk once warm.
5. **Every cache has a verify mode that recomputes and panics on mismatch.**
   - It is wired into the rules test binary's `init`: `rules/derivedmemo_verify_test.go:22-35` or `rules/legalskip_verify_test.go:6`.
   - It is reachable at link time through `derivedMemoVerifyFlag`, which `make enginebench-verify` uses.
6. **Clone policy.**
   - Every new Engine field carries a `clone:` tag; `TestClonePolicyEveryFieldTagged` (`rules/clone_policy_test.go:124`) fails otherwise. Caches are `reset` (or Spare-pooled), as every walk cache is today.
   - A by-value Engine copy must not write the original's arrays (the `ownWalkClasses` guard, `rules/walk_objclass.go:414`).
   - Hypothetical and search clones therefore start cold, and a probe inside `offerProbeDepth > 0` never caches (`rules/walk_objclass.go:318-324`).

## 5. Recommendation

Land S0, then S1 (the measured win), then S2 and S3 in either order. Decide S4 on S0's numbers after S1–S3. Hold S5 behind an explicit gate.

These steps do not change any rules contract, any option, or the walk's structure. Each one removes recomputation of something that a measurement shows is constant or provably irrelevant.

## 6. Step plan

All test commands follow the per-test budget (2 GB, 2 CPU, 1 min):

```sh
systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB \
  go test -timeout 2m -run '<pattern>' ./rules
# and, since posed options project there:
systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB \
  go test -timeout 2m -run 'TestPotentialActionsGatedToViewerSeat|TestDecisionOption|TestLiveViewPreservesPriority' ./view
```

Measurement gates go through the Makefile, serialised on the shared heavy lock. Wait for the lock; never bypass it.

```sh
make enginebench-verify  REV=.          HEAVY_LOCK=/home/sadams/projects/gorge/.ds4/heavy.lock
make enginebench-pair    BASE=main CAND=. REPS=3 SECS=10 HEAVY_LOCK=/home/sadams/projects/gorge/.ds4/heavy.lock
make enginebench-profile REV=.          HEAVY_LOCK=/home/sadams/projects/gorge/.ds4/heavy.lock
```

**The byte-identity harness** for every step is the opt-in `TestLegalWalkDigest` (`rules/legal_walk_bench_test.go:114-120`).
- It writes a per-game digest of every priority decision's Options, PotentialActions and PaymentActions over every repo deck:

  ```sh
  LEGAL_WALK_DIGEST=/tmp/<id>-base.txt <capped go test> -run 'TestLegalWalkDigest$' ./rules   # on main
  LEGAL_WALK_DIGEST=/tmp/<id>-cand.txt <capped go test> -run 'TestLegalWalkDigest$' ./rules   # on the branch
  cmp /tmp/<id>-base.txt /tmp/<id>-cand.txt
  ```

- **Budget status — fits.** At this base, under the 2 GB / 2 CPU cap, it ran in 22.2 s (rules test binary already built) and wrote a 43-line digest.

### S0. `enginebench -walkstats`: the reuse oracle and gate counters

- **What.** Commit the §9.1 observer as an enginebench flag. It needs only the exported API (`Pending`, `L.Events`, `G`).
  - It prints the same-seat transition table and signature classes of §1.5, which are the input to S4/S5's kill criteria.
  - Optionally it also prints the gate counters of §1.6 behind a link-time flag (`-X ...rules.walkStatsFlag=1`), following the `derivedMemoVerifyFlag` pattern, so the default build pays one predictable branch at most. Without the flag, the counters are zero-cost dead code.
- **Files.** `cmd/enginebench/walkstats.go` (new), `cmd/enginebench/play.go` (two hook lines, as in §9.1), `cmd/enginebench/main.go` (flag). Optionally `rules/walk_stats.go` (new).
- **Done means:**
  - `go run ./cmd/enginebench -row random -pair A -secs 5 -walkstats` prints the table.
  - The numbers reproduce §1.5 within ±2 points.
  - The default `enginebench` JSON line is unchanged.
- **Tests:** `go vet ./cmd/enginebench` plus a 2-second smoke run under the cap.
- **Gate:** none. It is tooling.
- **Payoff:** none directly. It makes S4/S5 decidable.
- **Kill:** n/a.

### S1. Self-scoped cost statics stop defeating the offer floor (R1)

- **What.**
  - At collection time, mark each raise/reduce/set view whose `ValidCard$` is exactly `Card.Self` as `selfOnly`. Either add a bit on `staticView`, or keep a parallel `[]bool` / bitset per list in `costStaticViews` (`rules/statics.go:121-132`).
  - In `offerFloorRefuses` (`rules/legal_walk.go:112-133`), replace the "any cost static" test with: "every raise/reduce/set member is `selfOnly` with `Source != id`, and `validTarget` comes only from such members".
  - Track `validTarget` per kind (`validTargetSelfOnly`), so a self-only ValidTarget$ member on another object does not set it.
  - Everything after the floor stays as it is, including composition when the card is the source.
- **Why exact.**
  - A `Card.Self` member whose source is not the priced object fails `costStaticGate` target-independently. So the composition is the zero `costMods`, which `offerFloorRefuses`' own argument (`rules/legal_walk.go:88-111`) already requires.
  - It sets no `CostProvenanceSeen`, because `Card.Self` carries no cast-provenance token (`costStaticApplies`, `rules/statics_costapply.go:24`).
  - The retry is futile (`pay.OfferRetryFutile`, `rules/pay/costparts.go:464-469`).
  - The existing `walkSkipVerify` arm (`rules/legal_walk.go:77-79`) recomposes every refusal in the rules test binary and in `enginebench-verify` and panics on disagreement. The prototype ran that comparison 1.30M times with 0 mismatches (§1.7).
- **Edge cases to test explicitly** (each a table row in a new `rules/offer_floor_selfstatic_test.go`):
  - the self-static card itself, in hand, with an empty pool: no floor refusal; the composition runs and the card is offered when its reduced cost is payable (Ghalta's `TestGhaltaSelfReductionFromHand` is the precedent);
  - a different card while the self-static card sits in the library, hand or graveyard: the floor refuses;
  - a self-static plus a general reducer (e.g. a Biomancer's Familiar–style static): no floor;
  - a self-static on a merged pile or a face-down object;
  - a `Card.Self+<qualifier>` spelling: not `selfOnly`, so no floor;
  - an alternate-face probe (`offerCastableAsFace`): it does not call the floor today, and stays unchanged.
- **Files.** `rules/statics.go` (views struct), the cost-static collector that fills `costStaticViews` (`rules/statics_costscan.go` / `rules/walkcache.go` `boardStatics` fill), `rules/legal_walk.go`, a new test file.
- **Done means:**
  - Focused tests green:

    ```sh
    <capped> go test -timeout 2m -run 'TestGhaltaSelfReductionFromHand|TestValidTargetModifierRepricesBeforePayment|TestPotentialCostComposesReductionsForOneCandidate|TestFDNCastability|TestOfferFloorSelfStatic|TestHeads$|TestPotentialWalkSharedAcrossReaders' ./rules
    ```

    plus `./view` as above.
  - `TestLegalWalkDigest` files are byte-identical against main.
  - `make enginebench-verify` is clean.
- **Gate:** `make enginebench-pair BASE=main CAND=. ROWS=...` default rows, REPS=3.
- **Expected payoff** (prototype, §1.7): −5% (random A), −13% (random B), −10% (bot A) of walk CPU per ask, ≈ 1.5–4% of total CPU. A paired run on a box at load 15–25 has ±10–15% per-rep noise, so the walk-cum-per-ask from `enginebench-profile` is the primary metric and turns/s the secondary one.
- **Kill:** if `enginebench-profile` shows < 3% walk-CPU reduction on random-B (the row with two self-statics), or if the verify run trips on a case the edge table cannot classify.

### S2. Hoist the CastWithFlash lookup out of the per-card timing gate (R3)

- **What.** Two per-walk facts:
  - `flashBoard bool`: `len(e.activeStatics("CastWithFlash")) != 0`, read once, outside any face probe, like `boardFacts` (`rules/legal_walk_skip.go:39-61`).
  - A per-face constant: does the face carry its own CastWithFlash static. It goes in `walkFaceFacts` (`rules/walk_face_facts.go`) or `faceScanMemo`.

  `castWithFlash`'s fast path then reads the two bits before calling `withSelfStatics`. The walk passes them down; non-walk callers keep the current path.
- **Why exact.** `withSelfStatics(activeStatics(m), id, m)` is empty iff the board list is empty and the face carries none. Verify mode (`walkSkipVerify`) recomputes the original expression on every skip and panics on disagreement.
- **Files.** `rules/statics_cast.go` (only the fast path at :418-425), `rules/legal_walk.go` or `rules/legal_walk_skip.go` (the per-walk fact), `rules/walk_face_facts.go` (the face bit).
- **Done means:** the same focused set as S1, plus `-run 'Flash|MayFlash|Teamwork'` in `./rules`. Digest identical, verify clean.
- **Gate:** `enginebench-profile`. `castWithFlash` cum ≤ 0.02 s on random A.
- **Expected payoff:** ≤ 0.17 s of 4.17 (random A, 4%), ≤ 0.09 of 4.78 (random B, 2%), small on bot.
- **Kill:** < 1.5% walk cut on random A.

### S3. O(1) validity for the cross-walk board-statics scan (R2; followup.md item 7, remaining half)

- **What.** Give each static-zone summary slot (`staticZones`, `rules/static_zoneskip.go`) a dirty bit that the catch-up maintains, so `staticSourceIDs` and `boardScanMatches` stop comparing whole zone lists on every walk.
  - In `staticZonesCatchUp` (`rules/static_zoneskip.go:213-221`), per touched object id, mark dirty the slot it was last summarised in and the slot it is in now. That needs a dense `ObjID → slot` slice; no map.
  - Per event kind that reorders or refills a zone without naming its objects, mark that zone's slots dirty. The census of such kinds is part of this step: `Shuffle`, `LibraryOrder`, `Surveil` and any bulk move that carries ids only in a side field. Each kind is listed in a table with the reason, and a unit test asserts the table covers every `events.Kind` (fail closed: unknown kind → all dirty).
  - A clean slot answers "unchanged" without `slices.Equal`.
  - `boardScanMatches` then needs only (`staticTouchGen`, `crossWalkRetires`, all slots clean, loss unchanged).
- **Why exact.** Zone lists change only through `events.Apply`, and the catch-up sees every applied event (cursor `staticZonesEp`). Verify: the existing `staticZoneSkipVerify` (`staticZoneSkipVerifyOnce`, `rules/static_zoneskip.go:243-244`) is extended to compare the list on every clean answer.
- **Files.** `rules/static_zoneskip.go`, `rules/static_scan_reuse.go`, `rules/engine_*` for the new slice (tag `reset` or carried like `walkObjCls`; follow the existing `staticZonesEp` handling in `clone.go`).
- **Done means:**

  ```sh
  <capped> go test -timeout 2m -run 'TestStaticEmit|TestWalkClasses|StaticZone|TestHeads$' ./rules
  ```

  Digest identical, `enginebench-verify` clean.
- **Gate:** `enginebench-profile`. `gatherBoardScan` + `boardScanMatches` + `slices.Equal` under the walk drop by ≥ 60%.
- **Expected payoff:** ≤ 0.22–0.26 s on random A (5–6% of the walk). The same summaries feed other static scans (`activeScanUnchanged`, `rules/static_scan_reuse.go:224`), so some gain lands outside the walk.
- **Kill:** if the reorder-kind census cannot be made fail-closed without marking a common kind (e.g. `MoveZone`) as all-dirty, or if the walk cut is < 3%.
- **Collision:** `emit-trigidx` edits `rules/trigger_zoneskip.go` (a sibling file, not this one). `emit-evptr` changes how events are folded, by pointer (`rules/emit.go`, `rules/livelock.go`). Land S3 after both, and range events by index.

### S4. Carry per-object mana-ability membership across a seat's priority walks (R4; gated on S0)

- **Gate to start.** After S1–S3 land, S0 shows the mana gate still ≥ 8% of the walk on at least two of random A, random B and bot A. Profiles today give 4%, 12% and 6% respectively.
- **What.** For a seat's battlefield object, cache the mana membership with payability deferred: the list `ownManaMembers` already records for PotentialMana (`rules/walk_block_reuse.go:120-140`). The entry is valid while all of the following hold:
  - the object was not touched by any event since (the `staticZonesCatchUp` touch pattern, `rules/static_zoneskip.go:213-221`, extended with a per-object generation stamp);
  - `derivedSeq`, `staticTouchGen`, `crossWalkRetires` and `continuousVersion` are unchanged;
  - `G.Turn` is unchanged (summoning sickness, `pay.TapFlagsSick`);
  - every one of the object's mana SAs has all-plain `manaSAFacts` (`noActivation`, `noIsPresent`, `noPhaseGate`, `noCheckSVar`, `noLimit`; `rules/mana_safacts.go`, read via `manaFactsOf`);
  - the walk's board facts show no grants and no AddAbility carriers (`walkBoardFacts.hasGrants` / `addAbility`).

  Payability (`manaCostPayable`) is re-applied every walk, exactly as `ownManaMembers` does today. It is the only pool read.
- **Why exact (the argument the ticket must complete).**
  - Every input of the deferred membership is the object's own fields (event-touched), the derived characteristics (`derivedSeq`), the static and continuous registry, the turn (sickness), and the ability's own gates (all-plain facts mean none reads phase, board presence, SVars or activation counts).
  - Verify (`walkCacheVerify`) recomputes the membership on every hit and panics on difference.
- **Files.** A new `rules/mana_member_carry.go`; one call site in `rules/legal_walk_battlefield.go:99-104`.
- **Done means:** `-run 'TestPotentialWalkSharedAcrossReaders|Mana|TestHeads$'` in `./rules` plus `./view`; digest identical; verify clean.
- **Gate:** `enginebench-pair` plus profile.
- **Expected payoff:** ≤ the mana-gate cum (random B 0.59 s ≈ 12% of the walk; random A 4%; bot A 6%), minus the residual payability check.
- **Kill:** < 4% walk cut on random B, or any verify trip on the repo decks.
- **Hot-file note:** `rules/legal_walk_battlefield.go` is one call-site edit only. Add a new file rather than growing it.

### S5. Timing-only whole-walk replay (option a, restricted; gated, expected kill)

- **Gate to start.** After S1–S4, the walk is still ≥ 20% of CPU on the random rows, **and** S0 shows the class "`{Priority, DecisionAsk, DecisionMade, StepChange}` only, same sorcery bit, all pools equal" at ≥ 8% of priority walks on random A, random B and bot A.
- **What.** Record the previous posed walk per seat: its options, its log cursor, a pool stamp for every seat (the shape of `potentialPlayerStamp`, `rules/potential_walk_cache.go:131-149`), and `w.sorcery`. On the next posed walk for that seat, serve the recorded list (renumbered, through the same result path, including the arena tail) when all of the following hold:
  1. every event since is one of the four kinds;
  2. the pools and the sorcery bit are equal;
  3. `inertHeldOut` and `suppressedCast` are empty at both points (they are cleared on any other event kind, `rules/emit.go:705-709`, and are non-event walk inputs);
  4. the recorded walk set `timingCold`.

  `timingCold` is a new walk-level bit: every object the walk visited has a new `walkObjClass` bit `timingHot` clear, and the board summary shows no timing-gated static. `timingHot` is set conservatively from face facts:
  - any ability or static with `ActivationPhases$`, `SorcerySpeed$`, `Condition$`, `CheckSVar$`, `PlayerTurn`/`OpponentTurn`;
  - the keywords Ninjutsu, Foretell, Sneak, may-flash, Teamwork, Plot, Suspend, Prowl;
  - any face without configured facts.
- **Why it is the last step.**
  - **Soundness** rests on the completeness of `timingHot` over the ~70 `.Step` read sites in `rules` and `effects`. Verify mode (recompute the walk on every hit) can only catch a miss that the test corpus exercises.
  - **Payoff** is ≤ 10–11% of walks today (§1.5).
- **Kill:** any verify trip that a `timingHot` addition cannot classify, a served rate < 7% of walks, or < 3% walk-CPU cut.
- **Collision:** the result path (`rules/legal.go:225-265`) is the decision-arena ticket's. Land after it.

### Not planned here

- **R6 allocation:** owned by the decision-arena live-play ticket.
- **GC (26–32% of CPU):** owned by the pointer-free corpus work.
- **`unlockRoomFaceCost`** at 0.05–0.12 s: too small to plan for.

## 7. Collision map

| step | files touched | concurrent work | interaction / ordering |
|---|---|---|---|
| S0 | `cmd/enginebench/*` (new file + 2 hooks), optional `rules/walk_stats.go` (new) | bench Spare recycling (agentctl) edits enginebench's row loops | `play.go`'s `playRandom`/`playBot` loop bodies; the hooks are two lines. Rebase-trivial; land either order |
| S1 | `rules/statics.go`, the cost-static fill (`rules/statics_costscan.go`, `rules/walkcache.go`), `rules/legal_walk.go`, new test | **pointer-free corpus** (`docs/superpowers/specs/2026-10-06-pointer-free-corpus-design.md`, S2a–S4): card reads may move behind `Registry` accessors | S1 reads the static's `ValidCard$` through `staticView.ParamStr` (`rules/statics.go:728`), i.e. `cards.ParamSet`. Option A of that design keeps materialized `*Card` values and `ParamSet` unchanged (its §2.5 contract), so no conflict. Compute the `selfOnly` bit at view construction, not via any registry-level pointer |
| S2 | `rules/statics_cast.go` (fast path only), `rules/legal_walk_skip.go`, `rules/walk_face_facts.go` | pointer-free corpus: face facts live in `Face.ExtSlot` (`cards/slot.go:63`), which that design's §2.5.2 keeps identity-stable | none, provided the bit is a face fact |
| S3 | `rules/static_zoneskip.go`, `rules/static_scan_reuse.go`, an engine scratch field | **emit-trigidx** (`rules/trigger_zoneskip.go`, `rules/trigger_hotmerge.go`); **emit-evptr** (`rules/emit.go`, `rules/livelock.go`, entry folds by pointer); **event-log growth** (`events/log.go`: the catch-up assumes an append-only `L.Events` with `len` as cursor and detects rewinds by `n < ep`) | sequence after emit-trigidx and emit-evptr. If the event-log ticket changes the log to segments, the catch-up's `e.L.Events[ep:]` range must use its iterator. Check before starting |
| S4 | `rules/mana_member_carry.go` (new), 1 call site in `rules/legal_walk_battlefield.go` | **emit-renames** (`rules/setname.go`, `rules/setname_gate.go`: skips `refreshRenames`' `active()` rebuild) | S4 keys on `derivedSeq`/`activeBuildSeq`. Emit-renames changes when `active()` rebuilds, so it changes how often those counters move, not what they mean. Re-measure S4's hit rate after it lands |
| S5 | `rules/legal.go` result path, `rules/walk_objclass.go`, `rules/potential_walk_cache.go` | **decision arena for live play** (`rules/decision_arena.go`, `arenaOptions`; the walk's tail commit at `rules/legal.go:225-265`) | hard sequence: after the arena ticket. Served lists must go through the same tail/commit path |
| all | — | hot-files table (AGENTS.md) | none of the touched files is in the current top-5 hot list. `rules/legal_walk_battlefield.go` (877 lines) gets one call site only |

## 8. Risks

| risk | where it bites | mitigation |
|---|---|---|
| **Stale-cache class** (any cross-walk carry: S3, S4, S5) | an input changes without the key moving | every carry is keyed on an event-log cursor plus counters bumped only by events (`staticTouchGen`, `derivedSeq`, `continuousVersion`); a rewound log drops all. The verify flag recomputes every hit. The no-event probes (face flip, cost-composition exclusion) already bump `crossWalkRetires` (`retireCrossWalkMemo`), which every key includes |
| **Granted abilities, copies, face-down** | S4 membership; S1 `Card.Self` on a copy or pile | S4 disqualifies boards with grants or AddAbility carriers, and merged/face-down objects are always hot (`walkObjClass.manaHot`). S1: `Card.Self` is resolved against `sv.Source`; a copy is a different object; edge-case rows in S1's test |
| **Alternative costs, cost modifiers** | S1 floor | the floor is reached only for the plain `offerCastable` path. Alternate-face offers (`offerCastableAsFace`) never use it. Any non-self or self-on-this-card member keeps today's composition |
| **Mana restrictions (`RestrictValid$`), restricted pools** | S1, S4 | the floor already bails on `RestrictedMana` (`rules/legal_walk.go:127-130`). S4 re-checks payability every walk |
| **Commander tax** | S1 | the floor argument already bounds tax ≥ 0 from below (`rules/legal_walk.go:103-110`). Unchanged |
| **Flash grants** | S2 | the board half is read once per walk outside face probes. Target-conditional grants (Flash Photography, `rules/statics_cast.go:412-417`) still go through `castWithFlashTargets` whenever either bit is set |
| **Split second** | all | the filter runs after the walk on the final list (`rules/legal.go:210-212`). Untouched. S5 must replay before the filters and re-run them, or include `splitSecondHolds` in its key |
| **Priority windows** | S5 | only `askPriority`'s posed walk is ever recorded. Mana-ability payment windows (`manaWindowAsk`, `rules/cast_paymentplan.go:251`) build their own KChoose list and never call the walk. The inert hold-out and suppressed-cast sets are non-event inputs that S5 requires empty |
| **Hypothetical and search clones** | S3, S4, S5 | new fields are `reset` (or carried like `walkObjCls` with the owner guard). `TestClonePolicyEveryFieldTagged` forces the decision. Probes under `offerProbeDepth > 0` never cache. The sampler and azmcts clones (arena on) start cold, which is today's behaviour for every walk cache |
| **Verify-mode blind spots** | S4, S5 | verify catches only what the test corpus and bench decks exercise. That is why S5's soundness argument is weaker and S5 is gated. Run `TestLegalWalkDigest` (all repo decks, 2/4-seat and Commander) for every step |
| **Bench overfitting** | S1 | Rebuke and Tolarian Terror are in the bench decks, but the shape is corpus-wide (302 lines). Confirm S1's payoff on a repo-deck botbench sample, not only on the FDN pairs |

## 9. Reproducing the measurements

`/mnt/sata/gorge-training/enginebench/legalwalk-plan/` holds:
- the throwaway sources: `measure-src/zzwalkmeasure/` (the copied enginebench plus `measure.go`), `measure-src/scratch-rules.patch` (the counter and prototype diff against `rules/`) and `measure-src/zz_walkstats.go`;
- every run's stderr output (`wm-*`, `ws*-*`, `proto-*`);
- the pprof peeks (`pprof-peek/`).

None of it is committed.

### 9.1 Transition observer (§1.5, becomes S0)

**Setup.** Copy `cmd/enginebench` to an untracked `cmd/zzwalkmeasure`, add the file below, and add two hook lines:
- `wm.reset()` after `rules.New` in `playRandom`;
- `wm.observe(e, d)` right after `d := e.Pending()`.

For the bot row, pass `bench.Hooks{Setup: func(e){eng=e}, Decision: func(_, d, _, _){wm.observe(eng, d)}}`. This disables the sync answerer (a Decision hook does), which does not change priority asks. Finally, call `wm.report()` before the final JSON line.

The observer's core:

```go
// per posed KPriority decision d for seat p:
b, _ := json.Marshal(d.Options)                       // wire form, Index included
cur := prevAsk{opts: string(b), ev: len(e.L.Events),
    sorcery: e.G.Active == p && e.G.Step.IsMain() && len(e.G.Stack) == 0}
if pv := m.prev[p]; pv.ok {
    same := pv.opts == cur.opts
    // classify the kinds in e.L.Events[pv.ev:cur.ev]: all in {DecisionAsk,DecisionMade,Priority}?
    // all in that set plus {StepChange,ManaClear,Note}? same sorcery bit? the sorted kind-set signature
}
m.prev[p] = cur
```

**Running it.**

```sh
go build -o $SCRATCH/walkmeasure ./cmd/zzwalkmeasure
for row in random bot; do
  systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB \
    timeout 120 $SCRATCH/walkmeasure -row $row -pair A -secs 15 >/dev/null 2>$SCRATCH/wm-$row-A.txt
done
```

### 9.2 Gate counters and the S1 prototype (§1.6, §1.7)

This was done in a throwaway worktree (`scripts/agent-worktree.sh legalwalk-scratch main`), removed afterwards, with a package-level counter struct `rules.WalkStats`:
- **Walk entry** (`rules/legal.go:130`): count walks; for a posed walk (`forAsk && hyp == nil && !castsOnly`), count those at sorcery speed and those with an empty pool.
- **`offerCastable`** (`rules/legal_walk.go:71`): count calls, calls with an empty pool, and successes.
- **`offerFloorRefuses`:** each `return false` arm counts its own bail reason. For the statics arm, it also records `len(raise/reduce/set)`, `validTarget`, and the source card names.
- **`spellTimingOK`:** calls and false results.
- **The prototype:**
  - replaces the statics bail with `... && !(WalkProto && selfOnlyOther(statics, w.curID))`, where `selfOnlyOther` is true iff every raise/reduce/set member has `ParamStr(cards.PKValidCard) == "Card.Self"` and `Source != id`;
  - adds, under `WALKPROTO_CHECK=1`, a call to `offerCastableUsing` on every floor refusal that counts any `true` as a mismatch.

Runs:

```sh
WALKPROTO=1 WALKPROTO_CHECK=1 ./walkproto -row random -pair A -secs 6   # agreement
WALKPROTO=0 ./walkproto -row random -pair B -secs 8 -cpuprofile base.cpu   # CPU, then WALKPROTO=1
go tool pprof -top -cum ./walkproto base.cpu | grep legalActionsWalkWithWindow
```

### 9.3 Corpus census (§1.6)

```sh
cd /home/sadams/projects/gorge/.cards/cardsfolder
/usr/bin/grep -rlE '^S:Mode\$ (ReduceCost|RaiseCost|SetCost)' . | wc -l                                    # 598
/usr/bin/grep -rhE '^S:Mode\$ (ReduceCost|RaiseCost|SetCost)' . | /usr/bin/grep -c 'ValidCard\$ Card.Self' # 305
/usr/bin/grep -rhE '^S:Mode\$ (ReduceCost|RaiseCost|SetCost)' . | /usr/bin/grep 'ValidCard\$ Card.Self' | /usr/bin/grep -c 'EffectZone\$ All'  # 302
```
