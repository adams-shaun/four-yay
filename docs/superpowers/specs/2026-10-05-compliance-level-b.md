# XMage compliance level B — design (2026-10-05)

Status: design. Base `main@e24cf157c`. Extends
[`2026-10-02-xmage-compliance-oracle-design.md`](2026-10-02-xmage-compliance-oracle-design.md)
(§1 defines level B, rollout ticket X9) and section 11.3 C6 of
[`2026-10-03-rules-engine-lasagna-design.md`](2026-10-03-rules-engine-lasagna-design.md).
Every `path:line` below was read at the base SHA. Anything not read from code
is marked **H** (hypothesis) and is re-measured by the ticket that owns it.

## 0. Where level A stands (read from code)

- `gate.LevelMeaning("B")` is a stub: "level A plus activated abilities, trigger
  modes, attacks/blocks and statics (no templates yet)"
  (`compliance/gate/gate.go:34-35`), and `gate.Check` returns one `*` problem
  for any level other than A (`compliance/gate/gate.go:138-140`).
- `templates.Generate` makes exactly ONE item per card from `Faces[0]`:
  play-land, counter-spell or cast-resolve (`compliance/oraclegen/templates/generate.go:29-50`).
  `templates.All` lists those three (`generate.go:29`).
- `oraclegen.NewItem` hard-codes `CR 601.2` and `Why "generated level-A
  scenario"` into the scenario bytes (`compliance/oraclegen/gen.go:404-409`);
  the verdict's `ScenarioSHA` is the sha of the whole item
  (`compliance/gate/gate.go:52-56`). Changing those bytes stales every level-A row.
- Verdict rows are keyed `card -> template` (`compliance/verdicts.go:108-111`),
  so a card can hold any number of rows as long as each has its own template
  string.
- The generator's `Step` carries `op/seat/card/mana/targets/attackers/defender/
  blocks/step/decision/answers/to` (`compliance/oraclegen/gen.go:42-64`) — no
  `ability`, `active` or `amount`.
- gorge's runner (`rules/oracle_run.go`) already implements `mana`, `cast`,
  `activate`, `play`, `resolve`, `attack`, `block`, `pass`, `pass_to`, `move`,
  `life` (`rules/oracle_run.go:1569-1596`, `do` at `:880-1198`). `activate`
  selects an option by label substring (`:946`); `pass_to` takes `step` and
  `active` (`:1154-1179`). It decodes with `DisallowUnknownFields`
  (`rules/oracle_export.go:25-33`), so a driver-only field cannot ride inside
  a step.
- `decision.Option.Ability` is the index into the source `Face().Abilities`
  (`decision/decision.go:351-358`); the battlefield walk sets it
  (`rules/legal_walk_battlefield.go:441-447`).
- `(*oracleRun).do` sits exactly at its codeshape ceiling, 319 lines
  (`internal/codeshape/ratchet_test.go:342`). Any runner change must keep it
  at or under 319 by moving logic into helpers.
- The XMage driver's `step` switch handles `mana, attack, pass_to, block, cast,
  play, resolve` and throws on anything else
  (`tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java:592-739`).
  Every step runs at the one constant `TURN = 1` (`:65`, `:343`); `pass_to`
  maps only declare-attackers / declare-blockers / main1 / main2 (`:612-630`).
  The snapshot emits `attacking`/`blocking` (`:992-993`) but no keywords.
- The comparator renders each permanent as controller, owner, name, types,
  colours, P/T, tapped, face-down, damage, counters, attachment, attacking,
  blocking (`compliance/oraclediff/compare.go:345-393`). **Keywords are not
  compared**, although gorge's snapshot carries them
  (`rules/oracle_snapshot.go:59`).
- A frozen expectation keeps only the fields that CHANGE after the setup
  checkpoint (`compliance/oraclediff/freeze.go:56-87`). An effect already
  present at setup is compared once (Compare walks every checkpoint,
  `compare.go:98-104`) but never frozen, so the gate could not hold gorge to it.
- `RatchetEntry` is `{level, outstanding}` at level A only
  (`compliance/adopt/status.go:333-339`); `oraclediff status -all` returns
  before the ratchet when `level != "A"` (`cmd/oraclediff/statusall.go:101`).
  `rule`, `triage` and `refreeze` find a card's item through
  `templates.Generate` (`cmd/oraclediff/main.go:582`, `cmd/oraclediff/triage.go:55`,
  `cmd/oraclediff/refreeze.go:44`), i.e. they can address only the level-A row.
  A shape key includes the raw template string (`compliance/shape/shape.go:44-67`).

## 1. What a card needs for level B

Level B ⊇ level A: a card first passes every level-A check unchanged. Basic
lands, cards XMage lacks, and cards covered by a hand-authored oracle scenario
are treated exactly as at level A (`gate.go:185-213`): a hand scenario covers
the card wholesale at level B too (a deliberate, tightenable choice).

Every other card has a list of **requirements**, derived from its IR, over
every face. A requirement key is `<family>#<face>.<slot>`:

