package cost

import (
	"reflect"
	"testing"
)

func TestCostEqualCoversEveryField(t *testing.T) {
	if n := reflect.TypeOf(Cost{}).NumField(); n != costFieldCount {
		t.Fatalf("Cost has %d fields, Equal covers %d: extend Cost.Equal (equal.go) and costFieldCount", n, costFieldCount)
	}
	if reflect.TypeOf(CostPart{}).Comparable() == false {
		t.Fatal("CostPart is no longer comparable: Cost.Equal's sliceEqual needs a typed element compare")
	}
}

func TestCostEqualMatchesDeepEqual(t *testing.T) {
	cases := []Cost{{}, {Tap: true}, {Sac: []CostPart{}}, {Sac: []CostPart{{N: 1, Spec: "Creature"}}}, {Generic: 2, Unknown: []string{"x"}}}
	for i := range cases {
		for j := range cases {
			if got, want := cases[i].Equal(&cases[j]), reflect.DeepEqual(cases[i], cases[j]); got != want {
				t.Fatalf("Equal(%d,%d)=%v DeepEqual=%v", i, j, got, want)
			}
		}
		if got, want := cases[i].IsZero(), reflect.ValueOf(cases[i]).IsZero(); got != want {
			t.Fatalf("IsZero(%d)=%v reflect=%v", i, got, want)
		}
	}
}
