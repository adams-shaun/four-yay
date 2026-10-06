package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestZoneChangeCounterVictimRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"The Ooze", "Costume Closet"} {
		t.Run(name, func(t *testing.T) {
			it := triggerRequirement(t, reg, name, "trigger#0.0", "trigger.ltb-other")
			p0 := it.Scenario.Setup["p0"]
			if p0.Counters[bearsProbe]["P1P1"] != 1 {
				t.Fatalf("precondition: victim has no +1/+1 counter: %+v", p0.Counters)
			}
			found := false
			for _, card := range p0.Battlefield {
				found = found || card == bearsProbe
			}
			if !found {
				t.Fatal("precondition: counter victim is not on the battlefield")
			}
			if !stackedAfterCause(t, reg, it.Scenario, name, "0") {
				t.Fatal("counter-bearing victim never puts the trigger on the stack")
			}
			// Removing the counter must disable the trigger, not merely alter
			// the recipe's serialized setup.
			p0.Counters = nil
			it.Scenario.Setup["p0"] = p0
			if stackedAfterCause(t, reg, it.Scenario, name, "0") {
				t.Fatal("unmodified victim also triggers: counter fixture proves nothing")
			}
		})
	}
}

func TestZoneChangeCounterProbeSatisfiesFilter(t *testing.T) {
	reg := loadGenRegistry(t)
	bears, ok := reg.Lookup(bearsProbe)
	if !ok {
		t.Fatal("precondition: Bears missing from corpus")
	}
	for _, filter := range []string{"Creature.YouCtrl+counters_GE1_P1P1", "Creature.modified+YouCtrl", "Creature.YouCtrl+HasCounters"} {
		fp := newFilterProbe(filter, state.ZBattlefield)
		if !fp.decided || fp.accepts(bears) {
			t.Fatalf("precondition: unmodified Bears must fail %q", filter)
		}
		fp.p1p1 = true
		if !fp.accepts(bears) {
			t.Errorf("counter-bearing Bears rejected by %q", filter)
		}
	}
}
