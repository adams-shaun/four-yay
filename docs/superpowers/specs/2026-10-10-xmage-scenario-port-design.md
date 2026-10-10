# XMage interaction-scenario port

Status: design and prototype (branch `wt/rs-xmagetests`). Author: rs-xmagetests
seat, 2026-10-10.

## Why

Level B (`compliance/levelb/`) checks one card's mechanism in isolation, and the
oracle audit (`rules/testdata/oracle/`) checks one card against its printed
text. Neither one exercises the cross-card rules: CR 613 layer and dependency
order, competing replacement effects (616), copy (707), simultaneous events
and SBA (704), and multi-step combat. XMage's `Mage.Tests` and Forge's
`GameSimulationTest` contain thousands of hand-written scenarios that cover
exactly these. Porting them gives gorge a third, independent source of
expected behaviour.

## Survey

### XMage `Mage.Tests`

The checkout is the compliance oracle's: `/mnt/sata/gorge-training/xmageoracle/mage`
at `6b602a1c8`. It is MIT-licensed. There are 2056 Java files and **6967 `@Test`
methods**. The census script output is in
`/mnt/sata/gorge-training/xmagetests/census.txt`.

| directory | tests | rules area |
|---|---:|---|
| `cards/single` | 2443 | per-card regressions (often interactions) |
| `cards/abilities` | 1488 | keywords, activated/triggered ability mechanics |
| `cards/triggers` | 368 | trigger timing and ordering (CR 603) |
| `cards/cost` | 301 | cost modification and alternative costs (601.2f, 118) |
| `cards/continuous` | 281 | **layers (CR 613)**, including `LayerTests` and `DependentEffectsTest` |
| `cards/replacement` | 222 | **replacement and prevention (614–616)** |
| `cards/copy` | 187 | **copy (707)**: Clone, spell copies, token copies |
| `cards/mana` | 163 | mana abilities and restrictions |
| `combat` | 101 | first strike, damage assignment, block restrictions |
| `cards/rules` | 71 | legend rule, attachments, can't-X rules |
| `rollback` | 7 | XMage's own rollback (no gorge analogue, out of scope) |
| `sba`, `lki` | 2 | SBA and LKI regressions |
| multiplayer, commander, AI, serverside, … | ~600 | mostly out of scope |

The test DSL lives in `CardTestPlayerBase`. Ranked by call frequency across the
corpus:

| call | count | call | count |
|---|---:|---|---:|
| `addCard` | 25065 | `assertPowerToughness` | 1433 |
| `castSpell` | 6886 | `assertHandCount` | 1280 |
| `setStopAt` | 6503 | `checkPlayableAbility` | 1027 |
| `assertPermanentCount` | 5001 | `assertExileCount` | 840 |
| `setChoice` | 4146 | `assertCounterCount` | 803 |
| `assertLife` | 3324 | `block` | 411 |
| `assertGraveyardCount` | 3229 | `activateManaAbility` | 345 |
| `activateAbility` | 2076 | `setModeChoice` | 245 |
| `addTarget` | 2031 | `runCode` | 215 |
| `attack` | 1553 | `addCustomCardWithAbility` | 123 |

### Forge

The checkout is the Forge oracle's: `/mnt/sata/gorge-training/forgeoracle/forge`
at `e27d2bdb2`. Forge is GPL-3.0.

- `forge-gui-desktop/src/test`: 60 files and 232 tests. The rules-relevant
  ones are `ai/simulation/GameSimulationTest.java`, with **78 tests**: layer and
  control dependency, Kalitas vs mass removal, Teysa, LKI, Necrotic Ooze,
  Volrath's Shapeshifter, and others. They build state with
  `addCard`/`addCardToZone` on a live `Game` and assert with JUnit. The test
  *logic* is GPL. The facts in it (card names, expected counts) can be
  re-derived, but transcribing whole tests is a derivative-work question.
  Treat them as a **reading list**: re-author each one from Oracle text and
  the CR, as the oracle audit does. Do not translate them mechanically.
