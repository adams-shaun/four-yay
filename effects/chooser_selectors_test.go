package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestChooserSelectorsResolveThroughDefined(t *testing.T) {
	h := newHost(t, 3)
	card := mkCard(t, "Name:ChooserSelectorFixture\nTypes:Creature\nPT:1/1\nOracle:x\n")
	source := h.g.AddObject(card, 0)
	object := h.g.AddObject(card, 1)
	if h.g.Obj(source.ID) == nil || h.g.Obj(object.ID) == nil {
		t.Fatal("precondition: fixture objects were not added")
	}
	if h.g.Obj(object.ID).Controller != 1 {
		t.Fatalf("precondition: object controller = %d, want 1", h.g.Obj(object.ID).Controller)
	}
	c := &Ctx{Source: source.ID, Controller: 0,
		Remembered: []state.Target{{Player: 1, IsPlayer: true}},
		TriggerContext: TriggerContext{
			TriggerPlayer: state.Target{Player: 1, IsPlayer: true},
			TriggerCard:   object.ID,
		}}
	if len(c.Remembered) == 0 || !c.Remembered[0].IsPlayer || c.Remembered[0].Player == c.Controller {
		t.Fatal("precondition: Remembered player is not bound to a different live seat")
	}
	if c.TriggerPlayer.Player == c.Controller {
		t.Fatal("precondition: TriggeredPlayer is not bound to a different seat")
	}
	if h.g.Obj(c.TriggerCard) == nil {
		t.Fatal("precondition: TriggerCard object is absent")
	}

	for _, spelling := range []string{"Remembered", "TriggeredPlayer", "TriggeredCardController"} {
		for _, got := range []state.PlayerID{
			searchChooser(h, c, ChangeZoneOf(chooserSA(spelling)), 2),
			hiddenPickChooser(h, c, ChangeZoneOf(chooserSA(spelling)), 2),
		} {
			if got != 1 {
				t.Fatalf("Chooser$ %s resolved to seat %d, want bound seat 1", spelling, got)
			}
		}
	}

	// An unknown selector and a known-but-unbound/dead selector retain the
	// caller-specific defaults; an absent selector keeps the hidden owner.
	for _, spelling := range []string{"DefendingPlayer", "TriggeredPlayer"} {
		unbound := *c
		unbound.Remembered = nil
		unbound.TriggerPlayer = state.Target{}
		if got := searchChooser(h, &unbound, ChangeZoneOf(chooserSA(spelling)), 0); got != 0 {
			t.Fatalf("searchChooser %s fallback = %d, want controller 0", spelling, got)
		}
		if got := hiddenPickChooser(h, &unbound, ChangeZoneOf(chooserSA(spelling)), 2); got != 2 {
			t.Fatalf("hiddenPickChooser %s fallback = %d, want owner 2", spelling, got)
		}
	}
	dead := *c
	h.g.Players[1].Lost = true
	if got := searchChooser(h, &dead, ChangeZoneOf(chooserSA("Remembered")), 0); got != 0 {
		t.Fatalf("searchChooser dead Remembered = %d, want controller 0", got)
	}
	if got := hiddenPickChooser(h, &dead, ChangeZoneOf(chooserSA("Remembered")), 2); got != 2 {
		t.Fatalf("hiddenPickChooser dead Remembered = %d, want owner 2", got)
	}
	if got := hiddenPickChooser(h, c, ChangeZoneOf(&cards.SA{}), 2); got != 2 {
		t.Fatalf("hiddenPickChooser absent Chooser$ = %d, want owner 2", got)
	}
}
