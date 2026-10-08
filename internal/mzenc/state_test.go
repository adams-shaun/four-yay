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
		f.AddFeature("MAIN1") // see Step: the ActionType/step name mapping below
		f.AddFeature("PRIORITY")
		f.AddFeature("priority")
	})
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing global id %d (want %v got %v)", id, want, got)
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
