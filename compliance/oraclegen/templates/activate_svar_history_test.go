// Focused tests for the CheckSVar$ gate setups this ticket adds to
// activationSVarPrelude: the legendary-combat-damage prelude (FIN Blitzball)
// and the artifact-sacrifice prelude (MKM Detective's Satchel). Each drives a
// REAL corpus card whose activated ability is offered only when the gate
// holds, asserts the gate really is the one under test (so a regression that
// drops the prelude fails loudly), and asserts the generated item plays
// through gorge.
package templates

import (
	"strings"
	"testing"
)

// TestActivateLegendaryCombatDamageGate drives Blitzball, whose "Draw two
// cards" ability (activate#0.1) is gated on an opponent being dealt combat
// damage by a legendary creature this turn. The prelude must attack p1 with a
// legendary creature and return to a main phase.
func TestActivateLegendaryCombatDamageGate(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Blitzball"
	it, req := activateRequirement(t, reg, name, "activate#0.1")
	sa := abilityOf(t, reg, name, req)
	// Precondition: the gate really is the legendary combat-damage count.
	body := svarBodyOf(t, reg, name, sa)
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "wasdealtcombatdamagethisturnby") || !strings.Contains(lower, "legendary") {
		t.Fatalf("precondition: %s CheckSVar$ body = %q, want wasDealtCombatDamageThisTurnBy + Legendary", name, body)
	}
	// The setup carries a legendary attacker that dealt the combat damage.
	if !seatHasName(it.Scenario.Setup["p0"].Battlefield, "Elanor Gardner") {
		t.Fatalf("%s: no legendary attacker on battlefield: %v", name, it.Scenario.Setup["p0"].Battlefield)
	}
	attacked := false
	for _, st := range it.Scenario.Steps {
		if st.Op == "attack" && st.Seat == 0 && st.Defender == "p1" {
			attacked = true
		}
	}
	if !attacked {
		t.Fatalf("%s: no p0 attack step in %v", name, it.Scenario.Steps)
	}
	assertRestrictionItem(t, reg, name, it)
}

// TestActivateArtifactSacrificeGate drives Detective's Satchel, whose "{T}:
// Make a Thopter" ability (activate#0.0) is gated on having sacrificed an
// artifact this turn. The prelude must sacrifice an ARTIFACT (Ornithopter),
// not the shared sacrifice prelude's creature (Grizzly Bears).
func TestActivateArtifactSacrificeGate(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Detective's Satchel"
	it, req := activateRequirement(t, reg, name, "activate#0.0")
	sa := abilityOf(t, reg, name, req)
	// Precondition: the gate really is the artifact-sacrificed-this-turn count.
	body := svarBodyOf(t, reg, name, sa)
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "sacrificedthisturn") || !strings.Contains(lower, "artifact") {
		t.Fatalf("precondition: %s CheckSVar$ body = %q, want SacrificedThisTurn + Artifact", name, body)
	}
	// The setup sacrifices an artifact: Ornithopter is on the battlefield and
	// a Village Rites cast resolves to sacrifice it.
	if !seatHasName(it.Scenario.Setup["p0"].Battlefield, "Ornithopter") {
		t.Fatalf("%s: no artifact sacrificee on battlefield: %v", name, it.Scenario.Setup["p0"].Battlefield)
	}
	sacrificed := false
	for _, st := range it.Scenario.Steps {
		if st.Op == "cast" && strings.HasSuffix(st.Card, ":Village Rites") {
			sacrificed = true
		}
	}
	if !sacrificed {
		t.Fatalf("%s: no Village Rites sacrifice cast in %v", name, it.Scenario.Steps)
	}
	assertRestrictionItem(t, reg, name, it)
}
