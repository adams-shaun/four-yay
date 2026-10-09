package templates_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
)

// Analyze the Pollen's mandatory basic-land search: the level-B static item
// must pick the Grizzly Bears in its own gorge answers AND script XMage the
// same card, so XMage's mandatory TargetCardInLibrary 1..1 ask is not
// answered with the [target_skip] it rejects ("Wrong skip command found").
func TestStaticLibrarySearchPicksFirstCard(t *testing.T) {
	reg := corpusReg(t)
	c, ok := reg.Lookup("Analyze the Pollen")
	if !ok {
		t.Fatal("precondition: Analyze the Pollen is not in the corpus")
	}
	reqs := levelb.Requirements(c)
	if len(reqs) == 0 {
		t.Fatal("precondition: Analyze the Pollen carries no level-B requirement")
	}
	req := reqs[0]
	if req.Key != "static#0.0" {
		t.Fatalf("precondition: requirement key = %q, want static#0.0", req.Key)
	}
	it, skip := templates.GenerateB(reg, "Analyze the Pollen", req)
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}

	// Precondition: the scenario's library top holds the card the search
	// finds; without it the item would legitimately keep the decline.
	top := it.Scenario.Setup["p0"].LibraryTop
	found := false
	for _, n := range top {
		if n == "Grizzly Bears" {
			found = true
		}
	}
	if !found {
		t.Fatalf("precondition: p0 library_top %v has no Grizzly Bears to find", top)
	}

	// gorge's own answer: a choose pick taking the Grizzly Bears, on the
	// resolve step that posed the search.
	gorgeAt := -1
	for i, s := range it.Scenario.Steps {
		for _, a := range s.Answers {
			if a.Kind == "choose" && len(a.Pick) == 1 && a.Pick[0] == "Grizzly Bears" {
				gorgeAt = i
			}
		}
	}
	if gorgeAt < 0 {
		t.Fatalf("gorge answers do not pick the Grizzly Bears in any step: %+v", it.Scenario.Steps)
	}

	// XMage's scripted answer: a target naming the same card, never a skip.
	var gotTarget, gotSkip bool
	for _, a := range it.XAnswers[gorgeAt] {
		if a.Seat == 0 && a.Kind == "target" && a.Value == "Grizzly Bears" {
			gotTarget = true
		}
		if a.Value == "[target_skip]" || a.Value == "[choice_skip]" {
			gotSkip = true
		}
	}
	if !gotTarget {
		t.Fatalf("xmage_answers %+v do not target the Grizzly Bears", it.XAnswers[gorgeAt])
	}
	if gotSkip {
		t.Fatalf("xmage_answers %+v still carry a skip", it.XAnswers[gorgeAt])
	}
}
