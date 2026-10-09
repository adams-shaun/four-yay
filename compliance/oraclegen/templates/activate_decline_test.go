package templates_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/rules"
)

// Sparring Dummy's "you may put a land from your graveyard or exile into
// your hand": gorge declined the hidden pick, and XMage poses the optional
// ask on its target queue only — so the script must be the lone
// [target_skip], not the declined pair whose "no" boolean has no chooseUse
// to answer ("Found wrong choice command").
func TestActivateDeclinedHiddenPickIsTargetSkipOnly(t *testing.T) {
	reg := corpusReg(t)
	c, ok := reg.Lookup("Sparring Dummy")
	if !ok {
		t.Fatal("precondition: Sparring Dummy is not in the corpus")
	}
	var req levelb.Requirement
	found := false
	for _, r := range levelb.Requirements(c) {
		if r.Key == "activate#0.0" {
			req, found = r, true
		}
	}
	if !found {
		t.Fatal("precondition: Sparring Dummy has no activate#0.0 requirement")
	}
	it, skip := templates.GenerateB(reg, "Sparring Dummy", req)
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}

	// Precondition: the replayed scenario really carries the declined
	// hidden-pick ask the script answers.
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	declined := 0
	for _, d := range res.Decisions {
		if d.Resume != "hidden_pick" || len(d.Picks) != 0 || d.Step < 0 || d.Step >= len(it.XAnswers) {
			continue
		}
		declined++
		want := oraclegen.XAnswer{Seat: 0, Kind: "target", Value: "[target_skip]"}
		got := it.XAnswers[d.Step]
		if len(got) != 1 || got[0] != want {
			t.Fatalf("hidden-pick step %d answers %+v, want the lone target skip", d.Step, got)
		}
	}
	if declined == 0 {
		t.Fatal("precondition: the replay carries no declined hidden-pick ask")
	}
}
