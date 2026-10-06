package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func TestXAnswersSetupETBColourIsQueuedBeforeSetup(t *testing.T) {
	d := rules.OracleDecision{
		Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb",
		Picks: []string{"White"}, PickKinds: []string{"color"},
	}
	if d.Step >= 0 || d.Resume != "etb" || len(d.PickKinds) != 1 || d.PickKinds[0] != "color" {
		t.Fatalf("precondition: decision is not a setup ETB colour choice: %+v", d)
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	want := [][]XAnswer{{{Seat: 0, Kind: "setup_choice", Value: "White"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("setup colour answers = %#v, want %#v", got, want)
	}
}

func TestXAnswersDoesNotTreatGameplayColourAsSetup(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "etb",
		Picks: []string{"Blue"}, PickKinds: []string{"color"},
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	if len(got) != 1 || len(got[0]) != 1 || got[0][0].Kind != "choice" || got[0][0].Value != "Blue" {
		t.Fatalf("gameplay colour answer = %#v, want ordinary choice", got)
	}
}
