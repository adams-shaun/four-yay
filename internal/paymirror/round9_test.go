package paymirror

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMatchProductionsRespectsAnyColourAmount pins anyLabelFits: an
// any-colour option whose label names a literal amount is no candidate for a
// witness of another total, while "Add any color" (no amount) still is.
func TestMatchProductionsRespectsAnyColourAmount(t *testing.T) {
	d := &decision.Decision{Options: []decision.Option{
		{Index: 0, Kind: "mana", Ability: 0, Label: "Add Combo ColorIdentity"},
		{Index: 1, Kind: "mana", Ability: 1, Label: "Add three mana of any one color"},
	}}
	one := decision.ManaAmount{0, 0, 0, 0, 1, 0}
	if got := matchProductions(d, one, 0); len(got) != 0 {
		t.Fatalf("matchProductions(G) = %v, want none (the three-mana grant cannot make one G)", got)
	}
	three := decision.ManaAmount{0, 0, 0, 0, 3, 0}
	if got := matchProductions(d, three, 1); len(got) != 1 || got[0] != 1 {
		t.Fatalf("matchProductions(GGG) = %v, want [1]", got)
	}
	d.Options[0].Label = "Add any color"
	if got := matchProductions(d, one, 0); len(got) != 1 || got[0] != 0 {
		t.Fatalf("matchProductions(G) with a plain any-colour option = %v, want [0]", got)
	}
	for tail, n := range map[string]int{"three mana of any one color": 3, "21 mana in any combination of colors": 21, "any color": 0} {
		if got, _ := labeledAnyCount(tail); got != n {
			t.Errorf("labeledAnyCount(%q) = %d, want %d", tail, got, n)
		}
	}
}

// TestFloatOnlyTriggerOrderNeedsFloatCreatedTriggers pins the proof gating
// the skipped order ask: every option but at most one must claim a distinct
// triggered-ability object the float created for the asking player.
func TestFloatOnlyTriggerOrderNeedsFloatCreatedTriggers(t *testing.T) {
	konrad, other := state.ObjID(78), state.ObjID(90)
	trig := func(i int, src state.ObjID) decision.Option {
		return decision.Option{Index: i, Kind: "trigger", Obj: src}
	}
	created := map[floatTriggerKey]int{{0, konrad}: 2}
	r := Recorded{Kind: decision.KTriggerOrder, Player: 0, Options: []decision.Option{trig(0, konrad), trig(1, konrad)}}
	if !floatOnlyTriggerOrder(r, created) {
		t.Fatal("two float-created triggers: want skippable")
	}
	r3 := r
	r3.Options = append(append([]decision.Option(nil), r.Options...), trig(2, other))
	if !floatOnlyTriggerOrder(r3, created) {
		t.Fatal("two float triggers plus one other (no ask without them): want skippable")
	}
	r4 := r3
	r4.Options = append(append([]decision.Option(nil), r3.Options...), trig(3, other))
	if floatOnlyTriggerOrder(r4, created) {
		t.Fatal("two non-float triggers would still be ordered on the float route: want not skippable")
	}
	r5 := r
	r5.Options = append(append([]decision.Option(nil), r.Options...), trig(2, konrad))
	if floatOnlyTriggerOrder(r5, map[floatTriggerKey]int{{0, konrad}: 1}) {
		t.Fatal("two A options past the float-created objects from the source: want not skippable")
	}
	if floatOnlyTriggerOrder(r, map[floatTriggerKey]int{{1, konrad}: 2}) {
		t.Fatal("the float's triggers belong to another player: want not skippable")
	}
	rt := r
	rt.Kind = decision.KTarget
	if floatOnlyTriggerOrder(rt, created) {
		t.Fatal("a target ask is never skipped")
	}
}

// TestMaskNewObjectsKeepsOldIDsAndPayloadWords pins maskNewObjects: only an
// ObjID of an object created since the fork is masked; older objects and
// tagged payload words past the arena are kept.
func TestMaskNewObjectsKeepsOldIDsAndPayloadWords(t *testing.T) {
	evs := []events.Event{
		{Kind: events.MoveZone, Obj: 461, IDs: []state.ObjID{12, 470}},
		{Kind: events.Damage, Obj: 5, IDs: []state.ObjID{2147483649}, Pairs: [][2]state.ObjID{{455, 2147483650}}},
	}
	got := maskNewObjects(evs, 450, 480)
	if got[0].Obj != 0 || got[0].IDs[0] != 12 || got[0].IDs[1] != 0 {
		t.Errorf("masked %+v, want Obj 0 and IDs [12 0]", got[0])
	}
	if got[1].Obj != 5 || got[1].IDs[0] != 2147483649 || got[1].Pairs[0] != [2]state.ObjID{0, 2147483650} {
		t.Errorf("masked %+v, want Obj 5, IDs kept, Pairs [0 word]", got[1])
	}
	if evs[0].Obj != 461 || evs[0].IDs[1] != 470 || evs[1].Pairs[0][0] != 455 {
		t.Error("maskNewObjects wrote through to its input")
	}
}
