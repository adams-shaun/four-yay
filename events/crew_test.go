package events

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestCrewKindString pins the CR 702.122 Crew Kind's wire name, following the
// per-Kind Test<Kind>KindString convention (apply_test.go's comment at the
// kind-list loop). The name is what log.json serialises and what a consumer
// reads back, so a silent rename would break replay compatibility while
// leaving the hash chain intact.
func TestCrewKindString(t *testing.T) {
	if got := Crew.String(); got != "crew" {
		t.Fatalf("Crew.String() = %q, want %q", got, "crew")
	}
}

// TestCrewFoldsAndClears is the fold-level proof for the Crew event: Apply
// records the crewing creature's (turn, Vehicle) pairing, drops a duplicate
// pair, and TurnChange clears both halves. It is a unit companion to the
// rules-side end-to-end test (rules TestSetAudit_tmt_TurtleVan_...): the
// behaviour under test is that the pairing is source-relative state, not a
// bare boolean.
func TestCrewFoldsAndClears(t *testing.T) {
	g, l := twoPlayer(t)
	bear := g.Zone(state.ZLibrary, 0)[0]
	van := g.AddObject(g.Obj(bear).Card, 0).ID
	// Precondition: the two ids must differ, or the source-relative assertion
	// below is vacuous.
	if bear == van {
		t.Fatalf("fixture: bear and van share id %v", bear)
	}
	Emit(g, l, Event{Kind: MoveZone, Obj: bear, From: state.ZLibrary, To: state.ZBattlefield})
	Emit(g, l, Event{Kind: MoveZone, Obj: van, From: state.ZLibrary, To: state.ZBattlefield})

	Emit(g, l, Event{Kind: Crew, Obj: bear, Player: 0, IDs: []state.ObjID{van}})
	o := g.Obj(bear)
	if o == nil || o.CrewedTurn != g.Turn {
		t.Fatalf("after Crew: CrewedTurn = %d, want %d", o.CrewedTurn, g.Turn)
	}
	if len(o.CrewedVehicles) != 1 || o.CrewedVehicles[0] != van {
		t.Fatalf("after Crew: CrewedVehicles = %v, want [%v]", o.CrewedVehicles, van)
	}
	// A duplicate pairing (the same creature crews the same Vehicle twice in
	// a turn, having untapped) records one entry, not two.
	Emit(g, l, Event{Kind: Crew, Obj: bear, Player: 0, IDs: []state.ObjID{van}})
	if got := len(g.Obj(bear).CrewedVehicles); got != 1 {
		t.Fatalf("duplicate Crew: CrewedVehicles = %v, want one entry", g.Obj(bear).CrewedVehicles)
	}
	// A second Vehicle appends.
	van2 := g.AddObject(g.Obj(bear).Card, 0).ID
	Emit(g, l, Event{Kind: Crew, Obj: bear, Player: 0, IDs: []state.ObjID{van2}})
	if got := len(g.Obj(bear).CrewedVehicles); got != 2 {
		t.Fatalf("second Vehicle: CrewedVehicles = %v, want two entries", g.Obj(bear).CrewedVehicles)
	}

	Emit(g, l, Event{Kind: TurnChange, Player: 1})
	if got := g.Obj(bear).CrewedVehicles; len(got) != 0 {
		t.Fatalf("TurnChange did not clear CrewedVehicles: %v", got)
	}
	if got := g.Obj(bear).CrewedTurn; got != 0 {
		t.Fatalf("TurnChange did not clear CrewedTurn: %d", got)
	}
}
