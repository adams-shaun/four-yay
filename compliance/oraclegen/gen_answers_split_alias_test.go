package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// Two same-name permanents have different scenario refs. XMage's targetName
// looks up the complete ref (including #2) to find the correct setup alias.
func TestXAnswersDamageSplitPreservesDuplicateTargetAliases(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "damage_split",
		Options: 2, Min: 3, Max: 3,
		Picks:    []string{"Grizzly Bears", "Grizzly Bears", "Grizzly Bears"},
		PickIdx:  []int{1, 0, 1},
		PickRefs: []string{"p1:Grizzly Bears#2", "p1:Grizzly Bears", "p1:Grizzly Bears#2"},
	}
	if d.Picks[0] != d.Picks[1] || d.PickRefs[0] == d.PickRefs[1] {
		t.Fatalf("precondition: same printed name, distinct setup refs: %+v", d)
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)[0]
	want := []XAnswer{
		{Seat: 0, Kind: "target", Value: "p1:Grizzly Bears#2^X=2"},
		{Seat: 0, Kind: "target", Value: "p1:Grizzly Bears^X=1"},
	}
	if len(got) != len(want) {
		t.Fatalf("split answers = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("split answer %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
