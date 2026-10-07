# Mana-member carry (legal-walk §S4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cut per-object mana-ability membership recomputation on the live priority walk by carrying a seat's own-battlefield membership across its walks, so enginebench `-row random` / `-row bot` games/s rise.

**Architecture:** A new per-engine value cluster `manaMemberCarry` (`rules/mana_member_carry.go`) holds a dense `ObjID`-indexed entry table of deferred-payability mana-ability lists, keyed on the `BoardReadKey` board stamp plus `staticTouchGen`, `crossWalkRetires`, `turn` and a per-object touch generation. The priority walk's own-battlefield mana loop (`rules/legal_walk_battlefield.go`) reads the carry on a hit and populates it on a miss; the potential record `w.rec` is still fed on both paths. Payability is never cached — it is re-applied every walk. Verify mode recomputes every hit and panics.

**Tech Stack:** Go (no cgo, no third-party deps), gorge rules engine, enginebench.

## Global Constraints

- No cgo and no third-party dependencies in the card pipeline and rules core.
- All state mutation goes through `events.Apply`; the walk is a pure read (this change emits no event and writes no `state.Game` field).
- No nondeterminism: no wall clock, no ambient randomness, no `map` range that can reach an event.
- Every new Engine field carries a `clone:` tag; `TestClonePolicyEveryFieldTagged` must pass.
- Byte-identical offers: no option's order, contents, `Index`, label, `Mode`, `AltCostIndex` or `Cost` marker may change.
- Tests capped: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run X ./pkg`.
- Never `git add -A`; stage explicit paths.
- Never commit Forge card scripts.

---

### Task 1: The carry cluster — storage, key and lookup/store

**Files:**
- Create: `rules/mana_member_carry.go`
- Modify: `rules/engine_scratch.go` (add the cluster field)
- Test: `rules/mana_member_carry_test.go`

**Interfaces:**
- Produces:
  - `type manaMemberBoardStamp struct { lineage *events.Log; derivedSeq, staticTouchGen, crossWalkRetires uint64; continuousVersion int; tapeEpoch uint64; objs int; turn int32 }`
  - `type manaMemberEntry struct { gen, objTouch uint64; all []*cards.SA; n int32; set bool }`
  - `type manaMemberCarry struct { owner *Engine; entries []manaMemberEntry; touch []uint64; stamp manaMemberBoardStamp; stampSet bool; gen uint64; hits, misses uint64 }`
  - `func (c *manaMemberCarry) lookup(id state.ObjID, cur manaMemberBoardStamp, touch uint64) ([]*cards.SA, bool)`
  - `func (c *manaMemberCarry) store(id state.ObjID, all []*cards.SA, cur manaMemberBoardStamp, touch uint64)`
  - `func (c *manaMemberCarry) touchObj(i int)`
  - `func (e *Engine) manaBoardStamp() manaMemberBoardStamp`
  - `func (e *Engine) manaTouchOf(id state.ObjID) uint64`
  - `func (e *Engine) ownManaCarry() `
  - `func (e *Engine) manaTouchBumpIdx(i int)`
  - `func (e *Engine) manaTouchBumpAll()`
  - `func (e *Engine) manaMemberLookup(id state.ObjID) ([]*cards.SA, bool)`
  - `func (e *Engine) manaMemberStore(id state.ObjID, all []*cards.SA)`
  - `func (e *Engine) ManaCarryStats() (hits, misses uint64)`

- [ ] **Step 1: Write the failing test**

`rules/mana_member_carry_test.go`:

```go
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestManaMemberCarryRoundTripAndInvalidation(t *testing.T) {
	var c manaMemberCarry
	s1 := manaMemberBoardStamp{derivedSeq: 5, turn: 1}
	s2 := manaMemberBoardStamp{derivedSeq: 5, turn: 2}

	a := &cards.SA{Line: "a"}
	b := &cards.SA{Line: "b"}
	c.store(7, []*cards.SA{a, b}, s1, 0)

	got, ok := c.lookup(7, s1, 0)
	if !ok || len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("round trip: ok=%v got=%v", ok, got)
	}
	// A different board stamp is a miss.
	if _, ok := c.lookup(7, s2, 0); ok {
		t.Fatal("turn change must miss")
	}
	// A board change retires every entry until re-stored at s2.
	if _, ok := c.lookup(7, s1, 0); ok {
		t.Fatal("entry stored at s1 must not hit after the carry saw s2")
	}
	c.store(7, []*cards.SA{a}, s2, 0)
	if got, ok := c.lookup(7, s2, 0); !ok || len(got) != 1 {
		t.Fatalf("re-store: ok=%v got=%v", ok, got)
	}
	// A per-object touch is a miss.
	if _, ok := c.lookup(7, s2, 1); ok {
		t.Fatal("object touch must miss")
	}
	// An unknown object is a miss, not a panic.
	if _, ok := c.lookup(9999, s2, 0); ok {
		t.Fatal("unknown object must miss")
	}
}

