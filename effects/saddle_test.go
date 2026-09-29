package effects

import "testing"

// TestSaddlePredicateIsRecognizedAndTurnRelative pins the effects.IsSaddled
// predicate (CR 702.171b, task agent-20260929T014717Z-0f0a84be): it is a
// recognised predicate (so the census does not drift from the matcher) and it
// matches ONLY on the turn the designation was stamped, so "until end of
// turn" expires without a second event.
func TestSaddlePredicateIsRecognizedAndTurnRelative(t *testing.T) {
	g, id := board(t)
	bear := g.Obj(id["myBear"])
	if bear == nil {
		t.Fatal("precondition: myBear missing from the board")
	}
	for _, spec := range []string{"Card.Self+IsSaddled", "Creature.IsSaddled+YouCtrl"} {
		if got := UnknownPredicates(spec); len(got) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want no unknown predicate", spec, got)
		}
	}

	// Unsaddled: no match.
	g.Turn = 3
	bear.SaddledTurn = 0
	if MatchesSpec(g, "Creature.IsSaddled", bear.ID, 0) {
		t.Fatal("IsSaddled matched an unsaddled creature")
	}

	// Saddled on the current turn: match.
	bear.SaddledTurn = 3
	if !MatchesSpec(g, "Creature.IsSaddled", bear.ID, 0) {
		t.Fatal("IsSaddled did not match a creature saddled on the current turn")
	}

	// The same stamp on a later turn: the designation expired.
	g.Turn = 4
	if MatchesSpec(g, "Creature.IsSaddled", bear.ID, 0) {
		t.Fatal("IsSaddled matched across a turn boundary; CR 702.171b ends it at end of turn")
	}
}
