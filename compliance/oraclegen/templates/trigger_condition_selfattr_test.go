// Focused tests for the self-attribute gates this ticket clears: The Mind
// Stone's ∞ trigger is offered only once its own Harness activation has run
// (the prelude activates it in turn 1's main phase), and Cryptex's Surveil
// ability is offered only with five unlock counters on it (the setup carries
// them).
package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestActivateSelfStateCountersCryptex(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Cryptex"
	it, req := activateRequirement(t, reg, name, "activate#0.1")
	sa := abilityOf(t, reg, name, req)
	// Precondition: the gate really is the five-unlock-counter floor.
	gate := strings.ToLower(sa.ParamStr(cards.PKIsPresent))
	if !strings.Contains(gate, "counters_ge5_unlock") {
		t.Fatalf("precondition: %s IsPresent$ = %q, want counters_GE5_UNLOCK", name, gate)
	}
	// The setup carries five unlock counters on the source itself.
	n := 0
	for _, kind := range it.Scenario.Setup["p0"].Counters[name] {
		n += int(kind)
	}
	if n < 5 {
		t.Fatalf("%s: setup counters %v, want at least 5 unlock counters", name, it.Scenario.Setup["p0"].Counters)
	}
	assertRestrictionItem(t, reg, name, it)
}

func TestTriggerSelfActivatedAttributeMindStone(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "The Mind Stone"
	it := triggerItem(t, reg, name, "trigger#0.0")
	// Precondition: the gate really is the card's own harnessed attribute,
	// granted by its activated Harness ability.
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	gate := strings.ToLower(card.Faces[0].Triggers[0].ParamStr(cards.PKIsPresent))
	if !strings.Contains(gate, "harnessed") {
		t.Fatalf("precondition: %s IsPresent$ = %q, want harnessed", name, gate)
	}
	granted := false
	for _, sa := range card.Faces[0].Abilities {
		body := strings.ToLower(sa.Line)
		if sa.IsActivated() && strings.Contains(body, "alterattribute") && strings.Contains(body, "attributes$ harnessed") {
			granted = true
		}
	}
	if !granted {
		t.Fatalf("precondition: %s grants harnessed by no activated ability", name)
	}
	// The prelude activates the source itself before the trigger's checkpoint.
	activated := false
	for _, st := range it.Scenario.Steps {
		if st.Op == "activate" && st.Card == "p0:"+name && st.AbilityIndex != nil {
			activated = true
		}
	}
	if !activated {
		t.Fatalf("%s: no activate step on the source: %v", name, it.Scenario.Steps)
	}
}

func TestTriggerSelfActivatedAttributeSoulStone(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "The Soul Stone"
	// Precondition: the gate really is the card's own harnessed attribute,
	// granted by an activated Harness ability whose cost exiles a creature.
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	gate := strings.ToLower(card.Faces[0].Triggers[0].ParamStr(cards.PKIsPresent))
	if !strings.Contains(gate, "harnessed") {
		t.Fatalf("precondition: %s IsPresent$ = %q, want harnessed", name, gate)
	}
	var grantCost string
	for _, sa := range card.Faces[0].Abilities {
		body := strings.ToLower(sa.Line)
		if sa.IsActivated() && strings.Contains(body, "alterattribute") && strings.Contains(body, "attributes$ harnessed") {
			grantCost = sa.ParamStr(cards.PKCost)
		}
	}
	if grantCost == "" {
		t.Fatalf("precondition: %s grants harnessed by no activated ability", name)
	}
	if !strings.Contains(strings.ToUpper(grantCost), "EXILE<1/CREATURE>") {
		t.Fatalf("precondition: harness cost %q, want an Exile<1/Creature> part", grantCost)
	}
	it := triggerItem(t, reg, name, "trigger#0.0")
	// The harness prelude carries the cost fixture: setup p0 controls the
	// creature the Exile cost consumes while the activation plays.
	creature := false
	for _, bf := range it.Scenario.Setup["p0"].Battlefield {
		if bf == "Colossal Dreadmaw" {
			creature = true
		}
	}
	if !creature {
		t.Fatalf("%s: setup battlefield %v, want the Exile cost's Colossal Dreadmaw fixture", name, it.Scenario.Setup["p0"].Battlefield)
	}
	// XMage scripts the cost pick on the harness activate step.
	act := 0
	for i, st := range it.Scenario.Steps {
		if st.Op != "activate" || st.Card != "p0:"+name {
			continue
		}
		act++
		pick := false
		for _, xa := range it.XAnswers[i] {
			if xa.Kind == "choice" && strings.EqualFold(xa.Value, "Colossal Dreadmaw") {
				pick = true
			}
		}
		if !pick {
			t.Fatalf("%s step %d: XAnswers %v, want the Exile cost's Colossal Dreadmaw pick", name, i, it.XAnswers[i])
		}
	}
	if act != 1 {
		t.Fatalf("%s: %d harness activate steps, want 1: %v", name, act, it.Scenario.Steps)
	}
}
