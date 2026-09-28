package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestDiscardMultiTargetAsksEveryTarget is the end-to-end carrier for the
// per-target cursor: a TgtChoose discard whose acting-player list holds TWO
// players (`Defined$ You & Opponent` — caster seat 0, opponent seat 1) must
// ask each one in order, and each answer must be applied to the target that
// gave it. The pre-fix walk consumed every resume's answer at target 0's
// cursor (DiscardTarget was never threaded from the resume point), so target
// 0's answer was re-applied to a hand that never held target 1's cards and
// target 1 was re-asked forever — targets 2..n were abandoned.
func TestDiscardMultiTargetAsksEveryTarget(t *testing.T) {
	t.Parallel()
	ms := "Name:MultiMind\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Discard | Defined$ You & Opponent | Mode$ TgtChoose | NumCards$ 1\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 96, ms)

	// Seat 0's hand: frog, cat (front card frog — picking cat proves the
	// choice is honoured). Seat 1's hand: bird, dog (front card bird).
	frog := e.G.AddObject(card(t, "Name:Frog\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	cat := e.G.AddObject(card(t, "Name:Cat\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	bird := e.G.AddObject(card(t, "Name:Bird\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	dog := e.G.AddObject(card(t, "Name:Dog\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, o := range []*state.Object{frog, cat, bird, dog} {
		e.G.Obj(o.ID).Zone = state.ZHand
	}
	e.G.SetZone(state.ZHand, 0, []state.ObjID{frog.ID, cat.ID, id})
	e.G.SetZone(state.ZHand, 1, []state.ObjID{bird.ID, dog.ID})
	if e.G.Zone(state.ZHand, 0)[0] != frog.ID || e.G.Zone(state.ZHand, 1)[0] != bird.ID {
		t.Fatal("precondition: the hands' front cards are not frog/bird — the choice-honoured assertions below would not discriminate")
	}

	addMana(t, e, 0, "B")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the first target's KModes discard decision, got %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("first ask = seat %d, want target 0 = the caster seat 0", d.Player)
	}
	if len(d.Options) != 2 {
		t.Fatalf("first ask options = %d, want seat 0's 2 hand cards", len(d.Options))
	}

	// Target 0 picks its SECOND card (cat), not the front card.
	submitChoices(t, e, d.Options[1].Index)

	// Target 1 must then be asked ITS OWN question, over ITS OWN hand — not
	// re-asked target 0's question, and not skipped.
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the second target's KModes discard decision, got %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("second ask = seat %d, want target 1 = seat 1 (targets 2..n were abandoned)", d.Player)
	}
	if len(d.Options) != 2 {
		t.Fatalf("second ask options = %d, want seat 1's 2 hand cards (not target 0's)", len(d.Options))
	}
	for _, o := range d.Options {
		if o.Obj == frog.ID || o.Obj == cat.ID {
			t.Fatal("target 1 was offered target 0's hand — the answer cursor did not move")
		}
	}
	submitChoices(t, e, d.Options[1].Index)
	passUntilStackEmpty(t, e, 20)

	// Each answer moved exactly the card its own target named.
	if z := e.G.Obj(cat.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("target 0's chosen card (cat) zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(dog.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("target 1's chosen card (dog) zone = %s, want Graveyard — the answer was lost or re-asked", z)
	}
	if z := e.G.Obj(frog.ID).Zone; z != state.ZHand {
		t.Fatalf("target 0's un-chosen card (frog) zone = %s, want Hand", z)
	}
	if z := e.G.Obj(bird.ID).Zone; z != state.ZHand {
		t.Fatalf("target 1's un-chosen card (bird) zone = %s, want Hand", z)
	}
}
