package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
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

// TestRingTemptsYouAsksAndAppliesTheChosenBearer is the hosted leaf: a player
// controlling two eligible creatures is POSED a real KChoose (CR 701.54a,
// "choose a creature you control") and can designate a creature other than
// the deterministic first-in-zone-order default. Re-entry applies the answer
// and the emitted event records the chosen bearer.
func TestRingTemptsYouAsksAndAppliesTheChosenBearer(t *testing.T) {
	h := newHost(t, 2)
	bear := ringCreature(t, h, 0, "Bear")
	gorilla := ringCreature(t, h, 0, "Gorilla")
	// Precondition: the two creatures are distinct and both eligible, so the
	// offered options differ and the choice is real.
	if bear == gorilla {
		t.Fatal("precondition: fixtures are the same object")
	}
	if !h.IsCreature(bear) || !h.IsCreature(gorilla) {
		t.Fatal("precondition: fixtures are not creatures")
	}
	h.askResult = true // a real engine host offers the decision

	ring := sa(t, "DB$ RingTemptsYou")
	Resolve(h, &Ctx{Controller: 0}, ring)

	d := h.lastAsk
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "ring_bearer" {
		t.Fatalf("decision = %+v, want a ring_bearer KChoose", d)
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("decision = %+v, want seat 0 choose-one", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("options = %+v, want both eligible creatures offered", d.Options)
	}
	offered := map[state.ObjID]bool{}
	for _, o := range d.Options {
		offered[o.Obj] = true
	}
	if !offered[bear] || !offered[gorilla] {
		t.Fatalf("options = %+v, want both %d and %d offered", d.Options, bear, gorilla)
	}
	// Nothing was emitted while the ask is pending: the count cannot move
	// before the answer, so a suspension cannot double-count.
	if len(h.log) != 0 {
		t.Fatalf("log = %+v, want nothing emitted while the ask is pending", h.log)
	}

	// Simulate rules' resume arm: the answer's Obj goes to Ctx.RingBearerPick,
	// RingBearerDone marks "answered", and the effect re-enters.
	c2 := &Ctx{Controller: 0, RingBearerPick: gorilla, RingBearerDone: true}
	Resolve(h, c2, ring)

	ev := firstRingTempt(t, h)
	if ev.Obj != gorilla || ev.Amount != 1 || ev.Player != 0 {
		t.Fatalf("event = %+v, want bearer gorilla %d at amount 1 for seat 0", ev, gorilla)
	}
	if h.g.Players[0].RingTempted != 1 || h.g.Players[0].RingBearer != gorilla {
		t.Fatalf("fold: tempted %d bearer %d, want 1/%d",
			h.g.Players[0].RingTempted, h.g.Players[0].RingBearer, gorilla)
	}
	// fx42: the re-entry consumed and cleared the answer fields.
	if c2.RingBearerPick != 0 || c2.RingBearerDone {
		t.Fatalf("answer fields not cleared: %+v", c2)
	}
}

// TestRingTemptsYouCanKeepTheExistingBearer: the existing Ring-bearer is one
// of the offered options, and answering with it keeps the designation while
// the count still rises (CR 701.54a keeps the bearer "until another creature
// becomes your Ring-bearer"). The board is the shape where keep-existing and
// first-in-zone-order disagree: Bear is first, Gorilla is the bearer.
func TestRingTemptsYouCanKeepTheExistingBearer(t *testing.T) {
	h := newHost(t, 2)
	ringCreature(t, h, 0, "Bear")
	gorilla := ringCreature(t, h, 0, "Gorilla")
	h.Emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: gorilla, Amount: 1})
	if h.g.Players[0].RingBearer != gorilla {
		t.Fatalf("precondition: bearer %d, want %d", h.g.Players[0].RingBearer, gorilla)
	}
	h.log = nil // keep firstRingTempt to this temptation's one event

	h.askResult = true
	ring := sa(t, "DB$ RingTemptsYou")
	Resolve(h, &Ctx{Controller: 0}, ring)
	if d := h.lastAsk; d == nil || d.ResumeKind != "ring_bearer" {
		t.Fatalf("no ring_bearer ask posed: %+v", d)
	}
	// The current bearer must be offered so the player may keep it.
	offered := false
	for _, o := range h.lastAsk.Options {
		if o.Obj == gorilla {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("options = %+v, want the existing bearer %d offered", h.lastAsk.Options, gorilla)
	}

	c2 := &Ctx{Controller: 0, RingBearerPick: gorilla, RingBearerDone: true}
	Resolve(h, c2, ring)
	ev := firstRingTempt(t, h)
	if ev.Obj != gorilla {
		t.Fatalf("event bearer = %d, want the kept bearer %d", ev.Obj, gorilla)
	}
	if h.g.Players[0].RingTempted != 2 || h.g.Players[0].RingBearer != gorilla {
		t.Fatalf("count %d bearer %d, want 2/%d — the kept bearer was re-designated",
			h.g.Players[0].RingTempted, h.g.Players[0].RingBearer, gorilla)
	}
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

// TestRingTemptsYouReentryDoesNotDoubleCount: the ask suspends BEFORE any
// event is emitted, so a rule answered then re-entered exactly once yields
// one event and one count increment — never two.
func TestRingTemptsYouReentryDoesNotDoubleCount(t *testing.T) {
	h := newHost(t, 2)
	bear := ringCreature(t, h, 0, "Bear")
	gorilla := ringCreature(t, h, 0, "Gorilla")
	h.askResult = true
	ring := sa(t, "DB$ RingTemptsYou")

	Resolve(h, &Ctx{Controller: 0}, ring)
	if len(h.log) != 0 || h.g.Players[0].RingTempted != 0 {
		t.Fatalf("first pass logged %+v with count %d, want nothing before the answer",
			h.log, h.g.Players[0].RingTempted)
	}
	Resolve(h, &Ctx{Controller: 0, RingBearerPick: bear, RingBearerDone: true}, ring)

	if n := len(ringTemptEvents(h.log)); n != 1 {
		t.Fatalf("RingTemptsYou events = %d, want 1 (a re-entry must not re-emit)", n)
	}
	if h.g.Players[0].RingTempted != 1 {
		t.Fatalf("count = %d, want 1 (a re-entry must not double-count)", h.g.Players[0].RingTempted)
	}
	if h.g.Players[0].RingBearer != bear {
		t.Fatalf("bearer = %d, want %d", h.g.Players[0].RingBearer, bear)
	}
	_ = gorilla
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

// TestRingTemptsYouStaleAnswerFallsBackToTheDefault: a re-entered answer that
// names a creature no longer on the battlefield under the player (the board
// changed while the ask was pending) must not designate an illegal bearer —
// it falls back to the deterministic default, and the temptation still counts.
func TestRingTemptsYouStaleAnswerFallsBackToTheDefault(t *testing.T) {
	h := newHost(t, 2)
	bear := ringCreature(t, h, 0, "Bear")
	gorilla := ringCreature(t, h, 0, "Gorilla")
	// Precondition: gorilla is on the battlefield and controlled by seat 0
	// before the answer, so the stale check below is real.
	if o := h.g.Obj(gorilla); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatal("precondition: gorilla not a controlled battlefield creature")
	}
	h.Emit(events.Event{Kind: events.MoveZone, Obj: gorilla, From: state.ZBattlefield, To: state.ZGraveyard})
	h.log = nil

	// The answer names the now-stale gorilla; Bear is the only remaining
	// creature, so the default is Bear.
	Resolve(h, &Ctx{Controller: 0, RingBearerPick: gorilla, RingBearerDone: true},
		sa(t, "DB$ RingTemptsYou"))

	ev := firstRingTempt(t, h)
	if ev.Obj != bear {
		t.Fatalf("event bearer = %d, want the default %d (a stale answer must not be applied)", ev.Obj, bear)
	}
	if h.g.Players[0].RingTempted != 1 || h.g.Players[0].RingBearer != bear {
		t.Fatalf("count %d bearer %d, want 1/%d", h.g.Players[0].RingTempted, h.g.Players[0].RingBearer, bear)
	}
}
