package oraclegen

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestChooseTargetsRewritesCastFromDecisions: a cast step's Targets must
// become exactly the target decisions gorge's runner recorded for that step,
// in order -- the fixture's own over-offered list is discarded. This is the
// class fix for every "the scenario lists a target gorge did not choose"
// card: XMage's castSpell rejects a target list whose count does not match
// the ability's.
func TestChooseTargetsRewritesCastFromDecisions(t *testing.T) {
	sc := Scenario{
		Setup: map[string]Seat{"p0": {Hand: []string{"Conduct Electricity"}}},
		Steps: []Step{
			{Op: "cast", Seat: 0, Card: "p0:Conduct Electricity", Mana: "CCCCR",
				Targets: []string{"p1:Grizzly Bears", "p1:Grizzly Bears"}},
			{Op: "resolve"},
		},
	}
	ds := []rules.OracleDecision{
		{Step: 0, Kind: "target", Via: "target", PickRefs: []string{"p1:Grizzly Bears"}},
	}
	got, castSteps := chooseTargets(sc, ds)

	if !castSteps[0] {
		t.Fatalf("cast step 0 not marked as carrying its targets: %v", castSteps)
	}
	want := []string{"p1:Grizzly Bears"}
	if strings.Join(got.Steps[0].Targets, "|") != strings.Join(want, "|") {
		t.Fatalf("cast targets = %v, want %v (the fixture surplus must be gone)", got.Steps[0].Targets, want)
	}
	if len(got.Steps[1].Targets) != 0 {
		t.Fatalf("resolve step gained targets: %v", got.Steps[1].Targets)
	}
	// The input must not be mutated in place: callers reuse the pre-rewrite
	// scenario (counterWith replays it) and a shared alias would corrupt it.
	if len(sc.Steps[0].Targets) != 2 {
		t.Fatalf("chooseTargets mutated its input scenario: %v", sc.Steps[0].Targets)
	}
}

// TestChooseTargetsClearsCastWithNoDecision: a cast that posed no target
// decision (every optional slot declined, or a spell with no targets) must
// carry none of the fixture's surplus targets. Divergent Equation's optional
// "up to X" target with an empty legal pool is the live carrier.
func TestChooseTargetsClearsCastWithNoDecision(t *testing.T) {
	sc := Scenario{
		Steps: []Step{
			{Op: "cast", Seat: 0, Card: "p0:Divergent Equation", Targets: []string{"p0:Grizzly Bears"}},
			{Op: "resolve"},
		},
	}
	got, castSteps := chooseTargets(sc, nil)

	if len(castSteps) != 0 {
		t.Fatalf("castSteps = %v, want none", castSteps)
	}
	if len(got.Steps[0].Targets) != 0 {
		t.Fatalf("cast targets = %v, want none", got.Steps[0].Targets)
	}
}

// TestChooseTargetsLeavesResolveTargetsScripted: a target decision posed at a
// resolve step is not part of the cast, so it must NOT be rewritten into the
// step's Targets (it travels to XMage as a scripted answer instead); the
// cast step marker must not name it either.
func TestChooseTargetsLeavesResolveTargetsScripted(t *testing.T) {
	sc := Scenario{
		Steps: []Step{
			{Op: "cast", Seat: 0, Card: "p0:Some Spell"},
			{Op: "resolve", Targets: []string{"p1:Grizzly Bears"}},
		},
	}
	ds := []rules.OracleDecision{
		{Step: 1, Kind: "target", Via: "target", PickRefs: []string{"p1:Grizzly Bears"}},
	}
	got, castSteps := chooseTargets(sc, ds)

	if len(castSteps) != 0 {
		t.Fatalf("a resolve-step target marked a cast step: %v", castSteps)
	}
	if len(got.Steps[1].Targets) != 1 || got.Steps[1].Targets[0] != "p1:Grizzly Bears" {
		t.Fatalf("resolve step targets = %v, want the declared one", got.Steps[1].Targets)
	}
}

// TestXanswersScriptsResolveTargetsOnly: a cast step's target decisions reach
// XMage through castSpell, so xanswers must not script them again; a resolve
// step's target decisions have no cast to ride and must be scripted.
func TestXanswersScriptsResolveTargetsOnly(t *testing.T) {
	ds := []rules.OracleDecision{
		{Step: 0, Kind: "target", Via: "target", PickRefs: []string{"p1:Grizzly Bears"}},
		{Step: 1, Kind: "target", Via: "target", PickRefs: []string{"p0:Serra Angel"}},
	}
	got := xanswers(ds, 2, nil, map[int]bool{0: true})

	if len(got[0]) != 0 {
		t.Fatalf("cast step 0 got scripted target answers %v; they ride castSpell", got[0])
	}
	if len(got[1]) != 1 || got[1][0] != (XAnswer{Seat: 0, Kind: "target", Value: "Serra Angel"}) {
		t.Fatalf("resolve step 1 answers = %v, want the Serena Angel target scripted", got[1])
	}
}
