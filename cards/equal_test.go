package cards

import (
	"reflect"
	"testing"
)

// TestEqualCoversEveryField holds equal.go's field lists to the structs: a
// field added to an IR node fails here until its Equal function compares it.
func TestEqualCoversEveryField(t *testing.T) {
	for _, c := range []struct {
		v    any
		want int
	}{
		{SA{}, saFieldCount}, {Trigger{}, triggerFieldCount}, {Static{}, staticFieldCount},
		{Repl{}, replFieldCount}, {ParamSet{}, paramSetFieldCount}, {Face{}, faceFieldCount},
	} {
		ty := reflect.TypeOf(c.v)
		if n := ty.NumField(); n != c.want {
			t.Errorf("%s has %d fields, equal.go covers %d: extend its Equal function and count", ty, n, c.want)
		}
	}
}

// TestEqualMatchesDeepEqual pins the typed functions to reflect.DeepEqual on
// hand-built nodes that differ in one place each.
func TestEqualMatchesDeepEqual(t *testing.T) {
	mk := func() *Face {
		sub := &SA{Kind: "DB", API: "Draw", Params: map[string]string{"NumCards": "1"}}
		sa := &SA{Kind: "AB", API: "Mana", Params: map[string]string{"Produced": "G"}, Sub: sub, Line: "x"}
		eff := &SA{Kind: "DB", API: "Pump"}
		return &Face{Name: "F", Types: []string{"Creature"}, Abilities: []*SA{sa},
			Triggers: []Trigger{{Mode: "ChangesZone", Params: map[string]string{"A": "b"}, Effect: eff}},
			Statics:  []Static{{Mode: "Continuous"}}, Repls: []Repl{{Event: "Moved", With: eff}},
			SVars:    map[string]string{"X": "1"}}
	}
	muts := []func(f *Face){
		func(f *Face) {},
		func(f *Face) { f.Types = []string{} },
		func(f *Face) { f.Types = nil },
		func(f *Face) { f.Abilities[0].Sub.Params["NumCards"] = "2" },
		func(f *Face) { f.Abilities[0].Sub = nil },
		func(f *Face) { f.Triggers[0].Effect.API = "Draw" },
		func(f *Face) { f.Repls[0].With = nil },
		func(f *Face) { f.SVars = map[string]string{} },
		func(f *Face) { f.Statics = nil },
		func(f *Face) { f.Abilities[0].ps = newParamSet(f.Abilities[0].Params) },
		func(f *Face) { f.power = 3 },
	}
	for i, mi := range muts {
		for j, mj := range muts {
			a, b := mk(), mk()
			mi(a)
			mj(b)
			if got, want := FaceEqual(a, b), reflect.DeepEqual(*a, *b); got != want {
				t.Errorf("FaceEqual(mut %d, mut %d) = %v, DeepEqual %v", i, j, got, want)
			}
			if got, want := SAEqual(a.Abilities[0], b.Abilities[0]), reflect.DeepEqual(*a.Abilities[0], *b.Abilities[0]); got != want {
				t.Errorf("SAEqual(mut %d, mut %d) = %v, DeepEqual %v", i, j, got, want)
			}
		}
	}
}
