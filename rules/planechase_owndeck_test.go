package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPlaneswalkFromScanRejectsAnotherSeatsPlane is the MINOR pin from the t2
// verdict: checkPlaneswalkTriggers' PlaneswalkedFrom arm must validate that
// ev.Obj belongs to ev.Player's OWN planar deck, exactly as the chaos scan's
// currentPlane boundary does. A hand-built PlanarWalk naming a foreign seat's
// plane in Obj must queue nothing (and, once drained, draw nothing), while a
// positive control with the seat's own plane proves the away trigger still
// fires.
//
// Every real emitter sets Obj to departingPlane(ev.Player) (the seat's own
// plane), so only a fuzzed/hand-built event reaches the negative arm -- which
// is exactly why the boundary is asserted here.
func TestPlaneswalkFromScanRejectsAnotherSeatsPlane(t *testing.T) {
	away := planeWalkedFromProbe(t, "Probe Away")
	theirs := planeWalkedFromProbe(t, "Probe Theirs")
	// Seat 0 gets the away probe; seat 1 gets an identical probe in its own
	// deck, so the foreign plane's away ability WOULD draw if it fired.
	cfg := twoDeckConfig(t, 83101, []*cards.Card{away}, []*cards.Card{theirs})
	e := New(cfg)
	e.Advance()

	myDeck := e.G.Zone(state.ZPlanarDeck, 0)
	theirDeck := e.G.Zone(state.ZPlanarDeck, 1)
	if len(myDeck) == 0 || len(theirDeck) == 0 {
		t.Fatalf("precondition: my deck %v, their deck %v", myDeck, theirDeck)
	}
	foreignPlane := theirDeck[0]
	if zoneContainsID(myDeck, foreignPlane) {
		t.Fatalf("precondition: plane %d is in both decks", foreignPlane)
	}
	if o := e.G.Obj(foreignPlane); o == nil || o.Zone != state.ZPlanarDeck {
		t.Fatalf("precondition: foreign plane %d is not in ZPlanarDeck", foreignPlane)
	}

	// Negative: seat 0's walk names seat 1's plane as the plane left behind.
	e.pendingTriggers = nil
	before := len(e.L.Events)
	e.emit(events.Event{Kind: events.PlanarWalk, Player: 0, Obj: foreignPlane})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("a PlanarWalk naming another seat's plane queued %d triggers: %+v", len(e.pendingTriggers), e.pendingTriggers)
	}
	drainPlaneTriggers(t, e, 10)
	if got := drawsAfter(e.L.Events[before:], 0); got != 0 {
		t.Fatalf("a foreign-plane PlanarWalk made seat 0 draw %d cards, want 0", got)
	}
	if got := drawsAfter(e.L.Events[before:], 1); got != 0 {
		t.Fatalf("a foreign-plane PlanarWalk made seat 1 draw %d cards, want 0", got)
	}

	// Positive control: the seat's OWN departing plane still fires its away
	// trigger, so the boundary rejects the foreign plane without breaking the
	// real case.
	e.pendingTriggers = nil
	before2 := len(e.L.Events)
	e.emit(events.Event{Kind: events.PlanarWalk, Player: 0, Obj: myDeck[0]})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("a PlanarWalk naming the seat's own plane queued no trigger")
	}
	drainPlaneTriggers(t, e, 20)
	if got := drawsAfter(e.L.Events[before2:], 0); got != 1 {
		t.Fatalf("the own-plane PlanarWalk drew %d cards, want exactly 1 from the away trigger", got)
	}
	replayCheck(t, e, cfg)
}
