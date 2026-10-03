package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// firstRingTempt returns the single RingTemptsYou event in the log, failing
// the test if there is not exactly one (the CR 701.54a leaf: a temptation is
// one event, however the bearer was chosen).
func firstRingTempt(t *testing.T, h *fakeHost) events.Event {
	t.Helper()
	var got []events.Event
	for _, e := range h.log {
		if e.Kind == events.RingTemptsYou {
			got = append(got, e)
		}
	}
	if len(got) != 1 {
		t.Fatalf("RingTemptsYou events = %d (%+v), want exactly 1", len(got), got)
	}
	return got[0]
}

// TestRingTemptsYouSingleCreatureDoesNotAsk: strict-supersets — with exactly
// one eligible creature the deterministic answer IS the only legal answer, so
// no decision is posed and the creature is designated silently.
func TestRingTemptsYouSingleCreatureDoesNotAsk(t *testing.T) {
	h := newHost(t, 2)
	bear := ringCreature(t, h, 0, "Bear")
	// Precondition: exactly one eligible creature, and the ask path would be
	// taken if there were two (so a silent pass is not the no-host fallback).
	if n := len(h.g.Zone(state.ZBattlefield, 0)); n != 1 {
		t.Fatalf("precondition: battlefield holds %d, want 1", n)
	}
	h.askResult = true // a host that WOULD answer an ask

	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ RingTemptsYou"))

	if h.askCount != 0 || h.lastAsk != nil {
		t.Fatalf("ask posed for a single creature: count %d decision %+v", h.askCount, h.lastAsk)
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Fatalf("unexpected Note %q on the single-creature path", e.Text)
		}
	}
	ev := firstRingTempt(t, h)
	if ev.Obj != bear || h.g.Players[0].RingBearer != bear {
		t.Fatalf("bearer %d (event %d), want the sole creature %d", h.g.Players[0].RingBearer, ev.Obj, bear)
	}
	if h.g.Players[0].RingTempted != 1 {
		t.Fatalf("count = %d, want 1", h.g.Players[0].RingTempted)
	}
}

// ringTemptEvents returns the RingTemptsYou events in the log.
func ringTemptEvents(log []events.Event) []events.Event {
	var got []events.Event
	for _, e := range log {
		if e.Kind == events.RingTemptsYou {
			got = append(got, e)
		}
	}
	return got
}

// TestRingTemptsYouOffersTheExistingBearerFirst pins the option ORDER: the
// deterministic default (the existing Ring-bearer) is option 0, so the
// shared KChoose first-option policy -- botpolicy's arm and every R-9 clamp
// fallback -- answers "keep the existing Ring-bearer", exactly as the no-host
// stand-in and the pre-choice engine did. Without this, a hosted bot would
// silently switch its bearer off the deterministic default.
func TestRingTemptsYouOffersTheExistingBearerFirst(t *testing.T) {
	h := newHost(t, 2)
	ringCreature(t, h, 0, "Bear") // first in zone order
	gorilla := ringCreature(t, h, 0, "Gorilla")
	// Precondition: Gorilla is the existing bearer and is NOT first in zone
	// order, so "existing bearer first" and "zone order" disagree -- the
	// ordering assertion below can actually fail.
	h.Emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: gorilla, Amount: 1})
	if h.g.Players[0].RingBearer != gorilla {
		t.Fatalf("precondition: bearer %d, want %d", h.g.Players[0].RingBearer, gorilla)
	}
	if first := h.g.Zone(state.ZBattlefield, 0)[0]; first == gorilla {
		t.Fatal("precondition: gorilla is already first in zone order")
	}
	h.log = nil
	h.askResult = true

	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ RingTemptsYou"))
	d := h.lastAsk
	if d == nil || len(d.Options) != 2 {
		t.Fatalf("decision = %+v, want a two-option ring_bearer ask", d)
	}
	if d.Options[0].Obj != gorilla {
		t.Fatalf("first option = %d, want the existing bearer %d (deterministic default first)",
			d.Options[0].Obj, gorilla)
	}
}
