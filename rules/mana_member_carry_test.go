package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
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

// newManaCarryTestEngine parks a two-seat game at seat 0's main1 with real
// Snow-Covered Swamps on the battlefield (own mana sources), via the existing
// corpus fixture.
func newManaCarryTestEngine(t *testing.T) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, _, _ := witheringEngine(t, reg, 3)
	return e
}

func TestWalkClassTouchBumpsManaTouch(t *testing.T) {
	// A touched object that is not provably static-cold must get a fresh
	// mana touch generation, so the carry cannot serve stale membership.
	e := newManaCarryTestEngine(t)
	o := e.G.Obj(1)
	if o == nil {
		t.Skip("no object 1 in the test board")
	}
	// Force the recompute path: a cleared class is not provably unchanged,
	// so walkClassTouch re-derives it rather than taking the
	// fingerprint-unchanged early return.
	i := int(o.ID) - 1
	e.ownWalkClasses()
	if i >= 0 && i < len(e.walkObjCls) {
		e.walkObjCls[i].set = false
	}
	before := e.manaTouchOf(o.ID)
	e.walkClassTouch(o)
	if e.manaTouchOf(o.ID) == before {
		t.Fatalf("touch did not move the mana generation (still %d)", before)
	}
	// The drop-all path bumps every object's generation too.
	e.walkClassDropAll()
	if e.manaTouchOf(o.ID) <= before {
		t.Fatalf("drop-all did not move the mana generation")
	}
}
