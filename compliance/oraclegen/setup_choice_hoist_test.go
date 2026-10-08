package oraclegen

import (
	"reflect"
	"testing"
)

// A template reorder (the activate cost hoist) that puts a gameplay answer
// ahead of a setup_choice must be repaired: the as-enters answer leads.
func TestHoistSetupChoicesMovesThemToTheFront(t *testing.T) {
	answers := [][]XAnswer{{}, {
		{Seat: 0, Kind: "choice", Value: "Colossal Dreadmaw"},
		{Seat: 0, Kind: "setup_choice", Value: "Dinosaur"},
		{Seat: 0, Kind: "choice", Value: "White"},
	}}
	HoistSetupChoices(answers, 1)
	want := []XAnswer{
		{Seat: 0, Kind: "setup_choice", Value: "Dinosaur"},
		{Seat: 0, Kind: "choice", Value: "Colossal Dreadmaw"},
		{Seat: 0, Kind: "choice", Value: "White"},
	}
	if !reflect.DeepEqual(answers[1], want) {
		t.Fatalf("answers = %#v, want %#v", answers[1], want)
	}
}

// Multiple setup answers keep their relative order, and a step with none is
// left untouched (so the helper never perturbs an ordinary scenario).
func TestHoistSetupChoicesPreservesOrderAndNoOps(t *testing.T) {
	answers := [][]XAnswer{{}, {
		{Seat: 0, Kind: "choice", Value: "White"},
		{Seat: 0, Kind: "setup_choice", Value: "Dinosaur"},
		{Seat: 1, Kind: "setup_choice", Value: "Elf"},
	}}
	HoistSetupChoices(answers, 1)
	want := []XAnswer{
		{Seat: 0, Kind: "setup_choice", Value: "Dinosaur"},
		{Seat: 1, Kind: "setup_choice", Value: "Elf"},
		{Seat: 0, Kind: "choice", Value: "White"},
	}
	if !reflect.DeepEqual(answers[1], want) {
		t.Fatalf("answers = %#v, want %#v", answers[1], want)
	}

	plain := [][]XAnswer{{}, {{Seat: 0, Kind: "choice", Value: "White"}}}
	HoistSetupChoices(plain, 1)
	if !reflect.DeepEqual(plain[1], []XAnswer{{Seat: 0, Kind: "choice", Value: "White"}}) {
		t.Fatalf("no-setup step changed: %#v", plain[1])
	}
}
