# mzenc StateEncoder walkers Implementation Plan (Milestones 2–3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port MageZero v0.2's `StateEncoder` walkers into `internal/mzenc` so a gorGE game state produces the MageZero feature-id set (Milestone 1's `Features` hash engine already produces byte-identical ids).

**Architecture:** A new file `internal/mzenc/state.go` walks a `view.View` — the engine's projection of `state.Game` — calling the Milestone-1 `Features` tree exactly as upstream `StateEncoder.java` (`/tmp/mzref/StateEncoder.java`, `WillWroble/mage @ cb7e9c6f`) walks the XMage `Game`. Traversal order is load-bearing (it drives `#n` occurrence cardinality), so each walker mirrors its Java counterpart's order. Feature families the gorGE `view.View` cannot expose are NOT emitted; they are named in an `unsupportedFeatures` register with a `TestExtractorCoverage` ratchet that fails in both directions (a new unemitted family fails; a registered family that becomes emittable fails as stale). The walker takes `view.View` + an optional `view.Chars`, mirroring `internal/policynet/features.go`, so tests build synthetic `View` literals and need no card corpus.

**Tech Stack:** Go 1.25 (`github.com/adams-shaun/gorge`); packages `internal/mzenc` (new walker), `view`, `state`; `internal/policynet/features.go` is the reference for how a gorGE `view.View` maps to MageZero-style features.

## Global Constraints

- Pure Go, **no cgo, no third-party dependencies** in `internal/mzenc`.
- No wall clock, no ambient randomness, no `map` range order that can reach an id. Replace every unsorted map iteration with a sorted key walk before emitting (upstream uses `TreeMap`/`getCardsSorted`; gorGE must sort explicitly).
- The byte-identical gate from Milestone 1 (`TestHashOracle`) must stay green; this plan adds NO new hashing, only walks that feed the existing `Features` tree.
- Tests fit the operator budget: 2 GB RSS, 2 vCPU, 1 min wall. Run focused and capped:
  `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run <TestName> ./internal/mzenc`
  Never `go test ./...`, never `-count=1`.
- Conventional Commits (`feat(mzenc): …`, `test(mzenc): …`). Work in the worktree; `.cards` is symlinked; the walker tests need no corpus (synthetic `view.View`).
- `decisionType` is the `ActionEncoder.ActionType` ordinal: PRIORITY 0, CHOOSE_NUM 1, BLANK 2, CHOOSE_TARGET 3, MAKE_CHOICE 4, CHOOSE_USE 5. Action/option indexing is OUT OF SCOPE.
- **Reference:** `/tmp/mzref/StateEncoder.java` (723 lines) is the port source. Re-fetch if absent:
  `curl -fsSL https://raw.githubusercontent.com/WillWroble/mage/cb7e9c6f/Mage.Server.Plugins/Mage.Player.AI/src/main/java/mage/player/ai/encoder/StateEncoder.java -o /tmp/mzref/StateEncoder.java`

## File Structure

- `internal/mzenc/state.go` — **the walkers** (create). Public entry + all `process*` helpers.
- `internal/mzenc/state_test.go` — **walker unit tests** (create). Synthetic `view.View` literals; assert on `ProcessState`'s returned id set via the `Features` engine's `FeatureMap` reverse lookup, or by recomputing expected ids with a local `Features` build.
- `internal/mzenc/coverage.go` — **the `unsupportedFeatures` register and its doc** (create).
- `internal/mzenc/coverage_test.go` — **`TestExtractorCoverage`** (create): the both-directions ratchet.
- `internal/mzenc/state_stack.go` — **stack/command/exile walkers** (create at Milestone 3), kept out of `state.go` so each file stays holdable in context.

Shared internal type used by every task (define once, in Task 1):

```go
// walker carries one ProcessState call's state: the encoder being fed, the
// seat the state is encoded for, and the per-card name index for attachments.
type walker struct {
    e    *Encoder
    seat state.PlayerID
    // names maps a battlefield object id to its card name, for the
    // "X Blocking" / "attached to X" families (upstream game.getPermanent).
    names map[state.ObjID]string
    // unsupported records a family name the view cannot expose, once per call.
    unsupported map[string]bool
}
```

`ProcessState` (the one public entry; Consumes from Milestone 1, Produces for every task):

```go
// DefaultTableSize is MageZero v0.2's Features.TABLE_SIZE (Integer.MAX_VALUE).
const DefaultTableSize int64 = 2_147_483_647

// ProcessState walks an omniscient gorGE view and returns the MageZero
// feature-id set for the given decision. ch may be nil (derived facts
// degrade, mirroring view's own nil-Chars contract). decisionType is the
// ActionEncoder.ActionType ordinal; decisionsText is hashed as one feature
// (cleanString applied). The result matches StateEncoder.processState's
// id-set shape: a set, so colliding ids collapse.
func ProcessState(v view.View, ch view.Chars, seat state.PlayerID, decisionType int, decisionsText string) map[int32]struct{}
```

---

### Task 1: Walker skeleton, `cleanString`, decision-type names, and the globals family

