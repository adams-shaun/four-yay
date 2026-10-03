package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Mode$ Hand whole-hand wheel arm (Reforge the Soul, Windfall, Magus of
// the Wheel, Dark Deal). The helpers come from cardflow_discard_test.go
// (discardBoard, creature, land) and the other effect test files (mkCard, sa,
// askHost): a 2-seat board whose seat 0 is the caster and seat 1 the target.

// discardEvents counts the canonical discard moves in a host's log.
func discardEvents(log []events.Event) []events.Event {
	var out []events.Event
	for _, ev := range log {
		if events.IsDiscard(ev) {
			out = append(out, ev)
		}
	}
	return out
}

// TestDiscardHandDiscardsEveryCardInHandOrder pins the wheel shape: Mode$
// Hand discards the ENTIRE target hand (Forge's DiscardEffect HAND mode,
// which never reads NumCards$), not the front card. Every card leaves hand in
// one order and lands in the graveyard with its own events.Discard, no ask is
// posed (a mandatory line has no choice to record), and no stand-in Note is
// emitted.
func TestDiscardHandDiscardsEveryCardInHandOrder(t *testing.T) {
	ah, c, ids := discardBoard(t,
		creature(t, "Frog"), creature(t, "Bird"), land(t, "Islet"))
	s := sa(t, "SP$ Discard | ValidTgts$ Player | Mode$ Hand")

	effDiscard(ah, c, s)

	if ah.asked != nil {
		t.Fatal("Mode$ Hand asked a decision")
	}
	for i, id := range ids {
		if !inZone(ah.g, state.ZGraveyard, 1, id) {
			t.Fatalf("hand card %d was not discarded", i)
		}
		if inZone(ah.g, state.ZHand, 1, id) {
			t.Fatalf("hand card %d is still in hand", i)
		}
	}
	if len(ah.g.Zone(state.ZHand, 1)) != 0 {
		t.Fatalf("hand size = %d, want 0", len(ah.g.Zone(state.ZHand, 1)))
	}
	moves := discardEvents(ah.log)
	if len(moves) != len(ids) {
		t.Fatalf("discard events = %d, want one per hand card (%d)", len(moves), len(ids))
	}
	for i, ev := range moves {
		if ev.Obj != ids[i] {
			t.Fatalf("discard event %d = obj %d, want hand-order card %d", i, ev.Obj, ids[i])
		}
	}
	for _, ev := range ah.log {
		if ev.Kind == events.Note {
			t.Fatalf("mandatory Mode$ Hand emitted a stand-in Note: %q", ev.Text)
		}
	}
}

// TestDiscardHandRememberDiscardedRiderAppliesPerCard pins the Windfall
// rider: RememberDiscarded$ records EVERY discarded card in the resolution's
// Remembered set (and event-backed on the source), not just the first — what
// the chained "draw cards equal to the greatest number discarded" reads.
func TestDiscardHandRememberDiscardedRiderAppliesPerCard(t *testing.T) {
	ah, c, ids := discardBoard(t, creature(t, "Frog"), creature(t, "Bird"), land(t, "Islet"))
	s := sa(t, "SP$ Discard | ValidTgts$ Player | Mode$ Hand | RememberDiscarded$ True")

	effDiscard(ah, c, s)

	if len(c.Remembered) != len(ids) {
		t.Fatalf("ctx Remembered = %d entries, want %d", len(c.Remembered), len(ids))
	}
	for i, id := range ids {
		if !targetIn(c.Remembered, state.Target{Obj: id}) {
			t.Fatalf("ctx Remembered is missing hand card %d", i)
		}
	}
	// Event-backed on the source too (what Card.IsRemembered matches later).
	src := ah.g.Obj(c.Source)
	if src == nil {
		t.Fatal("source object missing")
	}
	for i, id := range ids {
		if !targetIn(src.Remembered, state.Target{Obj: id}) {
			t.Fatalf("source Remembered is missing hand card %d", i)
		}
	}
}

// TestDiscardHandEmptyHandDiscardsNothingWithoutError pins the empty-hand
// boundary: a target with no cards discards nothing, emits no events, poses
// no ask and does not error.
func TestDiscardHandEmptyHandDiscardsNothingWithoutError(t *testing.T) {
	ah, c, _ := discardBoard(t)
	s := sa(t, "SP$ Discard | ValidTgts$ Player | Mode$ Hand")

	effDiscard(ah, c, s)

	if ah.asked != nil {
		t.Fatal("empty-hand Mode$ Hand asked a decision")
	}
	if len(discardEvents(ah.log)) != 0 {
		t.Fatal("empty-hand Mode$ Hand emitted discard events")
	}
	for _, ev := range ah.log {
		if ev.Kind == events.Note {
			t.Fatalf("empty-hand Mode$ Hand emitted a Note: %q", ev.Text)
		}
	}
}

// TestDiscardHandDoesNotTouchTheCastersHand pins the boundary the wheel
// relies on per acting player: with Defined$ naming both players (Reforge
// the Soul's compiled shape is Defined$ Player), each player's own hand is
// discarded and nothing crosses players. Here the caster (seat 0) holds the
// spell and its own hand card; the target's discard must not touch seat 0's
// card, and a Defined-resolved multi-player walk hits both.
func TestDiscardHandDiscardsBothPlayersUnderDefinedPlayer(t *testing.T) {
	ah := &askHost{}
	ah.g = state.NewGame(names(2))
	src := ah.g.AddObject(mkCard(t, "Name:Reforge the Soul\nTypes:Sorcery\nOracle:x\n"), 0)
	casterCard := ah.g.AddObject(creature(t, "Own Hand Card"), 0)
	casterCard.Zone = state.ZHand
	ah.g.SetZone(state.ZHand, 0, []state.ObjID{casterCard.ID})
	targetCard := ah.g.AddObject(creature(t, "Frog"), 1)
	targetCard.Zone = state.ZHand
	ah.g.SetZone(state.ZHand, 1, []state.ObjID{targetCard.ID})
	// Reforge the Soul's compiled acting set: Defined$ Player (every player).
	c := &Ctx{Source: src.ID, Controller: 0,
		Targets: []state.Target{{Player: 0, IsPlayer: true}, {Player: 1, IsPlayer: true}}}
	s := sa(t, "SP$ Discard | Mode$ Hand | Defined$ Player")

	effDiscard(ah, c, s)

	if !inZone(ah.g, state.ZGraveyard, 0, casterCard.ID) {
		t.Fatal("caster's hand card was not discarded")
	}
	if !inZone(ah.g, state.ZGraveyard, 1, targetCard.ID) {
		t.Fatal("target's hand card was not discarded")
	}
	if len(ah.g.Zone(state.ZHand, 0)) != 0 || len(ah.g.Zone(state.ZHand, 1)) != 0 {
		t.Fatal("a hand was not emptied")
	}
}
