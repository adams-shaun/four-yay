# XMage compliance oracle — design (2026-10-02)

Status: design, approved in conversation 2026-10-02. No code yet.

Supersedes the Forge choice in
[`2026-09-29-cross-engine-oracle-audit.md`](2026-09-29-cross-engine-oracle-audit.md)
and replaces the seeder's "Oracle-audit the next 25 validated cards" tickets
once rollout ticket X10 lands. The scenario format and triage vocabulary of
[`2026-09-27-oracle-text-card-audit.md`](2026-09-27-oracle-text-card-audit.md)
stay; this design generates scenarios instead of authoring them.

## 1. Goal

"gorge is compliant with **[the declared sets]**", as a statement CI enforces.
A set is declared at a level:

- **Level A**: every card in the set has at least one generated scenario that
  agrees with the oracle, or whose disagreement was triaged as the oracle's
  error.
- **Level B**: every ability of every card (each spell ability, mode,
  activated ability, trigger and static) has its own such scenario.

A first, then B, using the same machinery. The first declared set is
**Reality Fracture (FRA)**, released 2026-10-02.

The work must be reusable: a future set costs only its new cards and any new
templates, never a fresh audit project.

## 2. Why XMage

Measured 2026-10-02:

| | XMage | Forge |
|---|---|---|
| Licence | **MIT**: derived manifests and driver source may be committed | GPL-3.0: nothing derived may be committed |
| Independence from gorge | Hand-written Java per card; a different reading of the Oracle text | Runs the same scripts gorge compiles; a script bug agrees with itself |
| Headless harness | `Mage.Tests` `CardTestPlayerBase`: targets, auto-paid mana, `setChoice`/`setModeChoice`, attack/block, strict choose mode, P/T/counter/zone/stack/pool/attachment checks | `gamesimulationtests`: no cost payment, targeting hard-wired to the acting player, no ordinal refs (forgedelta spike REPORT.md) |
| Set membership | 589 set classes under `Mage.Sets/src/mage/sets/` | `forge-gui/res/editions` (GPL; `forgec fetch` deliberately skips it, `cmd/forgec/coverage.go:17`) |

FRA specifically: Forge's edition file lists 285 distinct cards; XMage's
`RealityFracture.java` implements 269 of them. The 16 it lacks (Command the
Stage, Extrapolate the Impossible, Grim Repriser, Hapatra, the Desert Fang and Hapatra, the Desert Frost, Jace's
Machinations, Kiora of Salt and Sand, Loot, the Anomaly, …) need the existing
hand-authored Oracle audit. XMage has only 2 test files for FRA cards, so
porting XMage's own tests contributes nothing here (it remains worth doing for
older sets).

Risk accepted: XMage's FRA implementations are days old. Every disagreement is
decided by Oracle text plus the CR, never by either engine, so a young oracle
costs triage time, not correctness.

## 3. Unit of verification: the card

Compliance is computed, not audited.

- A **verdict** is keyed by `(card name, oracle_sha, template id, scenario
  hash)`. It stays valid until the card's Oracle text, the generator, gorge's
  head or `XMAGE_REF` changes in a way that changes that scenario's hash or
  result.
- A set is compliant at level L when every card in its manifest has passing
  verdicts at L. Manifests for all 589 XMage sets are generated once, so
  declaring a later set is free for every card already verified, including
  reprints.
- The pipeline runs over the **whole corpus** (gorge playable cards that XMage
  also implements), in priority order: FRA, then Standard, then repo-deck
  cards, then the rest. Sets light up as their last card passes.
- **Templates are keyed by ability shape** (IR API or trigger mode), not by
  card. A new mechanic adds one template and every card carrying it, in every
  set, benefits.
- **Rulings are reused.** A triage ruling recorded against an ability shape
  classifies every later disagreement with the same shape automatically.

## 4. Resource budget: 8 vCPU, 8 GB RAM

- One heavy job at a time, inside `systemd-run --user --scope -p
  MemoryMax=7G`.
- **XMage build** is its own job, run once per `XMAGE_REF` bump: Maven with
  `MAVEN_OPTS=-Xmx3g`, `-T 2`. Compiling `Mage.Sets` (~30k classes) is the
  peak.
- **Runs**: 2 XMage JVMs at `-Xmx2500m` each, separate processes
  (`CardTestPlayerBase` holds static state, so in-JVM threads are unsafe), plus
  one gorge runner at `GOMEMLIMIT=1GiB`, `-p 1`: about 6.5 GB. Gorge runs
  ahead; XMage is the pace-setter. Each JVM is long-lived and reads scenarios
  in a loop, since startup plus card-DB load costs tens of seconds.
- **Estimate, measured in X1/X4**: at 100–200 ms per warm XMage scenario, a
  full-corpus pass (~90k scenarios) takes roughly 1.5–3 h on 2 JVMs; later
  passes are incremental. FRA alone (~1k scenarios) takes minutes.