**Files:**
- Create: `internal/mzenc/state.go`
- Test: `internal/mzenc/state_test.go`

**Interfaces:**
- Consumes: `NewEncoder(table int64) *Encoder`, `(*Encoder).Root() *Node`, `(*Encoder).IDs() map[int32]struct{}`, `(*Node).AddFeature(string)`, `(*Node).SubFeatures(string, bool) *Node` (Milestone 1, `internal/mzenc/features.go`).
- Produces: `const DefaultTableSize int64`, `func ProcessState(v view.View, ch view.Chars, seat state.PlayerID, decisionType int, decisionsText string) map[int32]struct{}`, `func cleanString(string) string`, `var actionTypeNames [6]string`, and the `walker` struct.

- [ ] **Step 1: Write the failing test**

```go
package mzenc

import (
    "testing"

    "github.com/adams-shaun/gorge/state"
    "github.com/adams-shaun/gorge/view"
)

// idsFor is the test helper: rebuild a Features tree with the SAME public
// operations ProcessState must perform, and return its id set, so a test can
// assert the walker emitted exactly the expected families. Built per test;
// no corpus, no game.
func idsFor(build func(f *Node)) map[int32]struct{} {
    e := NewEncoder(DefaultTableSize)
    build(e.Root())
    return e.IDs()
}

func TestProcessStateGlobals(t *testing.T) {
    v := view.View{Viewer: 0, Step: "StepMain1", Phase: "main1"}
    got := ProcessState(v, nil, 0, 0, "priority")

    // upstream processState (StateEncoder.java:634-641): a step feature, the
    // decisionType name, and the cleaned decisionsText, all at the root.
    want := idsFor(func(f *Node) {
        f.AddFeature("MAIN1") // see Step: the ActionType/step name mapping below
        f.AddFeature("PRIORITY")
        f.AddFeature("priority")
    })
    for id := range want {
        if _, ok := got[id]; !ok {
            t.Fatalf("missing global id %d (want %v got %v)", id, want, got)
        }
    }
}

func TestCleanStringStripsUUIDTagsAndAngleBrackets(t *testing.T) {
    cases := map[string]string{
        "Lightning Bolt [1a2b3c]": "Lightning Bolt",
        "<b>Flying</b>":           "Flying",
        "plain":                   "plain",
        "":                        "",
    }
    for in, want := range cases {
        if got := cleanString(in); got != want {
            t.Errorf("cleanString(%q)=%q want %q", in, got, want)
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestProcessStateGlobals|TestCleanString' ./internal/mzenc`
Expected: FAIL — `undefined: ProcessState`, `undefined: cleanString`, `undefined: DefaultTableSize`.

- [ ] **Step 3: Write minimal implementation**

```go
package mzenc

import (
    "regexp"

    "github.com/adams-shaun/gorge/state"
    "github.com/adams-shaun/gorge/view"
)

// DefaultTableSize is MageZero v0.2's Features.TABLE_SIZE (Integer.MAX_VALUE).
const DefaultTableSize int64 = 2_147_483_647

// actionTypeNames mirrors ActionEncoder.ActionType.toString(), the six
// decision-type feature names (StateEncoder.processState:639).
var actionTypeNames = [6]string{"PRIORITY", "CHOOSE_NUM", "BLANK", "CHOOSE_TARGET", "MAKE_CHOICE", "CHOOSE_USE"}

// stepName maps a gorGE Step to the upstream TurnStepType name used as a
// global phase feature. Only the steps StateEncoder emits a name for are
// listed; an unmapped step emits nothing (upstream guards on getPhase()!=null).
var stepName = map[state.Step]string{
    state.StepUntap:            "UNTAP",
    state.StepUpkeep:           "UPKEEP",
    state.StepDraw:             "DRAW",
    state.StepMain1:            "MAIN1",
    state.StepBeginCombat:      "BEGIN_COMBAT",
    state.StepDeclareAttackers: "DECLARE_ATTACKERS",
    state.StepDeclareBlockers:  "DECLARE_BLOCKERS",
    state.StepCombatDamage:     "COMBAT_DAMAGE",
    state.StepEndCombat:        "END_COMBAT",
    state.StepMain2:            "PRECOMBAT_MAIN", // upstream's second-main name
    state.StepEnd:              "END",
    state.StepCleanup:          "CLEANUP",
}

var (
    uuidTagRE = regexp.MustCompile(" [0-9a-f]+")
    angleRE   = regexp.MustCompile("<[^>]*>")
)

// cleanString ports StateEncoder.cleanString (StateEncoder.java:682-689):
// remove " [hex]" UUID tags and every <...> span.
func cleanString(s string) string {
    if s == "" {
        return s
    }
    s = uuidTagRE.ReplaceAllString(s, "")
    return angleRE.ReplaceAllString(s, "")
}

type walker struct {
    e           *Encoder
    seat        state.PlayerID
    names       map[state.ObjID]string
    unsupported map[string]bool
}

// ProcessState walks an omniscient gorGE view and returns the MageZero
// feature-id set for the given decision.
func ProcessState(v view.View, ch view.Chars, seat state.PlayerID, decisionType int, decisionsText string) map[int32]struct{} {
    e := NewEncoder(DefaultTableSize)
    root := e.Root()
    w := &walker{e: e, seat: seat, unsupported: map[string]bool{}}
    // globals (StateEncoder.java:634-641)
    if name, ok := stepName[state.Step(v.Step)]; ok {
        root.AddFeature(name)
    }
    if decisionType >= 0 && decisionType < len(actionTypeNames) {
        root.AddFeature(actionTypeNames[decisionType])
    }
    root.AddFeature(cleanString(decisionsText))
    return e.IDs()
}
```

