package mzenc

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// idsFor is the test helper: rebuild a Features tree with the SAME public
// operations ProcessState must perform, and return its id set, so a test can
// assert the walker emitted exactly the expected families. Built per test;
// no corpus, no game.
func idsFor(build func(f *Node)) map[int32]struct{} {
	e := NewEncoder(DefaultTableSize)
	build(e.Root())
	return e.IDs()
}

func TestProcessStateGlobals(t *testing.T) {
	v := view.View{Viewer: 0, Step: "main1", Phase: "main1"}
	got := ProcessState(v, nil, 0, 0, "priority")

	// upstream processState (StateEncoder.java:634-641): a step feature, the
	// decisionType name, and the cleaned decisionsText, all at the root.
	want := idsFor(func(f *Node) {
		f.AddFeature("PRECOMBAT_MAIN") // see Step: the ActionType/step name mapping below
		f.AddFeature("PRIORITY")
		f.AddFeature("priority")
	})
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing global id %d (want %v got %v)", id, want, got)
		}
	}
}

func TestProcessStatePlayerScalars(t *testing.T) {
	v := view.View{
		Players: []view.PlayerView{
			{ID: 0, Life: 20, LibrarySize: 53, HandSize: 7, Pool: map[string]int32{"W": 2}},
			{ID: 1, Life: 18, LibrarySize: 60, HandSize: 5},
		},
	}
	got := ProcessState(v, nil, 0, 0, "x")

	want := idsFor(func(f *Node) {
		me := f.SubFeatures("Player", true)
		me.AddNumericFeature("LifeTotal", 20, true)
		me.AddNumericFeature("LibraryCount", 53, true)
		me.AddNumericFeature("CardsInHand", 7, true)
		me.AddFeature("IsActivePlayer")
		me.AddFeature("IsDecisionPlayer")
		mp := me.SubFeatures("ManaPool", false)
		mp.AddNumericFeature("WhiteMana", 2, true)
		opp := f.SubFeatures("Opponent", true)
		opp.AddNumericFeature("LifeTotal", 18, true)
		opp.AddNumericFeature("LibraryCount", 60, true)
		opp.AddNumericFeature("CardsInHand", 5, true)
	})
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing player id %d", id)
		}
	}
}

func TestCleanStringStripsUUIDTagsAndAngleBrackets(t *testing.T) {
	cases := map[string]string{
		"Lightning Bolt [1a2b3c]": "Lightning Bolt",
		"<b>Flying</b>":           "Flying",
		"plain":                   "plain",
		"":                        "",
	}
	for in, want := range cases {
		if got := cleanString(in); got != want {
			t.Errorf("cleanString(%q)=%q want %q", in, got, want)
		}
	}
}

// TestStepNameTable pins every mapped step to its expected upstream
// TurnStepType name, one row per state.Step, so the spelling that drives a
// hashed feature id is locked and cannot shift if state.Step.String() changes.
// The exact spellings are an assumption validated against XMage's enum
// (deferred to a live oracle, mzenc design §9).
func TestStepNameTable(t *testing.T) {
	cases := []struct {
		step state.Step
		name string
	}{
		{state.StepUntap, "UNTAP"},
		{state.StepUpkeep, "UPKEEP"},
		{state.StepDraw, "DRAW"},
		{state.StepMain1, "PRECOMBAT_MAIN"},
		{state.StepBeginCombat, "BEGIN_COMBAT"},
		{state.StepDeclareAttackers, "DECLARE_ATTACKERS"},
		{state.StepDeclareBlockers, "DECLARE_BLOCKERS"},
		{state.StepCombatDamage, "COMBAT_DAMAGE"},
		{state.StepEndCombat, "END_COMBAT"},
		{state.StepMain2, "POSTCOMBAT_MAIN"},
		{state.StepEnd, "END_TURN"},
		{state.StepCleanup, "CLEANUP"},
	}
	for _, c := range cases {
		got, ok := stepName[c.step.String()]
		if !ok {
			t.Errorf("stepName missing key %q", c.step.String())
			continue
		}
		if got != c.name {
			t.Errorf("stepName[%q]=%q want %q", c.step.String(), got, c.name)
		}
	}
}

// TestStepNameCoversAllSteps holds stepName to state.Step's own String()
// spellings: every defined Step must have a key, and that key must be exactly
// what Step.String() produces, so the table cannot drift from the state
// package's single home of the step names.
func TestStepNameCoversAllSteps(t *testing.T) {
	for _, s := range state.AllSteps().Steps() {
		name, ok := stepName[s.String()]
		if !ok {
			t.Errorf("stepName missing key %q (state.Step %d)", s.String(), s)
			continue
		}
		if name == "" {
			t.Errorf("stepName[%q] is empty", s.String())
		}
	}
}
