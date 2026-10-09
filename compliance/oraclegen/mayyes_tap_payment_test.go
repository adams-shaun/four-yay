package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// mayYes must not force-answer a declined payment-tap ask. The tap changes
// which mana the cast spends; a forced tap leaves gorge pool mana floating
// where a pool-paid cast spends everything, and the two engines then disagree
// at the cast checkpoint (level-B D6: The Wandering Rescuer, Lofty Dreams).
// The empty fallback answer is the aligned behaviour: both engines pay the
// whole cost from the pool.
func TestMayYesSkipsDeclinedTapPaymentAsks(t *testing.T) {
	sc := Scenario{Steps: []Step{{Op: "cast"}}}
	ds := []rules.OracleDecision{
		{Step: 0, GorgeKind: "choose", First: "Tap Grizzly Bears for 1", Options: 1, Via: "fallback"},
		{Step: 0, GorgeKind: "choose", First: "Tap Grizzly Bears for G", Options: 1, Via: "fallback"},
		{Step: 0, GorgeKind: "choose", First: "Tap Goldmaw Champion to waterbend for 1", Options: 1, Via: "fallback"},
		{Step: 0, GorgeKind: "choose", First: "Tap Goldmaw Champion instead of paying 1", Options: 1, Via: "fallback"},
		{Step: 0, GorgeKind: "choose", First: "Tap Grizzly Bears (reduce by 2)", Options: 1, Via: "fallback"},
		// Control 1: a declined ask that is not a payment tap stays injected.
		{Step: 0, GorgeKind: "choose", First: "Island", Options: 2, Via: "fallback"},
		// Control 2: a tap ask gorge's fallback ANSWERED (picks non-empty)
		// keeps its injected answer -- removing it would change every item
		// that already replays the tap byte-for-byte.
		{Step: 0, GorgeKind: "choose", First: "Tap Grizzly Bears for 1", Options: 2, Via: "fallback",
			Picks: []string{"Tap Grizzly Bears for 1"}, PickIdx: []int{0}},
	}
	got, changed := MayYes(sc, ds)
	if !changed {
		t.Fatal("MayYes changed nothing; the controls assert the mechanism")
	}
	want := []Answer{
		{Kind: "choose", Pick: []string{"Island"}},
		{Kind: "choose", Pick: []string{"Tap Grizzly Bears for 1"}},
	}
	if !reflect.DeepEqual(got.Steps[0].Answers, want) {
		t.Fatalf("answers = %+v, want %+v (declined tap-payment asks skipped, answered tap and non-tap asks kept)", got.Steps[0].Answers, want)
	}
}

// tapPaymentAsk must not match a mana wheel's "Tap <name> for mana" label or
// an unrelated word: the skip is only for the payment-tap family.
func TestTapPaymentAskLabels(t *testing.T) {
	yes := []string{
		"Tap Grizzly Bears for 1", "Tap Grizzly Bears for W", "Tap Grizzly Bears for U",
		"Tap Grizzly Bears for B", "Tap Grizzly Bears for R", "Tap Grizzly Bears for G",
		"Tap Grizzly Bears to waterbend for 1", "Tap Grizzly Bears instead of paying 1",
		"Tap Grizzly Bears (reduce by 3)",
	}
	no := []string{
		"Tap Grizzly Bears for mana", "Tap Grizzly Bears for any color", "Island",
		"Tap Grizzly Bears", "Pass priority", "",
	}
	for _, l := range yes {
		if !tapPaymentAsk(l) {
			t.Errorf("tapPaymentAsk(%q) = false, want true", l)
		}
	}
	for _, l := range no {
		if tapPaymentAsk(l) {
			t.Errorf("tapPaymentAsk(%q) = true, want false", l)
		}
	}
}
