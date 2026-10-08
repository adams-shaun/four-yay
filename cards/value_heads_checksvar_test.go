package cards

import (
	"reflect"
	"testing"
)

// TestValueHeadAbilityLineCheckSVar covers the inline-gate carrier: an
// A:AB$ / A:SP$ line gated by CheckSVar$ Count$… must attribute its head,
// and the shapes the gate helper must NOT classify (a Count$ token in
// descriptive text, a non-Count$ gate value, a gate on a line the walk does
// not visit) must stay unclassified.
func TestValueHeadAbilityLineCheckSVar(t *testing.T) {
	f := &Face{
		Abilities: []*SA{{
			Kind:   "AB",
			API:    "Draw",
			Params: map[string]string{"CheckSVar": "Count$CountersRemovedThisTurn ENERGY You", "SVarCompare": "GE4"},
		}},
	}
	if got, want := f.ValueHeads(), []string{"count:CountersRemovedThisTurn"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ValueHeads() = %v, want %v", got, want)
	}

	// ConditionCheckSVar$ is the rider spelling and is a carrier too.
	rider := &Face{Abilities: []*SA{{
		Kind:   "SP",
		API:    "UntapAll",
		Params: map[string]string{"ConditionCheckSVar": "Count$FinishedUpkeepsThisTurn"},
	}}}
	if got, want := rider.ValueHeads(), []string{"count:FinishedUpkeepsThisTurn"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ConditionCheckSVar ValueHeads() = %v, want %v", got, want)
	}

	// A SubAbility chain is a carrier (the attr walk recurses).
	sub := &Face{Abilities: []*SA{{
		Kind:   "AB",
		API:    "Activate",
		Params: map[string]string{"Execute": "TrigDraw"},
		Sub: &SA{
			Kind:   "DB",
			API:    "Draw",
			Params: map[string]string{"CheckSVar": "Count$ThisTurnCast_Card.YouCtrl"},
		},
	}}}
	if got, want := sub.ValueHeads(), []string{"count:ThisTurnCast"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SubAbility ValueHeads() = %v, want %v", got, want)
	}

	// Negative shapes: a Count$ token in descriptive text or a non-gate
	// parameter, a gate whose value is a plain SVar name, a numeric gate,
	// and a gate on a static line the attr walk does not visit.
	for _, sa := range []*SA{
		{Kind: "AB", API: "Draw", Params: map[string]string{"SpellDescription": "Count$CountersRemovedThisTurn"}},
		{Kind: "AB", API: "Draw", Params: map[string]string{"Cost": "Count$CountersRemovedThisTurn"}},
		{Kind: "AB", API: "Draw", Params: map[string]string{"CheckSVar": "PlayerCount"}},
		{Kind: "AB", API: "Draw", Params: map[string]string{"CheckSVar": "3"}},
		{Kind: "AB", API: "Draw", Params: map[string]string{"CheckSVar": "SomeSVarName"}},
	} {
		if got := cardsValueHeads(sa); len(got) != 0 {
			t.Errorf("ValueHeads() with %+v = %v, want none", sa, got)
		}
	}

	// A gate on a static's Params is outside the attr walk's scope (the
	// follow-up ticket); it must stay unattributed here.
	static := &Face{
		Statics: []Static{{Params: map[string]string{"CheckSVar": "Count$CommittedCrimeThisTurn"}}},
	}
	if got := static.ValueHeads(); len(got) != 0 {
		t.Errorf("static gate ValueHeads() = %v, want none", got)
	}
	if got := ValueHeadGateExpression(map[string]string{"CheckSVar": "Count$CommittedCrimeThisTurn"}); len(got) != 1 {
		t.Errorf("gate helper itself must still classify a static gate expression: %v", got)
	}
}

// cardsValueHeads wraps one SA in a face with no other carriers, so a
// negative case cannot be satisfied by an unrelated attribute.
func cardsValueHeads(sa *SA) []string {
	return (&Face{Abilities: []*SA{sa}}).ValueHeads()
}
