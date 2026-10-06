package searchprobe

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestEqualMatchesDeepEqual: the typed equalities agree with
// reflect.DeepEqual on every pair of a set covering nil against empty,
// each field differing on its own, and equal copies; and the field counts
// they were written against still hold.
func TestEqualMatchesDeepEqual(t *testing.T) {
	for typ, n := range map[reflect.Type]int{
		reflect.TypeFor[Identity]():         3,
		reflect.TypeFor[ObservedDecision](): 9,
		reflect.TypeFor[ObservedOption]():   3,
		reflect.TypeFor[Action]():           12,
	} {
		if typ.NumField() != n {
			t.Fatalf("%s has %d fields, equal.go was written against %d: extend it", typ, typ.NumField(), n)
		}
		for i := 0; i < typ.NumField(); i++ {
			if !typ.Field(i).Type.Comparable() || typ.Field(i).Type.Kind() == reflect.Pointer {
				if typ.Field(i).Type.Kind() != reflect.Slice {
					t.Fatalf("%s.%s is not a pointer-free comparable field", typ, typ.Field(i).Name)
				}
			}
		}
	}
	ids := [][]Identity{nil, {}, {{ID: 1, Name: "a", Owner: 1}}, {{ID: 1, Name: "a", Owner: 0}}, {{ID: 1, Name: "b", Owner: 1}}, {{ID: 2, Name: "a", Owner: 1}}, {{ID: 1, Name: "a", Owner: 1}, {}}}
	for _, a := range ids {
		for _, b := range ids {
			if got, want := IdentitiesEqual(a, b), reflect.DeepEqual(a, b); got != want {
				t.Fatalf("IdentitiesEqual(%v, %v) = %v, DeepEqual %v", a, b, got, want)
			}
		}
		c := append(a[:0:0], a...)
		if a != nil && !IdentitiesEqual(a, c) {
			t.Fatal("a copy differs")
		}
	}
	base := ObservedDecision{Player: 1, Kind: decision.KTarget, Min: 1, Max: 2, Source: 3, Options: []ObservedOption{{Action: Action{Kind: "x", Obj: 4}, Group: 1, Required: true}}, EffectAPI: "Pump", DamageKnown: true, Damage: 2}
	muts := []func(*ObservedDecision){
		func(d *ObservedDecision) {},
		func(d *ObservedDecision) { d.Player = 0 },
		func(d *ObservedDecision) { d.Kind = decision.KPriority },
		func(d *ObservedDecision) { d.Min = 0 },
		func(d *ObservedDecision) { d.Max = 1 },
		func(d *ObservedDecision) { d.Source = 0 },
		func(d *ObservedDecision) { d.Options = nil },
		func(d *ObservedDecision) { d.Options = []ObservedOption{} },
		func(d *ObservedDecision) { d.Options = []ObservedOption{{Action: Action{Kind: "x", Obj: 4}, Group: 1}} },
		func(d *ObservedDecision) { d.Options = []ObservedOption{{Action: Action{Kind: "x", Obj: 4, Value: "v"}, Group: 1, Required: true}} },
		func(d *ObservedDecision) { d.EffectAPI = "" },
		func(d *ObservedDecision) { d.DamageKnown = false },
		func(d *ObservedDecision) { d.Damage = 0 },
	}
	decs := []*ObservedDecision{nil}
	for _, m := range muts {
		d := base
		d.Options = append([]ObservedOption(nil), base.Options...)
		m(&d)
		decs = append(decs, &d)
	}
	for _, a := range decs {
		for _, b := range decs {
			if got, want := ObservedDecisionEqual(a, b), reflect.DeepEqual(a, b); got != want {
				t.Fatalf("ObservedDecisionEqual(%+v, %+v) = %v, DeepEqual %v", a, b, got, want)
			}
		}
	}
}
