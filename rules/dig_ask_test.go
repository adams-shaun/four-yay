package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The dig1 look-and-take end-to-end leaves: an Ancient Stirrings-shaped
// Dig cast through the real engine poses the KChoose mid-resolution ask,
// the answer moves exactly the picked card, and the whole game replays from
// the log. The effects-package halves live in effects/dig_ask_test.go.

// digSrc is Ancient Stirrings's shape without the ForceRevealToController$
// (a parameter the engine never reads): look at the top five, you may take
// one Creature.
const digSrc = "Name:DigMe\nManaCost:G\nTypes:Sorcery\n" +
	"A:SP$ Dig | Defined$ You | DigNum$ 5 | ChangeNum$ 1 | Optional$ True | ChangeValid$ Creature\nOracle:x\n"

// digCreature is the extra creature card seeded into seat 0's deck.
const digCreature = "Name:Elk\nTypes:Creature\nPT:2/2\nOracle:x\n"

// digFixture builds an engine whose seat 0 can cast digSrc, then reorders
// seat 0's library through a LibraryOrder event so the top five are exactly
// [Elk, non-creature, Elk, non-creature, non-creature] -- a strict-superset
// window (two eligible, ChangeNum 1) that must ask.
func digFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, digSrc, digCreature, digCreature)
	addMana(t, e, 0, "G")
	// The seeded shuffle may have drawn an Elk into the opening hand; move
	// any hand-borne Elks back to the library through the event path first.
	for _, oid := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(oid); o != nil && o.Face() != nil && o.Face().Name == "Elk" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: oid, From: state.ZHand, To: state.ZLibrary, Player: 0})
		}
	}
	// Reorder the library through the event path: find the two Elks and
	// park them at window positions 0 and 2, whatever the seeded shuffle
	// did. events.Apply's LibraryOrder sets the whole zone, so the emitted
	// list must be the COMPLETE library.
	lib := e.G.Zone(state.ZLibrary, 0)
	elks := make([]state.ObjID, 0, 2)
	rest := make([]state.ObjID, 0, len(lib))
	for _, oid := range lib {
		if o := e.G.Obj(oid); o != nil && o.Face() != nil && o.Face().Name == "Elk" && len(elks) < 2 {
			elks = append(elks, oid)
			continue
		}
		rest = append(rest, oid)
	}
	if len(elks) != 2 || len(rest) < 3 {
		t.Fatalf("fixture library lacks the window: elks %d rest %d", len(elks), len(rest))
	}
	want := append([]state.ObjID(nil), elks[0], rest[0], elks[1])
	want = append(want, rest[1:]...)
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: want, Secret: true})
	return e, cfg, id
}

// TestDigAskEndToEndSuspendsAndHonoursTheAnswer casts DigMe through the real
// engine, checks the posed KChoose, answers with the SECOND eligible card
// (proving the answer, not a first-eligible default, is honoured), and
// replays the whole log byte-for-byte.
func TestDigAskEndToEndSuspendsAndHonoursTheAnswer(t *testing.T) {
	t.Parallel()
	e, cfg, id := digFixture(t, 61)
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)

	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "dig" {
		t.Fatalf("expected a pending dig KChoose, got %+v", d)
	}
	if d.Player != 0 || d.Min != 0 || d.Max != 1 {
		t.Fatalf("Min/Max/Player = %d/%d/%d, want 0/1/0 (Optional take of one, the library's owner)", d.Min, d.Max, d.Player)
	}
	if len(d.Options) != 2 {
		t.Fatalf("options = %d, want 2 (the two Elks; the non-creatures must not be pickable)", len(d.Options))
	}
	for _, o := range d.Options {
		if o.Label != "Elk" {
			t.Fatalf("option label = %q, want the face name", o.Label)
		}
	}
	if !strings.Contains(d.Prompt, "Look at the top 5") {
		t.Fatalf("prompt = %q", d.Prompt)
	}
	// The look is on the log, Secret to the owner, carrying the window.
	var look *events.Event
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.Note && ev.Text == "looks at the top of the library" {
			look = ev
		}
	}
	if look == nil || !look.Secret || look.Player != 0 || len(look.IDs) != 5 {
		t.Fatalf("look Note = %+v, want a Secret owner's Note carrying the 5-card window", look)
	}
	if o := e.G.Obj(id); o.Zone != state.ZStack {
		t.Fatalf("the dig must suspend with the spell on the stack, zone %s", o.Zone)
	}

	picked := d.Options[1].Obj
	submitChoices(t, e, 1)
	passUntilStackEmpty(t, e, 20)

	if hand := e.G.Zone(state.ZHand, 0); len(hand) == 0 || hand[len(hand)-1] != picked {
		t.Fatalf("hand does not end with the picked card %v: %v", picked, hand)
	}
	libAfter := e.G.Zone(state.ZLibrary, 0)
	if len(libAfter) != len(libBefore)-1 {
		t.Fatalf("library size %d, want %d", len(libAfter), len(libBefore)-1)
	}
	// The untaken window cards sit at the BOTTOM in their existing relative
	// order (the ordered-bottom ask was answered in the offered order by the
	// drain, and the arrange's untouched remainder is everything below the
	// window); the cards that were below the window stay directly on top.
	wantRest := make([]state.ObjID, 0, 4)
	for _, oid := range libBefore[:5] {
		if oid != picked {
			wantRest = append(wantRest, oid)
		}
	}
	base := libAfter[:len(libAfter)-len(wantRest)]
	for i, oid := range wantRest {
		if libAfter[len(base)+i] != oid {
			t.Fatalf("library bottom[%d] = %v, want %v (the rest went to the bottom in their existing order)", i, libAfter[len(base)+i], oid)
		}
	}
	replayCheck(t, e, cfg)
}

// TestDigAskDeclineEndToEnd is the Optional-decline leaf through the real
// engine: answering the ask with NO cards (Min 0) takes nothing, the spell
// still resolves, and the whole window goes to the library's BOTTOM in its
// existing relative order (the default remainder destination).
func TestDigAskDeclineEndToEnd(t *testing.T) {
	t.Parallel()
	e, cfg, id := digFixture(t, 62)
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)

	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "dig" {
		t.Fatalf("expected a pending dig KChoose, got %+v", d)
	}
	submitChoices(t, e) // an empty answer: decline the take
	passUntilStackEmpty(t, e, 20)

	libAfter := e.G.Zone(state.ZLibrary, 0)
	if len(libAfter) != len(libBefore) {
		t.Fatalf("library size %d, want %d: a declined take moves nothing in or out", len(libAfter), len(libBefore))
	}
	// The declined window went to the BOTTOM in its existing relative order
	// (the default remainder destination; the drain answers the ordered-bottom
	// ask in the offered order, which is the window's existing order); the
	// cards that were below the window are now on top, in their order.
	for i, oid := range libBefore[5:] {
		if libAfter[i] != oid {
			t.Fatalf("library[%d] = %v, want %v (the below-window cards surfaced)", i, libAfter[i], oid)
		}
	}
	for i, oid := range libBefore[:5] {
		if libAfter[len(libBefore)-5+i] != oid {
			t.Fatalf("library bottom[%d] = %v, want %v (the declined window bottomed in its existing order)", i, libAfter[len(libBefore)-5+i], oid)
		}
	}
	replayCheck(t, e, cfg)
}