Note: `state.Step(v.Step)` — the view stores `Step string`. If `view.View.Step` is already `string`, parse via a lookup table keyed by string instead: build `stepNameByString` from `stepName` with `state.Step`'s `String()` values, OR change the map key to the string the view carries. Confirm `view.View.Step`'s type in `view/view.go:112` (it is `string`), so key the map by string:

```go
var stepName = map[string]string{"untap": "UNTAP", /* … the Step.String() spellings … */}
```

Populate it from `state.Step`'s own `String()` method for every step that maps, so the keys cannot drift (a `state`-side `TestStepNameCoversAll` asserts every `state.Step` constant that `PhaseOf` groups is either mapped or explicitly listed as intentionally-omitted).

- [ ] **Step 4: Run tests to verify they pass**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestProcessStateGlobals|TestCleanString' ./internal/mzenc`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/state.go internal/mzenc/state_test.go
git commit -m "feat(mzenc): walker skeleton, cleanString and the globals family"
```

---

### Task 2: The per-player scalar families (life, library, hand count, mana pool, counters)

**Files:**
- Modify: `internal/mzenc/state.go`
- Test: `internal/mzenc/state_test.go`

**Interfaces:**
- Consumes: `walker`, `ProcessState`, `cleanString` (Task 1).
- Produces: `func (w *walker) processPlayer(f *Node, pv *view.PlayerView, isDecisionPlayer bool)`; `func (w *walker) processManaPool(f *Node, pool map[string]int32)`.

- [ ] **Step 1: Write the failing test**

```go
func TestProcessStatePlayerScalars(t *testing.T) {
    v := view.View{
        Players: []view.PlayerView{
            {ID: 0, Life: 20, LibrarySize: 53, HandSize: 7, Pool: map[string]int32{"W": 2}},
            {ID: 1, Life: 18, LibrarySize: 60, HandSize: 5},
        },
    }
    got := ProcessState(v, nil, 0, 0, "x")

    want := idsFor(func(f *Node) {
        me := f.SubFeatures("Player", true)
        me.AddNumericFeature("LifeTotal", 20, true)
        me.AddNumericFeature("LibraryCount", 53, true)
        me.AddNumericFeature("CardsInHand", 7, true)
        me.AddFeature("IsActivePlayer", true)
        me.AddFeature("IsDecisionPlayer", true)
        mp := me.SubFeatures("ManaPool", false)
        mp.AddNumericFeature("WhiteMana", 2, true)
        opp := f.SubFeatures("Opponent", true)
        opp.AddNumericFeature("LifeTotal", 18, true)
        opp.AddNumericFeature("LibraryCount", 60, true)
        opp.AddNumericFeature("CardsInHand", 5, true)
    })
    for id := range want {
        if _, ok := got[id]; !ok {
            t.Fatalf("missing player id %d", id)
        }
    }
}
```

Note: with `perfectInfo` true (the default) upstream walks every hand (StateEncoder.java:601-604); a hand family is Task 4. Here, because `v.Players[].Hand` is empty, `CardsInHand` is the correct fallback only when the hand is NOT walked. Keep this task's implementation: walk `Hand` (Task 4) when non-nil-or-perfect, else emit `CardsInHand`. For a nil `ch` and empty hands this test's `CardsInHand` expectation holds.

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStatePlayerScalars ./internal/mzenc`
Expected: FAIL — missing ids (no player walk yet).

- [ ] **Step 3: Write minimal implementation**

Port `processPlayer` (StateEncoder.java:543-619) for the scalar families only. The two players are walked under the EXACT subtrees `"Player"` (the seat) and `"Opponent"` (the other seat), in `v.Players` order; add `IsActivePlayer` when `pv.ID == v.Active`, `IsDecisionPlayer` when `pv.ID == seat` (upstream compares `decisionPlayerId`). `ManaPool` is `f.SubFeatures("ManaPool", false)` then `processMana` (StateEncoder.java:438-444) mapping gorGE pool keys `W/U/B/R/G/C` to `WhiteMana/BlueMana/BlackMana/RedMana/GreenMana/ColorlessMana` via the fixed order in `policynet`'s `poolKeys`. Player counters are not exposed by `view.PlayerView` — record `w.unsupported["PlayerCounters"]` and emit nothing.

- [ ] **Step 4: Run test to verify it passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStatePlayerScalars ./internal/mzenc`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/state.go internal/mzenc/state_test.go
git commit -m "feat(mzenc): per-player scalar families (life, library, hand count, mana pool)"
```

