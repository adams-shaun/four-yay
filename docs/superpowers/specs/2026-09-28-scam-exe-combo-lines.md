# Scam.EXE: combo audit and real-engine loop experiments

**Research baseline:** `86a41a2f7`, 2026-09-28. See the
[shortcut design](2026-09-28-loops-and-shortcuts-design.md) for CR sources,
protocol, safety, bot/search and implementation tickets.

## 1. Sources and the actual list

The authority for this exercise is the supplied snapshot of
[Saitin's “Scam.EXE|| Rakdos the Muscle cEDH”](https://moxfield.com/decks/ev1ALilkekSHpx-D6bG6Og),
labelled updated 2026-01-07, with Rakdos, the Muscle as commander. The supplied
primer links [Play to Win](https://youtu.be/uJbnvz0xGDQ) (brief dates that feature
2024-11-17). Those dates are supplied metadata, not independently established
by a video watch or live Moxfield export. The list itself, not a reconstruction
of the video deck, was measured.

- Input: `.ds4/scam-exe-decklist.txt`, SHA256
  `e6f4cef72b1c80536e1c774e87555c60018b034f4a5f18a3c4aaf6dfb83a309d`.
- Primer: `.ds4/scam-exe-primer.md`, especially its six numbered infinite-combo
  families. The prose below is an independent rules analysis, not a copied primer.
- Committed fixture: `rules/testdata/scam-exe.json`, **100 cards / 100 distinct
  names**, commander included in the cards array as deckimport's format expects.
- Card rules: inspected the pinned `.cards/cardsfolder` scripts and their Oracle
  text, including both faces of the MDFCs and Sephiroth. `FORGE_REF` is
  `95f04e8a04c8925fa97cb226fc3341cabcc90a53`. No script text is committed.
- The old `.ds4/r1-draft` budget list is **not** this list. Only its fixture/
  instrumentation idea was reused, not its deck or Skeleton/Plunderer combo.

Import, after changing the single `COMMANDER 1 ...` line to a Commander section
followed by a Deck section in ignored scratch:

```sh
GOMAXPROCS=4 GOMEMLIMIT=2GiB go run -p 1 ./cmd/deckimport \
  -in .ds4/scratch/import-list.txt -out rules/testdata/scam-exe.json \
  -name 'Scam.EXE|| Rakdos the Muscle cEDH — Saitin' \
  -notes 'Moxfield ev1ALilkekSHpx-D6bG6Og; supplied snapshot updated 2026-01-07. Research only, not a supported repo deck.'
```

Measured output: `cards: 100 resolved: 100 (100.0%)`, all present, commander
eligible. **deckimport checks existence/construction, not primitive support.**
`rules/acceptance_test.go` uses `Registry.Unsupported(card, effects.Supported())`;
`make report` invokes `forgec report` for corpus-wide registration coverage.
Our focused `TestLoopPrototypeCoverage` uses that same per-card check plus
`scanPackages().derived()` / `cardCensusLabels` from the parameter census. No
full-corpus report or repo-deck ratchet was needed or changed.

### Important list/primer discrepancies

Not present: **Dualcaster Mage, Saw in Half, Altar of Dementia**, as well as the
brief's six older names **Molten Duplication, Cathodion, Nine-Lives Familiar,
The One Ring, Blood Artist, Mayhem Devil**. The first three omissions materially
change which primer combos the current 100 can assemble. All nine nevertheless
exist in the corpus and remain represented in the research catalog or audit.
Nine-Lives Familiar is the corpus lookup spelling (hyphenated).

| Family | Assembled from this 100? | Measured engine result |
|---|---|---|
| 1 Rakdos / Altar / Miner | Yes | 1, 20, 100 cycles pass |
| 1 Sephiroth / Altar / Miner | Yes | 10 cycles pass, including fourth-resolution transformation |
| 1 Rakdos / Soultrader / Miner | Yes, but life-bounded alone | 10 cycles pass; life 20 → 10 |
| 1 Soultrader / Miner / Sephiroth, with or without Rakdos | Yes | Both combined variants run 10 cycles; pilot life conserved, opponent loses 10 |
| 2 Rakdos / Ashnod / Thug, optionally Poxwalkers | Yes | N=1 and N=20 repeatable lines pass; 101/2,020 events, 23/384 intents |
| 2 Artist / Devil payoffs | No, those payoffs absent | Thug base line passes; payoffs not independently simulated |
| 3 Dualcaster / Molten or Saw | No | Each off-list reference line runs 20 loop-bearing copies |
| 4 Monk / Offering / Saw | No, Saw absent | Off-list reference line runs 20 steady-state cycles |
| 5 Nightmare / mana-creature pair | Yes for several pairs | Gix / Urabrask runs 1 and 20 full two-activation rounds |
| 6 Breach / Dementia / Poxwalkers / LED | No, Dementia absent | Off-list reference line runs 1 and 20 cycles, library-bounded |
| 6 Cathodion variant | No, Cathodion and Dementia absent | Rules/resource audit only, not a measured engine loop |

“Runs” means a fixed, unopposed fixture sequence through the real engine, **not**
complete support for every interaction that each card can participate in.

## 2. Verified loop lines and piloting details

Notation: a cycle boundary has pilot priority and the declared stack state;
“infinite” below always means an arbitrarily large **finite** declared repeat
count, not a program that never returns. Except where specified, graveyard
replacement, loss of a required piece, countering a critical spell/trigger,
or prohibiting entry/casting breaks a line. Opponents retain normal legal
interaction windows, not a chance to interrupt payment of a cost.

### 1A. Rakdos + Phyrexian Altar + Forsaken Miner

Start with all three on your battlefield; no seed mana required.

1. Activate Altar, sacrifice Miner, choose black. Mana ability resolves without
   using the stack. Miner is now in the graveyard and B is available.
2. Put Rakdos's sacrifice trigger on the stack targeting an **opponent**. This
   target declaration commits a crime while Miner is in its trigger zone.
3. The Miner trigger goes above Rakdos. All pass; pay B and return Miner.
4. Resolve Rakdos: exile one card (Miner's mana value) from that opponent's
   library, with Rakdos's play permission. Return to the boundary.

Net: one sacrifice/death/entry, mana neutral, **zero storm** (no spell cast),
one library card exiled while cards remain. Once a library is empty it remains
a legal target: crimes and Miner recursion can continue, but library output
saturates. Do not claim the table is dead merely because libraries are empty;
players lose on failed draws, or an added drain payoff kills them now.

Rakdos is the repeatable crime source and library payoff, **not** the sacrifice
outlet: his own activated sacrifice ability is once per turn. Targeting yourself
with Rakdos does not generate this crime. Decline Miner's payment to stop.

### 1B. Soultrader and Sephiroth variants — not interchangeable single pieces

**Warren Soultrader replaces Altar:** each activation costs one life and creates
a Treasure on resolution. The Rakdos/crime/Miner triggers resolve **before** the
Treasure ability. Have one B (or a previously available Treasure) to bridge the
first Miner payment. Then spend the newly made Treasure for B to restore the
seed. Net mana zero, life −1, one death/entry and one Treasure created/spent.
This alone is **bounded by life**, and paying the last life loses before gain.

**Add Sephiroth:** each Miner death targets an opponent for drain 1/gain 1;
the gain offsets Soultrader's life cost (start above one life). Its targeting
also supplies a crime, so **Soultrader + Miner + Sephiroth works without Rakdos**.
Rakdos adds library exile and an extra crime trigger, not an extra Miner return:
a redundant Miner trigger does not return the same graveyard object twice.
Order and decline redundant payments explicitly.

**Sephiroth substitutes for Rakdos with Altar:** sacrifice Miner for B, target
opponent with Sephiroth's death trigger, return Miner for B, drain/gain one.
At the fourth drain resolution this turn Sephiroth transforms; the emblem
continues death-drain/crime production. Model this as two macro phases, not an
unchanging front-face action template. The fixture verifies the transformed
face after four and successful drains through ten. Removing Sephiroth after
its emblem exists does not remove the emblem.

**Sephiroth does not replace Altar alone.** His entry/attack sacrifice is not a
free repeatedly activatable outlet. The primer's wording requires Soultrader
**and** Sephiroth to supply the Altar-like sustainable resource path. Countering
the initial return/drain, blocking life gain in the Soultrader version, graveyard
hate, or removing the outlet breaks it. Altar/Sephiroth need not gain life to
fund recurrence, although drain-prevention can remove its win.

### 2. Rakdos + Ashnod's Altar + Golgari Thug

Start all three on battlefield, your main phase. This is not dredging.

1. Sacrifice Thug to Altar for CC. Both Thug's death trigger and Rakdos trigger.
2. Put **Rakdos first (bottom)** targeting yourself; put **Thug second (top)**
   targeting the Thug now in your graveyard.
3. Resolve Thug: put it on top of your library. Resolve Rakdos: exile the top two,
   Thug plus one other card if present.
4. Use Rakdos's permission to cast Thug from exile. Its mana value is two;
   Rakdos permits CC to pay its normal 1B. Pass and resolve it.

Net: mana neutral, storm +1, death/entry +1, at most one **other** own library
card exiled. It continues after all other cards are gone: Thug alone is returned
to top, exiled, recast. “Infinitely exile your own deck” means repeated access,
not an infinite number of distinct cards. Choosing spells based on newly exiled
unknown cards is a new decision, not part of a predictable shortcut.

Payoffs:

- **Blood Artist:** one targeted drain/gain per Thug death (absent here).
- **Mayhem Devil:** one damage per sacrifice (absent here).
- **Poxwalkers:** begin in graveyard; every Thug cast from exile returns it
  tapped. Sacrifice it to Altar for **net +CC** per full Thug/Poxwalker cycle;
  Rakdos exiles three from an opponent for that sacrifice. Tap status does not
  prevent sacrifice. This does not immediately make an empty-library player
  lose; choose a further win action or survive to their draw.
- Current **Sephiroth** is a drain payoff too. **Skittering Precursor** can add
  sacrifice-produced Spawn; these are adjuncts, not repairs for failed Thug
  placement.

Breaks: wrong trigger order (Thug misses exile), graveyard exile, Rakdos removal
before its permission exists, casting restrictions, or disrupting the recursion.
**Engine blocker:** the object-target path of `effects.effChangeZone` moves
Thug to the library but omits ordinary `LibraryPosition=0` placement. The
library tail handles shuffle/alternative placement, not this normal case.
The ordered triggers are correct; Rakdos exiles two different cards. The strict
reproducer fails rather than “helping” by manually putting Thug on top.

### 3A. Dualcaster Mage + Molten Duplication (both absent)

Need another creature/artifact as initial target, Duplication's 1R and Mage's
1RR, and a window to hold priority after casting Duplication.

1. Cast Duplication targeting the seed permanent; respond with Mage.
2. Mage enters and targets original Duplication on the stack with its copy
   trigger. Retarget the copy to Mage.
3. Copy resolves, making an artifact Mage token with haste. That token's ETB
   targets the original spell, still on the stack. Repeat.
4. End by directing a copy to a non-Mage seed; let the original resolve.

Each loop-bearing copy adds one 2/2 hasty Mage token (mana value three); no
mana per repeat and **no extra storm** for copies. Win with a sufficiently large
attack or sacrifice/entry payoffs. Rakdos is optional and converts sacrificed
Mage copies into three-card exile triggers. Sacrifices are at the **next end
step**, not “end of combat” as the primer says. Stop by choosing the seed;
remove the only Mage at the appropriate window or counter the original to
break. This is arbitrarily repeatable while the original spell remains.

### 3B. Dualcaster Mage + Saw in Half (both absent)

Cast Saw for 2B on another creature, respond with Mage for 1RR. Copy Saw and
retarget it to Mage. Mage dies and produces two Mage tokens, each with a copy
trigger. One trigger copies original Saw to repeat; the spare trigger can copy
another suitable spell kept on the stack, or use a harmless terminal target.

Per repeating Saw copy: one death, two ETBs, net one Mage, one spare copy
trigger; no mana or storm per copy. The initial 2/2 becomes **1/1** copies,
which remain 1/1 under further halving/rounding up; mana value remains three.
They **do not gain haste** from Saw. A separate damaging spell on the stack
can be copied repeatedly to win immediately; a token army alone waits for
combat unless another payoff is present. Rakdos triggers only on **sacrifice**,
not Saw's destruction, so a separate outlet is needed for its exile payoff.
Indestructibility/death replacement prevents Saw's token creation. Countering
original Saw or choosing not to continue the Mage target chain stops it.

### 4. Pinnacle Monk + Burnt Offering + Saw in Half (Saw absent)

Bootstrap with one Monk, Saw available and Offering accessible in graveyard;
Saw splits Monk into two copies whose ETBs return Saw and Offering. At the
steady boundary have **two Monks**, the two instants in hand and one B.

1. Cast Offering for B, sacrificing Monk A as an additional cost. Produce five
   mana (Monk copies retain mana value five); choose enough black.
2. Cast Saw for 2B targeting Monk B. On resolution it dies and creates two
   Monks. Prowess triggers resolve normally; copied mana value stays five.
3. Target Offering with one ETB and Saw with the other. Return both to hand.

Net: **+1 mana**, +2 casts/storm, two deaths, two entries, **zero Monks** relative
to the two-Monk boundary. This primary loop does not grow a token army per
iteration. After floating arbitrary mana, switch to a second phase repeatedly
casting/recovering Saw without Offering: spend three mana per added Monk. That
explains how arbitrary Monks can still be obtained. Rakdos adds exile five on
the Offering sacrifice, not on Saw's destruction. A drain payoff or a subsequent
spell wins; the three pieces alone do not directly damage opponents. Countering
Saw, removing a recursion target or preventing the death breaks the cycle.

### 5. Chthonian Nightmare and mana-creature pairs

Nightmare's entry grants three energy. Its sorcery-speed activation **pays X
energy**, sacrifices a creature and returns Nightmare to hand as costs to
reanimate a different graveyard creature of mana value X. It does not target
the creature being sacrificed from the battlefield as the chosen graveyard
card. Recast Nightmare for 1B between activations. A full alternating round
requires two casts costing **2BB** and spends `MV(A)+MV(B)` of the six new energy.
Check **prefix** budgets as well as net totals: mana arriving later cannot pay
an earlier cost.

1. Start Nightmare + A on battlefield, B in graveyard, three energy and the
   necessary seed resources.
2. Sacrifice A/return Nightmare/pay energy for B; resolve B and relevant mana
   triggers. Recast Nightmare, resolve its energy trigger.
3. Sacrifice B/return Nightmare/pay energy for A; resolve A and mana. Recast
   Nightmare. This is one full round.

| Pair | Mana production / full round | Energy net | Sustainable? |
|---|---|---|---|
| Priest of Gix / Priest of Urabrask | BBB + RRR − 2BB = +2 total | 0 | Yes; Gix's entry supplies initial black in fixture |
| Gix / Charming Scoundrel (Treasure mode) | BBB + one Treasure − four = 0 | +1 | Yes with prefix funding |
| Gix / Cathodion (absent) | BBB + CCC − four = +2 | 0 | Yes; black from Gix |
| Atsushi / Charming Scoundrel | three death Treasures + one entry Treasure − four = 0 | 0 | Yes; start Atsushi on battlefield to fund first recast |
| Atsushi / Gix | three Treasures + BBB − four = +2 | **−1** | Energy-bounded without another source! |
| Two low-yield Treasure creatures | Usually fewer than four mana | Often positive | Not automatically a mana-sustainable loop |

Thus the primer's “make 2BB every two activations” criterion is necessary but
**not sufficient**: energy matters. Cathodion/colorless mana cannot alone pay
the two black pips. Name Sticker Goblin explicitly excludes entry from graveyard
or exile, so is not a reanimation mana substitute. Gix plus one-mana death-
Treasure fodder (Impulsive Pilferer, Shambling Ghast, Greedy Freebooter) can be
mana-neutral; account for their modes/scry asks. Recasting Nightmare gives
storm +2 per full round; creature reanimation itself is not casting.

Rakdos exiles mana-value-sized chunks on each sacrifice. Sephiroth drains on
each death, including the transformation breakpoint. Without a payoff,
mana-neutral pairs merely loop; a +mana pair needs a mana sink to win. End by
not activating/recasting Nightmare. Sorcery timing, graveyard hate, targeting
restrictions, entry prevention, and resource deficits break it. Only the
Gix/Urabrask representative is measured here, not every combination in this table.

### 6. Underworld Breach + Altar of Dementia + Poxwalkers / Cathodion

The current list lacks Dementia and Cathodion. These are primer reference lines,
not a claim that the 100 contains them.

**Poxwalkers + LED:** start Breach/Altar on battlefield, Poxwalkers and LED in
graveyard, with three other expendable graveyard cards.

1. Escape LED for zero mana, exiling three **other** graveyard cards. Preserve
   Poxwalkers; its cast-from-not-hand trigger returns it tapped.
2. Resolve LED, then activate it at a legal instant-speed window: discard hand,
   sacrifice LED, add three mana of one color.
3. Sacrifice Poxwalkers to Dementia, targeting yourself, mill three (power three).
   Those cards finance the next LED escape. Repeat while fuel exists.

Net: +3 mana, +1 cast/storm, one death/entry, self-mill three, exile three fuel.
**Bounded**, because the library/graveyard supply is finite. A zero-cost spell
that returns to the graveyard can serve as the repeated cast; LED supplies mana
and self-sacrifice. Do not exile the recurring engine pieces as costs. If you
mill opponents instead, you need an independent escape-fuel supply. Rakdos is
optional; each Poxwalker sacrifice can additionally exile three from an opponent
or yourself. Self-exile is **not** mill fuel and can shorten the available run.

**Cathodion:** escape Cathodion for three mana plus three other cards, sacrifice
it to Dementia to mill three, regain CCC from its death. This subcycle is mana
neutral after seed CCC and gives storm +1; the extra free-spell/LED path is not
required for this Cathodion subcycle and costs additional graveyard fuel if
added. Cathodion does not trigger-return for free like Poxwalkers. Both versions
are bounded access/mana engines, not a standalone immediate kill. Remove Breach,
exile the creature, or exhaust fuel to stop them.

### Other major lines present in the current list

These are **derived candidates/value lines**, not additional measured full-loop
certificates:

- **Skittering Precursor** adds one Spawn per nontoken sacrifice to Miner/
  Altar/Rakdos (or Thug). Keep it or sacrifice it for colorless mana. Spawn
  sacrifices do not recursively create more Spawn. This turns an otherwise
  mana-neutral recurrence into a resource engine; reuse the underlying line's
  blockers. Rakdos's exile on a Spawn's sacrifice is zero mana value.
- **Timeline Culler + Phyrexian Altar + Rakdos:** sacrifice Culler for B, warp
  it from graveyard for B and two life, sacrifice before the delayed exile.
  Each repeat casts once, loses two life and yields Rakdos exile two. This is
  life-bounded; Sephiroth alone offsets only one of the two life. Stop before
  lethal payment or the end-step exile. No tap requirement prevents recurrence.
- **Breach + LED + Poxwalkers + Umbral Collar Zealot:** Zealot can sacrifice
  Poxwalkers to surveil one instead of the absent Dementia's mill three. LED
  still makes three mana, but this consumes net two graveyard fuel per repeat.
  It is a shorter bounded storm line, with optional Rakdos exile on sacrifices.
- **Evoke/ritual storm:** Grief/Fury/Ingot Chewer/Shriekmaw plus Rakdos turn
  low/free casting costs and real sacrifices into four-/five-mana-value exile
  access. Burnt Offering, Sacrifice, Culling the Weak, Infernal Plunge and
  Songs of the Damned generate finite bursts; Dargo discounts use sacrifice
  history. These are not an invariant fixed loop without a recurrence engine.
  K'rrik and Treasonous Ogre turn life into costs/mana, not unlimited resources.
- **Assembly/protection:** Buried Alive/Entomb/tutors find or seed Miner,
  Poxwalkers and Nightmare lines; Goblin Recruiter and Flamekin Harbinger stack
  known cards for Rakdos access. Opposition Agent plus Wishclaw is a search/
  denial line, not a self-repeating combo. Spider-Punk/Hexing Squelcher resist
  counters; neither grants blanket protection from removal or graveyard hate.
- Historical **Nine-Lives Familiar** returns at the next end step with a
  declining revival count, not immediately/free forever. **The One Ring** is a
  finite draw engine without an additional untap/recursion loop. Neither is in
  the current list or repairs a missing primer combo piece.

This is an enumeration of the primer families and additional lines identified
in this audit, not a completeness proof over all interactions among 100 cards.

## 3. Prototype, measurements, and tutorial transcript

Committed harness: `rules/loops_prototype_test.go`; declarative research seed:
`rules/testdata/loop-combos.json` (10 family records, variants, roles, resource
needs, ordered bodies, deltas, breaks and evidence labels). It is intentionally
not an executable production schema; `TestLoopPrototypeCatalog` checks names,
required fields and unique IDs.

The `.cards` symlink **already existed** and was readable. `loopCorpus` fails
rather than silently skipping if it is missing. Tests use production corpus
cards and tokens, `New`, `toMain1`, logged `emit` setup moves and ordinary
`Submit` answers. No direct Game mutation or per-iteration resource injection.
The pilot fixture is the combo pieces plus 80 basic-land filler; opponent is
80 basics. This isolates the loop, **not** a natural goldfish of the whole 100,
a four-seat cEDH win-rate estimate, or a legal Commander deck-construction test.
Off-list pieces are explicitly labelled reference experiments. The separate
100-card fixture is used for coverage/membership, not silently substituted by
the earlier budget deck.

Every measured boundary saves a clone and replays the **actual recorded intents**
from that boundary, requiring equal event arrays and chain heads. Miner also
reruns its script from fresh setup and checks equality. This is stronger than
only testing final life totals, but it is **fixture-boundary replay**, not
`replay.Replay(Config,log)` from a natural match: setup events are not player
intents. Optional raw logs are not complete `cmd/repro` feedback snapshots.

Reproduce (roughly 23 seconds in the measured run):

```sh
GOMAXPROCS=4 GOMEMLIMIT=2GiB go test -p 1 \
  -run '^TestLoopPrototype' ./rules/ -count=1 -v
# Optional full event/intent logs, ignored and potentially private:
GORGE_LOOP_TRANSCRIPTS="$PWD/.ds4/scratch/loop-transcripts" \
GOMAXPROCS=4 GOMEMLIMIT=2GiB go test -p 1 \
  -run '^TestLoopPrototype' ./rules/ -count=1 -v
```

Results from `.ds4/scratch/loops-pass.log` and the subsequent
`.ds4/scratch/soul-variants.log`; events are **after setup boundary**,
intent counts are **whole fixture totals**. Timing includes first-run loop and
optional JSON export, excludes corpus load/setup and cleanup replay; single
shared-machine samples, not benchmark claims or a throughput SLA.

| Line / N | Events | Intents | Final resources / observation | ms / iteration | Head |
|---|---:|---:|---|---:|---|
| Miner 1 | 42 | 13 | mana 0, life 20/20 | 0.905 | `8eee2f2c7370de68` |
| Miner 20 | 840 | 184 | mana 0, 20 opponent cards exiled | 2.608 | `a241173aa84fbc3c` |
| Miner 100 | 4173 | 904 | opponent library empty, game **not over** | 54.617 | `359fc8718c8dddc2` |
| Sephiroth 10 | 423 | 97 | life 30/10, mana 0, transformed | — | `ff889fd724f58ad5` |
| Soultrader / Rakdos 10 | 620 | 124 | life 10/20, seed B retained | — | `550d3b397c3433ff` |
| Soultrader / Sephiroth 10 | 623 | 127 | life 20/10, seed B retained | — | `90468e935436dd6f` |
| Soultrader / both 10 | 963 | 207 | life 20/10, redundant Miner triggers handled | — | `9e799eea0cdde2f8` |
| Thug 1 | 101 | 23 | 1 completed; 2 colorless mana in pool | 1.5 (reported 2026-09-28) | — |
| Thug 20 | 2,020 | 384 | 20 completed; 40 colorless mana in pool | 17.3 (reported 2026-09-28) | — |
| Nightmare 1 round | 136 | 35 | 2B + 3R, energy 3 | 3.374 | `39c2118a3e4cb160` |
| Nightmare 20 rounds | 2738 | 529 | B + 42R, energy 3 | 2.810 | `6ece1ee638ba73a8` |
| Dualcaster / Molten 20 | 613 | 116 | 21 total Mages after stopping | 4.453 | `82be90d7f2b914ad` |
| Dualcaster / Saw 20 | 1190 | 236 | 21 total Mages after stopping | 8.647 | `8d7b1fb4f507955e` |
| Monk 20 | 2180 | 420 | seed B → 21B, two Monks recurring | 18.964 | `0dbd6c8b1aa56d72` |
| Breach 1 | 71 | 17 | +3B, self-mill 3 | 2.025 | `0f89bd614d034659` |
| Breach 20 | 1344 | 264 | +60B, self-mill 60 | 2.214 | `c1140a7debca58b7` |

Dualcaster N counts the copies that target a Mage; termination copies and the
original resolving are included in events. Nightmare N counts **two** activations
and recasts. Setup Gix entry provides the initial BBB. Monk bootstrap is outside
its 20 counted cycles. Breach starts with three basic cards moved to graveyard;
all later fuel comes from real milling/LED discard, not fixture injections.

No successful run tripped the default engine watcher. This harness bypasses
the host and therefore **does not measure** `MaxDecisionsPerTurn`/`MaxIntents`.
The sharp Miner cost increase between 20 and 100 is measured, not explained:
profile growing permission/history/object walks before attributing it. Network
batching alone cannot remove that CPU cost. Counts beyond 100 and million-token
execution were not attempted.

### Readable event-level walkthroughs

The numbered instructions in §2 are the pilot tutorial. Below are checkpoints
from actual streams; all intervening priority/decision events remain in JSON.
IDs are fixture-local, not reusable script identities.

**Miner, first cycle** (`miner-1.json`):

1. Sacrifice Miner object 83 to Altar 82, choose B; it moves battlefield →
   graveyard and logs one mana addition.
2. Rakdos 81 pushes its trigger and targets player 1. This creates Miner's
   crime trigger on top. Nothing has exiled a library card yet.
3. Both players pass. Pay B at the explicit trigger-cost decision; Miner moves
   graveyard → battlefield and the pool returns to zero.
4. Pass for Rakdos; one opponent library card moves to exile. A permission
   clock tick and cleanup Note follow. Return to pilot priority: **42 events**.
5. Repeat that same pilot instruction 20 times: **840 events**, Miner always
   restored. After 100, empty library does not end the game; the test asserts it.

**Thug placement defect — resolved.** The original captured trace (`thug-blocked.json`)
showed the old object-target `ChangeZone` path moving Thug to the library without
placing it on top. That defect was fixed in merge `9d2200501` (`effects/zone.go`);
the repeatable prototype now passes normally and under strict mode. Its measured
N=1 line is 101 events / 23 intents with 2 colorless mana remaining; N=20 is
2,020 events / 384 intents with 40 colorless mana remaining. The old trace is
historical baseline data, not the current result.

Five corpus files contain the candidate DB ChangeZone / Graveyard → Library /
LibraryPosition 0 / ValidTgts shape: Golgari Thug, Flitting Guerrilla,
Hag Hedge-Mage, Meldweb Curator, Boseiju Reaches Skyward // Branch of Boseiju.
This is a **candidate-shape census**, not five separately verified defects.
The original placement issue is covered by the Thug loop regression; no new
engine change is part of this update.

**Nightmare tutorial checkpoint:** choose X=3, sacrifice Gix, target Urabrask;
Nightmare returns to hand and energy is spent as real costs. Urabrask enters,
RRR arrives. Pay 1B to recast Nightmare, gain three energy. Repeat in reverse,
Gix supplies BBB, recast Nightmare. Energy returns to three; total pool gains
two. After 20 rounds the roles/zones are restored and 43 mana remains (seed
three plus net forty), with 2,738 events. No X/target/payment choices are skipped.

**Dualcaster tutorial checkpoint:** after original spell and Mage casts, answer
ETB target with original spell; at `copy_targets`, select a living Mage. Do
that 20 times, then select Blood Pet. For Saw, pending spare ETBs are allowed
to finish using the seed target (later copies with a now-illegal inherited
target fizzle rather than restarting). Both streams finish with 21 Mages and
an empty stack; Mage count is asserted, not inferred from click count.

**Monk tutorial checkpoint:** bootstrap Saw into two Monks; allocate their
ETB targets separately. Offering sacrifices one, asks for a **five-unit B/R
allocation** (five distinct black choices, not five duplicates of one option),
then Saw destroys the survivor. The two new ETBs recover the two spells.
After cycle 20, both spells have returned again and pool is 21B: net +20 from
seed B. This exposed a harness-answer mistake during development, not an engine
bug; the final script answers the existing mana decision correctly.

**Breach tutorial checkpoint:** each escape chooses three expendable basic
cards, never LED/Poxwalkers. The Poxwalker trigger resolves before the LED
spell. LED's real ability discards the hand and produces BBB. Sacrifice the
returned tapped Poxwalkers to Dementia and mill yourself three. At cycle 20,
60 cards were milled and 60 black mana produced; no unbounded-fuel claim is made.

## 4. Support status per card

Measured with `TestLoopPrototypeCoverage` on this baseline: **all 100 current
cards and all 9 historical reference cards return `primitives=[]`, `params=[]`.**
That is zero unregistered primitives and zero unread parameter/unmodelled cost
labels under the two existing tooling checks. It is **not** proof of complete
behavior; the supported-labelled Thug demonstrably fails its combo. No deck was
added to `internal/testutil/decks`, no ratchet or golden was changed.

Per-card inventory follows. Every name below has the same measured tooling
status **P0 / Q0** (empty primitive / parameter lists). “Reference” means absent
from the supplied 100; no additional missing primitive can honestly be named
for those cards at this baseline.

| Current 100: P0 / Q0 for each entry | Current 100: P0 / Q0 for each entry |
|---|---|
| Rakdos, the Muscle | "Name Sticker" Goblin |
| Ancient Tomb | Arcane Signet |
| Arid Mesa | Ashnod's Altar |
| Atsushi, the Blazing Sky | Badlands |
| Blazemire Verge | Blood Crypt |
| Blood Pet | Bloodstained Mire |
| Boggart Trawler // Boggart Bog | Buried Alive |
| Burnt Offering | Cabal Ritual |
| Cabal Therapy | Cavern of Souls |
| Charming Scoundrel | Chrome Mox |
| Chthonian Nightmare | City of Brass |
| Command Tower | Crystal Vein |
| Culling the Weak | Dargo, the Shipwrecker |
| Dark Ritual | Deflecting Swat |
| Demonic Tutor | Diabolic Intent |
| Entomb | Flamekin Harbinger |
| Flare of Duplication | Forsaken Miner |
| Fury | Gamble |
| Gleaming Barrier | Goblin Recruiter |
| Golgari Thug (**behavior measured: N=1/N=20 pass**) | Greedy Freebooter |
| Grief | Grim Monolith |
| Hexing Squelcher | Imperial Seal |
| Impulsive Pilferer | Infernal Plunge |
| Ingot Chewer | Jet Medallion |
| K'rrik, Son of Yawgmoth | Lion's Eye Diamond |
| Lotus Petal | Luxury Suite |
| Mana Confluence | Mana Vault |
| Marsh Flats | Master of Dark Rites |
| Mount Doom | Mountain |
| Myr Moonvessel | Opposition Agent |
| Orcish Bowmasters | Party Thrasher |
| Phyrexian Altar | Phyrexian Tower |
| Pinnacle Monk // Mystic Peak | Polluted Delta |
| Poxwalkers | Priest of Gix |
| Priest of Urabrask | Pyroblast |
| Ragavan, Nimble Pilferer | Reanimate |
| Reckless Barbarian | Red Elemental Blast |
| Redirect Lightning | Sacrifice |
| Scalding Tarn | Sephiroth, Fabled SOLDIER // Sephiroth, One-Winged Angel |
| Shambling Ghast | Shriekmaw |
| Simian Spirit Guide | Skirk Prospector |
| Skittering Precursor | Sol Ring |
| Songs of the Damned | Spider-Punk |
| Starting Town | Sulfurous Springs |
| Swamp | Talisman of Indulgence |
| Timeline Culler | Treasonous Ogre |
| Umbral Collar Zealot | Underworld Breach |
| Vampiric Tutor | Verdant Catacombs |
| Viscera Seer | Warren Soultrader |
| Wishclaw Talisman | Wooded Foothills |

| Historical reference card | Primitive / parameter result |
|---|---|
| Dualcaster Mage | P0 / Q0 |
| Molten Duplication | P0 / Q0 |
| Saw in Half | P0 / Q0 |
| Cathodion | P0 / Q0 |
| Nine-Lives Familiar | P0 / Q0 |
| The One Ring | P0 / Q0 |
| Blood Artist | P0 / Q0 |
| Mayhem Devil | P0 / Q0 |
| Altar of Dementia | P0 / Q0 |

## 5. Conclusions and remaining scope

The small exact-execution prototype already pilots real recurrence well past
20 iterations without production changes. First-class shortcuts principally
need consent, semantic action binding, resource/cycle certificates, operational
budgets and replay grouping—not a blanket removal of loop guards.

The one demonstrated engine defect is targeted top-library placement. The
Miner 20→100 timing growth deserves profiling. No broader behavior fix, ratchet
change, 100-card natural-game run, exhaustive Nightmare-pair sweep, human
interruption test or host-cap experiment is claimed here. Additional derived
candidates and untested Nightmare pairs remain explicit follow-up simulations rather than invented successful results. Implement L0–L11 in the
companion design; preserve the measured strict reproducer until L0 closes it.
