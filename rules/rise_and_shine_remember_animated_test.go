package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Rise and Shine is the corpus's sole RememberAnimated$ carrier: the Animate's
// RememberAnimated$ True must remember every artifact it animated so the
// chained DBPutCounter's Defined$ Remembered places four +1/+1 counters on it
// ("put four +1/+1 counters on each artifact that became a creature this
// way"). Before the read the Remembered set was empty, the chained sub saw
// nothing, and no counters landed. The trailing DBCleanup (ClearRemembered$
// True) must also leave the source's persistent list empty afterwards.
func TestRiseAndShineRememberAnimatedPutsFourCounters(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Rise and Shine", "Sol Ring")
	ring := searchMoveByName(t, e, "Sol Ring", state.ZBattlefield)
	id := searchMoveByName(t, e, "Rise and Shine", state.ZHand)
	addMana(t, e, 0, "UU") // cost {1}{U} plus slack

	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Rise and Shine: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target ask after the cast, got %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == ring {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("the animated artifact was not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(ring); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("animated ring = %+v, want it still on the battlefield", o)
	} else {
		if n := o.Counter("P1P1"); n != 4 {
			t.Fatalf("ring P1P1 counters = %d, want 4 (RememberAnimated$ feeds the chained Defined$ Remembered PutCounter)", n)
		}
		if !e.IsCreature(ring) {
			t.Fatal("the ring did not become a creature")
		}
		dd := e.Derived(ring)
		if dd.Power != 4 || dd.Toughness != 4 {
			t.Fatalf("animated ring = %d/%d, want 4/4 (a 0/0 with four +1/+1)", dd.Power, dd.Toughness)
		}
	}
	// DBCleanup's ClearRemembered$ must leave the source's persistent list
	// empty after the chain (the counters themselves are the ctx half's work,
	// done before cleanup).
	if o := e.G.Obj(id); o != nil && len(o.Remembered) != 0 {
		t.Fatalf("source Remembered after cleanup = %+v, want empty", o.Remembered)
	}
	replayCheck(t, e, cfg)
}
