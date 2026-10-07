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
