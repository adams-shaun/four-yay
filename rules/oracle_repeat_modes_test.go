package rules

// Regression tests for the oracle runner's handling of a CanRepeatModes$
// Charm (CR 601.2b: "Choose N. You may choose the same mode more than
// once."). The decision it poses carries Repeatable (rules/resolution_chain.go
// modeDecisionForChoices), and a scripted answer that picks the same mode
// more than once must SUBMIT the same option index again. Before the fix the
// runner kept a single one-use map across the picks, so the second "Proliferate"
// found no unused option and the run died "answer ... not offered".

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// repeatModesRun plays one repeat-mode Charm scenario and returns the run.
func repeatModesRun(t *testing.T, sc oracleScenario) *oracleRun {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	fails, _, run := runOracleScenario(reg, sc)
	if len(fails) > 0 {
		t.Fatalf("repeat-modes scenario failed:\n  %s", strings.Join(fails, "\n  "))
	}
	return run
}

// modesDecision returns the run's single modes decision, failing if none was
// posed. It is the precondition the repeat assertion depends on.
func modesDecision(t *testing.T, run *oracleRun) OracleDecision {
	t.Helper()
	var found *OracleDecision
	for i := range run.decisions {
		if run.decisions[i].GorgeKind == "modes" {
			found = &run.decisions[i]
		}
	}
	if found == nil {
		t.Fatal("precondition: the scenario posed no modes decision")
	}
	return *found
}

// TestOracleRepeatModesSubmitsDuplicateIndex: Planewide Celebration
// (CharmNum$ 4, CanRepeatModes$ True) has a targetless mode, so answering
// "Proliferate" four times must submit the same option index four times and
// resolve.
func TestOracleRepeatModesSubmitsDuplicateIndex(t *testing.T) {
	run := repeatModesRun(t, oracleScenario{
		Name: "repeat-modes-same-pick",
		Setup: map[string]oracleSeat{
			"p0": {Hand: []string{"Planewide Celebration"}},
		},
		Steps: []oracleStep{
			{Op: "cast", Seat: 0, Card: "p0:Planewide Celebration", Mana: "CCCCCGG",
				Answers: []oracleAnswer{{Kind: "modes",
					Pick: []string{"Proliferate", "Proliferate", "Proliferate", "Proliferate"}}}},
			{Op: "resolve"},
		},
	})
	d := modesDecision(t, run)
	if d.Min != 4 || d.Max != 4 {
		t.Fatalf("precondition: modes decision = min %d max %d, want 4/4", d.Min, d.Max)
	}
	if len(d.PickIdx) != 4 {
		t.Fatalf("modes answer submitted %v, want four picks", d.PickIdx)
	}
	for i, idx := range d.PickIdx {
		if idx != d.PickIdx[0] {
			t.Fatalf("repeat pick %d submitted index %d, want the first pick's %d: %v", i, idx, d.PickIdx[0], d.PickIdx)
		}
	}
}

// TestOracleNonRepeatModesRejectsDuplicateIndex: the same answer against a
// Charm WITHOUT CanRepeatModes$ must still fail, and fail as "not offered",
// proving the one-use rule is intact for every other decision.
func TestOracleNonRepeatModesRejectsDuplicateIndex(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := oracleScenario{
		Name: "non-repeat-modes-same-pick",
		Setup: map[string]oracleSeat{
			"p0": {Hand: []string{"Ashling's Command"}},
		},
		Steps: []oracleStep{
			{Op: "cast", Seat: 0, Card: "p0:Ashling's Command", Mana: "CCCUR",
				Answers: []oracleAnswer{{Kind: "modes",
					Pick: []string{"draws two cards", "draws two cards"}}}},
			{Op: "resolve"},
		},
	}
	fails, _, _ := runOracleScenario(reg, sc)
	if len(fails) == 0 {
		t.Fatal("a non-repeat Charm accepted the same mode twice")
	}
	var notOffered string
	for _, f := range fails {
		if strings.Contains(f, "not offered") {
			notOffered = f
		}
	}
	if notOffered == "" {
		t.Fatalf("expected a \"not offered\" failure, got %v", fails)
	}
}
