package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestWalkClassesRecycledCapacityIsUnset pins the object-class cache's
// growth over a recycled array: a clone copies its parent's classes into a
// Spare's array whose capacity past the copy still holds a spent engine's
// classes, and an object the clone creates later must be classified afresh,
// never read from that stale tail.
func TestWalkClassesRecycledCapacityIsUnset(t *testing.T) {
	land := card(t, "Name:Probe Land\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ x\nOracle:x\n")
	e := handEngine(t, land)
	e.walkClassesCatchUp()
	for _, id := range e.G.Zone(state.ZHand, 0) {
		e.walkClassOf(id)
	}
	if len(e.walkObjCls) == 0 {
		t.Fatal("setup: no class was cached")
	}
	// A spent engine's array: every slot a stale, set, mana-cold class.
	stale := make([]walkObjClass, len(e.walkObjCls)+16)
	for i := range stale {
		stale[i] = walkObjClass{set: true}
	}
	c := e.CloneInto(&Spare{walkCls: stale[:0]})
	if cap(c.walkObjCls) != len(stale) {
		t.Fatalf("setup: the clone did not adopt the recycled array (cap %d)", cap(c.walkObjCls))
	}
	o := c.G.AddObject(land, 0)
	c.walkClassesCatchUp()
	got := *c.walkClassOf(o.ID)
	if want := c.computeWalkObjClass(o); got.withoutFP() != want.withoutFP() || !got.manaHot {
		t.Fatalf("new object's class %+v, want %+v (a mana source is mana-hot)", got, want)
	}
}
