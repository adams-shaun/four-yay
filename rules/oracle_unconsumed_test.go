package rules

// Regression test for the harness self-check in rules/oracle_audit_test.go:
// a step's `answers` queue is consumed lazily by (*oracleRun).answer, so an
// entry no decision on that step matched used to be dropped silently and the
// scenario passed on the engine's fallback. runOracleScenario now fails such
// a step loudly, tagged with oracleUnconsumedMarker, and TestOracleAudit
// excuses exactly the rows the shrinking known-unconsumed-answers.json
// ratchet lists.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestOracleAuditUnconsumedAnswerFails(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	// A `pass` step submits exactly one priority decision and never consults
	// the answer queue, so any declared answer is left over by construction.
	// The control (no declared answer) must pass cleanly, proving the leftover
	// fail below is caused by the unconsumed answer and not a broken step.
	base := func() oracleScenario {
		return oracleScenario{
			Setup: map[string]oracleSeat{"p0": {Battlefield: []string{"Forest"}}},
			Steps: []oracleStep{{Op: "pass", Seat: 0}},
		}
	}

	control := base()
	controlFails, _, _ := runOracleScenario(reg, control)
	if len(controlFails) != 0 {
		t.Fatalf("precondition: the bare pass step must run cleanly, got %v", controlFails)
	}

	// The declared choose answer names a decision this pass step never poses.
	sc := base()
	sc.Steps[0].Answers = []oracleAnswer{{Kind: "choose", Pick: []string{"p0:Forest"}}}
	fails, _, _ := runOracleScenario(reg, sc)
	if len(fails) == 0 {
		t.Fatal("an unconsumed step answer did not fail the scenario")
	}
	var leftover string
	for _, f := range fails {
		if strings.Contains(f, oracleUnconsumedMarker) {
			leftover = f
		}
	}
	if leftover == "" {
		t.Fatalf("expected a fail tagged %q, got %v", oracleUnconsumedMarker, fails)
	}
	for _, want := range []string{"step 0", "(pass)", "p0:Forest"} {
		if !strings.Contains(leftover, want) {
			t.Fatalf("leftover fail %q does not name %q", leftover, want)
		}
	}
}
