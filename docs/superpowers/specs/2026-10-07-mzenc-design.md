# mzenc: a byte-identical Go port of MageZero v0.2's `StateEncoder`

Status: design (2026-10-07). Home: `internal/mzenc`. Scope: **state encoding
only**, hash engine proven **byte-identical** against the Java source by a
golden-vector oracle.

## 1. Why

MageZero (WillWroble/MageZero) trains a network whose input is a sparse set of
feature ids hashed into a 2^31-bin embedding table. We want to score that
network on gorGE game states, which needs gorGE to compute the same feature
ids the network was trained on. `internal/policynet` already has a
MageZero-*style* encoder, but its `hashID` is gorGE's own — its ids do not land
in MageZero's bins and cannot feed MageZero's table. This package reproduces
MageZero's encoder exactly at the hash level.

The reference is the Java in XMage fork `WillWroble/mage` @ master (v0.2,
`cb7e9c6f`; the source behind the v0.2.0-alpha bundle), package
`mage.player.ai.encoder`:

- `Features.java` (198 lines) — the feature tree and the hash.
- `StateEncoder.java` (723 lines) — the walkers that turn a `Game` into feature
  names.

v0.2 widens `Features.TABLE_SIZE` from 2,000,000 (public `2f35d9f7`) to
`Integer.MAX_VALUE` = 2,147,483,647. The port defaults to v0.2 and makes the
table size selectable for v0.1 comparisons.

## 2. Non-goals

- No JVM or Python in the normal build or test loop. The oracle generator is
  run by hand; its output is committed.
- No weight training, no model loading, no ONNX export (separate work).
- No assertion that gorGE's feature *strings* equal XMage's. The hash engine is
  proven byte-identical; the extractor's string coverage is **measured and
  reported**, never silently assumed. XMage hashes `ability.getRule()`,
  `effect.getText(mode)` and `manaCost.getText()`; gorGE has `SpellAPI`,
  Forge-notation `ManaCost` and `Keywords` instead. Text-derived features are
  the honesty boundary and are counted.
- Not a replacement for `internal/policynet`. Different hash space, different
  consumer.

## 3. Layout

```
internal/mzenc/
  features.go          # the Features tree + hash64/mix64 + thermometer
  featuremap.go        # index -> [namespace/name] research table (optional path)
  state.go             # processState + the process* walkers over gorGE state
  numeric.go           # numeric thermometer helper (Features.addNumericFeature)
  oracle_test.go       # golden-vector replay gate
  features_test.go     # hash/thermometer/cardinality unit tests
  testdata/
    golden/*.json      # committed oracle vectors
    javaharness/
      Features.java    # vendored, pinned copy of the v0.2 source
      FeaturesProbe.java  # stdin ops -> {ops, indices} JSON
      README.md        # regeneration instructions
```

## 4. Hash engine (`Features.go` port)

Port `Features.java` op-for-op. The load-bearing details, with Java line refs:

- Constants: `TABLE_SIZE` (default `2_147_483_647`), `GLOBAL_SEED =
  0x9E3779B185EBCA87`, `NUMERIC_BREAKPOINTS = {32,64,128,256,512}`
  (`Features.java:23-25`).
- `hash64(s, seed)` and `mix64(z)` byte-exact (`Features.java:173-197`):
  UTF-8 bytes, little-endian 8-byte chunks, `Long.rotateLeft(h,27)`, the
  `>>>33` avalanche with the two multipliers. Go `int64` arithmetic is two's
  complement like Java's; unsigned shifts must be `>>>`.
- `indexFor(h) = int(h<0 ? -h : h) % TABLE_SIZE` (`Features.java:169-172`).
- Sentinels: root seed `GLOBAL_SEED`; sub-feature seed `hash64(name,
  parent.seed)` (`Features.java:48-55`).
- **Occurrence / cardinality** (`Features.java:100-122`): `addFeature(name)`
  increments that node's `occurrences[name]` and hashes the key
  `name + "#" + n`; the n-th repeat of a name within one state is a distinct
  id. `getSubFeatures(name)` first `addFeature(name)`, then reuses/creates the
  node keyed `name#n` (`Features.java:77-97`).
- **Numeric thermometer** (`Features.java:124-144`): emit `name@b` for each
  breakpoint `b` with `b <= num`, then `name@n` for `n` in
  `0..min(num,20)-1`. Note the breakpoint loop is independent of the 0..19
  loop, so a value like 50 emits `@32` **and** `@0..@19`.
- **`passToParent`** propagates a call to the parent node before counting
  (`Features.java:106-111`); it is what makes sub-features also contribute an
  abstracted parent token.
- **`stateRefresh`** (`Features.java:146-152`) zeroes occurrence counts but
  keeps the node graph, so nodes are reused across states.
- Output is a Java `HashSet<Integer>` — a set, so colliding indices collapse.
  Go: `map[int32]struct{}` — the id is a Java `int`, so the `MinInt64` lattice
  in §4 can make it negative; mirror it rather than clamping to uint.
- `FeatureMap` / `useFeatureMap` / `uuid`→key map (`Features.java:117-118`,
  `156-168`) are ported only for the research/logging path; the id computation
  does not need them.

### The one Java quirk to mirror

`indexFor` negates `h` when negative. For `h == math.MinInt64` Java yields
`MinInt64` (overflow) and the modulo is negative; the port must reproduce the
same lattice rather than take `abs` naively. Covered by a unit test.

## 5. Feature extractor (`StateEncoder` walkers)

