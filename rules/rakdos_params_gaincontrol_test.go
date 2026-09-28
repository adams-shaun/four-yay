package rules

// The Rakdos-params brief, gap 1: ChangeZone's GainControl$ parameter.
// Reanimate is the brief's named corpus card: "Put target creature card from
// a graveyard onto the battlefield under your control. You lose life equal
// to its mana value." Before this work the GainControl$ key was never read,
// so the reanimated creature stayed under its OWNER's control.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestReanimateGainsControlOfGraveyardCreature(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Reanimate"))
	// An opposing creature with a known mana value, in seat 1's graveyard.
	victim := e.G.AddObject(card(t, "Name:Grave Titan\nManaCost:4 B B\nTypes:Creature Zombie Giant\nPT:6/6\nOracle:x\n"), 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim.ID, From: state.ZHand, To: state.ZGraveyard})

	spell := e.G.Zone(state.ZHand, 0)[0]
	addMana(t, e, 0, "B")
	castObjTargeting(t, e, spell, victim.ID)

	o := e.G.Obj(victim.ID)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("reanimated creature zone=%v, want battlefield", o)
	}
	if o.Controller != 0 {
		t.Fatalf("reanimated creature controller=%d, want 0 (GainControl$ True)", o.Controller)
	}
	if o.Owner != 1 {
		t.Fatalf("reanimated creature owner=%d, want 1 (control changes, ownership does not)", o.Owner)
	}
	// The SubAbility$ DBLoseLifeYou chain: You lose life equal to its mana
	// value (SVar:X:RememberedLKI$CardManaCost over the remembered LKI).
	if life := e.G.Players[0].Life; life != 14 {
		t.Fatalf("seat 0 life=%d, want 14 (start 20 - 6 mana value)", life)
	}
}

func TestReanimateLifeIsPaidEvenWhenTargetLeaves(t *testing.T) {
	t.Parallel()
	// GainControl$ is a parameter of the movement, not a separate effect: a
	// target that is gone at resolution fizzles the movement and the chained
	// lose-life reads no remembered LKI (Reanimate's documented CR 608.2b
	// fizzle), so no life is lost.
	e := handEngine(t, corpusAlternativeCard(t, "Reanimate"))
	victim := e.G.AddObject(card(t, "Name:Grave Titan\nManaCost:4 B B\nTypes:Creature Zombie Giant\nPT:6/6\nOracle:x\n"), 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim.ID, From: state.ZHand, To: state.ZGraveyard})

	spell := e.G.Zone(state.ZHand, 0)[0]
	addMana(t, e, 0, "B")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision: %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == victim.ID {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no option for graveyard target %d: %+v", victim.ID, d.Options)
	}
	submitChoices(t, e, tgt)
	// Remove the target before the spell resolves.
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim.ID, From: state.ZGraveyard, To: state.ZExile})
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Life != 20 {
		t.Fatalf("seat 0 life=%d, want 20 (fizzled Reanimate loses no life)", e.G.Players[0].Life)
	}
}
