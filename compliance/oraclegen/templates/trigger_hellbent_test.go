// Focused tests for the Hellbent condition prelude (solve_condition_preludes.go):
// Case of the Crimson Pulse's "To solve — Hellbent" row and its "Solved —"
// upkeep row are served only while p0's hand is empty at the end step, and the
// served scenarios carry the One with Nothing prelude that empties the ETB
// discard filler.
package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// assertHandEmptyAtEnd settles sc and requires p0's hand to be empty at every
// end-step snapshot. A scenario that never reaches an end step fails loudly
// rather than passing vacuously.
func assertHandEmptyAtEnd(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, name string) {
	t.Helper()
	_, res, ok := oraclegen.Settle(reg, sc)
	if !ok {
		t.Fatalf("%s: gorge cannot play the served scenario", name)
	}
	found := false
	for _, s := range res.Snapshots {
		if s.Step != "end" {
			continue
		}
		found = true
		if len(s.Players[0].Hand) != 0 {
			t.Fatalf("%s: p0 hand at the end step = %v, want empty (Hellbent)", name, s.Players[0].Hand)
		}
	}
	if !found {
		t.Fatalf("%s: scenario reaches no end-step snapshot: %v", name, sc.Steps)
	}
}

func TestTriggerHellbentCrimsonPulse(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Case of the Crimson Pulse"
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	// Precondition: the "To solve" trigger is the Hellbent gate this prelude
	// serves, so without an empty-hand setup the row stays skipped.
	solve := caseSolveTrigger(card.Faces[0])
	if solve == nil {
		t.Fatalf("precondition: %s carries no \"To solve\" trigger", name)
	}
	if !strings.EqualFold(strings.TrimSpace(solve.ParamStr(cards.PKHellbent)), "True") {
		t.Fatalf("precondition: %s To-solve trigger has no Hellbent gate: %q", name, solve.ParamStr(cards.PKHellbent))
	}

	// #0.1: the To-solve row itself. The hand-emptying cast and its resolve
	// precede the pass to the end step, and p0's hand is empty there.
	it := triggerItem(t, reg, name, "trigger#0.1")
	sc := it.Scenario
	if !containsString(sc.Setup["p0"].Battlefield, name) {
		t.Fatalf("precondition: source not on p0's battlefield: %v", sc.Setup["p0"].Battlefield)
	}
	if !containsString(sc.Setup["p0"].Hand, "Wastes") {
		t.Fatalf("precondition: ETB discard filler not in p0's hand: %v", sc.Setup["p0"].Hand)
	}
	castAt, resolveAt, endAt := -1, -1, -1
	for i, st := range sc.Steps {
		if st.Op == "cast" && st.Card == "p0:One with Nothing" {
			castAt = i
		}
		if castAt >= 0 && resolveAt < 0 && st.Op == "resolve" {
			resolveAt = i
		}
		if st.Op == "pass_to" && st.Step == "end" {
			endAt = i
			break
		}
	}
	if castAt < 0 || resolveAt < 0 || endAt < 0 || castAt > resolveAt || resolveAt > endAt {
		t.Fatalf("%s: scenario %v carries no One with Nothing prelude before the end step", name, sc.Steps)
	}
	assertHandEmptyAtEnd(t, reg, sc, name+" trigger#0.1")

	// #0.2: the Solved — upkeep row. Its scenario solves the Case first (pass
	// to the end step and resolve the To-solve trigger) and only then waits
	// for the Solved trigger's own upkeep.
	solved := triggerItem(t, reg, name, "trigger#0.2")
	sc2 := solved.Scenario
	solveEnd, solveResolve, upkeep := -1, -1, -1
	for i, st := range sc2.Steps {
		if st.Op == "pass_to" && st.Step == "end" && solveEnd < 0 {
			solveEnd = i
		}
		if solveEnd >= 0 && solveResolve < 0 && st.Op == "resolve" {
			solveResolve = i
		}
		if st.Op == "pass_to" && st.Step == "upkeep" {
			upkeep = i
		}
	}
	if solveEnd < 0 || solveResolve < 0 || upkeep < 0 || solveEnd > solveResolve || solveResolve > upkeep {
		t.Fatalf("%s: scenario %v has no solve sequence before the upkeep cause", name, sc2.Steps)
	}
	assertHandEmptyAtEnd(t, reg, sc2, name+" trigger#0.2")
}
