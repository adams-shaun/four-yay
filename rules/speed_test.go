package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestStartYourEnginesSpeedStartsWithControlChange is kw:Start your engines'
// CR 702.179a control-change leaf, using the real corpus Amonkhet Raceway. The
// rule is a state-based action, so a speed-less seat that comes to control a
// permanent with the keyword must go to speed 1 even when the permanent was
// already on the battlefield and merely changed controllers -- the case a
// battlefield-entry-only hook cannot see.
func TestStartYourEnginesSpeedStartsWithControlChange(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngineThree(t, reg,
		nil,
		[]*cards.Card{lookup(t, reg, "Amonkhet Raceway")},
		nil)
	// Precondition: seat 0 has no speed and controls no carrier before the
	// transfer, so a passing test cannot be a vacuous read of an
	// already-started seat.
	if got := e.G.Players[0].Speed; got != 0 {
		t.Fatalf("precondition: seat 0 speed before the transfer: %d, want 0", got)
	}
	id := moveByName(t, e, 1, "Amonkhet Raceway", state.ZBattlefield)
	// Precondition: the entry path starts seat 1, and leaves seat 0 alone --
	// the two compared values really differ.
	if got := e.G.Players[1].Speed; got != 1 {
		t.Fatalf("precondition: seat 1 speed after its raceway entered: %d, want 1", got)
	}
	if got := e.G.Players[0].Speed; got != 0 {
		t.Fatalf("precondition: seat 0 speed while seat 1 controls the raceway: %d, want 0", got)
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: raceway not on the battlefield under seat 1")
	}

	e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 0})
	if got := e.G.Players[0].Speed; got != 1 {
		t.Fatalf("seat 0 speed after gaining control: %d, want 1 (CR 702.179a SBA)", got)
	}
	// Exactly one "start your engines" grant for seat 0: the scan must not
	// re-emit on the recursive fold or on any later transfer.
	grants := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.SpeedChange && ev.Player == 0 &&
			ev.Text == "start your engines"
	})
	if grants != 1 {
		t.Fatalf("seat 0 start-your-engines grants: %d, want exactly 1", grants)
	}

	// Idempotence: transferring the carrier to seat 2 (also speed-less) starts
	// seat 2, and a transfer BACK to seat 0 -- already at speed 1 -- leaves it
	// untouched.
	e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 2})
	if got := e.G.Players[2].Speed; got != 1 {
		t.Fatalf("seat 2 speed after gaining control: %d, want 1", got)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 0})
	if got := e.G.Players[0].Speed; got != 1 {
		t.Fatalf("seat 0 speed after regaining control: %d, want 1 (unchanged)", got)
	}
	if got := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.SpeedChange && ev.Player == 0 &&
			ev.Text == "start your engines"
	}); got != 1 {
		t.Fatalf("seat 0 re-emitted the grant on a second transfer: %d, want 1", got)
	}
}
