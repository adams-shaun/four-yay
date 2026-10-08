package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// A setup permanent's "choose a creature type" ask (pick kind "type") is
// queued before setup like the colour ask, and a later gameplay answer keeps
// its own place in the step it belongs to.
func TestSetupCreatureTypeXAnswersAreQueuedBeforeSetup(t *testing.T) {
	setup := rules.OracleDecision{
		Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb",
		Picks: []string{"Human"}, PickKinds: []string{"type"},
	}
	if !IsSetupChoice(setup) {
		t.Fatalf("precondition: %+v is not recognised as a setup choice", setup)
	}
	got := XAnswers([]rules.OracleDecision{setup}, 1, nil)
	want := [][]XAnswer{{{Seat: 0, Kind: "setup_choice", Value: "Human"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("setup creature type answers = %#v, want %#v", got, want)
	}
}

func TestSetupCreatureTypeXAnswersWorkWithoutGameplaySteps(t *testing.T) {
	d := rules.OracleDecision{
		Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb",
		Picks: []string{"Elemental"}, PickKinds: []string{"type"},
	}
	got := XAnswers([]rules.OracleDecision{d}, 0, nil)
	want := [][]XAnswer{{{Seat: 0, Kind: "setup_choice", Value: "Elemental"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("zero-step setup creature type answers = %#v, want %#v", got, want)
	}
}

// A creature-type pick posed during gameplay is an ordinary choice, not a
// setup one.
func TestSetupCreatureTypeIsNotAppliedToGameplayPicks(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "etb",
		Picks: []string{"Elf"}, PickKinds: []string{"type"},
	}
	if IsSetupChoice(d) {
		t.Fatalf("gameplay decision %+v classified as a setup choice", d)
	}
}