`processState(game, decisionPlayerId, decisionType, decisionsText)` returns the
`Set<Integer>`. The port reads an **engine-side omniscient projection** of
`state.Game` (both players, both hands, both battlefields, stack, exile,
command zone, watchers), matching MageZero's `perfectInfo = true` default — the
setting it trains under. A `PerfectInfo bool` preserves the
`playerId==decisionPlayerId || perfectInfo` hand branch
(`StateEncoder.java:601-608`).

Traversal order is load-bearing because it drives `#n` cardinality. Mirror:

- Battlefield: `TreeMap` sorted by permanent `getValue(game, playerId)`
  (`StateEncoder.java:330-340`).
- Graveyard / hand / exile zones: `getCardsSorted`.
- Stack: iterator depth order (`StateEncoder.java:413-424`).
- Command zone: `game.getState().getCommand()` order, then helper emblems.
- `cleanString` (`StateEncoder.java:682-689`): strip ` [hex]` UUID tags and
  `<...>`.

Feature families emitted (each is either ported or on the explicit
`unsupportedFeatures` register — §7):

- globals: turn-step/phase, `decisionType` (the `ActionEncoder.ActionType`
  ordinal: PRIORITY 0, CHOOSE_NUM 1, BLANK 2, CHOOSE_TARGET 3, MAKE_CHOICE 4,
  CHOOSE_USE 5 — the enum is needed even though action *indexing* is out of
  scope), `decisionsText`.
- Stack: per object — depth, `isController`, stack-ability rule, targets,
  `Kicks`, `*_CostTag`, modes, `XValue`.
- Exile: per zone.
- Player / Opponent (both seats): micro-decision sequences (`ChosenTargets`,
  `ChosenChoices`, `UseChoices`, `AmountChoices`), `InPayManaMode`,
  `Activating`, `IsActivePlayer`, `IsDecisionPlayer`, `LifeTotal`,
  `CanPlayLand`, day/night, `LibraryCount`, attachments, player counters,
  `ManaPool` (six colours + conditional), Battlefield, Graveyard, Hand (or
  `CardsInHand`), CommandZone, GlobalWatchers.
- Permanent: static card (types, colours, subtypes, mana cost/value),
  `_dynamic` type/colour/subtype, `DynamicPermAbilities`, attachments,
  imprinted, paired, own exile zone, `TargetedBy`+`StackDepth`, flags
  (tapped/flipped/morphed/cloaked/suspected/renowned/monstrous/RingBearer/
  Room doors …), creature (`SummoningSick`, `CanAttack`, `CanBlock`,
  `Attacking`, blocker names, `Damage`, `Power`, `Toughness`).
- Watchers: `SpellsCastThisTurn`, `LifeGainedThisTurn`, `LifeLostThisTurn`,
  `TokensCreatedThisTurn`.

**String policy** (documented, measured in §7): Forge-native strings gorGE has
are emitted directly; XMage-only strings use a documented analogue where one
exists, else the feature is not emitted and the family is counted.

## 6. Oracle harness

`testdata/javaharness/FeaturesProbe.java` compiles against the vendored
`Features.java` (log4j is the only dependency; `Features` imports no XMage
code). It reads a scripted op sequence on stdin — the exact
`addFeature` / `addNumericFeature` / `getSubFeatures` / `uuid` calls — and
prints `{ "ops": [...], "indices": [...] }`.

A golden file `testdata/golden/<name>.json` holds both the op sequence and the
Java-computed index set. `oracle_test.go` replays the ops through the Go engine
and asserts **set equality of ids** — this is the byte-identical gate.

Regeneration is manual: `go test ./internal/mzenc -run TestHashOracle
-update` shells to `javac`/`java` when `MZENC_JAVA=1` and rewrites the golden
files; otherwise the test only consumes the committed JSON. The normal suite
never needs a JVM.

Vectors to include: single feature; repeated feature (cardinality `#1..#n`);
numeric at each breakpoint boundary (0, 19, 20, 31, 32, 50, 512, 513);
nested sub-features with `passToParent` true/false; a uuid-keyed feature;
negative-hash/`MinInt64` lattice; and a handful of captured *real* feature
sequences lifted from `StateEncoder` (hand-built op lists standing in for a
gorge state) to exercise deep namespace chains.

## 7. Testing and ratchet

- `TestHashOracle` — golden vectors, exact id-set equality. Hard gate.
- `TestThermometer`, `TestOccurrenceCardinality`, `TestIndexLattice` — unit
  expectations.
- `TestExtractorCoverage` — walks a gorGE game and asserts every MageZero
  feature family from §5 is either emitted or named in an
  `unsupportedFeatures` list; a new unhandled family fails. Mirrors the
  `knownUnsupported` ratchet, in both directions.
- `TestNoClockNoRand` — nothing in the package reads a wall clock or ambient
  randomness (gorGE hard rule); the encoder is pure.

Every test fits the operator budget (2 GB RSS, 2 vCPU, 1 min) and runs under
the capped `systemd-run` invocation.

## 8. Milestones

1. Hash engine + oracle harness + golden gate green (`features.go`,
   `oracle_test.go`).
2. Walkers for the static/global families (globals, players, battlefield,
   graveyard, hand, mana, stack skeleton) + coverage ratchet seeded.
3. Remaining families (attachments, imprinted, paired, exile zones,
   `TargetedBy`, watchers, command zone) + measured extractor-vs-XMage capture.

## 9. Open items deferred

- Action/option policy encoding (`ActionEncoder`, Java `String.hashCode`,
  4-head option vector) — separate spec.
- The ONNX/GNN serving question — separate work; this package only produces
  the state input.
- A live XMage `StateEncoder` oracle (approach B) — deferred unless
  extractor-vs-XMage agreement turns out too low to trust the measurement.
