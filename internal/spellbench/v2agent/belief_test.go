package v2agent

import (
	"bytes"
	"encoding/json"
	"strings"
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

// TestBeliefCountsFacesAgainstFullNames: an object showing the back face of
// an "A // B" entry counts against that entry, with or without full_name.
func TestBeliefCountsFacesAgainstFullNames(t *testing.T) {
	var gs GameStart
	if err := json.Unmarshal([]byte(`{"game_id":"g","seat":"p0",
		"own_deck":{"decklist":[{"name":"The Modern Age // Vector Glider","count":2},{"name":"Island","count":1}]}}`), &gs); err != nil {
		t.Fatal(err)
	}
	gs.Seat = "p0"
	b := NewBelief(&gs)
	name := func(s string) *string { return &s }
	d := &Decision{Seat: &SeatDecision{Observation: Observation{Viewer: "p0", Players: []Player{{Seat: "p0",
		Battlefield: []ObjectRecord{{ObjectRef: ObjectRef{ObjectID: "o-1", CardName: name("Vector Glider"), OwnerSeat: "p0", ControllerSeat: "p0", Zone: "battlefield"}, Permanent: &Permanent{}}},
		Graveyard: []ObjectRecord{{ObjectRef: ObjectRef{ObjectID: "o-2", CardName: name("The Modern Age"), OwnerSeat: "p0", ControllerSeat: "p0", Zone: "graveyard"},
			FullName: name("The Modern Age // Vector Glider")}}}}}}}
	r := b.Record("g", d)
	if len(r.OwnLibrary) != 1 || r.OwnLibrary["Island"] != 1 {
		t.Errorf("own library %v, want Island:1 only", r.OwnLibrary)
	}
}

// TestBeliefLogCoversRecoveredDecisions: the belief log keeps one line per
// answered choose on the recovery paths too -- a choose adopted without a
// game_start, answered by the fallback after the policy panics -- so the
// shadow check joins every decision the host saw answered.
func TestBeliefLogCoversRecoveredDecisions(t *testing.T) {
	var log bytes.Buffer
	a, err := New(&faulty{mode: "panic"}, Options{Name: "t", Version: "1", BeliefLog: &log})
	if err != nil {
		t.Fatal(err)
	}
	id, code := selection(t, a.HandleLine([]byte(rcChoose))) // no game_start
	if code != "" || id != 1 || a.Stats.GamesAdopted != 1 || a.Stats.PolicyFallbacks != 1 {
		t.Fatalf("answered %d %q, stats %+v", id, code, a.Stats)
	}
	lines := strings.Split(strings.TrimSpace(log.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("belief log has %d lines, want 1: %q", len(lines), log.String())
	}
	var r BeliefRecord
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil {
		t.Fatal(err)
	}
	if r.GameID != "g-1" || r.Seat != "p0" || r.SeatStep != 4 {
		t.Errorf("belief record keyed %s/%s/%d, want g-1/p0/4", r.GameID, r.Seat, r.SeatStep)
	}
}
