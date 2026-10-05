package oraclegen

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// A partial arrange's order phase is bounded by its picks, not its looked-at
// cards. A later choice may name a different looked-at card in the same step.
func TestArrangeOrderConsumerLeavesFollowingSameNameAnswer(t *testing.T) {
	decisions := []rules.OracleDecision{
		{
			Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange",
			Options: 3, Min: 0, Max: 1,
			Picks: []string{"Forest"}, PickIdx: []int{0},
			PickKinds: []string{"graveyard"},
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
		{Seat: 0, Kind: "choice", Value: "[choice_skip]"},
		{Seat: 0, Kind: "choice", Value: "Forest"},
		{Seat: 0, Kind: "choice", Value: "Island"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
	// This is a valid one-pick decision over three looked-at cards. The
	// consumer's one order slot is followed immediately by another answer
	// naming a looked-at card; using Options as the slot count steals it.
	if decisions[0].Options <= len(decisions[0].Picks) {
		t.Fatal("boundary requires more looked-at cards than emitted order picks")
	}
	if got[0][2].Value != decisions[0].Picks[0] || got[0][3].Value != "Island" {
		t.Fatal("expected one order label followed by a same-step answer")
	}

	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		"selectionEnded = true;",
		"int orderAnswersRemaining = selectionEnded ? selected.size() : 0;",
		"while (orderAnswersRemaining > 0 && !queue.isEmpty())",
		"orderAnswersRemaining--;",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("scripted library ordering does not bound its queue consumption by emitted picks: missing %q", required)
		}
	}
}
