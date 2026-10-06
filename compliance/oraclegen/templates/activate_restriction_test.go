// Tests for the activation-restriction preludes (ticket
// cli-20261006T132127Z-088fce64). Each case drives a REAL corpus card whose
// activated ability is offered only when a restriction gate holds, and asserts
// both that the generated item plays through gorge and that the setup actually
// carries the prerequisite board the gate names (so a regression that drops the
// prelude fails loudly rather than passing on a card that was never offered).
package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestActivateRestrictionPresentLand covers an IsPresent$ filter on a land: the
// DFT/DSK "Verge" cycle's second ability is offered only while the controller
// controls a Forest or a Plains. The prelude must place one of them.
func TestActivateRestrictionPresentLand(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Hushwood Verge"
	it, req := activateRequirement(t, reg, name, "activate#0.1")
	sa := abilityOf(t, reg, name, req)
	// Precondition: the ability really carries the restriction this test is
	// about; otherwise the prelude under test would never run.
	if got := sa.ParamStr(cards.PKIsPresent); !strings.Contains(got, "Forest.YouCtrl") || !strings.Contains(got, "Plains.YouCtrl") {
		t.Fatalf("precondition: %s IsPresent$ = %q, want Forest/Plains", name, got)
	}
	// Precondition: the gate binds -- the bare source alone is not a Forest
	// or Plains, so the row could not be served without the prelude.
	if hasForestOrPlains([]string{name}) {
		t.Fatalf("precondition: %s is itself a Forest or Plains", name)
	}
	if !hasForestOrPlains(it.Scenario.Setup["p0"].Battlefield) {
		t.Fatalf("%s setup has no Forest or Plains land: %v", name, it.Scenario.Setup["p0"].Battlefield)
	}
	assertRestrictionItem(t, reg, name, it)
}

// TestActivateRestrictionPresentCount covers a counted IsPresent$: Cryptic
// Caves is offered only with five or more lands you control, so the prelude
// must place at least five.
func TestActivateRestrictionPresentCount(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Cryptic Caves"
	it, req := activateRequirement(t, reg, name, "activate#0.1")
	sa := abilityOf(t, reg, name, req)
	if got := sa.ParamStr(cards.PKIsPresent); !strings.Contains(got, "Land.YouCtrl") {
		t.Fatalf("precondition: %s IsPresent$ = %q, want Land.YouCtrl", name, got)
	}
	if got := sa.ParamStr(cards.PKPresentCompare); !strings.EqualFold(strings.TrimSpace(got), "GE5") {
		t.Fatalf("precondition: %s PresentCompare$ = %q, want GE5", name, got)
	}
	lands := countLands(it.Scenario.Setup["p0"].Battlefield)
	if lands < 5 {
		t.Fatalf("%s setup has %d lands, want at least 5: %v", name, lands, it.Scenario.Setup["p0"].Battlefield)
	}
	assertRestrictionItem(t, reg, name, it)
}

// TestActivateRestrictionDelirium covers an Activation$ Delirium gate on an
// ability that functions from the graveyard: Balustrade Wurm's return is
// offered only with four or more card types among the graveyard cards.
func TestActivateRestrictionDelirium(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Balustrade Wurm"
	it, req := activateRequirement(t, reg, name, "activate#0.0")
	sa := abilityOf(t, reg, name, req)
	if got := sa.ParamStr(cards.PKActivation); !strings.EqualFold(strings.TrimSpace(got), "Delirium") {
		t.Fatalf("precondition: %s Activation$ = %q, want Delirium", name, got)
	}
	// Precondition: the card under test starts in the graveyard; the
	// graveyard ability is the one under test.
	grave := it.Scenario.Setup["p0"].Graveyard
	if !containsString(grave, name) {
		t.Fatalf("precondition: %s is not in p0's graveyard: %v", name, grave)
	}
	// Precondition: the gate binds -- a correctly built prelude reaches four
	// distinct card types, which the source card alone does not.
	if types := distinctGraveTypes(reg, grave); types < 4 {
		t.Fatalf("%s graveyard has %d distinct card types, want at least 4: %v", name, types, grave)
	}
	assertRestrictionItem(t, reg, name, it)
}

// abilityOf resolves the requirement's activated ability from the registry.
func abilityOf(t *testing.T, reg *cards.Registry, name string, req levelb.Requirement) *cards.SA {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok || req.Face < 0 || req.Face >= len(c.Faces) {
		t.Fatalf("%s: requirement face %d out of range", name, req.Face)
	}
	idx, err := strconv.Atoi(req.Slot)
	if err != nil || idx < 0 || idx >= len(c.Faces[req.Face].Abilities) {
		t.Fatalf("%s: requirement slot %q is not an ability index", name, req.Slot)
	}
	return c.Faces[req.Face].Abilities[idx]
}

// assertRestrictionItem checks the single activate step carries an XMage
// prefix and that the scenario replays cleanly through gorge.
func assertRestrictionItem(t *testing.T, reg *cards.Registry, name string, it oraclegen.Item) {
	t.Helper()
	if len(it.XAbility) != len(it.Scenario.Steps) {
		t.Fatalf("%s: xmage_ability has %d entries for %d steps", name, len(it.XAbility), len(it.Scenario.Steps))
	}
	activate := 0
	for i, st := range it.Scenario.Steps {
		if st.Op == "activate" {
			activate++
			if it.XAbility[i] == "" {
				t.Fatalf("%s step %d is an activate with no xmage_ability", name, i)
			}
		}
	}
	if activate != 1 {
		t.Fatalf("%s has %d activate steps, want exactly 1", name, activate)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("%s does not play through gorge: ok=%v fails=%v", name, ok, res.Fails)
	}
}

func hasForestOrPlains(names []string) bool {
	for _, n := range names {
		if n == "Forest" || n == "Plains" {
			return true
		}
	}
	return false
}

// distinctGraveTypes counts distinct card types among the graveyard cards.
func distinctGraveTypes(reg *cards.Registry, grave []string) int {
	seen := map[string]bool{}
	for _, name := range grave {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		for _, typ := range c.Faces[0].Types {
			seen[typ] = true
		}
	}
	return len(seen)
}

func countLands(battlefield []string) int {
	n := 0
	for _, name := range battlefield {
		switch name {
		case "Forest", "Island", "Swamp", "Mountain", "Plains", "Wastes":
			n++
		}
	}
	return n
}
