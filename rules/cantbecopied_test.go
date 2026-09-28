package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestCantBeCopiedBlocksStormCopies(t *testing.T) {
	protected := "Name:Test Uncopyable Storm\nManaCost:0\nTypes:Instant\nK:Storm\n" +
		"S:Mode$ CantBeCopied | ValidCard$ Card.Self | EffectZone$ Stack\n" +
		"A:SP$ Draw | NumCards$ 1\nOracle:x\n"
	setup := "Name:Test Setup Spell\nManaCost:R\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	e, _, _ := newFixtureDeck(t, 97, protected, setup)
	setupID := addToHand(t, e, 0, setup)
	stormID := addToHand(t, e, 0, protected)
	addMana(t, e, 0, "R")
	castObj(t, e, setupID)
	passUntilStackEmpty(t, e, 20)
	if e.CastThisTurn() != 1 {
		t.Fatalf("precondition: setup spell cast count = %d, want one before Storm", e.CastThisTurn())
	}

	castCardNow(t, e, "Test Uncopyable Storm")
	if e.G.Obj(stormID) == nil || e.G.Obj(stormID).Zone != state.ZStack {
		t.Fatalf("precondition: protected Storm spell %d is not on the stack", stormID)
	}
	if e.SpellCopyAllowed(stormID) {
		t.Fatal("precondition: CantBeCopied static did not prohibit this stack spell")
	}
	passUntilStackEmpty(t, e, 30)
	for _, o := range e.G.Objs {
		if o.IsCopy {
			t.Fatalf("protected spell produced copy %d", o.ID)
		}
	}
}
