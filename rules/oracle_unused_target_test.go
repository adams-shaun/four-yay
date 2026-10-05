package rules

// Regression test for the harness self-check the generator's target rewrite
// relies on: a step's `targets` queue is consumed lazily by
// (*oracleRun).answer, so a surplus target no decision on that step matched
// used to be dropped silently and the scenario passed on the engine's
// fallback. XMage rejects such a cast, so runOracleScenario now fails the
// step loudly, tagged with OracleUnusedTargetMarker. The generator tolerates
// exactly this marker while it learns gorge's real targets (Settle), then
// rewrites the scenario and re-verifies it (PlaysThrough) so no surplus
// survives into a generated scenario.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestOracleAuditUnusedTargetFails(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	// A `pass` step submits exactly one priority decision and never consults
	// the target queue, so any declared target is left over by construction.
	// The control (no declared target) must pass cleanly, proving the leftover
	// fail below is caused by the unconsumed target and not a broken step.
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

	// A target the pass step never uses must fail loudly.
	sc := base()
	sc.Steps[0].Targets = []string{"p1:Grizzly Bears"}
	fails, _, _ := runOracleScenario(reg, sc)
	if len(fails) == 0 {
		t.Fatal("an unused step target did not fail the scenario")
	}
	var leftover string
	for _, f := range fails {
		if strings.Contains(f, OracleUnusedTargetMarker) {
			leftover = f
		}
	}
	if leftover == "" {
		t.Fatalf("expected a fail tagged %q, got %v", OracleUnusedTargetMarker, fails)
	}
	for _, want := range []string{"step 0", "(pass)", "p1:Grizzly Bears"} {
		if !strings.Contains(leftover, want) {
			t.Fatalf("unused-target fail %q does not name %q", leftover, want)
		}
	}

	// The mirror case: a target a cast DOES consume must not trip the check.
	// Without this positive control the test would pass if the check failed
	// every target, consumed or not.
	consumed := oracleScenario{
		Setup: map[string]oracleSeat{"p0": {Hand: []string{"Shock"}}, "p1": {Battlefield: []string{"Grizzly Bears"}}},
		Steps: []oracleStep{
			{Op: "cast", Seat: 0, Card: "p0:Shock", Mana: "R", Targets: []string{"p1:Grizzly Bears"}},
			{Op: "resolve", Seat: 0},
		},
	}
	consumedFails, _, _ := runOracleScenario(reg, consumed)
	for _, f := range consumedFails {
		if strings.Contains(f, OracleUnusedTargetMarker) {
			t.Fatalf("a consumed target tripped the unused-target check: %v", consumedFails)
		}
	}
}
