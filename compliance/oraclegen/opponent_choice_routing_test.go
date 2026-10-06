package oraclegen

// The resolution-time answer shapes for a choose-the-creature ask posed to the
// OPPONENT. Trial of Agony's DBChoose ("that player chooses one of those
// creatures") is a two-option choose_n with Resume "choice" recorded for seat
// 1, the controller of the targeted creatures, not the caster. XMage poses it
// as makeChoose, which consumes one "choice" answer naming the card -- the
// same choice-queue shape as Unstable Glyphbridge, but with more than one
// option, so the answer must come from the generic pick path rather than a
// forced single option.

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestOpponentChoiceAnswerShape is pinned to the decision Trial of Agony's
// cast-resolve scenario records
// ({"step":1,"seat":1,"kind":"choose_n","options":2,"picks":[""],"pick_refs":["p1:Grizzly Bears"],
// "pick_kinds":["card"],"resume":"choice","min":1,"max":1}).
func TestOpponentChoiceAnswerShape(t *testing.T) {
	ds := []rules.OracleDecision{{
		Step: 1, Seat: 1, Kind: "choose_n", Resume: "choice", Options: 2,
		Picks: []string{""}, PickIdx: []int{0}, PickRefs: []string{"p1:Grizzly Bears"},
		ObjectPicks: []string{"p1:Grizzly Bears"}, PickKinds: []string{"card"}, Min: 1, Max: 1,
	}}
	// Precondition: more than one option, so this is not the forced
	// single-option shape the Glyphbridge test pins.
	if forcedSingleOption(ds[0]) {
		t.Fatalf("fixture is not the two-option opponent ask: %+v", ds[0])
	}
	if got := routed(t, ds, 2); !reflect.DeepEqual(got, []XAnswer{{1, "choice", "Grizzly Bears"}}) {
		t.Fatalf("opponent choice = %#v, want seat 1 choice Grizzly Bears", got)
	}
}
