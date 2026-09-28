package v2agent

import (
	"encoding/json"
	"testing"
)

// TestBeliefInfersHiddenMultisets: own library = own list minus every own
// card seen (tokens excluded); opponent hidden = its list minus its seen
// cards; a nameless card is counted, not subtracted.
func TestBeliefInfersHiddenMultisets(t *testing.T) {
	var gs GameStart
	if err := json.Unmarshal([]byte(`{"game_id":"g","seat":"p0",
		"own_deck":{"decklist":[{"name":"Mountain","count":3},{"name":"Lightning Bolt","count":2}]},
		"opponent_deck":{"decklist":[{"name":"Island","count":4}]}}`), &gs); err != nil {
		t.Fatal(err)
	}
	gs.Seat = "p0"
	b := NewBelief(&gs)
	step := int64(4)
	name := func(s string) *string { return &s }
	d := &Decision{SeatStep: &step, Seat: &SeatDecision{Observation: Observation{Viewer: "p0", Turn: 3, PhaseStep: "upkeep",
		Players: []Player{
			{Seat: "p0", Life: 18, HandCount: 1, LibraryCount: 2,
				Hand: []ObjectRecord{{ObjectRef: ObjectRef{ObjectID: "o-1", CardName: name("Lightning Bolt"), OwnerSeat: "p0", ControllerSeat: "p0", Zone: "hand"}}},
				Battlefield: []ObjectRecord{{ObjectRef: ObjectRef{ObjectID: "o-2", CardName: name("Mountain"), OwnerSeat: "p0", ControllerSeat: "p0", Zone: "battlefield"}, Permanent: &Permanent{Tapped: true}},
					{ObjectRef: ObjectRef{ObjectID: "o-3", CardName: name("Goblin"), OwnerSeat: "p0", ControllerSeat: "p0", Zone: "battlefield"}, Token: true, Permanent: &Permanent{}}},
				Graveyard: []ObjectRecord{{ObjectRef: ObjectRef{ObjectID: "o-4", CardName: name("Lightning Bolt"), OwnerSeat: "p0", ControllerSeat: "p0", Zone: "graveyard"}}}},
			{Seat: "p1", Life: 20, HandCount: 2, LibraryCount: 1,
				Exile: []ObjectRecord{{ObjectRef: ObjectRef{ObjectID: "o-5", OwnerSeat: "p1", ControllerSeat: "p1", Zone: "exile"}, FaceDown: true}}},
		}}}}
	r := b.Record("g", d)
	if r.OwnLibrary["Mountain"] != 2 || len(r.OwnLibrary) != 1 {
		t.Errorf("own library %v, want Mountain:2", r.OwnLibrary)
	}
	if r.OppHidden["Island"] != 4 || r.OppUnnamed != 1 || r.OppHiddenCount != 3 {
		t.Errorf("opp hidden %v unnamed %d count %d", r.OppHidden, r.OppUnnamed, r.OppHiddenCount)
	}
	if r.SeatStep != 4 || r.Turn != 3 || len(r.Objects) != 5 {
		t.Errorf("record %+v", r)
	}
}
