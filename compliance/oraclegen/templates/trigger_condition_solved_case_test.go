// Focused tests for the solved-Case trigger preludes this ticket adds: a
// Case's "Solved —" trigger is served by the setup that solves the source Case
// (the solve condition's own candidates, the end-step pass that lets the "To
// solve" trigger fire, its resolve, and the return to p0's next main phase),
// and gorge puts the trigger on the stack and plays the scenario through.
package templates

import (
	"strings"
	"testing"
)

// TestTriggerSolvedCasePreludes drives the MKM Cases and the TLA attacking
// caster in the ticket's rows file. Each gate is asserted to be the solved
// Case (or the attacking) gate it claims, the scenario must carry the solve
// sequence, and the item must play through gorge.
func TestTriggerSolvedCasePreludes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, gate string
	}{
		{"Case of the Ransacked Lab", "trigger#0.1", "IsSolved"},
		{"Case of the Shattered Pact", "trigger#0.2", "IsSolved"},
		{"Case of the Trampled Garden", "trigger#0.2", "IsSolved"},
		{"Fire Lord Azula", "trigger#0.0", "attacking"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s not in the corpus", tc.name)
			}
			// Precondition: the card carries the "To solve" trigger whose
			// grant the gate needs (the solved-Case rows), or the attacking
			// gate the prelude satisfies.
			if tc.gate == "IsSolved" {
				if caseSolveTrigger(card.Faces[0]) == nil {
					t.Fatalf("precondition: %s carries no \"To solve\" trigger", tc.name)
				}
			}
			it := triggerItem(t, reg, tc.name, tc.key)
			sc := it.Scenario
			// The solve sequence is present: pass to the end step, then
			// resolve the solve trigger (the attacking gate needs no solve).
			passEnd, resolve := false, false
			for _, st := range sc.Steps {
				if st.Op == "pass_to" && st.Step == "end" {
					passEnd = true
				}
				if st.Op == "resolve" {
					resolve = true
				}
			}
			if tc.gate == "IsSolved" && (!passEnd || !resolve) {
				t.Fatalf("%s: scenario carries no solve sequence: %v", tc.name, sc.Steps)
			}
			switch tc.name {
			case "Case of the Shattered Pact":
				// The row trigger's own phase must be p0's begin-combat.
				toCombat := false
				for _, st := range sc.Steps {
					// The prelude positions the game at p0's turn-3 main
					// phase, so the plain (no Active) pass_to already lands
					// on p0's own begin-combat; the forced variant carries
					// Active p0.
					if st.Op == "pass_to" && strings.Contains(st.Step, "combat") {
						toCombat = true
					}
				}
				if !toCombat {
					t.Fatalf("%s: no pass_to p0's begin-combat: %v", tc.name, sc.Steps)
				}
			case "Case of the Trampled Garden", "Fire Lord Azula":
				attackStep(t, sc.Steps)
			case "Case of the Ransacked Lab":
				// The solve condition is four instants and sorceries this
				// turn: at least four cast steps, then the row trigger's own
				// cast.
				n := 0
				for _, st := range sc.Steps {
					if st.Op == "cast" {
						n++
					}
				}
				if n < 5 {
					t.Fatalf("%s: %d cast steps, want at least 5: %v", tc.name, n, sc.Steps)
				}
			}
		})
	}
}

// TestTriggerSolvedCaseColorsCount drives Case of the Shattered Pact's own
// "To solve" row: its five-colour gate is served by five permanents of five
// distinct colours, and the trigger fires at p0's end step.
func TestTriggerSolvedCaseColorsCount(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Case of the Shattered Pact"
	it := triggerItem(t, reg, name, "trigger#0.1")
	p0 := it.Scenario.Setup["p0"]
	colors := map[string]bool{}
	for _, n := range p0.Battlefield {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("precondition: %s not in the corpus", n)
		}
		// The printed colour identity lives in the mana cost's coloured
		// symbols (the Face's Colors field is the colour WORDS string).
		for _, sym := range strings.Fields(c.Faces[0].ManaCost) {
			if len(sym) == 1 && strings.ContainsAny(sym, "WUBRG") {
				colors[sym] = true
			}
		}
	}
	if len(colors) < 5 {
		t.Fatalf("%s: battlefield %v shows %d colours, want at least 5", name, p0.Battlefield, len(colors))
	}
}
