package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

const aetherVialProjectionSrc = "Name:Aether Vial\nManaCost:1\nTypes:Artifact\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"

func TestSpellEffectiveCostThalia(t *testing.T) {
	e, _, id := newFixtureDeck(t, 9601, aetherVialProjectionSrc, taxWardenSrc)
	putCreature(t, e, 0, taxWardenSrc)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand {
		t.Fatalf("Aether Vial fixture must be in hand, got object=%+v", o)
	}
	if got := o.Face().ManaCost; got != "1" {
		t.Fatalf("printed precondition ManaCost = %q, want 1", got)
	}
	got := e.SpellEffectiveCost(0, id)
	if got != "2" || got == o.Face().ManaCost {
		t.Fatalf("SpellEffectiveCost = %q, printed = %q; want a changed effective cost 2", got, o.Face().ManaCost)
	}
	v := view.Project(e.G, e, 0, nil)
	for _, card := range v.Players[0].Hand {
		if card.ID == id {
			if card.EffectiveManaCost != "2" {
				t.Fatalf("view effective cost = %q, want 2", card.EffectiveManaCost)
			}
			return
		}
	}
	t.Fatalf("Aether Vial %d was not projected in its owner's hand", id)
}

func TestSpellEffectiveCostUntaxedOmitted(t *testing.T) {
	e, _, id := newFixtureDeck(t, 9602, aetherVialProjectionSrc)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand {
		t.Fatalf("Aether Vial fixture must be in hand, got object=%+v", o)
	}
	if got := e.SpellEffectiveCost(0, id); got != "" {
		t.Fatalf("untaxed effective cost = %q, want empty", got)
	}
	v := view.Project(e.G, e, 0, nil)
	for _, card := range v.Players[0].Hand {
		if card.ID == id {
			if card.EffectiveManaCost != "" {
				t.Fatalf("untaxed view effective cost = %q, want omitted", card.EffectiveManaCost)
			}
			return
		}
	}
	t.Fatalf("Aether Vial %d was not projected in its owner's hand", id)
}
