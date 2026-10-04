package pay

import (
	"reflect"
	"testing"
)

// TestCostModsZeroCoversEveryField pins CostModsZero's field list: a field
// added to CostMods must be added to the zero test (offerRetryFutile relies
// on it naming the whole composition) and to this list.
func TestCostModsZeroCoversEveryField(t *testing.T) {
	t.Parallel()
	want := []string{"Raises", "Extra", "RaiseCol", "RaiseGen", "RaiseLife", "Reduces", "SetFloor",
		"HasExtra", "Waterbend", "WaterbendX", "WaterbendPartX", "RaiseX"}
	typ := reflect.TypeOf(CostMods{})
	var got []string
	for i := 0; i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CostMods fields %v, CostModsZero covers %v: update CostModsZero and this list", got, want)
	}
	if !CostModsZero(&CostMods{}) {
		t.Fatal("the zero composition is not CostModsZero")
	}
	if CostModsZero(&CostMods{RaiseGen: 1}) || CostModsZero(&CostMods{WaterbendX: true}) || CostModsZero(&CostMods{Reduces: []CostMod{{}}}) {
		t.Fatal("a non-zero composition read as CostModsZero")
	}
}