## 5. Components

Committed in gorge (XMage-derived artefacts are MIT and credited in `NOTICE`):

| Path | Role |
|---|---|
| `XMAGE_REF` in `Makefile` | Pinned XMage commit, beside `FORGE_REF` |
| `compliance/manifests/<SET>.json` | Per-set card list (name, collector numbers, set code, `xmage_ref`), generated by `go run ./cmd/compliance manifest` from the set classes at `XMAGE_REF` |
| `compliance/declared.json` | Declared sets and levels, e.g. `{"FRA": "A"}` |
| `compliance/verdicts/<a-z>.jsonl` | One row per card × scenario: template id, scenario hash, frozen expectations, `xmage_ref`, gorge head, status. Sharded by first letter so concurrent branches do not collide on one hot file |
| `compliance/rulings/<shape>.json` | Reusable triage rulings keyed by ability shape: which side is wrong, the CR citation, the ticket |
| `compliance/vocab/keywords.json` | The one hand-maintained cross-engine map: CR 702 keyword names ← gorge keyword strings and XMage ability classes |
| `cmd/oraclegen` + `oraclegen/templates/<template>.go` | Deterministic scenario generator; one file per template |
| `rules/oracle_run.go` | The oracle runner promoted out of `rules/oracle_audit_test.go` into a non-test file so the pipeline can call it and receive a snapshot. It still builds setup only through logged events and acts only through `Submit`. `oracle_audit_test.go` becomes a thin caller; every existing scenario stays green |
| `cmd/oraclediff` | Runs gorge's side, reads XMage's snapshots, compares, writes verdicts and triage items |
| `compliance` test `TestDeclaredSetsCompliant` | The CI gate (§8) |
| `tools/xmageoracle/` | Java driver source: `pom.xml` and one `ScenarioReplay` runner extending `CardTestPlayerBase`, reading scenario JSON lines on stdin and writing snapshot JSON lines on stdout. `make test` never builds it |

Not committed, under `/mnt/sata/gorge-training/xmageoracle/`: the full XMage
clone at `XMAGE_REF`, a user-local Maven and `~/.m2`, the built jars and H2
card database, and the run cache (snapshots keyed by scenario hash).

## 6. Scenarios and answers

- Generated scenarios use the existing `rules/testdata/oracle` schema
  (`setup`/`steps`/`expect`, refs like `p1:Name#2`, the closed op set). They
  are not committed: the generator is deterministic, so a scenario is a pure
  function of (card, template, generator version) and is regenerated on
  demand.
- The generator may read gorge's IR to choose templates and legal targets.
  This does not bias the verdict, because expectations come from the oracle,
  not from the generator.
- **Every answer is scripted.** Targets by ref, "may" → yes, the first mode, X
  set to a stated value. The XMage driver maps them to `addTarget`,
  `setChoice` and `setModeChoice` under `setStrictChooseMode(true)`, so an
  unscripted XMage choice is a hard error, never an AI guess. Gorge's runner
  consumes the same answers through `Submit`. Leftover or missing answers on
  either side are a decision-shape divergence.

## 7. Snapshot and comparator

Both engines emit the same canonical snapshot after every step:

| Level | Fields |
|---|---|
| game | turn, step, active seat, priority seat, game over / winners |
| player | life, poison, player counters, hand (sorted names), graveyard (ordered), exile, command, library count + top 5 names, mana pool (`WUBRGC` counts) |
| permanent | ref, controller, owner, token, tapped, face-down, P/T (creatures), damage, counters, types + subtypes, colours, keywords (vocabulary-mapped), attached-to, attacking/blocking |
| stack | top first: kind (spell/ability), source ref, controller |
| decisions | in order: seat, normalized kind (`target`, `yesno`, `mode`, `choose_n`, `order`, `x`), option count, pick |

Identity: cards match by ref (controller, name, arrival order); tokens match by
characteristics (types, subtypes, P/T, colours), because the engines name
tokens differently. Unmapped keywords become `other:<text>`: logged, not
compared, until added to the vocabulary.

The comparator walks checkpoints in order and reports only the **first**
difference (the rest cascade from it). Never compared: object ids,
timestamps, library order below the top 5. Outcomes:

- `AGREE`
- `DIVERGE{checkpoint, field, gorge, xmage}`
- `DECISION{checkpoint, gorge_kind, xmage_kind}`
- `HARNESS{engine, msg}`: a driver could not express the step. A harness
  gap, never a verdict on the card.

**Freezing**: on `AGREE`, the fields that changed between the initial and
final snapshots, plus the decision log, become the verdict row's frozen
expectations, written in the `oracleExpect` vocabulary extended with
`decisions`, `attached_to` and `colors`. Unchanged fields are not frozen, so a
verdict does not depend on board state the card never touched.

## 8. Triage, rulings, fix tickets, and the CI gate

