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
