package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPersonalIncarnationOwnerActivatorOffer pins the end-to-end offer path
// for Personal Incarnation's printed ability: "Only Personal Incarnation's
// owner may activate this ability" (the AB line's `Activator$ Player.Owner`).
// The real corpus card is placed on the battlefield, then control is taken
// away from its owner. The OWNER (who no longer controls it) must be offered
// the ability, and the new non-owner controller must NOT -- that contrast is
// what proves the Activator$ gate really reads `Player.Owner` through the
// shared player-spec matcher, rather than falling back to the CR 602.2a
// controller default (which would invert the answer for both seats).
func TestPersonalIncarnationOwnerActivatorOffer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	piCard := lookup(t, reg, "Personal Incarnation")
	e := corpusEngine(t, reg, nil, nil)
	pi := onBoardCard(t, e, 0, piCard)

	// Precondition: the permanent is on the battlefield, seat 0 owns it, and
	// seat 0 controls it before the control change.
	o := e.G.Obj(pi)
	if o == nil || o.Zone != state.ZBattlefield || o.Owner != 0 || o.Controller != 0 {
		t.Fatalf("precondition: source not owned/controlled by seat 0 on the battlefield (%+v)", o)
	}
	if !hasAbilityOptionFor(e.legalActions(0), pi) {
		t.Fatal("Personal Incarnation's redirection ability was not offered to its owner while it was also the controller")
	}

	// Seat 1 takes control. The owner does NOT change.
	e.emit(events.Event{Kind: events.ControlChange, Obj: pi, Player: 1})
	if o := e.G.Obj(pi); o.Owner == o.Controller || o.Owner != 0 || o.Controller != 1 {
		t.Fatalf("precondition: owner/controller did not split after the control change (owner=%d controller=%d)",
			o.Owner, o.Controller)
	}

	// The new non-owner controller is denied: `Player.Owner` reads the
	// source object's owner (seat 0), not the controller (seat 1) and not
	// `you`.
	if hasAbilityOptionFor(e.legalActions(1), pi) {
		t.Fatal("the non-owner controller was offered Personal Incarnation's owner-only ability")
	}
	// The owner (who no longer controls it) IS offered: the Activator$ walk
	// scans every seat's battlefield and the matcher admits the owner.
	if !hasAbilityOptionFor(e.legalActions(0), pi) {
		t.Fatal("the owner was not offered Personal Incarnation's owner-only ability after losing control of it")
	}
}