---

### Task 3: The battlefield permanent family

**Files:**
- Modify: `internal/mzenc/state.go`
- Test: `internal/mzenc/state_test.go`

**Interfaces:**
- Consumes: `walker`, `cleanString`.
- Produces: `func (w *walker) processBattlefield(f *Node, pv *view.PlayerView, ch view.Chars)`; `func (w *walker) processPerm(f *Node, cv *view.CardView, ch view.Chars)`; `func keywordOf(cv *view.CardView, kw string) bool`.

- [ ] **Step 1: Write the failing test**

```go
func TestProcessStateBattlefieldDeterministicOrder(t *testing.T) {
    mk := func(id state.ObjID, name string, tapped bool) view.CardView {
        return view.CardView{ID: id, Name: name, Types: "Creature", Tapped: tapped,
            Power: 2, Toughness: 3, Keywords: []string{"Flying"}}
    }
    // Two battlefields; within one, names must be walked SORTED, so the
    // occurrence keys "#1"/"#2" are stable regardless of slice order.
    v := view.View{Players: []view.PlayerView{
        {ID: 0, Battlefield: []view.CardView{mk(10, "Grizzly Bears", false), mk(11, "Alpha", true)}},
        {ID: 1, Battlefield: nil},
    }}
    a := ProcessState(v, nil, 0, 0, "x")
    // swap the two permanents; the id SET must be identical (sorted walk).
    v.Players[0].Battlefield[0], v.Players[0].Battlefield[1] = v.Players[0].Battlefield[1], v.Players[0].Battlefield[0]
    b := ProcessState(v, nil, 0, 0, "x")
    if len(a) != len(b) {
        t.Fatalf("order-dependent id set: %d vs %d", len(a), len(b))
    }
    for id := range a {
        if _, ok := b[id]; !ok {
            t.Fatalf("order-dependent id set: id %d only in first", id)
        }
    }
    // The exact subtree the walk must emit for the tapped Alpha creature.
    want := idsFor(func(f *Node) {
        bf := f.SubFeatures("Battlefield", true)
        pa := bf.SubFeatures("Alpha", true)
        pa.AddFeature("Tapped", true)
        pa.AddFeature("creature", true) // type word, lowercased
        pa.AddNumericFeature("Power", 2, true)
        pa.AddNumericFeature("Toughness", 3, true)
        pg := bf.SubFeatures("Grizzly Bears", true)
        pg.AddNumericFeature("Power", 2, true)
    })
    for id := range want {
        if _, ok := a[id]; !ok {
            t.Fatalf("missing battlefield id %d", id)
        }
    }
}
```

