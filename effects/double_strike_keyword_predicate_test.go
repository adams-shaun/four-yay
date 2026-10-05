package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestDoubleStrikeKeywordPredicate pins the space-bearing `withDouble Strike`/
// `withoutDouble Strike` filter (Forge keeps the space in the keyword name,
// and the registry keys are compact). It is the filter behind Super-Adaptoid's
// double-strike counter leg -- `ConditionPresent$ Creature.targetedBy+withDouble
// Strike | ConditionPresent2$ Card.Self+withoutDouble Strike` -- which used to
// read as an UNKNOWN predicate, leaving the group unresolved and running the
// counter rider unconditionally (the MSH oracle divergence). The test asserts
// the predicate is KNOWN (UnknownPredicates empty), so the group resolves,
// and that it matches a Double Strike carrier and no one else.
func TestDoubleStrikeKeywordPredicate(t *testing.T) {
	g, ids := board(t)
	add := func(owner state.PlayerID, script string) state.ObjID {
		c, diags := cards.ParseBytes("double-strike.txt", []byte(script))
		if len(diags) != 0 {
			t.Fatalf("parse card: %v", diags)
		}
		c.Link()
		for _, f := range c.Faces {
			f.ApplyIntrinsics()
		}
		o := g.AddObject(c, owner)
		o.Zone = state.ZBattlefield
		g.SetZone(state.ZBattlefield, owner, append(g.Zone(state.ZBattlefield, owner), o.ID))
		return o.ID
	}
	ace := add(0, "Name:Double Striker\nTypes:Creature Human Soldier\nK:Double Strike\nOracle:x\n")
	bear := ids["myBear"]

	// The preconditions the match assertions rest on: a battlefield carrier of
	// the keyword and a battlefield creature without it.
	if o := g.Obj(ace); o.Zone != state.ZBattlefield || !o.Face().HasKeyword("Double Strike") {
		t.Fatalf("precondition: expected a battlefield creature with Double Strike, got zone=%s kw=%v", o.Zone, o.Face().Keywords)
	}
	if o := g.Obj(bear); o.Zone != state.ZBattlefield || o.Face().HasKeyword("Double Strike") {
		t.Fatal("precondition: expected a battlefield creature without Double Strike")
	}

	for _, spec := range []string{"Creature.withDouble Strike", "Creature.withoutDouble Strike"} {
		if unknown := UnknownPredicates(spec); len(unknown) != 0 {
			t.Fatalf("UnknownPredicates(%q) = %v, want none: the Double Strike keyword must be registered", spec, unknown)
		}
	}
	if _, ok := keywordPredicateFor("withDouble Strike"); !ok {
		t.Fatal("keywordPredicateFor(\"withDouble Strike\") rejected the space-bearing Forge spelling")
	}

	for _, tc := range []struct {
		spec string
		id   state.ObjID
		want bool
	}{
		{"Creature.withDouble Strike", ace, true},
		{"Creature.withoutDouble Strike", ace, false},
		{"Creature.withDouble Strike", bear, false},
		{"Creature.withoutDouble Strike", bear, true},
	} {
		if got := MatchesSpec(g, tc.spec, tc.id, 0); got != tc.want {
			t.Errorf("MatchesSpec(%q) = %v, want %v", tc.spec, got, tc.want)
		}
	}
}
