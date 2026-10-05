package oraclegen

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// Pin both sides of the queue boundary: arrange emits one order label per
// pick, and the Java consumer must stop after the looked-at cards' order slots
// even when the next decision's answer is another looked-at card's name.
func TestArrangeOrderConsumerLeavesFollowingSameNameAnswer(t *testing.T) {
	decisions := []rules.OracleDecision{
		{
			Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange",
			Options: 2, Min: 0, Max: 1,
			Picks: []string{"Forest", "Island"}, PickIdx: []int{0},
			PickKinds: []string{"graveyard", "graveyard"},
		},
		{
			Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose_n",
			Options: 2, Min: 1, Max: 1,
			Picks: []string{"Island"}, PickIdx: []int{1}, PickKinds: []string{"label"},
		},
	}
	got := xanswers(decisions, 1, nil, nil)
	want := [][]XAnswer{{
		{Seat: 0, Kind: "choice", Value: "Forest"},
		{Seat: 0, Kind: "choice", Value: "Island"},
		{Seat: 0, Kind: "choice", Value: "[choice_skip]"},
		{Seat: 0, Kind: "choice", Value: "Forest"},
		{Seat: 0, Kind: "choice", Value: "Island"},
		{Seat: 0, Kind: "choice", Value: "Island"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}

	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		"int orderAnswersRemaining = lookedAtOrder.size();",
		"while (orderAnswersRemaining > 0 && !queue.isEmpty())",
		"orderAnswersRemaining--;",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("scripted library ordering does not bound its queue consumption: missing %q", required)
		}
	}
}
