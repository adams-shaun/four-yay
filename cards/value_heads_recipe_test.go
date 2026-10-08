package cards

import (
	"reflect"
	"testing"
)

func TestValueHeadNestedRecipeParameters(t *testing.T) {
	f := &Face{
		Abilities: []*SA{{Kind: "SP", API: "Activate", Params: map[string]string{"Execute": "Treasure"}}},
		SVars: map[string]string{
			"Treasure": "DB$ Token | TokenAmount$ Count$CrewSize | TokenScript$ c_a_treasure",
		},
	}
	if got, want := f.ValueHeads(), []string{"count:CrewSize"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ValueHeads() = %v, want %v", got, want)
	}

	// The same token in descriptive text or a nonnumeric token parameter is
	// not an evaluator-read numeric recipe expression.
	for _, body := range []string{
		"DB$ Token | TokenScript$ Count$CrewSize",
		"DB$ Note | Description$ TokenAmount$ Count$CrewSize",
		"DB$ Token | TokenAmount$ 2 | SpellDescription$ Count$CrewSize",
		"ST$ Continuous | TokenAmount$ Count$CrewSize",
	} {
		if got := ValueHeadRecipeExpressions(body); len(got) != 0 {
			t.Errorf("ValueHeadRecipeExpressions(%q) = %v, want none", body, got)
		}
	}
}

// TestValueHeadAbilityLineTokenAmount covers the printed-ability carrier: a
// face with NO SVars at all (Rise of the Varmints) whose A:SP$ Token /
// A:AB$ Token line reads TokenAmount$ Count$… must still report the head,
// and the same negative shapes must stay unclassified.
func TestValueHeadAbilityLineTokenAmount(t *testing.T) {
	f := &Face{
		Abilities: []*SA{{
			Kind:   "AB",
			API:    "Token",
			Params: map[string]string{"TokenAmount": "Count$ThisTurnEntered_Battlefield_Mount.YouCtrl,Vehicle.YouCtrl"},
		}},
	}
	if got, want := f.ValueHeads(), []string{"count:ThisTurnEntered"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ValueHeads() = %v, want %v", got, want)
	}

	// The no-SVar path must not be short-circuited: a face whose only read is
	// a printed ability line still has a value head.
	if len(f.SVars) != 0 {
		t.Fatal("precondition: the fixture must carry no SVars to exercise the empty-SVar walk")
	}

	// An SP$ Token line (Rise of the Varmints' shape) is a valid carrier too.
	sp := &Face{Abilities: []*SA{{
		Kind:   "SP",
		API:    "Token",
		Params: map[string]string{"TokenAmount": "Count$ValidGraveyard Creature.YouCtrl"},
	}}}
	if got, want := sp.ValueHeads(), []string{"count:ValidGraveyard"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SP ability ValueHeads() = %v, want %v", got, want)
	}

	// A SubAbility chain and a trigger/replacement effect are carriers too.
	sub := &Face{Abilities: []*SA{{
		Kind:   "AB",
		API:    "Activate",
		Params: map[string]string{"Execute": "TrigToken"},
		Sub: &SA{
			Kind:   "DB",
			API:    "Token",
			Params: map[string]string{"TokenAmount": "Count$ValidGraveyard Creature.YouCtrl"},
		},
	}}}
	if got, want := sub.ValueHeads(), []string{"count:ValidGraveyard"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SubAbility ValueHeads() = %v, want %v", got, want)
	}

	// The negative shapes carried from the SVar recipe: a non-Token API, a
	// non-Count$ amount, and a Count$ token in descriptive text.
	for _, sa := range []*SA{
		{Kind: "AB", API: "Activate", Params: map[string]string{"TokenAmount": "Count$CrewSize"}},
		{Kind: "AB", API: "Token", Params: map[string]string{"TokenAmount": "2"}},
		{Kind: "AB", API: "Token", Params: map[string]string{"SpellDescription": "Count$CrewSize"}},
		{Kind: "ST", API: "Continuous", Params: map[string]string{"TokenAmount": "Count$CrewSize"}},
	} {
		if got := (&Face{Abilities: []*SA{sa}}).ValueHeads(); len(got) != 0 {
			t.Errorf("ValueHeads() with %+v = %v, want none", sa, got)
		}
	}
}
