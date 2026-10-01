package rules

import (
	"reflect"
	"strconv"
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
	if !costModsZero(&costMods{}) {
		t.Fatal("the zero composition is not costModsZero")
	}
	if costModsZero(&costMods{raiseGen: 1}) || costModsZero(&costMods{waterbendX: true}) || costModsZero(&costMods{reduces: []costMod{{}}}) {
		t.Fatal("a non-zero composition read as costModsZero")
	}
}

// TestParseInt10MatchesParseInt pins parseInt10 to strconv.ParseInt(s, 10,
// 64)'s accept/refuse verdict and value.
func TestParseInt10MatchesParseInt(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "0", "7", "-3", "+12", "+", "-", "1_000", "0x10", " 1", "1 ", "X",
		"Count$xPaid", "9223372036854775807", "9223372036854775808", "-9223372036854775808", "00012", "1e3", "٣"} {
		v, err := strconv.ParseInt(s, 10, 64)
		got, ok := parseInt10(s)
		if ok != (err == nil) || (ok && got != v) {
			t.Errorf("parseInt10(%q) = %d, %v; ParseInt = %d, %v", s, got, ok, v, err)
		}
	}
}
