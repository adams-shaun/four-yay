package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// The engine's yes-election is not the object chooser XMage poses: XMage
// asks directly for the card to discard / put onto the battlefield. Assert
// the actual selected card, not merely that a yes-looking label was skipped.
func TestCompositeMayDoesNotConsumeWholeHandElection(t *testing.T) {
	sc := Scenario{Steps: []Step{{Op: "cast", Card: "p0:Test Spell"}, {Op: "resolve"}}}
	res := rules.OracleResult{
		Snapshots: []rules.OracleSnapshot{
			{}, {Players: []rules.OracleSnapPlayer{{Seat: 0}}},
			{Players: []rules.OracleSnapPlayer{{Seat: 0, Graveyard: []string{"Wastes", "Test Spell"}}}},
		},
		Decisions: []rules.OracleDecision{{Step: 1, Seat: 0, Kind: "choose_n", Options: 2,
			Picks: []string{"Yes — discard your hand"}, PickRefs: []string{"Yes — discard your hand"}, PickKinds: []string{"yes"}}},
	}
	want := [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "Yes — discard your hand"}}}
	if got := xanswersForScenario(res, sc, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("whole-hand election = %#v, want %#v", got, want)
	}
}

func TestDiscardModeUsesChoiceQueueEvenForOneOption(t *testing.T) {
	d := rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", Options: 1, Min: 1, Max: 1,
		Picks: []string{"Discard Wastes"}, PickRefs: []string{"p0:Wastes"}, PickKinds: []string{"discard"}}
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "Wastes"}}}
	if got := xanswers([]rules.OracleDecision{d}, 1, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("one-option discard = %#v, want %#v", got, want)
	}
}

func TestCompositeMayAnswerWithExplicitPick(t *testing.T) {
	sc := Scenario{Steps: []Step{{Op: "cast", Card: "p0:Test Spell"}, {Op: "resolve"}}}
	res := rules.OracleResult{
		Decisions: []rules.OracleDecision{
			{Step: 1, Seat: 0, Kind: "choose_n", Options: 2, Picks: []string{"Yes — discard"}, PickRefs: []string{"Yes — discard"}, PickKinds: []string{"yes"}},
			{Step: 1, Seat: 0, Kind: "mode", Options: 2, Min: 1, Max: 1, Picks: []string{"Discard Island"}, PickRefs: []string{"p0:Island"}, PickKinds: []string{"discard"}},
		},
	}
	want := [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "Island"}}}
	if got := xanswersForScenario(res, sc, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit pick = %#v, want %#v", got, want)
	}
}

func TestCompositeMayAnswerPicksMovedCard(t *testing.T) {
	for _, tc := range []struct {
		name, label, zone, queue string
	}{
		{"discard", "Yes — discard", "graveyard", "choice"},
		{"put", "Yes — put into the battlefield", "battlefield", "choice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := Scenario{Steps: []Step{{Op: "cast", Card: "p0:Test Spell"}, {Op: "resolve"}}}
			before := rules.OracleSnapshot{Players: []rules.OracleSnapPlayer{{Seat: 0}}}
			after := rules.OracleSnapshot{Players: []rules.OracleSnapPlayer{{Seat: 0}}}
			switch tc.zone {
			case "graveyard":
				after.Players[0].Graveyard = []string{"Wastes", "Test Spell"}
			case "battlefield":
				after.Permanents = []rules.OracleSnapPerm{{Ref: "p0:Test Spell", Name: "Test Spell", Controller: 0}, {Ref: "p0:Wastes#27", Name: "Wastes", Controller: 0}}
			}
			res := rules.OracleResult{Snapshots: []rules.OracleSnapshot{before, before, after}, Decisions: []rules.OracleDecision{{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1, Picks: []string{tc.label}, PickRefs: []string{tc.label}, PickIdx: []int{0}, PickKinds: []string{"yes"}}}}
			if tc.zone == "graveyard" && (len(after.Players[0].Graveyard) != 2 || after.Players[0].Graveyard[0] == "Test Spell") {
				t.Fatal("discard fixture must include a moved card distinct from the resolving spell")
			}
			answers := []XAnswer{{Seat: 0, Kind: tc.queue, Value: "Wastes"}}
			if tc.zone == "battlefield" {
				answers = append(answers, XAnswer{Seat: 0, Kind: "choice", Value: "[choice_skip]"})
			}
			want := [][]XAnswer{nil, answers}
			if got := xanswersForScenario(res, sc, nil); !reflect.DeepEqual(got, want) {
				t.Fatalf("xanswersForScenario = %#v, want %#v", got, want)
			}
		})
	}
}
