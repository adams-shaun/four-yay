package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestBoardEqualMatchesDeepEqual(t *testing.T) {
	for typ, n := range map[reflect.Type]int{
		reflect.TypeFor[Creature]():  6,
		reflect.TypeFor[Commander](): 3,
	} {
		if typ.NumField() != n {
			t.Fatalf("%s has %d fields, board_equal.go was written against %d: extend it", typ, typ.NumField(), n)
		}
	}
	cr := []Creature{
		{}, {Keywords: []string{}}, {Keywords: []string{"Flying"}}, {Keywords: []string{"Flying", "Haste"}},
		{Power: 1}, {Toughness: 1}, {Damage: 1}, {Tapped: true}, {Controller: 1},
	}
	for i := range cr {
		for j := range cr {
			if got, want := creatureEqual(&cr[i], &cr[j]), reflect.DeepEqual(cr[i], cr[j]); got != want {
				t.Fatalf("creatures %d, %d: %v, DeepEqual %v", i, j, got, want)
			}
		}
	}
	cm := []Commander{
		{}, {Damage: map[state.PlayerID]int32{}}, {Damage: map[state.PlayerID]int32{1: 3}}, {Damage: map[state.PlayerID]int32{1: 4}},
		{Damage: map[state.PlayerID]int32{2: 3}}, {Casts: 1}, {InCommandZone: true},
	}
	for i := range cm {
		for j := range cm {
			if got, want := commanderEqual(&cm[i], &cm[j]), reflect.DeepEqual(cm[i], cm[j]); got != want {
				t.Fatalf("commanders %d, %d: %v, DeepEqual %v", i, j, got, want)
			}
		}
	}
}
