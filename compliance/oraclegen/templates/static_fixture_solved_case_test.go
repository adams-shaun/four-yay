package templates

import (
	"slices"
	"testing"
)

// Tests for the solved-Case static fixtures (static_fixture_solved_case.go,
// level-B class G7 rows "static needs a solved Case"): the Case's own
// end-step "To solve" trigger must have fired and resolved before the
// static's observation, so each test asserts the solve sequence is in the
// scenario's steps and runs the served scenario through the engine.

// TestSolvedCaseGatewayServes solves Case of the Gateway Express by
// attacking with three creatures and asserts the solved +1/+0 static on the
// compared probe in the final snapshot.
func TestSolvedCaseGatewayServes(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Case of the Gateway Express", "static#0.0", "static.continuous")
	// The solve sequence rides the fixture's afterSteps: the end-step pass,
	// the trigger's resolve, and the return to p0's next main phase.
	foundEnd, foundResolve, foundBack := false, false, false
	for _, s := range it.Steps {
		if s.Op == "pass_to" && s.Step == "end" {
			foundEnd = true
		}
		if s.Op == "resolve" {
			foundResolve = true
		}
		if s.Op == "pass_to" && s.Step == "main1" && s.Active == "p0" {
			foundBack = true
		}
	}
	if !foundEnd || !foundResolve || !foundBack {
		t.Fatalf("scenario lacks the end-step solve sequence (end=%v resolve=%v back=%v)", foundEnd, foundResolve, foundBack)
	}
	res, ok := runStatic(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("the served scenario does not replay: %v", res.Fails)
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	probePT := ""
	for _, p := range last.Permanents {
		if p.Ref == "p0:Grizzly Bears" && !p.Token {
			probePT = p.PT
		}
	}
	if probePT != "3/2" {
		t.Fatalf("the solved static's +1/+0 is not on the probe (PT %q): the Case did not solve", probePT)
	}
}

// TestSolvedCaseGorgonServes solves Case of the Gorgon's Kiss by putting
// three creature cards into graveyards and asserts the solved static's
// 4/4 Gorgon creature body with deathtouch and lifelink on the Case itself.
func TestSolvedCaseGorgonServes(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Case of the Gorgon's Kiss", "static#0.0", "static.continuous")
	// The deaths prelude is the fixture's signature: three targeted kill
	// casts resolve before the Case is cast, so three creature cards enter
	// graveyards this turn.
	deaths := 0
	for _, s := range it.Steps {
		if s.Op == "cast" && len(s.Targets) > 0 && !slices.Contains(s.Targets, "p0:"+it.Card) {
			deaths++
		}
	}
	if deaths < 3 {
		t.Fatalf("precondition: the scenario's deaths prelude is absent (targeted casts: %d)", deaths)
	}
	found := false
	for _, s := range it.Steps {
		if s.Op == "pass_to" && s.Step == "end" {
			found = true
		}
	}
	if !found {
		t.Fatal("scenario lacks the end-step pass the solve trigger fires at")
	}
	res, ok := runStatic(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("the served scenario does not replay: %v", res.Fails)
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	for _, p := range last.Permanents {
		if p.Name != "Case of the Gorgon's Kiss" {
			continue
		}
		if p.PT != "4/4" {
			t.Fatalf("the solved Case is not a 4/4 (PT %q): it did not solve", p.PT)
		}
		if !slices.Contains(p.Types, "Creature") {
			t.Fatalf("the solved Case is not a creature: %v", p.Types)
		}
		for _, kw := range []string{"Deathtouch", "Lifelink"} {
			if !slices.Contains(p.Keywords, kw) {
				t.Fatalf("the solved Case lacks %s: %v", kw, p.Keywords)
			}
		}
		return
	}
	t.Fatal("the Case is not on the battlefield in the final snapshot")
}
