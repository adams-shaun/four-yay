# Oracle-text card audit — design and pilot

Status: design + pilot (2026-09-27). The harness, 64 pilot scenarios over 25
cards and the ratchet are in `rules/oracle_audit_test.go` and
`rules/testdata/oracle/`. This document states the decisions; the pilot
section records what the pilot measured on `main @ 1ffb080ff`.

## 1. Problem

Card behaviour is compiled from Forge scripts. Almost every card test we
have asserts that the engine does what the **script** says. That catches
engine regressions. It cannot catch a script that is read wrongly or
incompletely, because the test author read the same script. Player reports
keep landing in this gap: a static whose "as long as" gate never switches,
a trigger that never fires from the command zone, a rider silently dropped.

The audit's rule is that **expected outcomes come from the printed card
(Oracle text) and the Comprehensive Rules, never from the script.** A
failure then means the engine plus the script disagrees with the printed
card, whichever side is wrong.

## 2. What exists (survey)

| Asset | What it gives the audit |
|---|---|
| `cards.Face.Oracle` | The IR keeps each face's `Oracle:` line (with `\n` escapes). The printed text is available at run time from `.cards` without reading any script line. |
| `rules` corpus test helpers | Per-file helpers (`moveByName`, `castCorpusSpell`, `addMana`, `driveToStep`, `corpusEngineCfg`, `replayFromLog`/`diffGames`). No shared scenario builder existed; each test hand-rolls setup. |
| `internal/testutil.CorpusRegistry` | Loads the corpus once per process. It SKIPS when `.cards` is absent, so a worktree without the corpus reads green. |
| `rules/acceptance_test.go` `knownUnsupported`, `rules/paramcensus_test.go` `knownUnsupportedParams` | Ratchets on "is every primitive registered / every param read". They measure **coverage, not correctness**. Purphoros's `RemoveType$` has been listed as unread in `paramcensus` all along, yet Purphoros ships in `valgavoth-endless-punishment` and behaves as an always-on 6/5 (§8). |
| `cmd/repro`, `feedback.EngineAt` | Turns a player report into a failing test at the exact board. It is reactive and per report; it does not audit a card. |
| `.ds4/issues`, `feedback/` | 1572 tickets and 125 player reports (classified in §3). |

## 3. Bug classes in the issue history

A background classification of all 1572 tickets and 125 feedback reports
(keyword pass, then a hand check of every player report). Counts are ±10%,
because many tickets span two buckets.

- 1302 of 1572 tickets are card-text work. The pipeline exists to grind card
  primitives, so that share is expected.
- About 40 of the 125 player reports are genuine card-text bugs. The rest are
  UI complaints that happen to name a card.

| Mechanism bucket | Total | From players | Examples |
|---|---:|---:|---|
| Trigger condition / not firing / firing wrongly | 260 | ~7 | Delver "may reveal" never posed; `trig:Attached` unregistered |
| Continuous effect / layers / keyword grants | 148 | 1 | lifelink not gaining; `stat:AssignCombatDamageAsUnblocked` |
| ETB / LTB / zone change | 143 | 10 | Vexing Devil never resolving its choice; ChangeZone ignoring `AttachedTo$` |
| Targeting / filter / restriction | 143 | 6 | "Time Lord" multi-word subtype failing closed |
| Dropped rider / unread param | 99 | 1 | `ActivationPhases$` unread; the Peace Offering census |
| Cost modifiers / alt & additional costs | 98 | 5 | Ashnod's Altar no sac option; K'rrik skipping the 601.2g window |
| Commander-specific | 69 | 7 | **Sidar Jabari** (Eminence), Azusa extra land plays |
| Conditional static ("as long as", `IsPresent$`) | 40 | 0 | `IsPresent$` read inconsistently per consumer |
| Replacement / prevention | 36 | 0 | `repl:Draw`, `repl:CreateToken` |
| Intervening-if / resolution-time condition | 27 | 1 | Blooming Marsh land count |
| Other card text | 239 | 2 | |

The three motivating reports resolved as follows:

- **Fatal Push** ("cast on an enemy, not removed", `fb-20260927T153930Z-a9e16775`):
  replay showed no Fatal Push in that match. The bot aimed Whirler Rogue's
  unblockable grant at an enemy creature, which is a bot-policy bug. The pilot
  still covers revolt (§8); it passes.
