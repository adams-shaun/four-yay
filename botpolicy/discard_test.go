package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestDiscardRetention(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cards []Card
		max   int
		want  []int
	}{
		{"basic development", []Card{{Basic: true}, {CMC: 1}, {CMC: 8}}, 1, []int{2}},
		{"nonbasic development", []Card{{CMC: 0}, {CMC: 1}, {CMC: 8}}, 1, []int{2}},
		{"cheap spell", []Card{{CMC: 1, Castable: true}, {CMC: 8, Castable: true}}, 1, []int{1}},
		{"expensive creature", []Card{{CMC: 1}, {CMC: 8, Creature: true, Power: 8}}, 1, []int{1}},
		{"cheap creature", []Card{{CMC: 1, Creature: true, Power: 1}, {CMC: 8}}, 1, []int{1}},
		{"class neutral ties", []Card{{CMC: 3, Creature: true}, {CMC: 3}}, 1, []int{0}},
		{"multiple sorted indices", []Card{{CMC: 5}, {Basic: true}, {CMC: 8}, {CMC: 1}}, 2, []int{0, 2}},
		{"only lands", []Card{{Basic: true}, {CMC: 0}}, 1, []int{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := Board{Cards: TableOf(map[state.ObjID]Card{})}
			d := decision.Decision{Kind: decision.KChoose, Min: tc.max, Max: tc.max}
			for i, c := range tc.cards {
				id := state.ObjID(i + 1)
				b.Cards.Set(id, c)
				d.Options = append(d.Options, decision.Option{Index: i, Kind: "discard", Obj: id})
			}
			for _, seed := range []uint64{1, 99} {
				in := Decide(b, &d, rng(seed))
				if err := d.Validate(in); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(in.Choices, tc.want) {
					t.Fatalf("got %v want %v", in.Choices, tc.want)
				}
			}
		})
	}
}

func TestCommanderZoneManaAxisBoundary(t *testing.T) {
	d := decision.Decision{Kind: decision.KCommanderZone, Min: 1, Max: 1, Options: []decision.Option{{Index: 0, Kind: "command_zone", Obj: 1}, {Index: 1, Kind: "leave", Obj: 1}}}
	for _, tc := range []struct {
		cmc, power, casts int32
		want              int
	}{
		{2, 2, 3, 0}, {2, 2, 4, 1}, {12, 12, 1, 0}, {12, 12, 2, 1},
		{2, 0, 3, 0},   // worth 30 minus 30: same zero boundary as chooseCast
		{0, 0, 100, 0}, // zero mana value carries zero scaled tax
	} {
		b := Board{Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, CMC: tc.cmc, Power: tc.power}}), Commanders: TableOf(map[state.ObjID]Commander{1: {Casts: tc.casts}})}
		if in := Decide(b, &d, rng(1)); !reflect.DeepEqual(in.Choices, []int{tc.want}) {
			t.Fatalf("%+v: %v", tc, in.Choices)
		}
	}
}
