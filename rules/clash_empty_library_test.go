package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestClashEmptyLibraryParticipantIsNotAsked pins the one participant shape
// that does NOT get a placement election: a clashing player with an empty
// library has no revealed card to place, so no ask is posed to them and no
// LibraryOrder may be emitted for them. A one-card library is NOT this shape
// (it still gets the election; see clash_placement_test.go) -- this test keeps
// the empty case from being conflated with it.
func TestClashEmptyLibraryParticipantIsNotAsked(t *testing.T) {
	e := combatEngine(t)
	marvo := onBoardCard(t, e, 0, unblockedCorpusCard(t, "m/marvo_deep_operative.txt"))
	e.G.Obj(marvo).SummonSick = false
	filler := putTopOfLibrary(t, e, card(t, "Name:Filler\nManaCost:1\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 0)
	high := putTopOfLibrary(t, e, card(t, "Name:High\nManaCost:5\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"), 0)
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		e.G.Obj(id).Zone = state.ZExile
	}
	e.G.SetZone(state.ZLibrary, 1, nil)
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		e.G.Obj(id).Zone = state.ZExile
	}
	e.G.SetZone(state.ZLibrary, 0, []state.ObjID{high, filler})
	// Precondition: seat 1's library really is empty while seat 0's top card
	// is a real card with a non-negative mana value, so the clash reveals
	// exactly one card.
	if got := e.G.Zone(state.ZLibrary, 1); len(got) != 0 {
		t.Fatalf("empty-library precondition: seat 1 library = %v", got)
	}
	if lib0 := e.G.Zone(state.ZLibrary, 0); len(lib0) != 2 || lib0[0] != high || e.G.Obj(high).Face().ManaValue() < 0 {
		t.Fatalf("seat 0 library precondition: %v (top MV %d)", lib0, e.G.Obj(high).Face().ManaValue())
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{marvo}})
	e.putTriggersOnStack()
	e.resolveTop()

	// Seat 0 (controller, the only owner with a revealed card) is asked; the
	// ask must route to seat 0 and carry the clash_placement continuation.
	d := e.Pending()
	if d == nil || d.ResumeKind != "clash_placement" || d.Kind != decision.KChoose || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("expected seat 0's clash placement ask, got %+v", d)
	}
	submitChoices(t, e, 1) // keep on top

	// Seat 1 must never have been asked: the next decision is the ordinary
	// priority window, not another clash placement, and no LibraryOrder was
	// emitted for the empty seat.
	if d := e.Pending(); d != nil && d.ResumeKind == "clash_placement" {
		t.Fatalf("empty-library seat 1 was asked for a placement: %+v", d)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.LibraryOrder && ev.Player == 1 {
			t.Fatalf("empty library emitted a LibraryOrder: %+v", ev)
		}
	}
	markerCount := map[state.PlayerID]int{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Clash {
			markerCount[ev.Player]++
		}
	}
	if markerCount[0] != 1 || markerCount[1] != 1 {
		t.Fatalf("clash markers = %v, want one per participant even when one library is empty", markerCount)
	}
	if hasNote(e, "unimplemented API Clash") {
		t.Fatal("Clash handler was not registered")
	}
}
