package events

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A restricted "until end of combat" unit spent during combat must take its
// combat-persistent tally with it: the end-of-combat demotion may then not
// empty an unrelated turn-persistent unit of the same colour in its place.
func TestRestrictedCombatPersistentSpendKeepsTurnPersistentUnit(t *testing.T) {
	g := twoSeatGame(t)
	g.Step = state.StepEndCombat
	p := &g.Players[0]
	rest := ManaRestrictionText("Spell", 7)
	Apply(g, Event{Kind: ManaAdd, Player: 0, Counter: "G", Amount: 1, Text: ManaCombatPersistentText(rest)})
	Apply(g, Event{Kind: ManaAdd, Player: 0, Counter: "G", Amount: 1, Text: ManaPersistentText("")})
	if p.Pool[state.MG] != 2 || p.PersistentMana[state.MG] != 2 || p.CombatMana[state.MG] != 1 ||
		len(p.RestrictedMana) != 1 || !p.RestrictedMana[0].Persistent || !p.RestrictedMana[0].Combat {
		t.Fatalf("precondition: one combat-persistent restricted G and one turn-persistent plain G: %+v", *p)
	}
	Apply(g, Event{Kind: ManaAdd, Player: 0, Counter: "G", Amount: -1, Text: rest})
	if p.Pool[state.MG] != 1 || p.PersistentMana[state.MG] != 1 || p.CombatMana[state.MG] != 0 || len(p.RestrictedMana) != 0 {
		t.Fatalf("restricted spend did not consume its combat-persistent provenance: %+v", *p)
	}
	Apply(g, Event{Kind: StepChange, Step: state.StepMain2})
	Apply(g, Event{Kind: ManaClear, Player: 0})
	if p.Pool[state.MG] != 1 || p.PersistentMana[state.MG] != 1 {
		t.Fatalf("end of combat emptied the unrelated turn-persistent unit: %+v", *p)
	}
}

// A restricted combat-persistent batch that survives combat is demoted with
// its units, so the boundary's ManaClear empties units and batch together
// instead of keeping a phantom restriction over mana that is gone.
func TestRestrictedCombatPersistentBatchDemotesAtEndCombat(t *testing.T) {
	g := twoSeatGame(t)
	g.Step = state.StepEndCombat
	p := &g.Players[0]
	rest := ManaRestrictionText("Spell", 7)
	Apply(g, Event{Kind: ManaAdd, Player: 0, Counter: "G", Amount: 2, Text: ManaCombatPersistentText(rest)})
	Apply(g, Event{Kind: ManaAdd, Player: 0, Counter: "G", Amount: 1, Text: ManaPersistentText("")})
	Apply(g, Event{Kind: ManaAdd, Player: 0, Counter: "G", Amount: -1, Text: rest})
	if p.Pool[state.MG] != 2 || p.CombatMana[state.MG] != 1 || len(p.RestrictedMana) != 1 ||
		p.RestrictedMana[0].Amount != 1 || !p.RestrictedMana[0].Combat {
		t.Fatalf("a partial restricted spend should leave one combat-restricted unit beside one turn-persistent unit: %+v", *p)
	}
	Apply(g, Event{Kind: StepChange, Step: state.StepMain2})
	if p.PersistentMana[state.MG] != 1 || p.CombatMana[state.MG] != 0 ||
		len(p.RestrictedMana) != 1 || p.RestrictedMana[0].Persistent || p.RestrictedMana[0].Combat {
		t.Fatalf("end of combat should demote the remaining restricted unit: %+v", *p)
	}
	Apply(g, Event{Kind: ManaClear, Player: 0})
	if p.Pool[state.MG] != 1 || p.PersistentMana[state.MG] != 1 || len(p.RestrictedMana) != 0 {
		t.Fatalf("boundary should clear only the demoted unit and its batch: %+v", *p)
	}
}
