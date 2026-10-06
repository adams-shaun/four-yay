package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
)

// TestTimetwisterChoiceAnswersOnTheYesNoQueue pins Turtles in Time: its
// per-player GenericChoice is XMage's chooseUse, so each seat's mode pick must
// script as a yes/no choice, not a numeric mode.
func TestTimetwisterChoiceAnswersOnTheYesNoQueue(t *testing.T) {
	f := &cards.Face{SVars: map[string]string{
		"DBGenericChoice": "DB$ GenericChoice | Defined$ Player | Choices$ Stargate,Homebody | AILogic$ Timetwister",
		"Stargate":        "DB$ Pump | SpellDescription$ Yes, shuffle.",
		"Homebody":        "DB$ Pump | SpellDescription$ NO, do not.",
	}}
	modes := modeNumbers(f)
	if modes["Yes, shuffle."] != ModeYesQueue || modes["NO, do not."] != ModeNoQueue {
		t.Fatalf("face scan = %#v, want yes/no sentinels", modes)
	}
	mk := func(seat int, label string, idx int) rules.OracleDecision {
		return rules.OracleDecision{Step: seat, Seat: seat, Kind: "mode", Options: 2, Min: 1, Max: 1,
			Picks: []string{label}, PickIdx: []int{idx}, PickKinds: []string{"mode"}, Resume: "modes"}
	}
	got := xanswers([]rules.OracleDecision{mk(0, "Yes, shuffle.", 0), mk(1, "NO, do not.", 1)}, 2, modes, nil)
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "yes"}}, {{Seat: 1, Kind: "choice", Value: "no"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %#v, want %#v", got, want)
	}
}
