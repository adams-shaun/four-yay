package templates_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
)

// Terramorphic Expanse's "{T}, Sacrifice": the level-B activate item must
// pick the Forest in its own gorge answers AND script XMage the same card, so
// XMage's mandatory TargetCardInLibrary 1..1 ask is not answered with the
// [target_skip] it rejects ("Wrong skip command found").
func TestActivateLibrarySearchPicksFirstCard(t *testing.T) {
	reg := corpusReg(t)
	c, ok := reg.Lookup("Terramorphic Expanse")
	if !ok {
		t.Fatal("precondition: Terramorphic Expanse is not in the corpus")
	}
	reqs := levelb.Requirements(c)
	if len(reqs) == 0 {
		t.Fatal("precondition: Terramorphic Expanse carries no level-B requirement")
	}
	req := reqs[0]
	if req.Key != "activate#0.0" {
		t.Fatalf("precondition: requirement key = %q, want activate#0.0", req.Key)
	}
	it, skip := templates.GenerateB(reg, "Terramorphic Expanse", req)
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}

	// Precondition: the scenario's library top holds a basic land the search
	// can find; without it the item would legitimately keep the decline.
	top := it.Scenario.Setup["p0"].LibraryTop
	found := false
	for _, n := range top {
		if n == "Forest" {
			found = true
		}
	}
	if !found {
		t.Fatalf("precondition: p0 library_top %v has no Forest to find", top)
	}

	step := -1
	for i, s := range it.Scenario.Steps {
		if s.Op == "activate" {
			step = i
		}
	}
	if step < 0 {
		t.Fatal("precondition: the item has no activate step")
	}

	// gorge's own answer: a choose pick taking the Forest, on whichever step
	// posed the search (the resolve step, where the ability resolves).
	gotGorge := false
	gorgeAt := -1
	for i, s := range it.Scenario.Steps {
		for _, a := range s.Answers {
			if a.Kind == "choose" && len(a.Pick) == 1 && a.Pick[0] == "Forest" {
				gotGorge, gorgeAt = true, i
			}
		}
	}
	if !gotGorge {
		t.Fatalf("gorge answers do not pick the Forest in any step: %+v", it.Scenario.Steps)
	}

	// XMage's scripted answer: a target naming the same card, never a skip.
	var gotTarget, gotSkip bool
	for _, a := range it.XAnswers[gorgeAt] {
		if a.Seat == 0 && a.Kind == "target" && a.Value == "Forest" {
			gotTarget = true
		}
		if a.Value == "[target_skip]" {
			gotSkip = true
		}
	}
	if !gotTarget {
		t.Fatalf("xmage_answers %+v do not target the Forest", it.XAnswers[gorgeAt])
	}
	if gotSkip {
		t.Fatalf("xmage_answers %+v still carry a target skip", it.XAnswers[gorgeAt])
	}
}
