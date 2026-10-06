package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestRepeatModeAnswersPreserveOrderAndDuplicates pins the mode queue shape
// XMage needs for a spell such as Cosmium Confluence: each selected mode is a
// separate numeric answer, including repeated selections.
func TestRepeatModeAnswersPreserveOrderAndDuplicates(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "mode", Options: 3, Min: 3, Max: 3,
		Picks:   []string{"mode one", "mode one", "mode two"},
		PickIdx: []int{0, 0, 1}, PickKinds: []string{"mode", "mode", "mode"},
	}
	got := xanswers([]rules.OracleDecision{d}, 1, nil, nil)
	want := [][]XAnswer{{
		{Seat: 0, Kind: "mode", Value: "1"},
		{Seat: 0, Kind: "mode", Value: "1"},
		{Seat: 0, Kind: "mode", Value: "2"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repeatable mode answers = %#v, want ordered duplicates %#v", got, want)
	}
}
