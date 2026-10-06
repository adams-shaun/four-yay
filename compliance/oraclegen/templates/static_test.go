package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// staticItemFor finds name's level-B requirement for key, checks its
// sub-family, and generates its item.
func staticItemFor(t *testing.T, reg *cards.Registry, name, key, sub string) oraclegen.Item {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		if r.Sub != sub {
			t.Fatalf("precondition: %s %s classified %s, want %s", name, key, r.Sub, sub)
		}
		it, skip := GenerateB(reg, name, r)
		if skip != nil {
			t.Fatalf("%s %s: %s", name, key, skip.Reason)
		}
		return it
	}
	t.Fatalf("precondition: %s carries no requirement %s", name, key)
	return oraclegen.Item{}
}

// runItemScenario replays an item's own scenario in gorge. withCard=false
// drops the card under test from p0's battlefield and clears every step
// expectation, giving the control the observation is measured against. It
// deep-copies the setup map and the step slice so the item itself is never
// mutated.
func runItemScenario(t *testing.T, reg *cards.Registry, it oraclegen.Item, withCard bool) rules.OracleResult {
	t.Helper()
	sc := it.Scenario
	setup := make(map[string]oraclegen.Seat, len(sc.Setup))
	for k, v := range sc.Setup {
		setup[k] = v
	}
	sc.Setup = setup
	sc.Steps = append([]oraclegen.Step(nil), sc.Steps...)
	if !withCard {
		p0 := sc.Setup["p0"]
		var kept []string
		for _, b := range p0.Battlefield {
			if b != it.Card {
				kept = append(kept, b)
			}
		}
		p0.Battlefield = kept
		sc.Setup["p0"] = p0
		for i := range sc.Steps {
			sc.Steps[i].Expect = nil
		}
	}
	res, ok := runStatic(reg, sc)
	if !ok {
		t.Fatalf("scenario does not replay")
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario fails: %v", res.Fails)
	}
	return res
}

// TestStaticDisableTriggersServesKarn: Karn, Argent Defender's DisableTriggers
// (static#0.0) is served by the stack observation. Precondition: the probe's
// enters-the-battlefield trigger DOES reach the stack without Karn, so the
// suppression the item asserts cannot pass vacuously.
func TestStaticDisableTriggersServesKarn(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Karn, Argent Defender", "static#0.0", "static.disable-triggers")

	// Precondition: the control (probe alone) shows the trigger on the stack.
	control := runItemScenario(t, reg, it, false)
	last := control.Snapshots[len(control.Snapshots)-1]
	if !triggerOnStack(last, "p0:"+disableTriggerProbe) {
		t.Fatalf("precondition: %s's enters trigger is not on the stack without Karn; the item would pass vacuously", disableTriggerProbe)
	}

	// Observation: with Karn, the trigger never reaches the stack.
	got := runItemScenario(t, reg, it, true)
	last = got.Snapshots[len(got.Snapshots)-1]
	if triggerOnStack(last, "p0:"+disableTriggerProbe) {
		t.Fatalf("Karn on the battlefield: %s's enters trigger is still on the stack", disableTriggerProbe)
	}
	// The item must carry the want=false expectation, or the "nothing
	// happens" claim is held by no field (the stack is empty at setup too).
	lastStep := it.Steps[len(it.Steps)-1]
	if len(lastStep.Expect) != 1 || lastStep.Expect[0].TriggerOnStack != "p0:"+disableTriggerProbe ||
		lastStep.Expect[0].Want == nil || *lastStep.Expect[0].Want {
		t.Fatalf("item's last step does not assert trigger_on_stack=%s want=false: %+v", disableTriggerProbe, lastStep.Expect)
	}
}

// TestStaticCombatDamageToughnessServesGhalta: Ghalta the Immovable's
// CombatDamageToughness (static#0.2) is served by combat damage.
// Precondition: the attacker deals its power (2) without Ghalta, so the
// raised damage the item freezes cannot pass vacuously.
func TestStaticCombatDamageToughnessServesGhalta(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Ghalta, the Immovable", "static#0.2", "static.combat-damage-toughness")

	control := runItemScenario(t, reg, it, false)
	base, ok := damageToDefender(control)
	if !ok {
		t.Fatalf("control has no combat-damage checkpoint")
	}
	if base != 2 {
		t.Fatalf("precondition: %s deals %d without Ghalta, want its power 2", toughnessAttacker, base)
	}

	got := runItemScenario(t, reg, it, true)
	dmg, ok := damageToDefender(got)
	if !ok {
		t.Fatalf("observation has no combat-damage checkpoint")
	}
	if dmg != 4 {
		t.Fatalf("with Ghalta, %s deals %d, want its toughness 4", toughnessAttacker, dmg)
	}
}