**Triage.** Each `DIVERGE`/`DECISION` becomes a triage item, worked in batches
by a review-tier seat that receives the Oracle text, the CR excerpts and both
snapshots, and (as in the W-series triage role) may read the script. Verdicts:

| Verdict | Action |
|---|---|
| `gorge_wrong` | Verdict row with status `gorge_wrong`; feeds a fix ticket |
| `xmage_wrong` | Expectation set by hand from Oracle + CR, with the CR cited in a ruling; counts as passing |
| `scenario_invalid` | The template produced an illegal or ambiguous scenario; fix the template |
| `harness` | Driver gap; driver ticket |

**Rulings** are recorded per ability shape (template id, IR API/mode plus the
relevant params, and the normalized field diff). A later disagreement matching
a ruling is classified automatically and marked `auto`; one in ten `auto`
classifications is sampled for review so a wrong ruling cannot spread
unchecked.

**Fix tickets** are seeded from committed `gorge_wrong` verdict rows grouped by
shape: **one ticket per root cause**, listing every affected card. This
replaces the git-excluded `.ds4/reward/defects.jsonl`, which seats wrote
inside their own worktrees and which never reached the seeder (no
`correct-defect` candidate was ever produced). When a fix merges, the next
pass flips the affected verdicts to passing.

**CI gate: `TestDeclaredSetsCompliant`.** For each set in
`compliance/declared.json`, every manifest card (basics excluded) must:

1. be in the corpus and fully supported: no missing primitive, no unread
   param;
2. have verdict rows covering the level: at least one template (A), or one per
   ability in the IR's ability inventory (B);
3. have no `gorge_wrong` row, and no row whose `oracle_sha` is stale;
4. still pass its frozen expectations against gorge at this head (scenarios
   are regenerated in the test, so no Java is involved).

Cards XMage does not implement count only through a hand-authored Oracle
scenario under `rules/testdata/oracle/`. The test runs only declared sets, so
its cost grows with the claim, not the corpus. A missing `.cards` must fail
this test, not skip it.

## 9. Rollout tickets

| Id | Ticket | Depends | Seat |
|---|---|---|---|
| X0 | `FORGE_REF` bump to upstream with FRA (`fb4d8091126051b0c579db5f3bfdcb7e03aae63d`, 2026-10-02; the current pin `95f04e8` is 2026-09-03 and holds only FRA's 30 reprints): re-pin golden heads, regenerate the feedback fixture, settle ratchet and `oracle_sha` fallout | — | strong |
| X1 | XMage build out of tree: user-local Maven, clone at `XMAGE_REF`, capped build, run one existing `Mage.Tests` card test, record build time, peak RSS and warm per-test latency | — | strong / hand |
| X2 | `cmd/compliance manifest` and all set manifests; `NOTICE` | — | cheap |
| X3 | Promote the runner to `rules/oracle_run.go` and add the snapshot emitter; all existing oracle scenarios unchanged and green | — | mid |
| X4 | `tools/xmageoracle` driver + snapshot. **Calibration gate**: replay the hand-authored pilot scenarios through both engines; the comparator must flag the pilot's known divergences (Purphoros `RemoveType$`, Relic Vial look-back, Sidar Jabari command zone) without being told | X1, X3 | strong |
| X5 | `cmd/oraclediff`: comparator, cache, verdict writer, triage queue | X3, X4 | mid |
| X6 | Generator v1 with the level-A templates (cast or play and resolve; activate each ability; attack). Calibration run on FRA's reprints and on FDN, whose hand audit gives a known answer to compare | X5 | mid |
| X7 | `TestDeclaredSetsCompliant` and an empty `declared.json` | X5 | cheap |
| X8 | FRA level-A pass: run, triage, file root-cause fix tickets; hand Oracle audit of the 16 cards XMage lacks; declare `FRA: A` when green | X0, X6, X7 | pipeline |
| X9 | Level-B templates (trigger modes, statics, modes) and the IR ability inventory; declare `FRA: B` | X8 | mid |
| X10 | Full-corpus incremental sweep on the heavy-job schedule; seeder integration (fix tickets from verdicts); retire the "Oracle-audit the next 25" seeder candidate | X8 | mid |

X0, X1, X2 and X3 are independent and start together. `rules/oracle_run.go`
(X3) and the `Makefile` pin (X0, X1) touch hot files; keep those changes small
and land them in sequence.

## 10. Revisit triggers

The operator approved this design with "if it looks like there are problems,
we revisit". Stop and come back if:

- X1 measures a warm XMage scenario well above 200 ms, or a JVM above 2.5 GB
  heap: the budget in §4 does not hold.
- X4's calibration misses a known pilot divergence: the snapshot or
  comparator is too coarse.
- X6's FDN calibration disagrees with the FDN hand audit on more than a
  handful of cards for reasons that are not real bugs in either engine: the
  templates or answer scripting are wrong.
- Triage volume on FRA does not fall as rulings accumulate: shape signatures
  are too narrow to reuse.
