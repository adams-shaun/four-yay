package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Flesh Duplicate's conditional rider remains outside the ETB whitelist.
// Drive the entry boundary directly: a non-cast entry must keep its loud
// fallback rather than silently install a partial copy.

// TestFleshDuplicateETBWithheld pins the conditional-keyword case. Flesh
// Duplicate's rider is AddKeywords$ IfNew Vanishing:3 ("vanishing 3 if that
// creature doesn't have vanishing"). effClone installs an AddKeywords$ member
// verbatim as a layer-6 grant and this build implements no IfNew conditional,
// so cards.KeywordHead would read the head as "IfNew Vanishing": no
// conditional test, no vanishing ability and no entry time counters. The
// carrier is withheld rather than offering a copy that silently drops it.
func TestFleshDuplicateETBWithheld(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Flesh Duplicate"))
	dread := e.G.AddObject(corpusAlternativeCard(t, "Colossal Dreadmaw"), 1)
	dread.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{dread.ID})
	id := e.G.Zone(state.ZHand, 0)[0]

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	if d := e.Pending(); d != nil {
		t.Fatalf("withheld carrier posed an entry decision: %+v", d)
	}
	if hasEvent(e, events.ClonePermanent, id) {
		t.Fatal("withheld carrier must not copy")
	}
	if !hasNote(e, "unimplemented API Clone") {
		t.Fatal("loud Clone fallback note missing from the log")
	}
	// It entered as ITSELF: still the printed 0/0 Flesh Duplicate, not the
	// 6/6 template. Nothing granted the IfNew rider.
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face().Name != "Flesh Duplicate" {
		t.Fatalf("flesh duplicate did not enter as itself: %+v", o)
	}
	if der := e.Derived(id); der.Power != 0 || der.Toughness != 0 {
		t.Fatalf("self-entry is %d/%d, want the printed 0/0", der.Power, der.Toughness)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == id {
			t.Fatalf("withheld carrier placed counters: %+v", ev)
		}
	}
	for _, o := range []state.ObjID{id, dread.ID} {
		if der := e.Derived(o); slices.ContainsFunc(der.Keywords, func(k string) bool {
			return k == "Vanishing:3" || k == "IfNew Vanishing:3"
		}) {
			t.Fatalf("object %d keywords leaked the unimplemented rider: %v", o, der.Keywords)
		}
	}
}
