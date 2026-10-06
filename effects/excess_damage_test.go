package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestExcessDamagePredicate(t *testing.T) {
	g, ids := board(t)
	o := g.Obj(ids["theirBig"])
	if o.Zone != state.ZBattlefield {
		t.Fatalf("fixture recipient zone = %v", o.Zone)
	}
	if got := UnknownPredicates("Creature.wasDealtExcessDamageThisTurn"); len(got) != 0 {
		t.Fatalf("predicate not recognized: %v", got)
	}
	if MatchesSpec(g, "Creature.wasDealtExcessDamageThisTurn", o.ID, 0) {
		t.Fatal("missing excess-damage history matched")
	}
	events.Apply(g, events.Event{Kind: events.ExcessDamage, Obj: o.ID})
	if !MatchesSpec(g, "Creature.wasDealtExcessDamageThisTurn", o.ID, 0) {
		t.Fatal("recorded excess damage did not match")
	}
}