- `forge-gui/res/puzzle/*.pzl`: **375 puzzles** (282 Possibility Storm `PS_*`,
  49 Perplexing Chimera `PC_*`, and others). They are present in the git tree
  but sparse-excluded from the checkout (`git show HEAD:forge-gui/res/puzzle/…`
  reads them). A puzzle is an INI file. `[metadata]` holds the goal ("Win this
  turn") and turns. `[state]` holds `humanlife`/`ailife` and per-zone card
  lists (`Name|Set:X|Tapped|Id:N|AttachedTo:N|Counters:…`).
  - **Loading one into gorge**: `[state]` maps almost one-to-one onto
    `oracleScenario.setup`:

    | `.pzl` field | gorge `setup` field |
    |---|---|
    | `humanbattlefield` | `p0.battlefield` |
    | `Tapped` | `tapped` |
    | `Counters` | `counters` |
    | `humanlife` | `life` |

    Two gaps: `AttachedTo:` needs a new `attached` setup field (today an
    aura can only be put on a permanent through an `op: move` with
    `attached_to`), and so does `|Transformed`/`|FaceDown`
    (`back_face` exists).
  - **What a puzzle tests**: a puzzle states a goal, not a script. On its own
    it tests a *solver*, not the rules. It becomes a rules test only when it
    is paired with its published solution line, which would then be scripted
    as `steps` with a final `life`/win expectation. Possibility Storm
    publishes solutions separately. They are not in the repo.
  - **Licence**: `.pzl` files ship in GPL Forge. Keep them, and anything
    generated from them, under a gitignored path, as with `.cards/`. The
    natural home is a loader that reads `.cards`-style from `git show` at a
    pinned SHA, not a committed copy.

  Verdict: puzzles are a good input later for *bot* evaluation ("can the
  search find the win") and a poor first source for rules confidence. Start
  with XMage.

## The harness to reuse

gorge already has the target format. The oracle-audit scenario schema
(`rules/oracle_run.go`, `oracleScenario`) is a declarative two-seat script:

- `setup`, per seat: hand, battlefield, tapped, graveyard, library, exile,
  counters and life.
- `steps`, from a closed vocabulary: `mana`, `cast`, `activate`, `play`,
  `resolve`, `attack`, `block`, `pass`, `pass_to`, `move`, `life`. Each step
  carries `targets`, scripted `answers` (`kind` plus `pick`), and inline
  `expect`.
- `expect`: P/T, types, keywords, counters, tapped, damage, life, zone counts
  by name, hand/graveyard sizes, stack size, offered actions,
  can-attack/can-block.

`runOracleScenario(reg, sc)` drives it only through player actions on a real
engine and replays it from the log. The XMage oracle driver
(`compliance/oraclegen`, `xmageoracle/driver`) already executes **the same
JSON** inside XMage. So a ported scenario can be cross-checked mechanically
against XMage itself, which removes the porter's transcription errors as a
source of false bugs.

The prototype adds:

- `rules/xmageport_test.go` (`TestXMagePortScenarios`). It loads
  `rules/testdata/xmageport/<family>/*.json`. Each file holds `source` plus
  scenarios. Each scenario is the `oracleScenario` schema embedded unchanged,
  plus `xmage` (the `Class#method` it came from) and an optional
  `known_bug`. A `known_bug` scenario is skipped with its reason. If it
  starts passing, the test fails ("stale row"), so the list only shrinks.
- `rules/testdata/xmageport/NOTICE`: XMage's MIT licence and the source
  commit.

This is a separate directory and a separate test. The oracle audit's
per-card ratchets and its "author never read the script" contract stay
untouched.

## Translator design (XMage Java → scenario JSON)

The tool is `cmd/xmageport` (pure Go, no dependencies). Input is a Java test
file or directory. Output is one JSON file per test class under
`rules/testdata/xmageport/<family>/`, plus a per-test **reason** for every
method it refuses.

### Parsing

XMage tests are highly regular. Each `@Test` method body is a flat sequence of
DSL statements. There is no need for a full Java parser. A tokenizer is
enough: it splits statements at `;` at brace depth 0 and parses call
arguments as literals or identifiers. Class-level `private static final
String x = "…"` constants are resolved first (`FirstStrikeTest.knight`).
The refusal cases are any `for`/`if`/lambda/`IntStream`, any
`getPermanent(...)` read followed by a JUnit assert, `runCode`,
`addCustomCardWithAbility`, and AI calls. They give **hard** or
**unknown-helper** classifications below.

### Mapping

| XMage | gorge scenario |
|---|---|
| `addCard(Zone.Z, playerA, "N", k)` | `setup.p0.<z>`: N repeated k times. **Lands** are dropped when they exist only to pay for spells. They are kept when their *own* presence is asserted or affected (a land named in an assertion or in a type-changing effect's filter). Mana is supplied per `cast` instead (below). |
| `castSpell(t, step, pX, "N"[, target])` | `pass_to` (t, step) if the clock is not already there, then `cast` with `mana` set to the card's mana cost (read from the corpus at generation time), plus `targets`. |
| `castSpell(..., true)` / `waitStackResolved` | `resolve` |
| `activateAbility(t, step, pX, "text"[, target])` | `activate`, with `ability_index` chosen by matching the XMage rule-text prefix against `Engine.Pending()` option labels at generation time (gorge labels do not carry the cost text: `"+1"` did not match Liliana's label in the prototype). |
| `playLand` | `play` |
| `attack(t, pX, "N"[, defender])` / `block(t, pY, "B", "A")` | `pass_to`, then `attack` / `block` |
| `setChoice(pX, "N" \| true \| false)` | an `answers` entry `{kind:any, pick:[N/yes/no]}` on the step that poses it. XMage queues choices globally, while gorge binds them to a step, so the translator attaches each choice to the next step that consumes a decision. The runner's leftover-answer check catches misplacement. |
| `addTarget(pX, "N")` | `targets` on the preceding cast/activate when it has none, otherwise an `answers` `{kind:target}` |
| `setModeChoice(pX, "1")` | `answers` `{kind:modes}` |
| `setStopAt(t, step)` + `execute()` | a final `pass_to` |
| `setLife(pX, n)` | `setup.pX.life` |
| `assertLife` | `expect.life` |
| `assertPermanentCount(pX, "N", n)` | `expect.count` {battlefield}. A token named `"X Token"` maps to `X`. |
| `assertGraveyardCount` / `assertExileCount` / `assertHandCount(p, n)` | `count` {graveyard/exile} / `hand_size` |
| `assertPowerToughness(pX, "N", p, t)` | `expect.pt` |
| `assertCounterCount("N", CounterType.C, n)` | `expect.counters` |
| `assertType` / `assertSubtype` / `assertNotSubtype` | `types` / `no_types` |
| `assertAbility(pX, "N", Ability, bool)` | `keywords` / `no_keywords`, for keyword abilities only |
| `assertTapped` | `tapped` |
| `check*` (mid-test) | an inline `expect` on the step at that clock point |
| `setStrictChooseMode(true)` | the default: gorge's runner is always strict about unconsumed answers |

### Semantic gaps the translator must handle, or refuse

1. **Clock.** XMage schedules each action by (turn, step). The oracle runner
   is sequential. The translator walks a monotone clock and inserts
   `pass_to {step, active}` between actions. Scenarios that run past turn 2
   need `turn` and library fillers, which the runner already provides.
2. **Mana.** XMage pays from lands on the battlefield. Generated `mana`
   strings use `C` for generic mana and the colour letters, and hybrid
   costs pick the first colour. A test whose subject is mana payment
   (`cards/mana`, `cards/cost`, the 345 `activateManaAbility` uses) is refused
   for now.
3. **Players.** The runner is two-seat. The `multiplayer` and `commander`
   tests (about 4%) are refused until the runner grows N seats.
4. **Choice addressing.** gorge picks are labels (`p1:Craw Wurm`, `yes`,
   `Advisor`). XMage names cards bare and refers to triggers by rule-text
   prefix ("When {this} enters"). The translator resolves both against the
   pending decision at **generation time**: it runs gorge once and records
   the label. That is the existing oraclegen practice, and it does not bias
   the verdict, because the expectation always comes from the XMage asserts.
5. **Disputed expectations.** XMage is not the CR. When an assert contradicts
   the CR or a ruling (for example `testMycosynthLatticeAndMarchOfTheMachinesAndHumility`
   asserts that Humility dies), the porter drops that one assertion and says
   so in `why`. The XMage driver replay of the same JSON tells the two cases
   apart: XMage-passes and gorge-fails means a gorge suspect; both fail means
   a translation error.
6. **Ignored tests.** `@Ignore` methods are skipped and listed.

### Feasibility

A syntactic census (`/mnt/sata/gorge-training/xmagetests/census.py`, to be
re-implemented in the Go tool) puts every method in one of four classes:

| class | tests | share |
|---|---:|---:|
| portable-direct: only setup, action and assert calls in the mapping table | 2461 | 35% |
| portable-with-answers: also `setChoice`/`addTarget`/`setModeChoice` | 2239 | 32% |
| unknown-helper: Java asserts over `getPermanent`, loops, local helpers | 1124 | 16% |
| hard: `runCode`, custom cards, AI, dice/coin, mana-option asserts | 1143 | 16% |

In the rules-interaction directories:

| directory | portable (direct + answers) |
|---|---|
| `continuous` | 165/281 (59%) |
| `replacement` | 172/222 (77%) |
| `copy` | 147/187 (79%) |
| `triggers` | 320/368 (87%) |
| `combat` | 74/101 (73%) |

**Estimate: about 65% syntactically portable, and about 45–50% mechanically
portable end to end** once the mana-payment, multiplayer and choice-addressing
refusals are applied. That estimate is unmeasured: it is the prototype's
hand-port experience (21 of 21 attempted ports expressible, 2 needing a
manual fix: an ability index and an ordinal `#2` ref) extrapolated over the
census. Treat it as a hypothesis for the tool's first run to measure.

## Prototype results

21 scenarios in 5 files (layers 7, replacement 7, copy 2, combat 5), all ported
by hand. **13 pass. 8 are `known_bug` skips covering 5 distinct root causes.**
Three of the eight are *control* scenarios added to isolate a cause.

| # | root cause | scenarios | location |
|---|---|---|---|
| B1 | `SetPower$`/`SetToughness$ AffectedX` is evaluated against the static's **source**, not the affected object. Opalescence and March of the Machines give every creature their own MV (4). | opalescence-enchanted-evening, control-opalescence-alone, (conspiracy, lattice) | `rules/chars/derive.go:127`, `:133` (the SubModify arm at `:155-165` does honour `AddPowerAffected`) |
| B2 | No CR 613.8 dependency ordering **within layer 4**. Type effects apply in pure timestamp order. | blood-moon-urborg, humility-march-lattice, conspiracy-opalescence-evening | `rules/chars/types.go:118` (the walk). `abilityDependencyOrder` (`derive.go:570`) covers layer 6 only. |
| B3 | DestroyAll moves victims one by one in seat order. Kalitas (seat 0) leaves before the opponent's creatures, so its move replacement no longer applies (CR 614 and simultaneity). The control with Kalitas surviving passes. Forge has the same case: `GameSimulationTest#testMassRemovalVsKalitas`. | kalitas-damnation | `effects/zone_bulk.go:155-170` |
| B4 | Prevention-shield riders: only `ShieldEffectTarget$ ParentTarget` and a `DealDamage` rider are implemented. Test of Faith (`Targeted`, PutCounter) prevents the damage but never adds counters. A loud Note is emitted. | doubling-season-test-of-faith, control-test-of-faith-alone | `effects/prevent_damage.go:96`, `rules/replacement_damage.go:212` |
| B5 | Phantasmal Image's Clone with `AddTypes$`/`AddTriggers$`/`AddSVars$` falls outside the ETB-copy whitelist, so it takes the loud `unimplemented API Clone` fallback. The Image enters as a 0/0 and dies. | phantasmal-image-copies-etb-trigger | `effects/clone.go:63-75` |

B1 and B2 are **silent**: the game plays on with wrong characteristics and no
Note. These are exactly the class the per-card Level B tests cannot see,
because each card looks right alone. B4 and B5 are loud, but both cards count
as "supported" in the coverage report.

## Next tickets

1. **B1 fix**: anchor `AffectedX` in the layer-7b set arm (small; check
   whether it moves `TestHeads`).
2. **B2**: CR 613.8 dependency order for layer 4 (and layer 5 for
   colour), reusing `abilityDependencyOrder`'s simulation approach. Larger.
3. **B3**: simultaneous move replacement in DestroyAll, and the same audit for
   `effDestroy` multi-target, `SacrificeAll` and `ChangeZoneAll`. Snapshot the
   active replacements before the batch.
4. **B4 and B5**: coverage gaps. Feed them to the coverage ratchet as unread
   params: Test of Faith's rider form and Phantasmal Image's clone riders
   are both corpus classes, so census them first.
5. **`cmd/xmageport`**: implement the translator above. Generate for
   `continuous`, `replacement`, `copy`, `triggers` and `combat`. Commit only
   scenarios that either pass or carry a triaged `known_bug`. Leave
   untriaged failures in a gitignored drop directory.
6. **Cross-check lane**: run each generated JSON through the XMage oracle
   driver to separate translation errors from gorge bugs before triage.
7. **Runner extensions** the port will need: an N-seat setup, `attached`
   setup, and an `ability_text` step selector that matches the corpus
   ability description.