- **Sidar Jabari** (`fb-20260927T160557Z-b958ef31`): a confirmed engine bug.
  `Engine.forEachObject` walks `ZLibrary..ZStack` and never `ZCommand`. The
  pilot reproduces it from the Oracle text alone.
- **Relic Vial**: no ticket exists. The pilot found a real Relic Vial bug that
  nobody had reported (§8, look-back).

Recurring roots, from 15 named examples read in full:

- 6 were "primitive missing or unregistered".
- 5 were "primitive exists but its logic is wrong".
- 4 were "param unread or mistranslated on a working primitive".

Only the first kind is reliably visible to today's ratchets (and the unread
param shows up in the census only as a table row, §2). The Oracle audit sees
all three, because it checks behaviour.

## 4. Card selection and priority

1. **Repo-deck cards first.** 1110 distinct cards across
   `internal/testutil/decks/*.json` (measured via `cmd/oraclepacket -decks`).
   These are the cards players actually play. Skip basics and vanilla
   creatures (no Oracle text).
2. **Cards named in feedback reports**, whatever the deck, at the head of
   every batch.
3. **Mechanism families, sampled by Oracle wording.** A family is selected by
   a regexp over Oracle text with `cmd/oraclepacket -grep`, so no script is
   read to choose it. Measured family sizes (repo decks / whole corpus):

   | Family | Oracle selector (abridged) | Decks | Corpus |
   |---|---|---:|---:|
   | conditional-static | `as long as` | 20 | 1361 |
   | ability-word condition | revolt, morbid, metalcraft, threshold, delirium, raid… | 12 | 425 |
   | intervening-if trigger | `(when\|whenever\|at the beginning of) …, if ` | 46 | 1559 |
   | spell conditional / instead | `instead` | 34 | 1219 |
   | cost-modifier | `costs {N} less/more` | 33 | 779 |
   | replacement / prevention | `would … instead`, `prevent` | 21 | 1259 |
   | ETB | `enters` | 296 | 7794 |
   | dies / LTB | `dies`, `leaves the battlefield` | 80 | 1964 |
   | combat restriction | `can't block/attack/be blocked`, `must` | 30 | 2091 |
   | command zone | `command zone`, `eminence` | 2 | 76 |

4. **The rest of the 33k corpus: stratified sampling.** Take N cards per
   family, weighted towards cards that share an exact script construct with a
   card that has already diverged. For example, Purphoros diverged on
   `RemoveType$`, so the other 28 `RemoveType$` carriers (the Theros gods)
   are next. A *selector* may read scripts to find siblings. It emits
   **names only**, and the author still sees only Oracle text.

## 5. Writing expectations without the script

### Who writes them

A mix, split by role:

- **Author: a cheap LLM seat** (the implement tier). It receives only:
  (a) the Oracle packet of the card under audit and of a vetted support-card
  list, (b) the relevant CR excerpts, (c) the scenario schema (§6), and
  (d) one worked example file. It writes one scenario file per card.
- **Reviewer: a stronger seat** (the review tier), with the same inputs and
  still no script. It checks each expectation against the Oracle text and
  the CR, and looks especially for the subtle ones: look-back (603.10a),
  intervening-if (603.4), characteristic changes (613), "you" vs "each
  player". Ambiguous rulings go to the operator.
- **Triage: a third role** (§7). It is the only role allowed to open the
  script, and only after the scenarios are committed.

### Keeping the script out of the author's context

- `cmd/oraclepacket` prints what a player can read on the card (name, cost,
  type line, P/T, Oracle text) plus `oracle_sha`, and nothing else.
  `cards/oracletext` (a subpackage, so `cards.CompilerFingerprint` and every IR cache are untouched) fails its test if the packet ever carries script syntax.
- **The authoring worktree has no `.cards`.** The dispatcher generates the
  batch's packets into a gitignored in-repo path (the pi jail cannot see
  `/tmp`), for example `.oracle-packets/<batch>.txt`, and does not link the
  corpus. There is then no script to read. The engine cannot run either
  (`CorpusRegistry` skips), which is also intended: the author cannot fit
  expectations to engine output. `TestOracleScenarioFilesWellFormed` needs no
  corpus and gives the author schema, vocabulary and ref-syntax feedback.
