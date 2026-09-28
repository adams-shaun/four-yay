package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
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
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -1})
	if e.sbaQuietNow() {
		t.Fatal("a LifeChange event must drop the quiet key")
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
