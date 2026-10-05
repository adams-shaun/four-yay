package effects

// The `Card.OwnedBy|ControlledBy Player.NotedFor<label>` control/ownership
// referent (task agent-20261005T013345Z-8182c5fb). Forge's PlayerProperty
// .NotedFor is a global player property written by a DB$ Pump body's
// NoteCardsFor$ parameter (effects.effPump -> events.PlayerNoted) and read by
// the shared player filter's `Player.NotedFor<label>` qualifier
// (effects/player_filter.go). Before this ticket the shared control/ownership
// classifier (controlReferent) did not know the qualifier, so the whole
// predicate failed closed and moved nothing: TMT Turtles in Time and TMT Step
// Between Worlds swept zero cards for their opted-in players, and the two
// Continuous statics (Archangel of Strife, Two Streams Facility) matched no
// affected creature.
//
// These tests pin the matcher and the UnknownPredicates census at their one
// home so the classifier, the resolver and the census cannot drift. The label
// is dynamic (Stargate, War, ...), which is why the arm is a prefix split and
// not a fixed StrCodes key; the two static carriers use different labels
// (War/Peace, RedWaterfall), so this test drives one dynamic label through the
// real grammar and a second through the classifier.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestOwnedByPlayerNotedFor exercises the ownership side of the referent: the
// noted seat's owned card matches through a differing controller, the other
// seat's card does not, and the census recognises both polarities.
func TestOwnedByPlayerNotedFor(t *testing.T) {
	g, _ := board(t)
	events.Apply(g, events.Event{Kind: events.PlayerNoted, Player: 0, Text: "Stargate"})

	// PRECONDITION: the note is written and the shared player filter reads it,
	// so a match below cannot pass on an empty note set.
	if !MatchesPlayerSpecFrom(g, "Player.NotedForStargate", 0, 0, 0) {
		t.Fatal("precondition: player 0 is not noted Stargate")
	}
	if MatchesPlayerSpecFrom(g, "Player.NotedForStargate", 1, 0, 0) {
		t.Fatal("precondition: player 1 must not be noted Stargate")
	}
	ownedByNoted := &state.Object{Owner: 0, Controller: 1}
	ownedByOther := &state.Object{Owner: 1, Controller: 0}
	// PRECONDITION: the owner/controller values under comparison differ, so a
	// match cannot come from confusing OwnedBy with ControlledBy.
	if ownedByNoted.Owner == ownedByNoted.Controller ||
		ownedByNoted.Owner == ownedByOther.Owner {
		t.Fatal("precondition: compared owner/control values must differ")
	}

	for _, spec := range []string{
		"Card.OwnedBy Player.NotedForStargate",
		"Card.!OwnedBy Player.NotedForStargate",
	} {
		if unknown := UnknownPredicates(spec); len(unknown) != 0 {
			t.Fatalf("recognized referent %s reported unknown: %v", spec, unknown)
		}
	}
	if !MatchesObjectCtx(g, "Card.OwnedBy Player.NotedForStargate", ownedByNoted, SpecContext{}) {
		t.Fatal("noted owner's card did not match despite a different controller")
	}
	if MatchesObjectCtx(g, "Card.OwnedBy Player.NotedForStargate", ownedByOther, SpecContext{}) {
		t.Fatal("card owned by the unnoted player matched")
	}
	if !MatchesObjectCtx(g, "Card.!OwnedBy Player.NotedForStargate", ownedByOther, SpecContext{}) {
		t.Fatal("negated ownership did not match a bound different owner")
	}
	if MatchesObjectCtx(g, "Card.ControlledBy Player.NotedForStargate", ownedByNoted, SpecContext{}) {
		t.Fatal("OwnedBy referent incorrectly compared the candidate controller")
	}
}

// TestControlledByPlayerNotedFor exercises the control side, and a second
// dynamic label so the prefix split is not accidentally keyed on "Stargate".
func TestControlledByPlayerNotedFor(t *testing.T) {
	g, _ := board(t)
	events.Apply(g, events.Event{Kind: events.PlayerNoted, Player: 1, Text: "War"})

	if !MatchesPlayerSpecFrom(g, "Player.NotedForWar", 1, 0, 0) {
		t.Fatal("precondition: player 1 is not noted War")
	}
	controlledByNoted := &state.Object{Owner: 0, Controller: 1}
	controlledByOther := &state.Object{Owner: 1, Controller: 0}
	if controlledByNoted.Controller == controlledByNoted.Owner ||
		controlledByNoted.Controller == controlledByOther.Controller {
		t.Fatal("precondition: compared owner/control values must differ")
	}

	for _, spec := range []string{
		"Card.ControlledBy Player.NotedForWar",
		"Card.!ControlledBy Player.NotedForWar",
	} {
		if unknown := UnknownPredicates(spec); len(unknown) != 0 {
			t.Fatalf("recognized referent %s reported unknown: %v", spec, unknown)
		}
	}
	if !MatchesObjectCtx(g, "Card.ControlledBy Player.NotedForWar", controlledByNoted, SpecContext{}) {
		t.Fatal("noted controller's card did not match despite a different owner")
	}
	if MatchesObjectCtx(g, "Card.ControlledBy Player.NotedForWar", controlledByOther, SpecContext{}) {
		t.Fatal("card controlled by the unnoted player matched")
	}
	if !MatchesObjectCtx(g, "Card.!ControlledBy Player.NotedForWar", controlledByOther, SpecContext{}) {
		t.Fatal("negated control did not match a bound different controller")
	}
	if MatchesObjectCtx(g, "Card.OwnedBy Player.NotedForWar", controlledByNoted, SpecContext{}) {
		t.Fatal("ControlledBy referent incorrectly compared the candidate owner")
	}
}

// TestPlayerNotedForControlReferentStaysBounded guards the scope boundary: a
// bare or empty label stays unknown (wordPredicate's own rule), and an
// unrelated unrecognised Player.* ref is NOT admitted by this arm -- admitting
// it would silently stop UnknownPredicates reporting an unmodelled shape.
func TestPlayerNotedForControlReferentStaysBounded(t *testing.T) {
	g, _ := board(t)
	events.Apply(g, events.Event{Kind: events.PlayerNoted, Player: 0, Text: "Stargate"})

	for _, spec := range []string{
		"Card.OwnedBy Player.NotedFor",   // empty label
		"Card.OwnedBy Player.Opponent",   // a different, still-unmodelled ref
		"Card.ControlledBy Player.Other", // likewise
	} {
		if unknown := UnknownPredicates(spec); len(unknown) == 0 {
			t.Fatalf("%s should stay unknown (scope boundary), got no unknown report", spec)
		}
	}
	// And the bounded arm still matches when the note is present.
	if !MatchesObjectCtx(g, "Card.OwnedBy Player.NotedForStargate",
		&state.Object{Owner: 0, Controller: 0}, SpecContext{}) {
		t.Fatal("the bounded NotedFor arm stopped matching its noted owner")
	}
}