- **Commit order is the audit trail.** The author's commit contains only
  `rules/testdata/oracle/**`, and it precedes any runner or engine change in
  the branch. The pilot did this: `8234e481a` and `720be0ad0` are the
  scenarios, written before the runner existed.
- Scenarios never contain script tokens. Each one states its reasoning in
  `why` (the author's paraphrase) and `cr` (rule numbers).

## 6. Scenario format and runner

One JSON file per card at `rules/testdata/oracle/<family>/<card-slug>.json`.
The decoder rejects unknown fields.

```json
{
  "card": "Fatal Push", "family": "spell-conditional", "oracle_sha": "294f3bc177dd",
  "author": "...",
  "scenarios": [{
    "name": "revolt-mana-value-3-destroyed",
    "cr": ["608.2c", "207.2c"],
    "why": "A permanent seat 0 controlled left the battlefield this turn, so ...",
    "format": "constructed | commander",
    "setup": {"p0": {"hand": [...], "battlefield": [...], "graveyard": [...], "library": [...],
                     "library_top": [...], "exile": [...], "command": [...], "life": 27},
              "p1": {...}},
    "setup_answers": [{"kind": "any", "pick": ["yes"]}],
    "steps": [
      {"op": "cast", "seat": 0, "card": "p0:Fatal Push", "mana": "B", "targets": ["p0:Savannah Lions"]},
      {"op": "resolve", "expect": [{"card": "p0:Savannah Lions", "zone": "graveyard"}]},
      {"op": "cast", "seat": 0, "card": "p0:Fatal Push#2", "mana": "B", "targets": ["p1:Centaur Courser"]},
      {"op": "resolve"}
    ],
    "expect": [{"card": "p1:Centaur Courser", "zone": "graveyard"}]
  }]
}
```

- **Refs:** `p1:Name` (the first of seat 1's cards with that name), `p1:Name#2`,
  `p0:token:Angel` (seat 0's battlefield token whose name contains "Angel"),
  and players `p0`/`p1`. A card keeps its ref across zones.
- **Ops (a closed set):**
  - `cast` (options: `kicked`, `cast_mode`, inline `mana`, `targets`, `answers`)
  - `activate` (`ability` is a label substring)
  - `play` (land drop)
  - `mana` (adds to the pool; this stands in for mana sources)
  - `resolve` (pass until the stack is empty)
  - `attack` / `block`
  - `pass_to` (`step`, `active`, or a `decision` kind)
  - `move` / `life` (stand-ins for an unspecified outside effect)
- **Decisions:** targets come from `targets`; other asks come from `answers`
  by decision kind. `yes`/`no` are abstract accept/decline and map onto
  whatever the engine poses (yes/no, "unless" modes, …). Anything unanswered
  takes the logged fallback: accept optional triggers, keep offered order,
  otherwise the first `Min` options.
- **Observables:**
  - per card: `zone`, `pt`, `keywords`/`no_keywords`, `types`/`no_types`,
    `tapped`, `damage`, `counters` (`+1/+1`, `void`, …)
  - per player: `life`, `hand_size`, `graveyard_size`, `pool`
  - stack: `stack_size`, `trigger_on_stack` (with `want`)
  - choices: `offered` (is a cast/activation offered, `want`) and `can_block`
    (is this block offered)
  - `count` (by zone, name, token)
- **Runner semantics** (the things authors must know):
  1. Two seats, seat 0 takes turn 1. Libraries are padded to 40 with
     *Wastes* (colourless, subtype-free).
  2. **Setup happens before turn 1 begins**, so seat 0's setup permanents
     are not summoning sick. Consequently their upkeep triggers **do** fire in
     turn 1, and so do enter-the-battlefield triggers of setup placements.
     Cast a card from hand when entering matters.
  3. Opening hands are emptied: a seat's hand is exactly its `hand` list.
  4. Setup life is not "gained this turn".
- **Replay:** every scenario must also replay byte-identically from its log.
  All setup and stand-in ops are logged events (`MoveZone`, `LifeChange`,
  `ManaAdd`, `LibraryOrder`). A replay divergence is its own failure and is
  never ratcheted.

The runner lives in `package rules` as a test file so it reuses the logged
setup seams (`emit`, `priorityRound`) and `replayFromLog`. Beyond setup, it
acts only through `Submit` on offered options, as a client does.

## 7. Triage

Each failing scenario gets one of four verdicts. The triage seat may now
read the script.

| Verdict | How to tell | Action |
|---|---|---|
| **Harness** | The runner cannot express or answer something the Oracle text allows, e.g. a graveyard cast offered as `mode=mayplay`, or a Vexing Devil choice posed as `unless_pay`/`unless_decline`. The error is prefixed `harness:`. | Fix the runner. Expectations are untouched. |
| **Scenario error** | The expectation contradicts Oracle + CR. Example: in the pilot the Sulfuric Vortex expectation ignored its own turn-1 upkeep trigger. | Correct the scenario, with the reviewer's agreement that this is a **rules** error and not a fit to engine output. The `why` records the reason. |
| **Script translation (gorge)** | The script encodes the Oracle text correctly, but gorge ignores or misreads a param. Check `paramcensus`: an unread param there confirms it. Example: `RemoveType$`. | Ratchet row + engine ticket. Search every script carrying the same construct (the selector in §4) and batch the siblings. |
| **Engine primitive** | The script is right and read, but rules machinery is wrong for every card that uses it. Examples: look-back, the command-zone walk. | Ratchet row + engine ticket that names the CR rule. Add a control scenario that localises the fault, as with Relic Vial's SBA vs destroy vs sacrifice split. |
| **Upstream script** (rare) | The Forge script itself misstates the Oracle text. | Cannot be fixed by committing a script (GPL). Report upstream or bump `FORGE_REF`. Ratchet row meanwhile. |

**Ratchet:** `oracleKnownDivergent` in `rules/oracle_audit_test.go` maps
`"<card>/<scenario>"` to the observed divergence, and works in both
directions like `knownUnsupported`:

- A listed scenario that now passes is **stale** and fails the build until its
  row is deleted.
- An unlisted failure fails the build.
- A row naming no scenario fails the build.
- Rows are added only by triage and each names its ticket. The author never
  sees or edits the table.

**Oracle drift:** `oracle_sha` is recorded per file. After a `FORGE_REF`
bump that changes a card's Oracle text (an erratum), the file fails as stale
and must be re-derived, never silently re-judged.

**Coverage number.** Report two figures per family and overall:

- *audited* = cards with a scenario file / cards selected, for example
  "repo decks, non-vanilla: 25 / ~900".
- *Oracle-conformant* = audited cards with no ratchet row. The pilot is at
  21 / 25.

A later `TestEveryRepoDeckCardHasOracleScenarios` can turn "audited" into a
shrinking `knownUnaudited` table, the same shape as the existing ratchets.

## 8. Pilot (main @ 1ffb080ff)

The pilot has 25 cards and 64 scenarios across 8 families. 60 pass. 4 fail
and are ratcheted, covering 3 distinct engine defects. All 64 replay from
their logs.

| Card | Family | Oracle-derived expectation (scenarios) | main |
|---|---|---|---|
| Fatal Push | spell-conditional | MV3 survives without revolt; MV2 destroyed; revolt (own Lions killed first) destroys MV3; revolt still spares MV5 | 4/4 pass |
| Tragic Slip | spell-conditional | no morbid: 3/3 → 2/2; morbid: 4/4 dies | 2/2 pass |
| Electrostatic Bolt | spell-conditional | artifact 4/4 takes 4 and dies; non-artifact 3/3 keeps 2 damage | 2/2 pass |
| Dispatch | spell-conditional | 3 artifacts: exile; 2: only tapped | 2/2 pass |
| Cabal Ritual | spell-conditional | 7 in graveyard → BBBBB; 6 (itself on the stack) → BBB | 2/2 pass |
| Relic Vial | conditional-static | Cleric + death drains; no Cleric no drain; Cleric cast this turn is live at once; **the only Cleric dying looks back (603.10a)** by destroy / by damage / by cost sacrifice | 4/6 pass: **destroy and sacrifice fail**, damage (SBA) passes |
| Righteous Valkyrie | conditional-static + trigger | +2/+2 at 27 not 26; turns on mid-turn; Cleric entering gains its toughness; a Cat does not | 5/5 pass |
| Kitesail Apprentice | conditional-static | unequipped 1/1; equipped 4/2 flying | 2/2 pass |
| Sunspear Shikari | conditional-static | equipped first strike + lifelink, else neither | 2/2 pass |
| Purphoros, God of the Forge | conditional-static | **devotion 1: not a creature**; devotion 5: 6/5; another creature entering deals 2 | 2/3 pass: **low devotion fails** |
| Gravecrawler | conditional-static | castable from the graveyard with a Zombie, not without | 2/2 pass |
| Thalia, Guardian of Thraben | cost-modifier | opponent's Bolt not castable on {R}; castable on {R}{R}, pool empty; creature spell untaxed | 3/3 pass |
| Ruby Medallion | cost-modifier | red 1R on {R}; green not reduced; opponent's Medallion doesn't reduce | 3/3 pass |
| Dauthi Voidwalker | replacement | opponent's creature exiled with a void counter; own cards go to the graveyard | 2/2 pass |
| Sulfuric Vortex | replacement | life gain replaced (after its turn-1 upkeep hit); each upkeep hits the active player | 2/2 pass |
| Geralf's Messenger | etb-ltb | enters tapped, drains 2; undying returns 4/3 with a counter, drains again | 2/2 pass |
| Blooming Marsh | etb-ltb | 2 other lands untapped; 3 tapped; opponent's lands don't count | 3/3 pass |
| Vexing Devil | etb-ltb | opponent takes 4 → Devil sacrificed; declines → 4/3 stays | 2/2 pass |
| Sidar Jabari of Zhalfir | trigger-condition | **Eminence from the command zone on a Knight attack**; no trigger on a non-Knight attack | 1/2 pass: **command-zone trigger fails** |
| Knight of the White Orchid | trigger-condition | fewer lands → fetch Plains; equal → no trigger | 2/2 pass |
| Goblin Bushwhacker | trigger-condition | kicked: team +1/+0 and haste; unkicked: nothing | 2/2 pass |
| Goblin Piledriver | trigger-condition | +2/+0 per other attacking Goblin; non-Goblins and non-attackers don't count | 3/3 pass |
| Resplendent Angel | trigger-condition | gained 6 → Angel token at end step; gained 3 → none | 2/2 pass |
| Delver of Secrets | trigger-condition | reveals an instant → 3/2 flier; a land on top or declining → stays 1/1 | 3/3 pass |
| Steel Leaf Champion | combat-restriction | Grizzly Bears (power 2) can't block it, Centaur Courser (power 3) can | 1/1 pass |

Several player-reported cards now pass: Delver, Vexing Devil, Blooming
Marsh, Goblin Piledriver. Those scenarios now pin the fixes against
regression.

### Defects found

1. **Purphoros (and the whole Theros god cycle) is always a creature.**
   - Script translation: `stat:Continuous RemoveType$` is never read.
     `layers.go` reads `AddType$`, `RemoveCardTypes$` and
     `RemoveCreatureTypes$` only.
   - `paramcensus` already lists the param as unread for Purphoros and Mogis.
     The coverage ratchet knew; the card still ships in a repo deck.
   - 29 corpus scripts carry `RemoveType$`.
   - Observed: devotion 1 gives a 6/5 indestructible creature. Expected: not
     a creature.
2. **No look-back for a granted leaves-the-battlefield trigger outside the SBA
   path (CR 603.10a).**
   - Relic Vial's `IsPresent$ Cleric.YouCtrl` grant of "whenever a creature
     you control dies" does not fire when the only Cleric is destroyed by a
     resolving spell, or sacrificed as a cost (including Relic Vial's own
     cost).
   - It does fire when the Cleric dies to damage, where the SBA path uses the
     pre-batch trigger snapshot.
   - Observed: life 20/20. Expected: 21/19.
   - Suspected cause: effect-driven and cost departures evaluate trigger
     eligibility on the post-move board, so the grant has already switched
     off.
   - Likely affects every `AddTrigger$` grant whose gate the departing
     permanent itself satisfies.
3. **Command-zone triggers never fire.** This is Sidar Jabari's Eminence,
   already ticketed as `fb-20260927T160557Z-b958ef31`, root-caused to
   `Engine.forEachObject` skipping `ZCommand`. The pilot reproduced it from
   the Oracle text alone. The same shape affects Edgar Markov, Inalla and
   Sahir.

### Triage record (pilot)

The pilot also produced two harness fixes and one scenario error, which
calibrate the triage verdicts:

- **Harness fix:** Gravecrawler's graveyard cast is offered as `mode=mayplay`.
- **Harness fix:** Vexing Devil poses `unless_pay`/`unless_decline`, not
  yes/no.
- **Scenario error:** Sulfuric Vortex. Setup precedes turn 1, so the Vortex
  correctly hits seat 0 in its own upkeep. My expectation missed that.
- Two Relic Vial control scenarios were added during triage, with CR-only
  expectations, to localise defect 2.

## 9. Licensing (flag for the operator)

- **Forge scripts: never committed.** Unchanged. Scenario files name cards
  and carry no script token. The runner reads the corpus at run time.
- **Oracle text: recommendation is not to commit it in bulk.** Oracle text is
  Wizards of the Coast's text, not Forge's GPL work, so committing it does not
  create a GPL problem. It is still third-party copyrighted text, and gorge
  is Apache-2.0. We gain nothing by vendoring it, because the corpus already
  supplies it at run time.
- **Scenarios therefore reference cards by name plus `oracle_sha`**, and
  `why` holds the author's paraphrase.
- A few `why` fields quote a short clause (e.g. "Red spells YOU cast") to pin
  the rules point. I believe that is fine as fair-use-scale quotation, but
  **the operator should confirm**. If not, the schema rule becomes
  "paraphrase only", and the reviewer checks it.
- Oracle packets written for seats are corpus-derived and go to a gitignored
  path.

## 10. Scale and rollout

**Ticket shape (one cheap-seat ticket = one family × 8–10 cards):**

- *Inputs, pre-generated by the dispatcher into the ticket's worktree, which
  has no `.cards`:* the Oracle packets of the batch plus the support-card
  list, the CR excerpts for the family, this document's §6, and one example
  file from the same family.
- *Output:* `rules/testdata/oracle/<family>/*.json` only, 2–4 scenarios per
  card, each covering the condition true AND false. Plus a green
  `go test ./rules -run TestOracleScenarioFilesWellFormed`.
- *Stop rules:* do not create or edit any `.go` file; do not read `.cards`
  (it is absent); a card the schema cannot express is listed in the report,
  not approximated.
- *Review (strong seat, no script):* expectation-vs-Oracle/CR check.
- *Gate / triage (strong seat with `.cards`):* run `make oracle-audit`, then
  classify every failure per §7. Harness gaps go to one follow-up ticket per
  gap. Engine and script failures get ratchet rows plus engine tickets. The
  gate merges when the suite is green with the ratchet.

**Batches and estimates:**

| Wave | Scope | Cards | Tickets (≈9 cards) |
|---|---|---:|---:|
| 0 | this pilot | 25 | — |
| 1 | repo-deck cards in the conditional-static, ability-word, intervening-if, cost-modifier, replacement, command-zone families (§4 table, deduplicated) | ~150 | ~17 |
| 2 | repo-deck ETB / dies / combat-restriction | ~400 | ~45 |
| 3 | remaining non-vanilla repo-deck cards | ~350 | ~40 |
| 4 | corpus siblings of every diverged construct (e.g. the 28 other `RemoveType$` cards, all `AddTrigger$`+`IsPresent$` grants) | ~300 | ~35 |
| 5 | stratified corpus sample, ~50 per family | ~500 | ~55 |

Pilot rates for calibration:

- Divergences: 4 of 64 scenarios (6%), 3 distinct defects in 25 cards. Every
  defect sits in a family the ratchets call supported.
- Harness gaps: two small ones in 25 cards.
- Expect waves 1–2 to need a handful of new ops: planeswalker loyalty
  activations, modal choices by label, multiple defenders, blockers with
  damage assignment, and "at end of turn" observations. Each is one harness
  ticket. The runner is ~1200 lines and each op is ~30.

## 11. Commands

```sh
make oracle-audit                              # all scenarios, -v
make oracle-audit ORACLE_RUN='Relic_Vial'      # one card
go test ./rules -run TestOracleScenarioFilesWellFormed   # corpus-free schema check
ORACLE_AUDIT_TRACE=1 go test ./rules -run TestOracleAudit -v   # transcripts for passes too
go run ./cmd/oraclepacket "Fatal Push"         # the author's view of a card
go run ./cmd/oraclepacket -decks internal/testutil/decks -grep '(?i)as long as' -names
```
