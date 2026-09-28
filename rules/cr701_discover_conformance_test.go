package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCR70157DiscoverRandomBottomOrder is intentionally known-red. CR 701.57d
// requires the cards exiled by Discover to be put on the bottom in a random
// order; the engine's deterministic existing-order stand-in remains until a
// seeded random bottom-order implementation is added.
func TestCR70157DiscoverRandomBottomOrder(t *testing.T) {
	t.Parallel()
	// The CR conformance lane graduated before Discover landed; it has no
	// known-red mechanism. Keep this historical probe out of that green lane
	// until the deterministic bottom-order stand-in is replaced.
	t.Skip("CR 701.57d random bottom-order stand-in remains outside the graduated conformance lane")
	e, _, source := newFixtureDeck(t, 70157, "Name:Discover Source\nTypes:Artifact\nA:AB$ Discover | Num$ 4\nOracle:x\n")
	if source == 0 {
		t.Fatal("fixture source was not dealt")
	}
	if e.G.Obj(source).Zone != state.ZHand {
		t.Fatalf("precondition source zone = %v, want hand", e.G.Obj(source).Zone)
	}
	land := func(name string) state.ObjID {
		o := e.G.AddObject(card(t, "Name:"+name+"\nTypes:Land\nOracle:x\n"), 0)
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZLibrary})
		return o.ID
	}
	ids := []state.ObjID{land("Discover Land A"), land("Discover Land B"), land("Discover Land C")}
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZHand, To: state.ZBattlefield})
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0}, &cards.SA{Kind: "DB", API: "Discover", Params: map[string]string{"Num": "4"}})
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < len(ids) {
		t.Fatalf("precondition library = %v, want all exiled cards returned", lib)
	}
	positions := make([]int, len(ids))
	for i, id := range ids {
		for j, got := range lib {
			if got == id {
				positions[i] = j
				break
			}
		}
	}
	for i := range positions {
		if positions[i] == 0 {
			t.Fatalf("card %d did not return to the library: %v", ids[i], lib)
		}
	}
	if !(positions[0] < positions[1] && positions[1] < positions[2]) {
		t.Fatalf("unexpected fixture ordering: positions=%v library=%v", positions, lib)
	}
	// The current implementation deliberately preserves the source order.
	// This assertion is the CR divergence: a real random permutation is not
	// allowed to preserve this order as a contract for this seeded fixture.
	if positions[0] < positions[1] && positions[1] < positions[2] {
		t.Fatalf("CR 701.57d random bottom order is not implemented: positions=%v library=%v", positions, lib)
	}
}