| family | one requirement per | slot | covered by A when |
|---|---|---|---|
| `activate` | `SA` with `Kind == "AB"` (mana abilities included) | index into `Face.Abilities` (the runner's `ability_index`) | never |
| `trigger` | entry of `Face.Triggers` | index into `Face.Triggers` | face 0, non-land, `Mode == ChangesZone`, `Destination$ Battlefield`, `ValidCard$` names `Card.Self`: cast-resolve settles the stack (`gen.go:466-504`), so the self-ETB trigger already resolved inside the agreeing level-A scenario |
| `static` | entry of `Face.Statics` | index into `Face.Statics` | never |
| `combat` | creature face whose keywords include a combat keyword (Flying, Reach, First/Double Strike, Deathtouch, Trample, Lifelink, Menace, Vigilance, Indestructible, Defender, Skulk, Fear, Intimidate, Shadow, Horsemanship, Flanking, Bushido, Afflict, Annihilator, Battle cry, Melee, Rampage, Provoke, Toxic, Infect, Wither) or a combat-legality static | `attack`, `block` (two requirements) | never |

Each requirement is classified into a **sub-family** that names the template
that serves it, or a **gap** that names why none does. The classifier is a
pure function of the IR (no gorge run), so the requirement set and therefore
the level-B outstanding count do not move when a template lands; only
verdicts move it.

| sub-family | rule | v1 template |
|---|---|---|
| `activate.battlefield` | `ActivationZone$` absent or `Battlefield`, API ≠ `Mana` | activate (§2.1) |
| `activate.mana` | API `Mana`, battlefield | activate (§2.1) |
| `activate.hand` | `ActivationZone$ Hand` (channel-style, cycling): the card starts in p0's hand | activate (§2.1) |
| `activate.graveyard` | `ActivationZone$ Graveyard`: the card starts in p0's graveyard | activate (§2.1) |
| `activate.zone:<Z>` | any other `ActivationZone$` | gap |
| `trigger.<recipe>` | mode in the recipe table (§2.2) | trigger |
| `trigger.gap:<Mode>` | any other mode or a parameter shape the recipe refuses | gap |
| `static.continuous` | `Mode == Continuous` | static (§2.3) |
| `static.cost` | `ReduceCost`/`RaiseCost` | gap (v2: cast a probe at the modified price) |
| `static.combat` | CantAttack, CantBlock, CantBlockBy, MustAttack, MustBlock, MinMaxBlocker, CanAttackDefender, CombatDamageToughness, CombatDamageNegatePower | gap (legality needs an "offered" observation, §6) |
| `static.gap:<Mode>` | anything else | gap |
| `combat.attack`, `combat.block` | §2.4 | combat |
| `*.face:<n>` | face > 0 | gap in v1 (templates serve `Faces[0]`, as level A does) |

**Measured** with a throwaway census at `e24cf157c` (this classification,
printed lists of the four level-A sets; ticket L1 re-measures and pins it):

| set | printed | cards with ≥1 req | activate (mana) | trigger covered-by-A / recipe / gap | static cont / cost / combat / gap | combat creatures |
|---|---|---|---|---|---|---|
| BIG | 30 | 28 | 15 (4) | 12 / 14 / 3 | 3 / 0 / 0 / 3 | 4 |
| EOE | 266 | 191 | 39 (28) | 75 / 78 / 17 | 64 / 9 / 4 / 3 | 49 |
| FDN | 517 | 355 | 93 (58) | 80 / 179 / 9 | 75 / 12 / 10 / 5 | 127 |
| FRA | 285 | 206 | 92 (32) | 65 / 82 / 23 | 39 / 7 / 5 / 5 | 63 |

(The activate column counts non-mana AB; FRA has 9 hand and 9 graveyard
activation zones, BIG 2 hand. The census also counted basic lands' mana
abilities, which the gate skips.) So FRA needs roughly 400 level-B scenarios
against 285 level-A ones (**H**: the exact figure is L1's census).

**Agree** at level B means what it means at level A, per requirement: the row
for that requirement's template string has status `agree` or `xmage_wrong`, is
for the scenario the generator makes now (`ScenarioSHA`), is not a sampled
automatic ruling still pending review, and gorge still meets its frozen
expectation (`gate.go:219-242`, `plan.go:76-94`). A requirement whose scenario
cannot be generated is outstanding as a template gap.

## 2. Templates

All level-B templates live in new files under
`compliance/oraclegen/templates/` and are dispatched from one new function,
`GenerateB(reg, name, req) (Item, *Skip)`, in a new `generate_b.go`; level-A
`Generate` is untouched. Each has its own `Template{ID, Version}`; the item's
template string is the requirement key (`activate#0.2`), so verdict rows stay
`card -> template` and versioning stays per family.

Level-B items are built by a new `oraclegen.NewLevelBItem(card, key, version,
cr, sc)` that writes `Why "generated level-B scenario"` and the family's CR
(602.2 activate, 603.2 trigger, 611.3 static, 506-510 combat). `NewItem` is not
touched, so no level-A byte, sha or verdict moves.

Every level-B template follows one rule forced by `freeze.go:56-87`: **the
behaviour under test must happen after the setup checkpoint**, so the frozen
expectation captures it.

### 2.1 activate

Setup: the card on p0's battlefield (untapped; setup permanents can attack and
tap — level-A combat fixtures already rely on this, `candidates.go:250-270`),
plus the target fixture the ability's chain needs (a new
`oraclegen.AbilitySlotSpecs(face, sa)` beside `SlotSpecs`, `candidates.go:81`,
feeding the existing `Fixtures`). Steps: `activate` with `ability_index`
(the requirement's slot) and `mana` = `PoolFor` of the cost's mana part; then
`resolve` for a non-mana ability (a mana ability does not use the stack).
Targets and XMage answers are rewritten from gorge's decisions exactly as
cast-resolve does (`ChooseTargets`, `XAnswersForScenario`, `cast.go:151-176`).

v1 cost tokens: mana, `T`, `Q`, `PayLife<n>`, `Sac<1/CARDNAME>`, a loyalty
`AddCounter`/`SubCounter` of the source. Any other cost token is a
`Skip{"activate cost gap: <token>"}`.

The XMage driver needs the ability's text. The item carries it outside the
scenario (the runner refuses unknown step fields) as `xmage_ability`, a slice
parallel to `steps` like `xmage_answers` (`gen.go:91-108`). **H1**: XMage's
`TestPlayer` selects an activated ability by a prefix of its rule text
(`activateAbility(turn, step, player, "{2}, {T}")`) and a mana ability through
`activateManaAbility`; the generator derives the prefix from `Face.Oracle`: the
k-th oracle line containing `": "` maps to the k-th non-keyword AB in IR
order, and a keyword-expanded AB (Equip, Cycling, Station, …) maps to its
keyword line ("Equip {2}"). When the counts differ, or two abilities share a
prefix, the template skips with `activate xmage text ambiguous`.

### 2.2 trigger

Setup: the card on p0's battlefield plus the recipe's probe; steps: the
recipe's cause, then `resolve`. Recipes are a table keyed by `(Mode, key
params)`; v1 uses only p0 actions, because p1 can act in turn 1 only after p0
passes, which a generated scenario does not script:

| recipe | applies to | cause steps | driver ops |
|---|---|---|---|
| `etb-other` | ChangesZone → Battlefield, ValidCard not Self | cast a probe creature (Grizzly Bears) | cast, resolve |
| `dies` | ChangesZone Battlefield → Graveyard, ValidCard Self | cast a destroy probe targeting the card | cast, resolve |
| `attacks` | Attacks / AttackersDeclared naming the card or its controller | `attack` the card at p1 | attack, resolve |
| `combat-damage` | DamageDone, ValidSource Self, combat | `attack`, `pass_to main2` | attack, pass_to |
| `spell-cast` | SpellCast by You | cast a probe spell matching `ValidCard$` (Shock at p1 for instant/sorcery, Grizzly Bears for creature) | cast |
| `becomes-target` | BecomesTarget, ValidTarget Self | cast Giant Growth on the card (creature only) | cast |
| `life-gained` | LifeGained, You | cast a lifegain probe | cast |
| `drawn` | Drawn, You | cast a draw probe | cast |
| `phase` | Phase (End of Turn, BeginCombat on your turn; Upkeep/Draw on your next turn) | `pass_to` that step (`active p0` for the next turn) | pass_to (new steps, turn advance) |

**H4**: the probe cards (destroy, lifegain, draw) exist in both corpora and
replay cleanly in XMage; the ticket picks them by a gorge `PlaysThrough` test
and the host replay confirms them. Opponent-caused events, LTB to other zones,
Taps, Sacrificed, CounterAdded, Scry/Surveil, Discarded, DamageDoneOnce and the
rest stay `trigger.gap:<Mode>`.

### 2.3 static

The static template is the level-A scenario (cast-resolve, or play-land for a
land) with **probe permanents on both seats** (p0 and p1 each hold Grizzly
Bears; p1 already does via `Baseline`, `gen.go:416-433`), so a continuous
effect that lands on either side appears after setup and is frozen. It serves
`static.continuous` only.

Keyword grants are the common case, and keywords are not compared today. The
comparator gains an **opt-in** field list on the item, `compare:
["keywords"]` (omitempty, so level-A items are byte-identical): with it, a
permanent's key gains `kw=<sorted, case-folded keywords ∩ evergreen set>` in
`permKeys` for `Compare`, `Canonical`, `Freeze` and `Meets`. The evergreen set
(Flying, First strike, Double strike, Deathtouch, Defender, Haste, Hexproof,
Indestructible, Lifelink, Menace, Reach, Trample, Vigilance) keeps the two
engines' vocabularies comparable (**H5**: XMage's ability names fold to these).
The driver emits a `keywords` array per permanent.

### 2.4 combat

- `combat#F.attack`: setup the card on p0's battlefield and a fixture blocker
  (Grizzly Bears) on p1's; steps `attack` (card at p1), then `block` (Grizzly
  Bears on the card) only if gorge offers that block, then `pass_to main2`.
  Checkpoints show the block, first-strike/deathtouch/trample/lifelink outcomes
  and deaths. Uses only ops the driver already has (`ScenarioReplay.java:601-640`).
