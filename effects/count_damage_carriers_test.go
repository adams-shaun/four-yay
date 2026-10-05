package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Pin every corpus carrier, including the filters whose source and recipient
// differ from Case and Ojer. Rith's excess-damage qualifier is a separate
// filter predicate; its SVar is pinned here so that gap cannot disappear.
func TestDamageCountOtherCorpusCarriers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	inferno, ok := reg.Lookup("Inferno Trap")
	if !ok {
		t.Fatal("corpus missing Inferno Trap")
	}
	wolverine, ok := reg.Lookup("Wolverine, Best There Is")
	if !ok {
		t.Fatal("corpus missing Wolverine")
	}
	rith, ok := reg.Lookup("Rith, Liberated Primeval")
	if !ok {
		t.Fatal("corpus missing Rith")
	}
	infernoBody := inferno.Faces[0].SVars["CreaturesDmg"]
	wolverineBody := wolverine.Faces[0].SVars["Y"]
	rithBody := rith.Faces[0].SVars["DragonCheck"]
	if infernoBody != "Count$NumDamageThisTurn Creature You" ||
		wolverineBody != "Count$NumDamageThisTurn Card.Self Creature.Other" ||
		rithBody != "Count$NumDamageThisTurn Card Creature.OppCtrl+wasDealtExcessDamageThisTurn,Planeswalker.OppCtrl+wasDealtExcessDamageThisTurn" {
		t.Fatalf("unexpected damage-count corpus SVars: %q; %q; %q", infernoBody, wolverineBody, rithBody)
	}
	h, c := fixtureHost(t)
	wolf := h.g.AddObject(wolverine, 0).ID
	other := h.g.AddObject(rith, 0).ID
	victim := h.g.AddObject(wolverine, 1).ID
	if h.g.Obj(wolf) == nil || h.g.Obj(other) == nil || h.g.Obj(victim) == nil {
		t.Fatal("precondition: damage sources and recipient must exist")
	}
	// One creature, two hits of three and two: Inferno asks for two
	// CREATURES, not five points or two hits.
	h.g.Obj(victim).DamageDealtThisTurn = []state.DamageDealtRecord{
		{Recipient: state.PlayerRef(0), Amount: 3}, {Recipient: state.PlayerRef(0), Amount: 2},
	}
	c.Source = wolf
	if got := EvalCount(h, c, infernoBody); got != 1 {
		t.Fatalf("Inferno count with one creature dealing five points = %d, want one source", got)
	}
	// Wolverine's own damage to another creature counts exactly once.
	h.g.Obj(wolf).DamageDealtThisTurn = []state.DamageDealtRecord{
		{Recipient: victim, RecipientZone: state.ZBattlefield, RecipientControl: 1, RecipientTypes: []string{"Creature"}, Amount: 3},
		{Recipient: victim, RecipientZone: state.ZBattlefield, RecipientControl: 1, RecipientTypes: []string{"Creature"}, Amount: 2},
	}
	if got := EvalCount(h, c, wolverineBody); got != 1 {
		t.Fatalf("Wolverine count for two hits on another creature = %d, want one source", got)
	}
	// Rith is source-independent: a single source with multiple hits on an
	// opponent's creature is one qualifying source (before the additional
	// excess-damage predicate is applied).
	h.g.Obj(other).DamageDealtThisTurn = append([]state.DamageDealtRecord(nil), h.g.Obj(wolf).DamageDealtThisTurn...)
	if got := EvalCount(h, c, "Count$NumDamageThisTurn Card Creature.OppCtrl"); got != 2 {
		t.Fatalf("Rith recipient shape with two distinct sources = %d, want 2, not four hits", got)
	}
	// Rith's explicit excess-damage predicate must not match ordinary
	// damage, even when the object was controlled by an opponent.
	if got := EvalCount(h, c, rithBody); got != 0 {
		t.Fatalf("Rith count without excess damage = %d, want 0", got)
	}
}
