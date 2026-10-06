package searchprobe

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// frameEqualBase is a frame whose every leaf and slice is populated, so each
// single mutation below is visible to reflect.DeepEqual.
func frameEqualBase() Frame {
	return Frame{
		Identities: []Identity{{ID: 1, Name: "a", Owner: 1}, {ID: 2, Name: "b", Owner: 0}},
		Events: []ObservedEvent{
			{Kind: events.Kind(3), Player: 1, Obj: 4, From: state.Zone(1), To: state.Zone(2), Amount: 5, Step: state.Step(2),
				Counter: "c", Text: "t", IDs: []uint32{7, 8}, Pairs: [][2]uint32{{1, 2}}, Secret: true},
			{Kind: events.Kind(4), IDs: []uint32{}, Pairs: [][2]uint32{}},
		},
		Decision: &ObservedDecision{Player: 1, Kind: decision.Kind("target"), Min: 1, Max: 2, Source: 9, EffectAPI: "x", DamageKnown: true, Damage: 3,
			Options: []ObservedOption{{Action: Action{Decision: decision.Kind("target"), Source: 1, Kind: "k", Obj: 2, Attacker: 3, Player: 1,
				Ability: 1, AltCostIndex: 2, Amount: 3, Mode: "m", SVar: "s", Value: "v"}, Group: 1, Required: true}}},
	}
}

// mutateNth applies the n-th single mutation (in walk order) to v and reports
// whether there was one: every scalar leaf is changed, and every slice is in
// turn set to nil, set to an empty non-nil slice, and truncated.
func mutateNth(v reflect.Value, n *int) bool {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return false
		}
		if *n == 0 {
			v.Set(reflect.Zero(v.Type()))
			return true
		}
		*n--
		return mutateNth(v.Elem(), n)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() && mutateNth(v.Field(i), n) {
				return true
			}
		}
		return false
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if mutateNth(v.Index(i), n) {
				return true
			}
		}
		return false
	case reflect.Slice:
		for variant := 0; variant < 3; variant++ {
			if *n == 0 {
				switch variant {
				case 0:
					v.Set(reflect.Zero(v.Type()))
				case 1:
					v.Set(reflect.MakeSlice(v.Type(), 0, 0))
				case 2:
					if v.Len() == 0 {
						v.Set(reflect.Append(v, reflect.Zero(v.Type().Elem())))
					} else {
						v.Set(v.Slice(0, v.Len()-1))
					}
				}
				return true
			}
			*n--
		}
		for i := 0; i < v.Len(); i++ {
			if mutateNth(v.Index(i), n) {
				return true
			}
		}
		return false
	case reflect.String:
		if *n == 0 {
			v.SetString(v.String() + "!")
			return true
		}
	case reflect.Bool:
		if *n == 0 {
			v.SetBool(!v.Bool())
			return true
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if *n == 0 {
			v.SetInt(v.Int() + 1)
			return true
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if *n == 0 {
			v.SetUint(v.Uint() + 1)
			return true
		}
	default:
		panic("frame_equal_test: unhandled kind " + v.Kind().String() + " (" + v.Type().String() + ")")
	}
	*n--
	return false
}

// TestFrameEqualityMatchesDeepEqual holds sameFrame's typed comparisons to
// reflect.DeepEqual under every single-leaf and slice-shape mutation of a
// fully populated frame, so a field added to Identity, ObservedEvent,
// ObservedDecision, ObservedOption or Action without a comparison fails here.
func TestFrameEqualityMatchesDeepEqual(t *testing.T) {
	a := frameEqualBase()
	if !sameFrame(a, frameEqualBase()) {
		t.Fatal("equal frames compare unequal")
	}
	mutations := 0
	for k := 0; ; k++ {
		b := frameEqualBase()
		n := k
		if !mutateNth(reflect.ValueOf(&b).Elem(), &n) {
			break
		}
		mutations++
		want := a.Board.Sum == b.Board.Sum && reflect.DeepEqual(a.Identities, b.Identities) && reflect.DeepEqual(a.Events, b.Events) && reflect.DeepEqual(a.Decision, b.Decision)
		if got := sameFrame(a, b); got != want {
			t.Fatalf("mutation %d: sameFrame = %v, reflect.DeepEqual = %v", k, got, want)
		}
		if got := sameFrame(b, a); got != want {
			t.Fatalf("mutation %d (swapped): sameFrame = %v, reflect.DeepEqual = %v", k, got, want)
		}
	}
	if mutations < 60 {
		t.Fatalf("only %d mutations walked; the fixture lost coverage", mutations)
	}
}
