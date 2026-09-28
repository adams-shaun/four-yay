package v2engine

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
)

// TestCompareShadowCountsByField: equal fields count as compared, unequal
// ones as mismatches, joined on (game, seat, seat_step).
func TestCompareShadowCountsByField(t *testing.T) {
	p := func(n int32) *int32 { return &n }
	truth := []TruthRecord{{GameID: "g", Seat: "p0", SeatStep: 1, Turn: 2, PhaseStep: "upkeep", Active: "p0",
		Players:    []TruthPlayer{{Seat: "p0", Life: 20, HandSize: 3, LibrarySize: 2}, {Seat: "p1", Life: 17, HandSize: 1, LibrarySize: 5}},
		Objects:    []TruthObject{{ObjectID: "o-1", Zone: "battlefield", Name: "Elf", Controller: "p0", Owner: "p0", Power: p(1), Toughness: p(1)}},
		OwnLibrary: map[string]int{"Forest": 2}, OppHand: map[string]int{"Island": 1}, OppLibrary: map[string]int{"Island": 5}}}
	belief := []v2agent.BeliefRecord{{GameID: "g", Seat: "p0", SeatStep: 1, Turn: 2, PhaseStep: "upkeep", Active: "p0",
		Players:    []v2agent.BeliefPlayer{{Seat: "p0", Life: 20, HandCount: 3, LibraryCount: 2}, {Seat: "p1", Life: 18, HandCount: 1, LibraryCount: 5}},
		Objects:    []v2agent.BeliefObject{{ObjectID: "o-1", Zone: "battlefield", Name: "Elf", Controller: "p0", Owner: "p0", Power: p(2), Toughness: p(1)}},
		OwnLibrary: map[string]int{"Forest": 2}, OwnLibraryCount: 2, OppHidden: map[string]int{"Island": 6}},
		{GameID: "g", Seat: "p0", SeatStep: 9}}
	rep := CompareShadow(truth, belief)
	got := map[string][2]int{}
	for _, f := range rep.Fields {
		got[f.Field] = [2]int{f.Compared, f.Mismatches}
	}
	for field, want := range map[string][2]int{
		"join:truth_record_present":          {2, 1},
		"player.opp.life":                    {1, 1},
		"player.own.life":                    {1, 0},
		"object.battlefield.power":           {1, 1},
		"object.battlefield.toughness":       {1, 0},
		"hidden.own_library.exact":           {1, 0},
		"hidden.opp_hand_plus_library.exact": {1, 0},
	} {
		if got[field] != want {
			t.Errorf("%s: got %v, want %v", field, got[field], want)
		}
	}
	if rep.Joined != 1 {
		t.Errorf("joined %d", rep.Joined)
	}
}
