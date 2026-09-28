package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// kw:ETBReplacement's trailing colon fields are the replacement's real
// applicability: the third field is the Zone the SOURCE must sit in
// (ActiveZones$) and the fourth is the filter the ENTERING object must match
// (ValidCard$), not the default Card.Self. Before this fix the expander took
// only the SVar name and always built ValidCard$ Card.Self, so Dearly
// Departed's graveyard static ("each Human creature you control enters with
// an additional +1/+1 counter") fired the PutCounter on Dearly Departed's OWN
// entry and never on a Human. Pinned on the real corpus card, per the brief's
// "Done" contract.
//
// The engine builders and helpers come from search_library_test.go
// (searchTestRegistry, searchCorpusCard, searchEngine, searchMoveByName) and
// cast_test.go (toMain1), all in this package. The decks are built from
// compiled corpus cards only, so no Forge script text is committed. Dearly
// Departed is in no legacy golden deck, so no chain head depends on it.

// etbZoneEngine seeds seat 0 with Dearly Departed plus a cheap green Human
// (Avacyn's Pilgrim), plays both into hand, and leaves the engine at Main 1.
func etbZoneEngine(t *testing.T) *Engine {
	t.Helper()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Dearly Departed", "Avacyn's Pilgrim")
	return e
}

// p1p1Of returns the number of +1/+1 counters on a named object for seat 0
// (the only seat these fixtures seed).
func p1p1Of(t *testing.T, e *Engine, name string) int32 {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Face() != nil && o.Face().Name == name {
			return o.Counter("P1P1")
		}
	}
	t.Fatalf("object %q not found on the board", name)
	return 0
}

// TestDearlyDepartedGraveyardPumpsAnEnteringHuman is the brief's primary
// case: with Dearly Departed in the graveyard, a Human that enters gets one
// extra +1/+1 counter. The Human is moved library -> battlefield directly so
// no cast/priority plumbing obscures the replacement.
func TestDearlyDepartedGraveyardPumpsAnEnteringHuman(t *testing.T) {
	t.Parallel()
	e := etbZoneEngine(t)
	searchMoveByName(t, e, "Dearly Departed", state.ZGraveyard)
	pilgrim := searchMoveByName(t, e, "Avacyn's Pilgrim", state.ZBattlefield)
	_ = pilgrim
	if got := p1p1Of(t, e, "Avacyn's Pilgrim"); got != 1 {
		t.Fatalf("Human entered with %d +1/+1 counters, want 1 (Dearly Departed in graveyard)", got)
	}
}

// TestDearlyDepartedBattlefieldDoesNotPumpAnEnteringHuman guards the
// ActiveZones$ half: on the battlefield the graveyard static is inert, so
// the same Human enters bare.
func TestDearlyDepartedBattlefieldDoesNotPumpAnEnteringHuman(t *testing.T) {
	t.Parallel()
	e := etbZoneEngine(t)
	searchMoveByName(t, e, "Dearly Departed", state.ZBattlefield)
	searchMoveByName(t, e, "Avacyn's Pilgrim", state.ZBattlefield)
	if got := p1p1Of(t, e, "Avacyn's Pilgrim"); got != 0 {
		t.Fatalf("Human entered with %d +1/+1 counters, want 0 (Dearly Departed on the battlefield)", got)
	}
}

// TestDearlyDepartedNoLongerTouchesItself guards the wrong-object defect: the
// expansion must not put the counter on Card.Self. Dearly Departed entering
// while itself in the graveyard is impossible, so move it to the battlefield
// (its own entry) and confirm it does not self-pump even though its printed
// static would, if mis-expanded to Card.Self, put a counter on it.
func TestDearlyDepartedNoLongerTouchesItself(t *testing.T) {
	t.Parallel()
	e := etbZoneEngine(t)
	searchMoveByName(t, e, "Dearly Departed", state.ZBattlefield)
	if got := p1p1Of(t, e, "Dearly Departed"); got != 0 {
		t.Fatalf("Dearly Departed entered with %d +1/+1 counters, want 0 (its expansion must target the entering filter, never Card.Self)", got)
	}
}

// TestBramblewoodParagonPumpsOtherWarriorsNotItself pins the same class on
// the far larger Battlefield-zone population (28 of the 29 AddExtraCounter
// carriers): the filter now selects OTHER Warriors, and ActiveZones$
// Battlefield makes the static inert from the graveyard. Bramblewood Paragon
// is in no legacy golden deck.
func TestBramblewoodParagonPumpsOtherWarriorsNotItself(t *testing.T) {
	t.Parallel()
	t.Run("on battlefield, other Warrior gets a counter", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, _ := searchEngine(t, reg, "Bramblewood Paragon", "Akki Avalanchers")
		searchMoveByName(t, e, "Bramblewood Paragon", state.ZBattlefield)
		searchMoveByName(t, e, "Akki Avalanchers", state.ZBattlefield)
		if got := p1p1Of(t, e, "Akki Avalanchers"); got != 1 {
			t.Fatalf("other Warrior entered with %d +1/+1 counters, want 1", got)
		}
		if got := p1p1Of(t, e, "Bramblewood Paragon"); got != 0 {
			t.Fatalf("Paragon pumped itself (%d counters); the filter is Creature...Other", got)
		}
	})
	t.Run("in graveyard, the static is inert", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, _ := searchEngine(t, reg, "Bramblewood Paragon", "Akki Avalanchers")
		searchMoveByName(t, e, "Bramblewood Paragon", state.ZGraveyard)
		searchMoveByName(t, e, "Akki Avalanchers", state.ZBattlefield)
		if got := p1p1Of(t, e, "Akki Avalanchers"); got != 0 {
			t.Fatalf("Warrior entered with %d +1/+1 counters, want 0 (Paragon in graveyard)", got)
		}
	})
}

// TestEtbReplacementZoneFilterIsRead asserts the expansion's parsed params
// directly, so a regression names the field rather than only the symptom.
func TestEtbReplacementZoneFilterIsRead(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	c, ok := reg.Lookup("Dearly Departed")
	if !ok {
		t.Fatal("Dearly Departed missing from corpus")
	}
	var found *cards.Repl
	for i := range c.Faces[0].Repls {
		if c.Faces[0].Repls[i].Params["Keyword"] == "ETBReplacement" {
			found = &c.Faces[0].Repls[i]
			break
		}
	}
	if found == nil {
		t.Fatal("Dearly Departed has no ETBReplacement repl")
	}
	if got := found.Params["ValidCard"]; got != "Creature.Human+YouCtrl" {
		t.Fatalf("ValidCard$ = %q, want Creature.Human+YouCtrl", got)
	}
	if got := found.Params["ActiveZones"]; got != "Graveyard" {
		t.Fatalf("ActiveZones$ = %q, want Graveyard", got)
	}
}