- `combat#F.block`: setup the card on p0's battlefield, an attacker fixture on
  p1's; steps `attack` seat 1 (gorge's attack op passes p0's turn itself,
  `oracle_run.go:1070-1112`), `block` seat 0 (the card on the attacker), then
  `pass_to main2` with `active p1`. Needs the driver to run steps on turn 2.

Legality (that a block is illegal in both engines, evasion, CantBlock) is not
observable through scripted declarations; it is §6 work.

## 3. Scenario ops

| op / field | gorge runner | XMage driver | needed by |
|---|---|---|---|
| `activate` + `ability_index` | `activate` exists; add `ability_index` matching `Option.Ability` (helper, `do` ≤ 319 lines) | new `activate` case using `xmage_ability` (H1), with the step's mana and targets as `cast` does | activate |
| `cast`, `resolve`, `attack`, `block`, `pass_to main2` | exist | exist | trigger, static, combat |
| `pass_to` begin-combat / end / next-turn upkeep, `active` | exists | new step mappings and a turn counter replacing the constant `TURN` for later steps (**H3**: `runCode`/`attack`/`block` accept any turn number) | trigger.phase, combat.block |
| `attack`/`block` on p1's turn | exists | use the current turn, seat from the step | combat.block |
| snapshot `keywords` | exists | emit per permanent | static |

