// Focused tests for the activation-restriction preludes this ticket adds:
// the solved-Case setup (MKM "Solved —" abilities) and the scry / left-
// graveyard turn-history setups. Each drives a REAL corpus card whose
// activated ability is offered only when the gate holds, asserts the gate
// really is the one under test (so a regression that drops the prelude fails
// loudly), and asserts the generated item plays through gorge.
package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestActivateSolvedCasePrelude drives the four MKM Case "Solved —" abilities
// in the ticket's rows file. A Case is solved only by its own end-step "To
// solve" trigger, so the generated scenario must carry the solve sequence
// (wait for the end step, resolve the solve trigger) and the ability must then
// be offered and play through gorge.
func TestActivateSolvedCasePrelude(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name string
		gate string
	}{
		{"Case of the Filched Falcon", "Activation"},
		{"Case of the Stashed Skeleton", "Activation"},
		{"Case of the Uneaten Feast", "Activation"},
		{"Case of the Burning Masks", "IsPresent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, req := activateRequirement(t, reg, tc.name, "activate#0.0")
			sa := abilityOf(t, reg, tc.name, req)
			// Precondition: the ability really carries the solved-Case gate,
			// or the prelude under test would never run.
			gate := sa.ParamStr(cards.PKActivation)
			if tc.gate == "IsPresent" {
				gate = sa.ParamStr(cards.PKIsPresent)
			}
			if !strings.Contains(strings.ToLower(gate), "solved") {
				t.Fatalf("precondition: %s %s gate = %q, want a solved-Case gate", tc.name, tc.gate, gate)
			}
			// Precondition: the Case is on the battlefield (the source of the
			// activation), so its solve trigger can fire.
			if !seatHasName(it.Scenario.Setup["p0"].Battlefield, tc.name) {
				t.Fatalf("%s: source not on battlefield: %v", tc.name, it.Scenario.Setup["p0"].Battlefield)
			}
			// The solve sequence is present: pass to the end step, then
			// resolve the "To solve" trigger.
			if !stepsHaveOp(it.Scenario.Steps, "pass_to") || !stepsHaveOp(it.Scenario.Steps, "resolve") {
				t.Fatalf("%s: scenario carries no solve sequence: %v", tc.name, it.Scenario.Steps)
			}
			assertRestrictionItem(t, reg, tc.name, it)
		})
	}
}

// TestActivateLeftGraveyardHistoryCount drives Bonecache Overseer, whose
// "{T}, Pay 1 life: Draw a card" is gated on Count$LeftGraveyardThisTurn GE3.
// The prelude must return three distinct creature cards from p0's graveyard.
func TestActivateLeftGraveyardHistoryCount(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Bonecache Overseer"
	it, req := activateRequirement(t, reg, name, "activate#0.0")
	sa := abilityOf(t, reg, name, req)
	// Precondition: the gate really is the three-cards-left-graveyard count.
	body := svarBodyOf(t, reg, name, sa)
	if !strings.Contains(strings.ToLower(body), "leftgraveyardthisturn") {
		t.Fatalf("precondition: %s CheckSVar$ body = %q, want LeftGraveyardThisTurn", name, body)
	}
	// The setup returns three graveyard cards, each a cast (leaving the
	// graveyard this turn).
	if got := len(it.Scenario.Setup["p0"].Graveyard); got < 3 {
		t.Fatalf("%s: graveyard has %d cards, want at least 3: %v", name, got, it.Scenario.Setup["p0"].Graveyard)
	}
	if got := countOp(it.Scenario.Steps, "cast"); got < 3 {
		t.Fatalf("%s: %d cast steps, want at least 3: %v", name, got, it.Scenario.Steps)
	}
	assertRestrictionItem(t, reg, name, it)
}

// TestActivateScryHistory drives Proctor of Potential, whose graveyard
// return ability is gated on Count$YouScryThisTurn/Plus.Y. The prelude must
// cast a scry spell so the turn's scry count is nonzero.
func TestActivateScryHistory(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Proctor of Potential"
	it, req := activateRequirement(t, reg, name, "activate#0.0")
	sa := abilityOf(t, reg, name, req)
	// Precondition: the gate really is the scry-this-turn count.
	body := svarBodyOf(t, reg, name, sa)
	if !strings.Contains(strings.ToLower(body), "youscrythisturn") {
		t.Fatalf("precondition: %s CheckSVar$ body = %q, want YouScryThisTurn", name, body)
	}
	// The setup casts a scry spell (Opt or Preordain) from hand.
	scry := false
	for _, st := range it.Scenario.Steps {
		if st.Op != "cast" {
			continue
		}
		for _, probe := range scryProbes {
			if strings.HasSuffix(st.Card, ":"+probe) {
				scry = true
			}
		}
	}
	if !scry {
		t.Fatalf("%s: no scry probe cast in %v", name, it.Scenario.Steps)
	}
	assertRestrictionItem(t, reg, name, it)
}

// svarBodyOf is the requirement ability's CheckSVar body, resolving an SVar
// name to its body or returning the inline Count$ expression.
func svarBodyOf(t *testing.T, reg *cards.Registry, name string, sa *cards.SA) string {
	t.Helper()
	check := sa.ParamStr(cards.PKCheckSVar)
	if check == "" {
		t.Fatalf("precondition: %s ability has no CheckSVar$", name)
	}
	c, _ := reg.Lookup(name)
	if body, ok := c.Faces[0].SVars[check]; ok {
		return body
	}
	return check
}

func seatHasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func stepsHaveOp(steps []oraclegen.Step, op string) bool {
	for _, st := range steps {
		if st.Op == op {
			return true
		}
	}
	return false
}

func countOp(steps []oraclegen.Step, op string) int {
	n := 0
	for _, st := range steps {
		if st.Op == op {
			n++
		}
	}
	return n
}
