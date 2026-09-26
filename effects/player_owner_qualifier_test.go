package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPlayerOwnerQualifier pins the Player.Owner player-spec qualifier
// (Personal Incarnation's AB line, "Activator$ Player.Owner" -- "Only
// CARDNAME's owner may activate this ability"): the seat that qualifies is
// the SOURCE OBJECT's owner, read through the same MatchesPlayerSpecFrom
// binding the Activator$ offer gate resolves, never the activating seat, the
// source's controller, or "you". Owner shares CardOwner's shape (Player/Any
// base, source-bound, fail closed with no source bound) and joins its
// negation whitelist, so Player.!Owner admits the non-owner controller
// without being able to invert an unbound-source failure into a match.
func TestPlayerOwnerQualifier(t *testing.T) {
	g := state.NewGame(names(2))
	src := g.AddObject(creature(t, "Incarnation"), 0)

	// Precondition: the object exists and seat 0 both owns and controls it
	// before the control change, so the later owner/controller contrast is
	// real.
	if src == nil || src.Owner != 0 || src.Controller != 0 {
		t.Fatalf("precondition: source object not owned and controlled by seat 0 (%+v)", src)
	}

	if !MatchesPlayerSpecFrom(g, "Player.Owner", 0, 0, src.ID) {
		t.Fatal("Player.Owner did not admit the source's owner")
	}
	if MatchesPlayerSpecFrom(g, "Player.Owner", 1, 0, src.ID) {
		t.Fatal("Player.Owner admitted a seat that is not the source's owner")
	}
	// CardOwner (the sibling spelling) still resolves the same seat.
	if !MatchesPlayerSpecFrom(g, "Player.CardOwner", 0, 0, src.ID) {
		t.Fatal("Player.CardOwner did not admit the source's owner")
	}

	// After the control change the OWNER is still seat 0, the CONTROLLER is
	// seat 1, and the two are different -- that contrast is what makes the
	// rest of the test meaningful. Personal Incarnation's printed ability is
	// "Only CARDNAME's owner may activate this ability", so if Owner read
	// "controller" or "you" the negative below would flip.
	events.Apply(g, events.Event{Kind: events.ControlChange, Obj: src.ID, Player: 1})
	if src.Owner == src.Controller {
		t.Fatal("precondition: owner and controller did not differ after the control change")
	}
	if src.Controller != 1 || src.Owner != 0 {
		t.Fatalf("precondition: owner=%d controller=%d", src.Owner, src.Controller)
	}
	// "You" is seat 1 (the controller) here, which matches how the Activator$
	// offer gate resolves it. The OWNER must still be the one that matches.
	if !MatchesPlayerSpecFrom(g, "Player.Owner", 0, 1, src.ID) {
		t.Fatal("Player.Owner did not admit the source's owner after the control change")
	}
	if MatchesPlayerSpecFrom(g, "Player.Owner", 1, 1, src.ID) {
		t.Fatal("Player.Owner admitted the source's controller, not its owner")
	}

	// The negated spelling admits the OTHER seat (the controller) and
	// excludes the owner.
	if MatchesPlayerSpecFrom(g, "Player.!Owner", 0, 1, src.ID) {
		t.Fatal("Player.!Owner admitted the source's owner")
	}
	if !MatchesPlayerSpecFrom(g, "Player.!Owner", 1, 1, src.ID) {
		t.Fatal("Player.!Owner did not admit the non-owner controller")
	}

	// No source bound: the positive qualifier must fail closed, and the
	// negated spelling must NOT be able to invert that failure into a
	// blanket match.
	if MatchesPlayerSpec(g, "Player.Owner", 0, 0) {
		t.Fatal("Player.Owner matched with no source bound")
	}
	if MatchesPlayerSpec(g, "Player.!Owner", 1, 1) {
		t.Fatal("Player.!Owner matched with no source bound")
	}

	// An invalid base never matches, even for the owner.
	if MatchesPlayerSpecFrom(g, "Opponent.Owner", 0, 0, src.ID) {
		t.Fatal("Opponent.Owner admitted the owner")
	}
}
