package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTriggeredCardsPredicate is the effects leaf for the Card.TriggeredCards
// filter predicate (Hedge Shredder, Toluz Clever Conductor's ChangeZoneAll
// ChangeType$):
//
//   - the census recognises the token: UnknownPredicates reports nothing (it
//     reported ["TriggeredCards"] before registration);
//   - with the triggering batch bound on SpecContext.Remembered, the candidate
//     matches exactly the remembered object and no other;
//   - with an empty Remembered the spec is a RESOLVED no-match (matches
//     nothing), the same fail-closed-no-match direction the Defined$
//     TriggeredCards spelling takes -- never an invented referent.
//
// Real corpus faces throughout, matching the shape MatchesObjectCtx is called
// with in the engine.
func TestTriggeredCardsPredicate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	remembered := corpusObject(t, reg, g, "Grizzly Bears")
	other := corpusObject(t, reg, g, "Goblin Piker")

	// Census agreement: the token is known to both matcher and census.
	if got := UnknownPredicates("Card.TriggeredCards"); len(got) != 0 {
		t.Errorf("UnknownPredicates(Card.TriggeredCards) = %v, want empty (the census must agree with the matcher)", got)
	}

	// The remembered object matches; a different object does not.
	sc := SpecContext{You: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
	if !MatchesObjectCtx(g, "Card.TriggeredCards", remembered, sc) {
		t.Errorf("Card.TriggeredCards must match the remembered object")
	}
	if MatchesObjectCtx(g, "Card.TriggeredCards", other, sc) {
		t.Errorf("Card.TriggeredCards must not match an object outside the remembered set")
	}

	// Empty Remembered is a resolved no-match, never a match.
	empty := SpecContext{You: 0}
	if MatchesObjectCtx(g, "Card.TriggeredCards", remembered, empty) {
		t.Errorf("Card.TriggeredCards must fail closed (match nothing) with an empty Remembered")
	}
}