`a` is the pre-swap result — assert on it; the swap proves order-independence of `b`.

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateBattlefieldDeterministicOrder ./internal/mzenc`
Expected: FAIL — no `Battlefield` subtree emitted (a and b both trivial but the spot-check fails / missing ids).

- [ ] **Step 3: Write minimal implementation**

Port `processBattlefield` (StateEncoder.java:330-340): sort permanents by `Name` then `ID` (upstream sorts by `getValue`, a name+id key), `f.SubFeatures(name, true)` per permanent, then `processPermBattlefield` (177-305) for the view-exposed subset:
- `Tapped` (line 181)
- static card features via `processCard` (Task 4's `Card`, card types, mana value) — call the shared `processCard` after Task 4 lands; in this task emit `f.AddFeature(strings.ToLower(t))` for each `strings.Fields(cv.Types)` word as the gorGE analogue of `ct.name()`.
- creature facts (289-304): `SummoningSick` when `cv.SummonSick`; `Attacking` when `cv.Attacking`; for each `cv.BlockedBy` name from `w.names`, `f.AddFeature(blockerName + " Blocking")`; `AddNumericFeature("Damage", cv.Damage)`, `"Power"`, `"Toughness"`.
- keywords: for each `cv.Keywords`, `f.AddFeature(strings.ToLower(kw))` (gorGE analogue of the ability-rule token).
- **Not exposable through `view.CardView` — record in `w.unsupported` and emit nothing:** `*_dynamic` types/subtypes/colours (no dynamic type list), dynamic abilities (no ability list), `CanAttack`/`CanBlock` (no engine predicate), flipped/harnessed/solved/suspected/RingBearer/Renowned/Monstrous/Cloaked/Disguised/Morphed/Room-door flags (not projected). Colour features (`RedCard` etc.) are not projected either — record `Colors`.
- Attachments/imprinted/paired/TargetedBy/permanent-exile are Milestone 3 (Task 8); record their family names now so the ratchet is honest.

- [ ] **Step 4: Run test to verify it passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateBattlefieldDeterministicOrder ./internal/mzenc`
Expected: PASS (order-independence).

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/state.go internal/mzenc/state_test.go
git commit -m "feat(mzenc): battlefield permanent family with sorted traversal"
```

---

### Task 4: `processCard` / `processCardInZone` and the graveyard + hand families

**Files:**
- Modify: `internal/mzenc/state.go`
- Test: `internal/mzenc/state_test.go`

**Interfaces:**
- Consumes: `walker`, `cleanString`, `keywordOf`.
- Produces: `func (w *walker) processCard(f *Node, cv *view.CardView, passToParent bool)`; `func (w *walker) processCardInZone(f *Node, cv *view.CardView, zone string, ch view.Chars)`; `func (w *walker) processGraveyard(f *Node, pv *view.PlayerView, ch view.Chars)`; `func (w *walker) processHand(f *Node, pv *view.PlayerView, ch view.Chars)`.

- [ ] **Step 1: Write the failing test**

```go
func TestProcessStateHandAndGraveyard(t *testing.T) {
    v := view.View{Players: []view.PlayerView{
        {ID: 0,
            Hand:      []view.CardView{{ID: 20, Name: "Counterspell", Types: "Instant", ManaCost: "U U"}},
            Graveyard: []view.CardView{{ID: 21, Name: "Bolt", Types: "Instant", ManaCost: "R"}},
        },
        {ID: 1, Hand: []view.CardView{{ID: 22, Name: "Forest", Types: "Land"}}},
    }}
    got := ProcessState(v, nil, 0, 0, "x")

    want := idsFor(func(f *Node) {
        me := f.SubFeatures("Player", true)
        h := me.SubFeatures("Hand", true)
        hc := h.SubFeatures("Counterspell", true)
        hc.AddFeature("Card", true)
        hc.AddFeature("instant", true)
        hc.AddNumericFeature("ManaValue", 2, true)
        gy := me.SubFeatures("Graveyard", true)
        gc := gy.SubFeatures("Bolt", true)
        gc.AddFeature("Card", true)
        gc.AddFeature("instant", true)
        // perfectInfo: the opponent's hand is walked too.
        oh := f.SubFeatures("Opponent", true).SubFeatures("Hand", true)
        oc := oh.SubFeatures("Forest", true)
        oc.AddFeature("Card", true)
        oc.AddFeature("land", true)
    })
    for id := range want {
        if _, ok := got[id]; !ok {
            t.Fatalf("missing hand/graveyard id %d", id)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateHandAndGraveyard ./internal/mzenc`
Expected: FAIL — no `Hand`/`Graveyard` subtrees.

- [ ] **Step 3: Write minimal implementation**

Port `processCard` (134-175) and `processCardInZone` (306-329):
- `processCard`: add `"Card"`; `"Permanent"` when the type line contains `Creature`/`Artifact`/`Enchantment`/`Land`/`Planeswalker`/`Battle` (upstream `c.isPermanent()`); type words; subtype words (none exposed — record `Subtypes` unsupported); `AddNumericFeature("ManaValue", mv)` where `mv` is computed from `cv.ManaCost` (reuse `policynet`'s `mvOf` logic; port a local `manaValue(string) int` — see `internal/policynet/option.go` for the parser to port, or a minimal Forge-notation counter). `passToParent` mirrors upstream's `f.passToParent` guard.
- `processCardInZone`: `processCard` then the zone's static/activated/triggered ability walks — **not exposable** (the view carries no ability list), so record `w.unsupported["CardAbilities"]` and emit only the card features.
- `processGraveyard`/`processHand`: sort by `Name` then `ID` (upstream `getCardsSorted`), `f.SubFeatures(name, true)` per card, `processCardInZone`.
- In `processPlayer` (Task 2), call `processHand` for EVERY player when `perfectInfo` (the plan's fixed `true`, StateEncoder.java:601); otherwise emit `CardsInHand`. Since the public entry has no `perfectInfo` arg (it is always omniscient), always walk the hand.

- [ ] **Step 4: Run test to verify it passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateHandAndGraveyard ./internal/mzenc`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/state.go internal/mzenc/state_test.go
git commit -m "feat(mzenc): processCard/processCardInZone and hand/graveyard families"
```

---

### Task 5: The stack family skeleton

**Files:**
- Modify: `internal/mzenc/state_stack.go` (create), `internal/mzenc/state.go`
- Test: `internal/mzenc/state_test.go`

**Interfaces:**
- Consumes: `walker`, `cleanString`.
- Produces: `func (w *walker) processStack(f *Node, v *view.View)`; `func (w *walker) processStackObject(f *Node, sv *view.StackView, depth int)`.

- [ ] **Step 1: Write the failing test**

```go
func TestProcessStateStackDepth(t *testing.T) {
    v := view.View{Stack: []view.StackView{
        {ID: 100, Name: "Lightning Bolt"},
        {ID: 101, Name: "Counterspell"},
    }}
    got := ProcessState(v, nil, 0, 0, "x")
    want := idsFor(func(f *Node) {
        st := f.SubFeatures("Stack", false)
        a := st.SubFeatures("Lightning Bolt", true)
        a.AddNumericFeature("Depth", 1, false)
        b := st.SubFeatures("Counterspell", true)
        b.AddNumericFeature("Depth", 2, false)
    })
    for id := range want {
        if _, ok := got[id]; !ok {
            t.Fatalf("missing stack id %d", id)
        }
    }
}
```

Read `view/stack.go` first: confirm the exact `StackView` field names used above (`ID`, `Name`, and a `Card *view.CardView`). Adjust the literal to the real fields; if `Name` is not a direct field, use `sv.Card.Name`.

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateStackDepth ./internal/mzenc`
Expected: FAIL — no `Stack` subtree.

- [ ] **Step 3: Write minimal implementation**

Port `processStack` (413-424): iterate `v.Stack` bottom→top (index 0 is the bottom, `view/view.go:129`), `depth++`, `f.SubFeatures(cleanString(name), true)`, `AddNumericFeature("Depth", depth, false)`, then `processStackObject` (353-411) for the view-exposed subset: `"isController"` when the stack object's controller == `w.seat`; `SpellAPI` as `f.parent.AddFeature(...)` analogue — emit `f.AddFeature(cv.SpellAPI)` when non-empty; targets (no target list in the view) → record `w.unsupported["StackTargets"]`; `Kicks`/cost-tags/modes/`XValue`/triggered-vs-activated → record `w.unsupported["StackAbilityDetail"]`.

- [ ] **Step 4: Run test to verify it passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateStackDepth ./internal/mzenc`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/state_stack.go internal/mzenc/state.go internal/mzenc/state_test.go
git commit -m "feat(mzenc): stack family skeleton with depth"
```

---

### Task 6: The `unsupportedFeatures` register and the `TestExtractorCoverage` ratchet

**Files:**
- Create: `internal/mzenc/coverage.go`, `internal/mzenc/coverage_test.go`
- Modify: `internal/mzenc/state.go` (call `w.unsupported[family]=true` at each unemittable family)

**Interfaces:**
- Consumes: `walker.unsupported`, `ProcessState`.
- Produces: `var unsupportedFeatures = map[string]string{ /* family → reason */ }`; `func walkerUnsupported(v view.View, seat state.PlayerID) map[string]bool` (test-only accessor, or a `ProcessStateReport` returning both ids and the unsupported set).

- [ ] **Step 1: Write the failing test**

```go
func TestExtractorCoverageRatchetMatches(t *testing.T) {
    // The families the SPEC §5 names, each either emitted by a non-empty
    // synthetic view or present in unsupportedFeatures. Both directions:
    // an unregistered-but-unemitted family fails; a registered family that
    // the walker now emits fails as stale.
    v := coverageView() // a synthetic View exercising every emit path
    emitted, unsupported := ProcessStateReport(v, nil, 0, 0, "x")
    for fam := range unsupportedFeatures {
        if emitted[fam] {
            t.Errorf("stale register entry %q: walker now emits it", fam)
        }
    }
    for fam := range emitted {
        if !specFamilies[fam] {
            t.Errorf("walker emitted unknown family %q not in spec §5", fam)
        }
    }
}
```

`ProcessStateReport(view, ch, seat, decisionType, text) (emitted map[string]bool, unsupported map[string]bool)` is produced here: it runs the same walk as `ProcessState` but records which families it added (a thin wrapper toggling a per-family flag inside each `process*`). Keep it in `coverage.go`, not on the hot path.

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestExtractorCoverageRatchetMatches ./internal/mzenc`
Expected: FAIL — `undefined: ProcessStateReport`, `undefined: unsupportedFeatures`, `undefined: specFamilies`.

- [ ] **Step 3: Write minimal implementation**

Create `coverage.go`: the `unsupportedFeatures` map (family → the gorGE accessor that is missing, e.g. `"Colors": "view.CardView exposes ManaCost, not a colour set"`), the `specFamilies` set transcribed from the mzenc design §5, and `ProcessStateReport` wrapping the walk. Seed `unsupportedFeatures` with the families Tasks 2–5 recorded (PlayerCounters, Colors, Subtypes, DynamicTypes, DynamicAbilities, CanAttack, CanBlock, PermanentFlags, Attachments, Imprinted, Paired, TargetedBy, PermanentExile, CardAbilities, StackTargets, StackAbilityDetail, Watchers, CommandZone, MicroDecisions, Exile, DayNight, CanPlayLand) — every one mirrored by an explicit `w.unsupported[...]` at its walk site. The test's both-directions check is the enforcement.

- [ ] **Step 4: Run test to verify it passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestExtractorCoverageRatchetMatches ./internal/mzenc`
Expected: PASS. Log line: the live count of unsupported families.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/coverage.go internal/mzenc/coverage_test.go internal/mzenc/state.go
git commit -m "feat(mzenc): unsupportedFeatures register and the coverage ratchet"
```

---

### Task 7 (Milestone 3): Static/global remainder — `processWatchers` analogues and the day/night + land-drop families

**Files:**
- Modify: `internal/mzenc/state.go`, `internal/mzenc/coverage.go`
- Test: `internal/mzenc/state_test.go`

**Interfaces:**
- Consumes: the Task 2 `processPlayer`, the Task 6 ratchet.
- Produces: `func (w *walker) processWatchers(f *Node, pv *view.PlayerView)`.

- [ ] **Step 1: Write the failing test**

```go
func TestProcessStateWatchersFromFields(t *testing.T) {
    // view.PlayerView carries no watcher counters today; confirm by reading
    // view/view.go. If absent, this test asserts the family is registered
    // unsupported and NOT emitted, and is the honest gate that blocks a
    // silent claim of support.
    v := coverageView()
    emitted, unsupported := ProcessStateReport(v, nil, 0, 0, "x")
    if emitted["GlobalWatchers"] && !unsupported["GlobalWatchers"] {
        t.Fatal("GlobalWatchers claimed emitted; no view field carries watcher counts")
    }
}
```

- [ ] **Step 2: Run test to verify it fails or passes-truthfully**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateWatchersFromFields ./internal/mzenc`
Expected: PASS if the register already lists `GlobalWatchers` (Task 6 seeded it); the point is the gate exists. If it FAILS, Task 6's seed is wrong — fix the register. No new emit code until a `view` field carries the counts; file that projection widening as a separate ticket and note it in the commit message.

- [ ] **Step 3: Commit (only if a code change was needed)**

```bash
git add internal/mzenc/state.go internal/mzenc/coverage.go
git commit -m "test(mzenc): pin GlobalWatchers as registered-unsupported, not silently dropped"
```

---

### Task 8 (Milestone 3): Attachments, imprinted, paired, exile zones, TargetedBy

**Files:**
- Modify: `internal/mzenc/state.go`, `internal/mzenc/coverage.go`
- Test: `internal/mzenc/state_test.go`

**Interfaces:**
- Consumes: `processCard`, `walk` helpers.
- Produces: `func (w *walker) processExile(f *Node, v *view.View)`; attachments/imprinted/paired walks inside `processPerm` when the view exposes them.

- [ ] **Step 1: Write the failing test**

```go
func TestProcessStateExileZones(t *testing.T) {
    v := view.View{Players: []view.PlayerView{
        {ID: 0, Exile: []view.CardView{{ID: 30, Name: "Exiled Card", Types: "Sorcery"}}},
    }}
    got := ProcessState(v, nil, 0, 0, "x")
    want := idsFor(func(f *Node) {
        ex := f.SubFeatures("Exile", true)
        // upstream nests by exile-zone name; the view exposes a flat list, so
        // the zone name is not available -- record unsupported, emit the card
        // directly under a fixed "ExileZone" subfeature.
        z := ex.SubFeatures("ExileZone", true)
        c := z.SubFeatures("Exiled Card", true)
        c.AddFeature("Card", true)
        c.AddFeature("sorcery", true)
    })
    for id := range want {
        if _, ok := got[id]; !ok {
            t.Fatalf("missing exile id %d", id)
        }
    }
}
```

The exact nesting (`ExileZone` fixed name vs per-zone) is a decision this task makes: the view is flat, so the per-zone nesting of StateEncoder.java:431-437 is NOT reproducible; record `w.unsupported["ExileZoneNames"]` and use one deterministic wrapper. Adjust the `want` subtree to the implementation.

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateExileZones ./internal/mzenc`
Expected: FAIL — no `Exile` subtree.

- [ ] **Step 3: Write minimal implementation**

Port `processExile`/`processExileZone` (425-437) with the flat-view caveat above. Attachments/imprinted/paired/TargetedBy remain unsupported (the view lacks `GetAttachments`/`GetImprinted`/`GetPairedCard`/stack-target lists); keep them in `unsupportedFeatures`, and remove the "PermanentExile" entry only when the flat exile walk lands.

- [ ] **Step 4: Run test to verify it passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateExileZones ./internal/mzenc`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/state.go internal/mzenc/coverage.go internal/mzenc/state_test.go
git commit -m "feat(mzenc): flat exile-zone family; pin attachments/imprinted/paired unsupported"
```

---

### Task 9 (Milestone 3): The full-package gate and the deterministic walk

**Files:**
- Test: `internal/mzenc/state_test.go`
- Modify: `internal/mzenc/doc.go` (record the walker's monkey-see coverage and the unsupported count)

**Interfaces:**
- Consumes: everything above.
- Produces: `TestProcessStateIsDeterministic`, `TestProcessStateNoMapRange`.

- [ ] **Step 1: Write the failing test**

```go
func TestProcessStateIsDeterministic(t *testing.T) {
    v := coverageView()
    a := ProcessState(v, nil, 0, 0, "x")
    b := ProcessState(v, nil, 0, 0, "x")
    if len(a) != len(b) {
        t.Fatalf("nondeterministic: %d vs %d", len(a), len(b))
    }
    for id := range a {
        if _, ok := b[id]; !ok {
            t.Fatalf("nondeterministic: id %d", id)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateIsDeterministic ./internal/mzenc`
Expected: PASS (the sorted walks make it deterministic). If FAIL, a `map` range reached an id — fix the walk, not the test.

- [ ] **Step 3: Run the whole package gate**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m ./internal/mzenc`
Expected: PASS — all Milestone 1 tests unchanged plus the new walker tests. Record in `doc.go` the live `unsupportedFeatures` count and the date.

- [ ] **Step 4: Commit**

```bash
git add internal/mzenc/state_test.go internal/mzenc/doc.go
git commit -m "test(mzenc): pin walker determinism and record the extractor coverage count"
```

---

### Task 10 (Milestone 3): The command-zone family

**Files:**
- Modify: `internal/mzenc/state.go`, `internal/mzenc/coverage.go`
- Test: `internal/mzenc/state_test.go`

**Interfaces:**
- Consumes: `walker`, `processCard`, `cleanString`.
- Produces: `func (w *walker) processCommandZone(f *Node, pv *view.PlayerView, ch view.Chars)`.

- [ ] **Step 1: Write the failing test**

```go
func TestProcessStateCommandZone(t *testing.T) {
    v := view.View{Players: []view.PlayerView{
        {ID: 0, Commanders: []view.CardView{{ID: 40, Name: "Krenko, Mob Boss", Types: "Creature"}}},
        {ID: 1},
    }}
    got := ProcessState(v, nil, 0, 0, "x")
    want := idsFor(func(f *Node) {
        me := f.SubFeatures("Player", true)
        cz := me.SubFeatures("CommandZone", false)
        com := cz.SubFeatures("Commander", true)
        com.AddFeature("Krenko, Mob Boss", true)
        c := com.SubFeatures("Krenko, Mob Boss", true)
        c.AddFeature("Card", true)
        c.AddFeature("creature", true)
    })
    for id := range want {
        if _, ok := got[id]; !ok {
            t.Fatalf("missing command-zone id %d", id)
        }
    }
}
```

Mirror the exact nesting the implementation emits (upstream `processCommandZone` at StateEncoder.java:458-497 puts a `"Commander"` subfeature, adds the commander NAME as a feature, then `processCard(commander sourceObject)`). Emblems: `view.PlayerView` exposes no emblem list, so record `w.unsupported["Emblem"]` and emit nothing.

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateCommandZone ./internal/mzenc`
Expected: FAIL — no `CommandZone` subtree.

- [ ] **Step 3: Write minimal implementation**

Port `processCommandZone` (458-497) for the commander roster: for each `pv.Commanders`, `cz := f.SubFeatures("CommandZone", false)`, `com := cz.SubFeatures("Commander", true)`, `com.AddFeature(cv.Name, true)`, then `processCard` on the commander CardView under `com.SubFeatures(cv.Name, true)`. Call it from `processPlayer` (Task 2) after the hand walk, matching upstream order. Emblems register unsupported. Remove the `CommandZone`/`Commander` entries Task 6 seeded from `unsupportedFeatures` (this task makes them emittable — the ratchet's both-directions check requires the deletion).

- [ ] **Step 4: Run test to verify it passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestProcessStateCommandZone ./internal/mzenc`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/state.go internal/mzenc/coverage.go internal/mzenc/state_test.go
git commit -m "feat(mzenc): command-zone commander family; register Emblem unsupported"
```

---

## Self-review

**Spec coverage (§5 families vs tasks):**

| §5 family | Task | Emitted or registered |
|---|---|---|
| globals (turn/step/phase, decisionType, decisionsText) | 1 | emitted |
| Stack object (depth, isController, spell API, targets, kicks, cost tags, modes, X) | 5 | depth/isController/API emitted; targets + ability detail registered |
| Exile per zone | 8 | flat emitted; zone names registered |
| Player/Opponent (life, library, hand, battlefield, graveyard, mana, counters, day/night, canPlayLand, micro-decisions, attachments, watchers) | 2,4 | life/library/hand/battlefield/graveyard/mana emitted; counters/day-night/canPlayLand/micro-decisions/attachments/watchers registered |
| Permanent (static card, dynamic types/colours/abilities, attachments, imprinted, paired, own exile, TargetedBy, flags, creature facts) | 3,4,8 | static card + creature facts emitted; dynamic/colours/flags/attachments/imprinted/paired/TargetedBy registered |
| Watchers | 7 | registered (no view field carries counts) |
| Command zone | 10 | commander roster emitted; Emblem registered |

Command-zone note: `v.Players[].Command` and `.Commanders` exist (`view/view.go:299-315`), so the commander-name family is emittable; emblems are not projected and are registered.

**Placeholder scan:** Tasks 3 and 5 have one deliberate "adjust to the real field" note (the `StackView` field names and the battlefield spot-check `want`), both because the exact `view` fields must be confirmed by reading `view/stack.go` at execution time; every other step carries concrete code.

**Type consistency:** `walker`, `ProcessState`, `ProcessStateReport`, `DefaultTableSize`, `cleanString`, `actionTypeNames`, `stepName`, `unsupportedFeatures`, `specFamilies` are used with the same signatures across tasks. `processCard`/`processCardInZone`/`processPerm`/`processBattlefield`/`processHand`/`processGraveyard`/`processStack`/`processStackObject`/`processExile` are the shared walker method names.

## Execution Handoff

Two execution options:

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — execute tasks in this session with checkpoints for review.
