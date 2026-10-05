package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func TestCompositeMayUsesRecordedMultiCardSubset(t *testing.T) {
	for _, tc := range []struct {
		name, label string
		zone        string
		want        []XAnswer
	}{
		{
			name:  "discard",
			label: "Yes — discard",
			zone:  "graveyard",
			want:  []XAnswer{{Seat: 0, Kind: "choice", Value: "Island"}, {Seat: 0, Kind: "choice", Value: "Forest"}, {Seat: 0, Kind: "choice", Value: "[choice_skip]"}},
		},
		{
			name:  "battlefield up to",
			label: "Yes — put into the battlefield",
			zone:  "battlefield",
			want:  []XAnswer{{Seat: 0, Kind: "choice", Value: "Island^Forest"}, {Seat: 0, Kind: "choice", Value: "[choice_skip]"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := Scenario{Steps: []Step{{Op: "cast", Card: "p0:Test Spell"}, {Op: "resolve"}}}
			// The moved objects include the two selected cards and an unselected
			// third card, so a snapshot delta cannot identify the chosen subset.
			var after rules.OracleSnapshot
			if tc.zone == "graveyard" {
				after.Players = []rules.OracleSnapPlayer{{Seat: 0, Graveyard: []string{"Island", "Forest", "Mountain", "Test Spell"}}}
			} else {
				after.Permanents = []rules.OracleSnapPerm{
					{Ref: "p0:Island", Name: "Island", Owner: 0},
					{Ref: "p0:Forest", Name: "Forest", Owner: 0},
					{Ref: "p0:Mountain", Name: "Mountain", Owner: 0},
				}
			}
			before := rules.OracleSnapshot{Players: []rules.OracleSnapPlayer{{Seat: 0}}}
			election := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", Options: 2,
				Picks: []string{tc.label}, PickRefs: []string{tc.label}, PickKinds: []string{"yes"}}
			pick := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 3,
				Min: 0, Max: 3, Picks: []string{"Island", "Forest"},
				PickRefs: []string{"p0:Island", "p0:Forest"}, ObjectPicks: []string{"p0:Island", "p0:Forest"},
				PickIdx: []int{0, 1}, PickKinds: []string{"card", "card"}}
			if tc.zone == "graveyard" {
				pick.Kind, pick.GorgeKind = "mode", "modes"
				pick.PickKinds = []string{"discard", "discard"}
			}
			res := rules.OracleResult{Snapshots: []rules.OracleSnapshot{before, before, after}, Decisions: []rules.OracleDecision{election, pick}}
			if len(pick.ObjectPicks) != 2 || pick.ObjectPicks[0] == pick.ObjectPicks[1] || pick.ObjectPicks[1] != "p0:Forest" {
				t.Fatalf("selected-object precondition failed: %#v", pick.ObjectPicks)
			}
			if tc.zone == "graveyard" {
				got := after.Players[0].Graveyard
				if len(got) != 4 || got[0] == got[1] || got[2] == got[0] || got[2] == got[1] {
					t.Fatalf("graveyard fixture does not contain distinct moved objects: %v", got)
				}
			} else if len(after.Permanents) != 3 || after.Permanents[0].Name == after.Permanents[1].Name || after.Permanents[1].Name == after.Permanents[2].Name {
				t.Fatalf("battlefield fixture does not contain distinct moved objects: %+v", after.Permanents)
			}
			got := xanswersForScenario(res, sc, nil, nil)
			want := [][]XAnswer{nil, tc.want}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("composite choices = %#v, want %#v", got, want)
			}
		})
	}
}

func TestCompositeMaySkipsUnrelatedObjectPickBeforeCardSelector(t *testing.T) {
	sc := Scenario{Steps: []Step{{Op: "cast", Card: "p0:Test Spell"}, {Op: "resolve"}}}
	before := rules.OracleSnapshot{Players: []rules.OracleSnapPlayer{{Seat: 0}}}
	after := rules.OracleSnapshot{Players: []rules.OracleSnapPlayer{{Seat: 0, Graveyard: []string{"Island", "Forest", "Test Spell"}}}}
	election := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", Options: 2,
		Picks: []string{"Yes — discard"}, PickRefs: []string{"Yes — discard"}, PickKinds: []string{"yes"}}
	unrelated := rules.OracleDecision{Step: 1, Seat: 0, Kind: "target", GorgeKind: "target", Options: 2,
		Picks: []string{"Mountain"}, PickRefs: []string{"p0:Mountain"}, ObjectPicks: []string{"p0:Mountain"},
		PickIdx: []int{1}, PickKinds: []string{"permanent"}}
	cardPick := rules.OracleDecision{Step: 1, Seat: 0, Kind: "mode", GorgeKind: "modes", Options: 3, Min: 0, Max: 3,
		Picks: []string{"Island", "Forest"}, PickRefs: []string{"p0:Island", "p0:Forest"},
		ObjectPicks: []string{"p0:Island", "p0:Forest"}, PickKinds: []string{"discard", "discard"}}
	if len(unrelated.ObjectPicks) != 1 || unrelated.PickKinds[0] == "discard" || len(cardPick.ObjectPicks) != 2 {
		t.Fatal("fixture must distinguish an unrelated object target from the two-card discard selector")
	}
	got := xanswersForScenario(rules.OracleResult{
		Snapshots: []rules.OracleSnapshot{before, before, after},
		Decisions: []rules.OracleDecision{election, unrelated, cardPick},
	}, sc, nil, nil)
	want := [][]XAnswer{nil, {
		{Seat: 0, Kind: "choice", Value: "Island"}, {Seat: 0, Kind: "choice", Value: "Forest"},
		{Seat: 0, Kind: "choice", Value: "[choice_skip]"}, {Seat: 0, Kind: "target", Value: "Mountain"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("composite and unrelated picks = %#v, want %#v", got, want)
	}
}