func TestManaMemberCarryTouchObjBumps(t *testing.T) {
	var c manaMemberCarry
	c.touchObj(2) // id 3
	c.touchObj(2)
	if c.touch[2] != 2 {
		t.Fatalf("touch = %d, want 2", c.touch[2])
	}
	if c.touch[1] != 0 {
		t.Fatalf("untouched neighbour = %d, want 0", c.touch[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestManaMemberCarry ./rules`
Expected: FAIL with `undefined: manaMemberCarry`.

- [ ] **Step 3: Write minimal implementation**

`rules/mana_member_carry.go`:

```go
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// manaMemberCarry caches, per own-battlefield object, the deferred-payability
// mana-ability membership across a seat's priority walks (legal-walk design
// §S4). Payability is never cached: the walk re-applies it every time.
//
// Validity: the board stamp at the last write, plus the object's own touch
// generation. A board-stamp move retires every entry through gen, so an entry
// stored before the move can never satisfy a lookup after it.
type manaMemberBoardStamp struct {
	lineage           *events.Log
	derivedSeq        uint64
	staticTouchGen    uint64
	crossWalkRetires  uint64
	continuousVersion int
	tapeEpoch         uint64
	objs              int
	turn              int32
}

type manaMemberEntry struct {
	gen      uint64
	objTouch uint64
	all      []*cards.SA
	n        int32
	set      bool
}

type manaMemberCarry struct {
	owner   *Engine
	entries []manaMemberEntry
	touch   []uint64
	stamp   manaMemberBoardStamp
	stampSet bool
	gen     uint64
	hits    uint64
	misses  uint64
}

// sync moves the carry to cur, retiring every older entry by bumping gen.
func (c *manaMemberCarry) sync(cur manaMemberBoardStamp) {
	if !c.stampSet || c.stamp != cur {
		c.gen++
		c.stamp, c.stampSet = cur, true
	}
}

func (c *manaMemberCarry) entryFor(i int) *manaMemberEntry {
	if i < 0 {
		return nil
	}
	if i >= len(c.entries) {
		grown := make([]manaMemberEntry, i+1, i+1+i/2+8)
		copy(grown, c.entries)
		c.entries = grown
	}
	return &c.entries[i]
}

// lookup returns id's cached membership when its entry is live at (cur, touch).
func (c *manaMemberCarry) lookup(id state.ObjID, cur manaMemberBoardStamp, touch uint64) ([]*cards.SA, bool) {
	if id == 0 {
		return nil, false
	}
	c.sync(cur)
	i := int(id) - 1
	if i >= len(c.entries) {
		c.misses++
		return nil, false
	}
	en := &c.entries[i]
	if !en.set || en.gen != c.gen || en.objTouch != touch {
		c.misses++
		return nil, false
	}
	c.hits++
	return en.all[:en.n], true
}

// store records id's deferred membership under (cur, touch).
func (c *manaMemberCarry) store(id state.ObjID, all []*cards.SA, cur manaMemberBoardStamp, touch uint64) {
	if id == 0 {
		return
	}
	c.sync(cur)
	en := c.entryFor(int(id) - 1)
	if en == nil {
		return
	}
	en.all = append(en.all[:0], all...)
	en.n = int32(len(en.all))
	en.gen, en.objTouch, en.set = c.gen, touch, true
}

// touchObj bumps the per-object generation at index i (id i+1).
func (c *manaMemberCarry) touchObj(i int) {
	if i < 0 {
		return
	}
	if i >= len(c.touch) {
		grown := make([]uint64, i+1, i+1+i/2+8)
		copy(grown, c.touch)
		c.touch = grown
	}
	c.touch[i]++
}

// manaBoardStamp is the derived memo's cross-walk key plus the mana-specific
// counters. tapeEpoch covers a kernel restore that rewinds state under the
// same log (rules/board_read_key.go:35-38).
func (e *Engine) manaBoardStamp() manaMemberBoardStamp {
	return manaMemberBoardStamp{
		lineage: e.L, derivedSeq: e.derivedSeq, staticTouchGen: e.staticTouchGen,
		crossWalkRetires: e.crossWalkRetires, continuousVersion: e.continuousVersion,
		tapeEpoch: uint64(e.tapeEpoch), objs: len(e.G.Objs), turn: e.G.Turn,
	}
}

func (e *Engine) manaTouchOf(id state.ObjID) uint64 {
	if id == 0 {
		return 0
	}
	if i := int(id) - 1; i >= 0 && i < len(e.manaCarry.touch) {
		return e.manaCarry.touch[i]
	}
	return 0
}

// ownManaCarry gives a by-value Engine copy its own carry, so it never writes
// the original's arrays (the walkObjCls pattern).
func (e *Engine) ownManaCarry() {
	if e.manaCarry.owner != e {
		e.manaCarry = manaMemberCarry{owner: e}
	}
}

// manaTouchBumpIdx owns the carry then bumps object id i+1's generation. The
// owner guard is mandatory: a by-value Engine copy shares the carry's backing
// arrays while manaCarry.owner still points at the original, so a bare
// touch[i]++ would corrupt the original's carry (the ownWalkClasses reason).
func (e *Engine) manaTouchBumpIdx(i int) {
	e.ownManaCarry()
	e.manaCarry.touchObj(i)
}

// manaTouchBumpAll owns the carry then bumps every object's generation -- the
// per-object twin of walkClassDropAll's staticTouchGen bump.
func (e *Engine) manaTouchBumpAll() {
	e.ownManaCarry()
	for i := range e.manaCarry.touch {
		e.manaCarry.touch[i]++
	}
}

func (e *Engine) manaMemberLookup(id state.ObjID) ([]*cards.SA, bool) {
	e.ownManaCarry()
	return e.manaCarry.lookup(id, e.manaBoardStamp(), e.manaTouchOf(id))
}

func (e *Engine) manaMemberStore(id state.ObjID, all []*cards.SA) {
	e.ownManaCarry()
	e.manaCarry.store(id, all, e.manaBoardStamp(), e.manaTouchOf(id))
}

// ManaCarryStats is a test-visible diagnostic.
func (e *Engine) ManaCarryStats() (hits, misses uint64) {
	return e.manaCarry.hits, e.manaCarry.misses
}
```

Add the field to `rules/engine_scratch.go` after `walkReuse` (around line 104):

```go
	// manaCarry caches per own-battlefield object mana-ability membership
	// across a seat's priority walks (rules/mana_member_carry.go). Clone
	// copies none: a clone starts cold (legal-walk design §4.6).
	manaCarry manaMemberCarry `clone:"reset"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestManaMemberCarry ./rules`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add rules/mana_member_carry.go rules/mana_member_carry_test.go rules/engine_scratch.go
git commit -m "rules: mana-member carry storage and board-stamp key"
```

---

### Task 2: Per-object touch generation

**Files:**
- Modify: `rules/walk_objclass.go` (`walkClassTouch`, around lines 359-401)
- Test: `rules/mana_member_carry_test.go`

**Interfaces:**
- Consumes: `manaMemberCarry.touchObj` (Task 1).
- Produces: every touched object's `manaTouchOf` generation moves, so a
  board-stamp-stable walk still misses a changed object.

- [ ] **Step 1: Write the failing test**

Append to `rules/mana_member_carry_test.go`:

```go
func TestWalkClassTouchBumpsManaTouch(t *testing.T) {
	// A touched object that is not provably static-cold must get a fresh
	// mana touch generation, so the carry cannot serve stale membership.
	e := newManaCarryTestEngine(t)
	o := e.G.Obj(1)
	if o == nil {
		t.Skip("no object 1 in the test board")
	}
	before := e.manaTouchOf(1)
	e.walkClassTouch(o)
	if e.manaTouchOf(1) == before {
		t.Fatalf("touch did not move the mana generation (still %d)", before)
	}
}
```

Add a small helper `newManaCarryTestEngine` that wraps the existing corpus
fixture `witheringEngine` (`rules/activation_limit_test.go:44`), which parks a
two-seat game at seat 0's main1 with real Snow-Covered Swamps on the
battlefield (own mana sources):

```go
func newManaCarryTestEngine(t *testing.T) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, _, _ := witheringEngine(t, reg, 3)
	return e
}
```

(`witheringEngine` already imports `internal/testutil`; add it if the test file
does not.)

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestWalkClassTouchBumpsManaTouch ./rules`
Expected: FAIL (generation unchanged).

- [ ] **Step 3: Write minimal implementation**

In `rules/walk_objclass.go`, bump the touched object's mana generation at every
point `walkClassTouch` drops or recomputes the class. The bump MUST go through
`e.manaTouchBumpIdx` (owner-guarded): a by-value Engine copy shares the carry's
backing arrays, so a direct `e.manaCarry.touchObj(i)` would corrupt the
original's carry. Add `e.manaTouchBumpIdx(i)` in:

1. the `i < 0 || i >= len(e.walkObjCls) || !e.walkObjCls[i].set` branch
   (`walk_objclass.go:362-365`), before its `staticTouchGen++`;
2. the `e.offerProbeDepth > 0` branch (`:366-372`), before its
   `staticTouchGen++`;
3. the recompute fall-through (`:396`), immediately after
   `e.walkObjCls[i] = fresh`.

Do **not** bump in the `old.fp == walkObjFPOf(o)` early-return branch (`:375`):
the fingerprint is unchanged, so the object's mana-relevant fields are
unchanged and a bump would only cost hits. The fingerprint covers card, face,
face index, flags, zone, controller, merged count, keyword count and counter
count (`walkObjFPOf`, `walk_objclass.go:104`), which is every object field a
member list reads.

Also add the touch drop where every class is dropped, in `walkClassDropAll`
(`:405`): replace `clear(e.walkObjCls)` with

```go
	clear(e.walkObjCls)
	e.manaTouchBumpAll()
```

(that path already bumps `staticTouchGen`, which the board stamp also sees;
bumping the per-object slice keeps the two consistent if the board stamp is
ever narrowed).

- [ ] **Step 4: Run tests to verify they pass**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestManaMemberCarry|TestWalkClassTouchBumpsManaTouch|TestWalk' ./rules`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add rules/walk_objclass.go rules/mana_member_carry_test.go
git commit -m "rules: per-object mana touch generation in walkClassTouch"
```

---

### Task 3: Extract the payability filter

**Files:**
- Modify: `rules/walk_block_reuse.go` (`ownManaMembers`, lines 120-140)
- Test: covered by the existing mana/legal-walk tests.

**Interfaces:**
- Produces: `func (e *Engine) filterPayableMana(dst, all []*cards.SA, p state.PlayerID, o *state.Object, id state.ObjID) []*cards.SA`
- Consumes: nothing new.

- [ ] **Step 1: Write the failing test**

There is no new behaviour; the safety net is the existing suite. Run it before
the refactor to record the baseline:

```bash
systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestPotentialWalkSharedAcrossReaders|Mana' ./rules
```
Expected: PASS (baseline).

- [ ] **Step 2: Refactor**

Add to `rules/walk_block_reuse.go`:

```go
// filterPayableMana applies the live-pool payability gate to a deferred
// membership list, writing into dst (which must not alias all).
func (e *Engine) filterPayableMana(dst, all []*cards.SA, p state.PlayerID, o *state.Object, id state.ObjID) []*cards.SA {
	out := dst[:0]
	for _, ma := range all {
		cc := e.compiledCostOf(ma.ParamStr(cards.PKCost))
		if mf := e.manaFactsOf(ma); mf != nil {
			cc = mf.cost
		}
		if e.manaCostPayable(p, o, id, cc, nil) {
			out = append(out, ma)
		}
	}
	return out
}
```

Rewrite `ownManaMembers` to call it:

```go
func (w *legalWalk) ownManaMembers(dst []*cards.SA, zi int, o *state.Object, id state.ObjID) []*cards.SA {
	e, p := w.e, w.p
	all := e.appendAvailableManaAbilitiesGate(dst, &w.actionStatics, p, id, true)
	w.rec.recordMembers(zi, all)
	out := e.filterPayableMana(dst, all, p, o, id)
	if walkCacheVerify {
		if want := e.appendAvailableManaAbilities(nil, &w.actionStatics, p, id); !slices.EqualFunc(want, out, pay.SameManaAbility) {
			panic(fmt.Sprintf("rules: recorded mana membership of %d filtered to %d abilities, the walk's has %d", id, len(out), len(want)))
		}
	}
	return out
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestPotentialWalkSharedAcrossReaders|Mana' ./rules`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add rules/walk_block_reuse.go
git commit -m "rules: extract filterPayableMana from ownManaMembers"
```

---

### Task 4: Wire the carry into the priority mana loop

**Files:**
- Modify: `rules/legal_walk_battlefield.go` (owns/mana loop, lines 66 and 99-107)
- Test: `rules/mana_member_carry_test.go` (hit/miss) and the digest suite

**Interfaces:**
- Consumes: `manaMemberLookup`, `manaMemberStore`, `filterPayableMana`.
- Produces: the priority walk's own-battlefield mana section served from the carry on a hit.

- [ ] **Step 1: Write the failing test**

Append to `rules/mana_member_carry_test.go` a test that poses the same walk
twice at the same board and asserts a hit on the second, with identical
membership. Use the package's existing repo-deck walk helper (search
`grep -rn "legalActions(.*\|Options()" rules/*_test.go` for the shortest one).
Shape:

```go
func TestManaCarryHitsSecondWalk(t *testing.T) {
	e := newManaCarryTestEngine(t) // same helper as Task 2
	first := e.legalActions(e.G.Active)
	h1, m1 := e.ManaCarryStats()
	second := e.legalActions(e.G.Active)
	h2, _ := e.ManaCarryStats()
	if h2 == h1 {
		t.Fatalf("second walk at the same board did not hit (hits %d -> %d, misses %d)", h1, h2, m1)
	}
	if !optionsEqual(first, second) {
		t.Fatal("carry changed the offered options")
	}
}
```

If `optionsEqual` is unexported and available in package `rules` tests, use it;
otherwise compare lengths and labels element-wise.

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestManaCarryHitsSecondWalk ./rules`
Expected: FAIL (no hits).

- [ ] **Step 3: Implement the wiring**

In `rules/legal_walk_battlefield.go`, after line 66 (`own := ...`) add:

```go
					ownSeat := z == state.ZBattlefield && zonePlayer == p
					carryable := ownSeat && !board.hasGrants && !board.addAbility
```

Replace lines 99-107 (the `var mas []*cards.SA` block and the `if own { … } else { … }`) with:

```go
						var mas []*cards.SA
						if carryable {
							if all, ok := e.manaMemberLookup(id); ok {
								if walkCacheVerify {
									want := e.appendAvailableManaAbilitiesGate(nil, actionStatics, p, id, true)
									if !slices.EqualFunc(want, all, pay.SameManaAbility) {
										panic(fmt.Sprintf("rules: mana-member carry of obj %d is stale", id))
									}
								}
								if own {
									w.rec.recordMembers(zi, all)
								}
								mas = e.filterPayableMana(masBuf[:0], all, p, o, id)
							} else {
								all := e.appendAvailableManaAbilitiesGate(masBuf[:0], actionStatics, p, id, true)
								e.manaMemberStore(id, all)
								if own {
									w.rec.recordMembers(zi, all)
								}
								mas = e.filterPayableMana(masBuf[:0], all, p, o, id)
							}
						} else if own {
							mas = w.ownManaMembers(masBuf[:0], zi, o, id)
						} else {
							mas = e.appendAvailableManaAbilities(masBuf[:0], actionStatics, p, id)
						}
						masBuf = mas
```

Confirm `slices`, `fmt` and `pay` are already imported in the file (they are:
the file uses `fmt`, `pay`, and `strings`; add `"slices"` to the import block
if absent).

- [ ] **Step 4: Run tests to verify they pass**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestManaCarryHitsSecondWalk|TestPotentialWalkSharedAcrossReaders|Mana' ./rules`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add rules/legal_walk_battlefield.go rules/mana_member_carry_test.go
git commit -m "rules: serve own-battlefield mana membership from the carry"
```

---

### Task 5: Verify mode over the repo decks

**Files:**
- Test: reuse the rules test binary's verify init.

**Interfaces:**
- Consumes: `walkCacheVerify` (already live in the rules test binary, `rules/legalskip_verify_test.go`).

- [ ] **Step 1: Run the verify-mode digest**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestLegalWalkDigest' ./rules`
Expected: PASS with no `mana-member carry ... is stale` panic.

- [ ] **Step 2: Run the heads and potential-walk tests**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestHeads$|TestPotentialWalkSharedAcrossReaders|TestEveryRepoDeck' ./rules ./view`
Expected: PASS; chain heads unchanged (do not regenerate `rules/testdata/heads/*`).

- [ ] **Step 3: Commit**

No code change expected. If any verify trip is found, fix the key before
proceeding and commit the fix with a clear message; do not paper over it.

---

### Task 6: Clone policy and cold-clone behaviour

**Files:**
- Test: `rules/mana_member_carry_test.go`
- Possibly modify: `rules/clone.go` only if the generator needs the tag (it should not).

**Interfaces:**
- Consumes: `Clone()`, `ManaCarryStats`.

- [ ] **Step 1: Write the failing test**

```go
func TestManaCarryColdOnClone(t *testing.T) {
	e := newManaCarryTestEngine(t)
	e.legalActions(e.G.Active) // warm the parent
	cl := e.Clone()
	h0, _ := cl.ManaCarryStats()
	cl.legalActions(cl.G.Active)
	h1, _ := cl.ManaCarryStats()
	if h0 != 0 {
		t.Fatalf("clone inherited carry stats: hits %d", h0)
	}
	if h1 != 0 {
		t.Fatalf("clone hit the parent's carry: hits %d", h1)
	}
}
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestManaCarryColdOnClone ./rules`
Expected: PASS (the `clone:"reset"` tag already zeroes the field). If it FAILS,
the field is not reset and the tag/placement must be fixed in
`rules/engine_scratch.go`.

- [ ] **Step 3: Run the clone-policy ratchet**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestClonePolicyEveryFieldTagged ./rules`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add rules/mana_member_carry_test.go
git commit -m "test: mana carry is cold on clones and clone policy holds"
```

---

### Task 7: Measurement — hit rate and walk cut

**Files:**
- Create: `cmd/enginebench/manacarry.go` (temporary measurement read)
- Modify: `cmd/enginebench/main.go`, `cmd/enginebench/play.go` (wire the flag)
- Test: none (measurement).

**Interfaces:**
- Consumes: `ManaCarryStats`.
- Produces: a `-manacarry` flag printing per-run hit/miss and hit/decision.

- [ ] **Step 1: Add the temporary read**

`cmd/enginebench/manacarry.go`:

```go
package main

// TEMPORARY measurement read (legal-walk §S4): prints the mana carry hit
// rate on the random/bot rows. Remove once the ticket lands.

import (
	"fmt"
	"os"

	"github.com/adams-shaun/gorge/rules"
)

var manaCarryFlag bool

type manaCarryAcc struct {
	hits, misses uint64
	last         uint64
}

var manaCarryState manaCarryAcc

func (a *manaCarryAcc) observe(e *rules.Engine) {
	if e == nil {
		return
	}
	h, m := e.ManaCarryStats()
	a.hits, a.misses = h, m
}

func manaCarryReport(w *os.File, decisions int) {
	a := &manaCarryState
	total := a.hits + a.misses
	rate := 0.0
	if total > 0 {
		rate = 100 * float64(a.hits) / float64(total)
	}
	fmt.Fprintf(w, "manacarry: hits %d  misses %d  hit-rate %.1f%%  hits/decision %.2f\n",
		a.hits, a.misses, rate, float64(a.hits)/float64(max(decisions, 1)))
}
```

Wire the flag in `cmd/enginebench/main.go` next to `-walkstats`:

```go
	manacarry := flag.Bool("manacarry", false, "random/bot rows: print the S4 mana carry hit rate to stderr")
```
and after `walkStatsFlag = *walkstats`:
```go
	manaCarryFlag = *manacarry
```

In `cmd/enginebench/play.go`, call `manaCarryState.observe(e)` where the
`-walkstats` hook observes (`:264-265` random, `:362` bot), and call
`manaCarryReport(os.Stderr, st.Decisions)` in `runRandom` and `runBot` beside
their `fill(...)` calls.

- [ ] **Step 2: Build**

Run: `go build -o /tmp/manacarry-enginebench ./cmd/enginebench`
Expected: builds.

- [ ] **Step 3: Measure hit rate**

Run, from `/home/sadams/projects/gorge/.worktrees/s4-census`:

```bash
for args in "-row random -pair A" "-row random -pair B" "-row bot -pair A"; do
  systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB \
    /tmp/manacarry-enginebench $args -secs 15 -manacarry 2>&1 | grep -E 'manacarry|"games_per_s"'
done
```
Expected: a hit rate per row.

- [ ] **Step 4: Measure the walk cut**

Build a baseline binary at `8c1bb84a9` (without the carry) in a scratch
worktree, then profile both with `-cpuprofile` on random A, random B and bot A,
and compare the `legalActionsWalkWithWindow` cumulative time. Report
`run  walk-cum base  walk-cum carry  cut%`.

- [ ] **Step 5: Evaluate the kill**

If the walk cut is `< 4%` on random B, or the hit rate is very low, do **not**
land the ticket as-is: move to the Approach 2 contingency in the spec
(`docs/superpowers/specs/2026-10-07-mana-member-carry-design.md` §8) and
narrow the board stamp to a single mana-member generation. Otherwise proceed.

- [ ] **Step 6: Remove the temporary read and commit the result**

Delete `cmd/enginebench/manacarry.go` and its wiring; record the numbers in the
ticket report (not in the repo). Commit any carry fix.

---

### Task 8: Full gate

**Files:** none.

- [ ] **Step 1: Focused rules and view**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestEveryRepoDeck|TestRepoDecks|TestRepoDeckGames|TestLegalWalkDigest|TestPotentialWalkSharedAcrossReaders|TestHeads$|TestClonePolicy' ./rules ./view`
Expected: PASS.

- [ ] **Step 2: Simulation replay**

Run: `make sim`
Expected: 20/20 `replay OK`.

- [ ] **Step 3: Full gate**

Run: `scripts/postmerge_full.sh <worktree> <sha>` (operator/CI; once per batch).
Expected: green.

- [ ] **Step 4: Report**

Report to the operator: files changed, the measured hit rate and walk cut per
row, the games/s delta, and any deviation (which goes in the commit message and
the report, never the AGENTS.md approximations table).

---

## Self-review

**Spec coverage:** §1 goal → Tasks 4, 7. §2 record-path finding → no task (context). §3 component/boundaries → Task 1. §4 data structures/key → Task 1, rewind via `tapeEpoch` in Task 1. §4 per-object touch → Task 2. §5 data flow → Task 4 (helper in Task 3). §6 correctness/verify → Tasks 4, 5. §7 invariants → Tasks 5 (byte-identical), 6 (clone), 1 (determinism/dense). §8 measurement/kill → Task 7. §9 test plan → Tasks 4-8. §10 files → task Files lists. §11 risks → Task 7 Step 5.

**Placeholder scan:** the only deferred item is Task 2's `newManaCarryTestEngine` helper, which instructs the implementer to reuse an existing helper and gives a fallback; Task 4's test reuses it. No `TBD`/`TODO`.

**Type consistency:** `manaMemberBoardStamp`, `manaMemberEntry`, `manaMemberCarry`, `manaMemberLookup`, `manaMemberStore`, `manaTouchOf`, `filterPayableMana`, `ManaCarryStats` are used consistently across Tasks 1-7.