Generator side: `oraclegen.Step` gains `ability_index` (`*int`) and `active`;
`Item` gains `xmage_ability` and `compare`. All are `omitempty`.

## 4. Verdicts, ratchets, status

- **Rows.** One row per requirement, template string = requirement key. No
  schema change to `VerdictRow`.
- **Gate.** `gate.Check(reg, root, set, "B")` runs the level-A checks, and for
  each card without a level-A problem, each requirement: covered-by-A passes;
  otherwise `GenerateB` → the same row checks as level A. Reasons reuse
  level-A wording with the key, so `adopt.Bucket` (`status.go:42-71`) buckets
  them unchanged ("no generated scenario (activate#0.1: activate cost gap:
  Discard)" is a template gap). The `*` stub at `gate.go:138-140` goes, and
  `LevelMeaning("B")` says what B covers and what stays gap.
- **Ratchet.** `RatchetEntry` gains `outstanding_b *int` (omitempty: absent
  means "not measured at B"). B outstanding counts unmet requirements plus the
  card's level-A problems. `oraclediff status -all -level B -sets … -write-ratchet`
  writes only `outstanding_b` for the measured sets; the level-A write
  (`RatchetOf`, `status.go:354-361`) must carry existing `outstanding_b`
  values forward instead of dropping them. `CheckRatchet` fails a measured B
  count above its floor. A new `TestLevelBRatchet` (compliance/adopt) measures
  only the sets that have an `outstanding_b` entry (BIG, EOE, FDN, FRA after
  L2), so its cost grows with the claim.
