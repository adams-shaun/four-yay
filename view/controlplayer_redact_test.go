package view

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestControlPlayerSeesControlledSecretEvent is the CR 720.4 redaction half:
// a Secret event emitted by a controlled player reaches the controller
// unchanged, while a non-controlling seat still gets shape-only. Mirrors
// arrange_redaction_test.go's synthetic-event shape so it exercises
// RedactEvent directly.
func TestControlPlayerSeesControlledSecretEvent(t *testing.T) {
	g := state.NewGame([]string{"a", "b", "c"})
	// Seat 0 controls seat 1 (control is keyed controlled -> controller).
	g.ControlledBy = map[state.PlayerID]state.PlayerID{1: 0}

	ev := events.Event{Seq: 9, Kind: events.Draw, Player: 1,
		From: state.ZLibrary, To: state.ZHand, IDs: []state.ObjID{1, 2, 3}, Secret: true}

	// Precondition: with no control a stranger's secret is redacted, so the
	// positive case below is a real widening rather than an always-pass.
	bare := state.NewGame([]string{"a", "b", "c"})
	if got := RedactEvent(bare, ev, 0); len(got.IDs) != 0 {
		t.Fatalf("test setup: a non-controller already sees secret IDs %v", got.IDs)
	}

	// The controller of the emitting seat keeps the payload.
	ctl := RedactEvent(g, ev, 0)
	if len(ctl.IDs) != 3 {
		t.Fatalf("controller of seat 1 lost its Secret payload: got IDs %v, want the full set", ctl.IDs)
	}
	if !ctl.Secret || ctl.Kind != events.Draw || ctl.Player != 1 {
		t.Fatalf("controller's event lost its shape: %+v", ctl)
	}

	// A non-controlling third seat still strips it to shape-only.
	other := RedactEvent(g, ev, 2)
	if len(other.IDs) != 0 {
		t.Fatalf("non-controlling seat 2 leaked secret IDs %v", other.IDs)
	}
	if other.Kind != events.Draw || other.Player != 1 || !other.Secret {
		t.Fatalf("redacted event lost its shape: %+v", other)
	}
}

// TestControlPlayerSeesControlledHiddenCard is the rule-(2)/(3)
// half: visibleTo must admit a hidden-zone card owned by a controlled seat
// when the viewer is that seat's controller, and only then.
func TestControlPlayerSeesControlledHiddenCard(t *testing.T) {
	g := state.NewGame([]string{"a", "b", "c"})
	o := g.AddObject(nil, 1) // owner 1, zone ZLibrary (hidden)
	g.SetZone(state.ZHand, 1, []state.ObjID{o.ID})

	// Precondition: seat 1 really owns a hidden-zone object, or visibleTo's
	// owner test would be vacuous.
	if got := g.Obj(o.ID); got == nil || got.Owner != 1 || !got.Zone.Hidden() {
		t.Fatalf("test setup: object %v is not a hidden card owned by seat 1", got)
	}

	g.ControlledBy = map[state.PlayerID]state.PlayerID{1: 0}
	if !visibleTo(g, o.ID, 0) {
		t.Fatal("controller of seat 1 cannot see seat 1's hidden card")
	}
	if visibleTo(g, o.ID, 2) {
		t.Fatal("non-controlling seat 2 can see seat 1's hidden card")
	}
	// A self-control entry must fail closed even if hand-built: seat 1 must
	// not gain sight of its own already-visible hand through the guard, and a
	// bogus entry must not grant a stranger anything.
	g.ControlledBy = map[state.PlayerID]state.PlayerID{1: 1}
	if visibleTo(g, o.ID, 2) {
		t.Fatal("bogus self-control entry granted seat 2 sight of seat 1's card")
	}
}
