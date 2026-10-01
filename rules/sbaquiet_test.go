package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestSBAQuietKey pins the quiet key's arming and invalidation: a pass loop
// that applied nothing arms it, a layer-inert event keeps it, any other event
// or a pending player-level loss drops it.
func TestSBAQuietKey(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	e.checkStateBased()
	if !e.sbaQuietNow() {
		t.Fatal("a pass loop that applied nothing should arm the quiet key")
	}
	e.emit(events.Event{Kind: events.Priority, Player: 0})
	if !e.sbaQuietNow() {
		t.Fatal("a layer-inert Priority event should keep the quiet key")
	}
	e.G.Players[1].Life = 0
	if e.sbaQuietNow() {
		t.Fatal("a player at 0 life must never be skipped past")
	}
	e.checkStateBased()
	if !e.G.Players[1].Lost {
		t.Fatal("player at 0 life should be marked Lost")
	}
	e.checkStateBased()
	if !e.sbaQuietNow() {
		t.Fatal("a quiet pass loop after the elimination should arm the key")
	}
	// LifeChange is a quiet kind (sbaQuietEvent): sbaQuietNow re-reads every
	// player's life on every call, so a non-lethal change keeps the key and
	// a lethal one is never skipped past.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -1})
	if !e.sbaQuietNow() {
		t.Fatal("a non-lethal LifeChange event should keep the quiet key")
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -e.G.Players[0].Life})
	if e.sbaQuietNow() {
		t.Fatal("a LifeChange to 0 life must drop the quiet key")
	}
	// A non-quiet kind drops it.
	e.checkStateBased()
	e.checkStateBased()
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 2, Counter: "EXPERIENCE", Amount: 1})
	if e.sbaQuietNow() {
		t.Fatal("a non-quiet event must drop the quiet key")
	}
}

// TestProvenanceGate pins the cast-provenance early-out: a spec with no
// provenance token passes unchanged, a token-carrying spec still evaluates.
func TestProvenanceGate(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{"Creature.YouCtrl", "Card.Other+nonLand", "Creature.wasCastByYou",
		"Card.!wasCastFromYourHand", "Spell.wasCastFromExile", "Card.CastSa Spell.Mayhem", "Card.wasCast",
		"Card.Colorless+YouCtrl+YouOwn+wasCastFromHand+cmcGE7"} {
		g := specProvenanceGate(spec)
		// wasCastFromHand is the effects-side predicate: it carries "Cast" but
		// no rules-side stage fires on it.
		want := spec != "Creature.YouCtrl" && spec != "Card.Other+nonLand" &&
			spec != "Card.Colorless+YouCtrl+YouOwn+wasCastFromHand+cmcGE7"
		if g.may != want {
			t.Errorf("%q: may=%v, want %v", spec, g.may, want)
		}
		if g.origin != (spec == "Spell.wasCastFromExile") {
			t.Errorf("%q: origin=%v", spec, g.origin)
		}
		if again := specProvenanceGate(spec); again != g {
			t.Errorf("%q: cached gate %+v differs from first %+v", spec, again, g)
		}
	}
}

// TestSBAQuietEventClasses pins sbaQuietEvent's per-event verdicts: an
// off-battlefield move of a non-token nothing is attached to is quiet; the
// same move is not once a permanent is attached to the moving card (the
// Animate Dead family's bearer) or the card is a token; a battlefield move
// and damage to an object never are; damage to a player is.
func TestSBAQuietEventClasses(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) < 2 {
		t.Fatalf("precondition: hand of %d", len(hand))
	}
	card, land := hand[0], hand[1]
	move := events.Event{Kind: events.MoveZone, Obj: card, From: state.ZHand, To: state.ZGraveyard}
	if !e.sbaQuietEvent(&move) {
		t.Fatal("an off-battlefield move of a plain card should be quiet")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: land, From: state.ZHand, To: state.ZBattlefield})
	if e.G.Obj(land).Zone != state.ZBattlefield {
		t.Fatal("precondition: the land should be on the battlefield")
	}
	leave := events.Event{Kind: events.MoveZone, Obj: land, From: state.ZBattlefield, To: state.ZGraveyard}
	if e.sbaQuietEvent(&leave) {
		t.Fatal("a battlefield move must not be quiet")
	}
	e.G.Obj(land).AttachedTo = card
	if e.sbaQuietEvent(&move) {
		t.Fatal("a move of a card a permanent is attached to must not be quiet")
	}
	e.G.Obj(land).AttachedTo = 0
	e.G.Obj(card).IsToken = true
	if e.sbaQuietEvent(&move) {
		t.Fatal("a token's move must not be quiet")
	}
	e.G.Obj(card).IsToken = false
	if e.sbaQuietEvent(&events.Event{Kind: events.Damage, Obj: land, Amount: 1}) {
		t.Fatal("damage to an object must not be quiet")
	}
	if !e.sbaQuietEvent(&events.Event{Kind: events.Damage, Player: 1, Amount: 1}) {
		t.Fatal("damage to a player should be quiet")
	}
}
