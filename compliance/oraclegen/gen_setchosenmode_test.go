package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestSetChosenModeAnswerKindFollowsTheFaceScan pins the two routes a "mode"
// decision takes to XMage. A DB$ GenericChoice | SetChosenMode$ True body (a
// Theros-style Siege) reads its pick through XMage's ChooseModeEffect ->
// controller.choose(Outcome.Neutral, Choice, game): the CHOICE queue, showing
// the option LABEL. An ordinary Charm reads it through chooseMode: the numeric
// mode queue. The two decisions are indistinguishable in band (same Kind
// "mode", same PickKinds ["mode"], a mid-resolution Charm even shares
// Resume "modes"); only the face scan's ModeChoiceQueue sentinel separates
// them, so this test drives the sentinel.
func TestSetChosenModeAnswerKindFollowsTheFaceScan(t *testing.T) {
	// The two faces' scans, as modeNumbers produces them.
	siegeModes := map[string]int{"Abzan": ModeChoiceQueue, "Mardu": ModeChoiceQueue}
	charmModes := map[string]int{"Creatures you control gain lifelink until end of turn.": 1}

	siege := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "mode", Options: 2, Min: 1, Max: 1,
		Picks: []string{"Abzan"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Barrensteppe Siege"}, PickKinds: []string{"mode"},
		Resume: "modes", GorgeKind: "modes", First: "Abzan",
	}
	charm := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "mode", Options: 2, Min: 1, Max: 1,
		Picks: []string{"Creatures you control gain lifelink until end of turn."}, PickIdx: []int{0},
		PickKinds: []string{"mode"}, Resume: "cast_modes", GorgeKind: "modes",
		First: "Creatures you control gain lifelink until end of turn.",
	}

	// Preconditions: both decisions really are mode asks carrying a labelled
	// pick, and only the face scan differs -- so a change that collapsed the
	// two routes would invalidate the comparison rather than pass silently.
	for name, d := range map[string]rules.OracleDecision{"siege": siege, "charm": charm} {
		if d.Kind != "mode" || len(d.Picks) != 1 || d.PickIdx[0] != 0 {
			t.Fatalf("%s fixture must be a one-pick mode ask: %+v", name, d)
		}
	}
	if pickKind(siege, 0) != "mode" || pickKind(charm, 0) != "mode" {
		t.Fatalf("both fixtures must share PickKinds [mode]: siege=%q charm=%q",
			pickKind(siege, 0), pickKind(charm, 0))
	}

	// A SetChosenMode pick answers on the choice queue with its LABEL.
	wantSiege := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "Abzan"}}}
	if got := xanswers([]rules.OracleDecision{siege}, 1, siegeModes, nil); !reflect.DeepEqual(got, wantSiege) {
		t.Fatalf("SetChosenMode answer = %#v, want %#v", got, wantSiege)
	}

	// An ordinary Charm keeps the numeric mode queue.
	wantCharm := [][]XAnswer{{{Seat: 0, Kind: "mode", Value: "1"}}}
	if got := xanswers([]rules.OracleDecision{charm}, 1, charmModes, nil); !reflect.DeepEqual(got, wantCharm) {
		t.Fatalf("charm answer = %#v, want %#v", got, wantCharm)
	}
}

// TestCharmModeNumbersAreUnaffectedByTheSentinel proves a label the charm scan
// resolves to a real 1-based position is never diverted: modeNumbers over an
// Azorius Charm face keeps "1", and the mode case emits a mode answer. It also
// proves the SetChosenMode scan does not fire on a Charm's own SVar bodies
// (which carry no SetChosenMode$).
func TestCharmModeNumbersAreUnaffectedByTheSentinel(t *testing.T) {
	modes := map[string]int{"Creatures you control gain lifelink until end of turn.": 1}
	if m, ok := modeNumberFor(rules.OracleDecision{Picks: []string{"Creatures you control gain lifelink until end of turn."}}, 0, modes); !ok || m != 1 {
		t.Fatalf("charm position = %d, %v; want 1, true", m, ok)
	}
	if m, ok := modeNumberFor(rules.OracleDecision{Picks: []string{"Abzan"}}, 0, modes); ok {
		t.Fatalf("an unknown label must not resolve: got %d, true", m)
	}
	// A non-charm modal (a Spree, a plain "choose one or more") with a label
	// outside the map still falls back to the option's 1-based position.
	plain := rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", Options: 3, Min: 1, Max: 1,
		Picks: []string{"Destroy target creature."}, PickIdx: []int{2}, PickKinds: []string{"mode"}}
	want := [][]XAnswer{{{Seat: 0, Kind: "mode", Value: "3"}}}
	if got := xanswers([]rules.OracleDecision{plain}, 1, modes, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("plain modal = %#v, want %#v", got, want)
	}
}
