package rules

import (
	"reflect"
	"testing"
)

// TestCostModsZeroCoversEveryField pins costModsZero's field list: a field
// added to costMods must be added to the zero test (offerRetryFutile relies
// on it naming the whole composition) and to this list.
func TestCostModsZeroCoversEveryField(t *testing.T) {
	t.Parallel()
	want := []string{"raises", "extra", "raiseCol", "raiseGen", "raiseLife", "reduces", "setFloor",
		"hasExtra", "waterbend", "waterbendX", "waterbendPartX", "raiseX"}
	typ := reflect.TypeOf(costMods{})
	var got []string
	for i := 0; i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("costMods fields %v, costModsZero covers %v: update costModsZero and this list", got, want)
	}
	if !costModsZero(costMods{}) {
		t.Fatal("the zero composition is not costModsZero")
	}
	if costModsZero(costMods{raiseGen: 1}) || costModsZero(costMods{waterbendX: true}) || costModsZero(costMods{reduces: []costMod{{}}}) {
		t.Fatal("a non-zero composition read as costModsZero")
	}
}