- **Declared level.** `declared.json` already accepts `"B"`
  (`verdicts.go:287-291`), and `TestDeclaredSetsCompliant` calls `Check` with
  the declared level (`gate_test.go:33-41`), so declaring `FRA: B` becomes a
  real check the day `Check` implements B. The level floor in `CheckRatchet`
  compares levels as strings (`status.go:223`), so B ≥ A holds.
- **Tooling.** `oraclediff gen -level B` emits the level-A item and every
  generable level-B item (the pass replays both); `scripts/compliance-pass.sh`
  takes `LEVEL`. `rule`, `triage`, `refreeze` and `show` address a row by
  `(card, template)` through a new `templates.ItemFor(reg, card, template)`.
  `shape.Of` keys a ruling by the template FAMILY (the part before `#`), so one
  shape ruling classifies every slot.
- **Host replay.** New items replay only on the host
  (`make compliance-pass SETS=… LEVEL=B`). The XMage build and result cache
  under `XMAGE_ORACLE_DIR` were lost on 2026-10-05: rebuild with `make
  xmage-oracle-setup` before any replay. A driver change needs a forced full
  replay (every scenario, not `plan`'s stale set), per
  `.ds4/operator-compliance-refresh.md`; the helper it names must be recreated
  with the setup.

## 5. Order of work

Seats cannot run XMage, and any branch touching `tools/xmageoracle/` parks at
the "xmage driver needs host replay" gate (`.agentctl/config.toml:220`) for a
host operator. So every family is a gorge-side ticket (generator, runner,
comparator, census) followed by a driver ticket that depends on it. Gorge-side
tickets can land before the driver: level B is declared nowhere, so a
new-template row reading `harness` or `no verdict` reddens no gate.

1. **L1** requirement classifier + census (`compliance/levelb`), no templates.
2. **L2** gate level B, `GenerateB` stub, B ratchet fields, `status -level B`,
   record `outstanding_b` for BIG/EOE/FDN/FRA.
3. **L3** row addressing: `gen -level B`, `ItemFor`, rule/triage/refreeze/show
   `-template`, shape family key, `NewLevelBItem`, pass `LEVEL`.
4. **L4** runner `ability_index` (rules; independent of L1-L3).
5. **L5** activate template → **L6** activate driver.
6. **L7** trigger turn-1 recipes → **L8** trigger phase recipes → **L9**
   trigger driver (pass_to steps, turn counter).
7. **L10** comparator keyword opt-in → **L11** static template → **L12**
   static driver (keywords).
8. **L13** combat templates → **L14** combat driver (p1's turn).

Then a host pass at `LEVEL=B` over the four sets, triage, and
`-write-ratchet`; `FRA: B` is declared when its B outstanding reaches 0.

## 6. Not in this plan

Activation from hand/graveyard, faces other than 0, cost statics, legality
statics (needs an `offered` observation in both engines — gorge has
`oracleOffered` expectations, `oracle_run.go:141-146`, XMage has none),
opponent-caused triggers, replacement effects (`Face.Repls`) and level-B
coverage for hand-scenario cards. Each is a visible `gap` sub-family in the
census, so its size is measured before anyone schedules it.

## 7. The `offered` observation (legality statics)

Status: design + the two positive-observable rows served (ticket
cli-20261006T035109Z-1f44e455). Base `main@8da804429`. Sections 1-6 stay as
written; this section replaces section 6's sketch ("legality statics need an
`offered` observation in both engines"). Every `path:line` in the XMage source
below was read at `$XMAGE_ORACLE_DIR/mage` (2026-10-06); the gorge paths at the
base SHA.

### 7.0 The problem

A legality static decides whether an action is legal. A scripted declaration
(`cast`, `attack`, `block`, `activate`) cannot observe a prohibition: if the
driver declares an illegal action XMage throws, and if it declares nothing the
scenario proves nothing. The FRA level-B pass left eight requirements that
observe something NOT happening or NOT being offered:

| card (requirement) | static | observation |
|---|---|---|
| Karn, Argent Defender `static#0.0` | DisableTriggers | a probe's ETB trigger does not reach the stack (**served**, §7.5) |
| Ghalta, the Immovable `static#0.2` | CombatDamageToughness | a power<toughness creature assigns its toughness in combat (**served**, §7.5) |
| Ghalta, the Immovable `static#0.1` | CanAttackDefender `Creature.YouCtrl` | which attackers are offered |
| Surveillance Phantasm `static#0.0` | CanAttackDefender `Card.Self` (CheckSVar) | which defenders it may attack |
| Tetsuko Umezawa, Fugitive `static#0.0` | CantBlockBy (p/t ≤ 1) | that a blocker is NOT offered |
| Proft, Sinister Mastermind `static#0.0` | CantBeCast unless X ≥ 7 | that the cast is not offered |
| Yuriko, Blade of the Mighty `static#0.0` | CantBeCast during combat | that a spell is not offered |
| Yuriko, Blade of the Mighty `static#0.1` | CantBeActivated during combat | that a non-mana ability is not offered |

Two of the eight are observable through an existing checkpoint and are served
now (§7.5). The other six need one new checkpoint, `offered`, in both engines.
The rest of this section specifies it.

### 7.1 The checkpoint

`offered` is a **snapshot field**, not a step op: it is a fact about the state
at a checkpoint, so every existing checkpoint (after setup and after each step)
can carry it, exactly as `stack` and `permanents` already do. It is a list of
the priority holder's legal actions, each keyed by what the two engines can
both name:

```
offered: [ {source: <card ref>, kind: cast|play|activate, label: <rule text>} ]
```

- `source` is the object's scenario ref (`p0:Proft, Sinister Mastermind`),
  which both engines already produce (`rules/oracle_snapshot.go`'s refs; the
  driver's `permanentRef`/aliases).
- `kind` is `cast` (a hand spell), `play` (a land), or `activate` (a
  battlefield/zone ability). It is the coarser, comparable vocabulary; a
  keyword ability folds to `activate`.
- `label` is the ability's rule text, as both engines render it. The driver
  gets it from `AbilityImpl.toString()` (`Mage/src/main/java/mage/abilities/AbilityImpl.java:1468`,
  which returns `getRule()`), the same string `TestPlayer.printAbilities`
  prints (`Mage.Tests/src/test/java/org/mage/test/player/TestPlayer.java:1333`).
  It is optional in the comparator (a match on `source` + `kind` is enough for
  "is X offered"; `label` distinguishes two abilities on one source).

Because `offered` is a list, "X is not offered" is the absence of an entry,
not a new field. A scenario asks it two ways, both already supported by the
frozen-expectation machinery:

- **Positive, by state:** the checkpoint's `offered` list contains/omits the
  entry. Compared like any other snapshot field (§7.4).
- **Negative, by assertion:** a step carries an expectation
  `expect: [{offered: {source, kind, label}, want: false}]`, which the runner
  checks and `Freeze` records as a `fails` field, so a later regression fails
  `Meets` (`compliance/oraclediff/freeze.go:79-106`).

The assertion form is the one the six rows need: a legality static suppresses
an option, and a suppressed option leaves no other trace.

### 7.2 gorge (the generator and the runner)

The runner already implements the assertion. `oracleExpect.Offered`
(`rules/oracle_run.go:148-153`, `:155-159`) carries `{seat, kind, card,
label}`; `r.check` (`:1540-1565`) fails unless the seat holds priority and the
pending `decision.Option` list contains (or, with `want:false`, omits) an
option whose object is `card`, whose kind matches (`o.Kind == kind`, with
`activate` accepting `o.Kind == "ability"`), and whose label matches
`oracleLabelMatches`; for `activate` it also scans the mana-ability labels
(`r.manaAbilityLabels`). That is exactly the legal-option walk this checkpoint
needs, already tested by the hand-authored `rules/testdata/oracle/**`
scenarios.

The generator gains one thing: an `Expect` on `oraclegen.Step`
(compliance/oraclegen/gen.go), omitted when empty so every level-A item stays
byte-identical. A level-B legality template then:

1. builds the scenario that would offer X absent the static;
2. replays it through the runner (`rules.RunOracleScenarioJSON`) and reads the
   pending priority decision's `Options` -- the **control**, proving X is
   offered without the static;
3. builds the scenario with the static's source on the battlefield, appends
   `expect: [{offered: {source, kind, label}, want: false}]` to the checkpoint
   step, and replays it -- the expectation must hold (the option is gone);
4. emits the item only when the two differ, so a scenario never asserts a
   static the engine does not implement.

`oraclegen.Step.Expect` (a subset of `oracleExpect`, marshalled under the same
JSON keys) is threaded to the runner by the existing `Item.Raw()`; no runner
change is needed. `oracleOffered`'s `Card` is a scenario ref and `Kind` a
string, so the generator emits exactly what the runner already decodes.

Choosing the checkpoint: for `CantBeCast`/`CantBeActivated` during combat
(`Phases$ BeginCombat->EndCombat`) the step that carries the expectation is a
`pass_to` into begin-combat (or any step whose checkpoint is inside combat),
and the control uses the same step outside combat. For `CantBlockBy`, the
checkpoint is a pending `decision.KBlockers`; the runner's existing
`CanBlock` expectation (`:1567-1594`) already names a (blocker, attacker) pair
and is the cheaper observation there -- the template should prefer it and use
`offered` only where `CanBlock` does not reach (which attacker is offered).

### 7.3 XMage (the driver and the playable list)

XMage's playable list is the mirror of gorge's option list. `TestPlayer`
exposes it (all delegates to the wrapped `TestComputerPlayer`/`PlayerImpl`):

- `TestPlayer.getPlayable(Game game, boolean hidden)` --
  `TestPlayer.java:3775-3777`; delegates to
  `PlayerImpl.getPlayable(Game originalGame, boolean hidden, Zone fromZone,
  boolean hideDuplicatedAbilities)` (`PlayerImpl.java:4356-4462`). It returns
  `List<ActivatedAbility>`: every spell and ability the player can currently
  cast or activate, already filtered by the engine's rule-modification checks
  (`game.getContinuousEffects().preventedByRuleModification(...)` at
  `PlayerImpl.java:4388,4397,4405`) -- so a `CantBeCast` static removes the
  entry here, exactly the fact to observe.
- `TestPlayer.getPlayableObjects(Game game, Zone zone)` --
  `TestPlayer.java:3780-3782`; `PlayerImpl.getPlayableObjects` at
  `PlayerImpl.java:4566`. It returns `PlayableObjectsList`, whose
  `getObjects()` (`PlayableObjectsList.java:67`) is a
  `Map<UUID, PlayableObjectStats>` keyed by source id, with
  `containsObject(UUID)` (`:43`) and `getPlayableAmount(UUID)` (`:59`). This is
  the shape to emit: one entry per playable source.
- `TestPlayer.canPlayLand()` -- `TestPlayer.java:3237-3239`;
  `PlayerImpl.canPlayLand()` at `PlayerImpl.java:1941` (`landsPlayed <
  landsPerTurn`). It answers `play` for the single-relevant-land case.

The driver emits `offered` in `snapshot(...)`
(`tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java:1060-1170`),
where the other per-checkpoint fields are built. For the checkpoint's priority
player (`g.getPriorityPlayerId()`, already computed at `:1066`):

```
for (ActivatedAbility a : seat(priority).getPlayable(g, true)) {
  o = { source: refName/controller of a.getSourceId(),   // a.getSourceId(), AbilityImpl.java:917
        kind: a instanceof PlayLandAbility ? "play"
              : a instanceof SpellAbility ? "cast" : "activate",
        label: a.toString() }                            // AbilityImpl.java:1468
}
```

The source ref is resolved with the same `seatIndex`/alias machinery the
permanents block uses (`:1096-1158`), and the `gorgeSpellings` rewrite at the
end of `snapshot` keeps the card-under-test's two spellings aligned. A
priority-holder change (the driver's `pass_to`) is already a step, so the
checkpoint after it carries the right player's list.

Cost note: `getPlayable` calls `originalGame.createSimulationForPlayableCalc()`
and `getManaAvailable` (`PlayerImpl.java:4357-4358`), so it is not free. The
driver should emit it only for checkpoints the item requests (the `compare`
list, §7.4), not for every snapshot of every scenario; a level-A item never
asks for it.

### 7.4 The comparator

`offered` joins the comparator's snapshot vocabulary
(`compliance/oraclediff/compare.go`) as a **non-default** field, behind the
item's existing opt-in `compare` list (`oraclegen.Item.Compare`,
`compliance/oraclegen/gen.go`), exactly as `keywords` is planned to
(section 2.3). Rules:

- Canonical form: sort entries by `(source, kind, label)` and join; the two
  engines emit their list in engine order, so the comparator must sort both
  sides (no map range may reach it -- the minted order is deterministic).
- A level-A item has `compare` empty, so its bytes and `ScenarioSHA` are
  unchanged and no level-A verdict moves.
- For an item with `compare: ["offered"]`, `FreezeOpts`/`MeetsOpts` treat the
  sorted `offered` string as one field per checkpoint, like `stack`. The
  assertion form (`expect.want:false`) still rides `fails`, independent of
  `compare`: the generator only needs `compare` for the positive, by-state
  form (the `CanAttackDefender` rows ask *which* defenders, not *whether* one
  is offered).

Label matching: XMage's rule text and gorge's option label are not
byte-identical (XMage prints "{2}, {T}: ..."). Like the runner's
`oracleLabelMatches`, the comparator should match a label by a normalised
prefix, and treat `label` as advisory: `source` + `kind` is the load-bearing
pair. The comparator records a mismatch as an ordinary field difference, so
the existing triage (`shape.Of`, `adopt.Bucket`) classifies it unchanged.

### 7.5 The two positive-observable rows (served now)

Neither needs the `offered` checkpoint; both are served by the new
`static` template (`compliance/oraclegen/templates/static.go`, template id
`static`, version 1), dispatched by `GenerateB` for the two new sub-families
`static.disable-triggers` and `static.combat-damage-toughness`
(`compliance/levelb/levelb.go`, `servableStaticModes`):

- **Karn, Argent Defender `static#0.0`** (DisableTriggers). Setup Karn on
  p0's battlefield and Elvish Visionary in hand; cast the Visionary, then two
  `pass` steps. Its ETB trigger reaches the stack after the spell resolves.
  The control (the Visionary alone) shows the trigger on the stack; with Karn
  it does not. Because an unchanged `stack` field is never frozen, the item's
  last step carries `expect: [{trigger_on_stack: "p0:Elvish Visionary",
  want: false}]`, so a regression fails the frozen `fails` field. The
  `want:false` assertion is the same negative-observation idea `offered` will
  generalise.
- **Ghalta, the Immovable `static#0.2`** (CombatDamageToughness). Setup Ghalta
  and Giant Spider (2/4, no defender) on p0's battlefield; `attack` Giant
  Spider at p1, then `pass_to main2`. Control: Giant Spider alone deals its
  power, 2 (p1 life 18). With Ghalta: it assigns its toughness, 4 (p1 life
  16). The damage is a change from setup, so it is frozen and `Meets` holds
  the static with no new field.

Both builders verify the control differs from the observation before emitting
the item, so neither passes vacuously. **Premise check:** section 2.4's
`combat` template (L13) is not landed at this base, so Ghalta `static#0.2` is
served by the `static` template, not by a combat template; the combat family
(L13) stays the home for the `combat#F.attack`/`combat#F.block` requirements.

### 7.6 Remaining work (not this ticket)

- **gorge generator:** a legality template per static shape that emits the
  `offered` expectation and its control (`CantBeCast`, `CantBeActivated`
  during combat, `CantBlockBy`, `CanAttackDefender`). `CanBlock` covers
  `CantBlockBy`; `offered` covers the rest.
- **XMage driver:** emit `offered` at a requested checkpoint (§7.3), mapped
  step names for begin-combat and the turn counter (section 3, H3), then a
  forced full host replay.
- **comparator:** the `compare: ["offered"]` field and its sorted canonical
  form (§7.4).
- Face > 0, non-battlefield activation and the other section 6 gaps are
  unchanged.
