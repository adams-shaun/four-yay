package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// counterAddedRows is one real card per counter-added shape: the card itself
// (P1P1 and the PLAN threshold), any creature, another creature, a creature
// you control, a filtered creature, the CounterAddedAll / CounterPlayerAddedAll
// / CounterTypeAddedAll recipient specs, and the optional-decider variants.
// Every row must generate a scenario that plays through gorge and puts the
// card's own trigger on the stack at its slot.
var counterAddedRows = []struct {
	name, key string
	// wantProbe is any one of the steps the scenario must contain: a P1P1 row
	// casts Battlegrowth, a PLAN row either casts Steady Progress or activates
	// a proliferate permanent.
	wantProbe []string
	// plan is the threshold n for a CounterAdded PLAN row: setup must hold
	// n-1 plan counters on the card and the proliferate must add the last.
	plan int
}{
	{"Pensive Professor", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"Berta, Wise Extrapolator", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"Exemplar of Light", "trigger#0.1", []string{"cast:p0:Battlegrowth"}, 0},
	{"Ant-Man, Colony Commander", "trigger#0.1", []string{"cast:p0:Battlegrowth"}, 0},
	{"Knight of Wundagore", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"Mikey & Leo, Chaos Order", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"Earth Kingdom General", "trigger#0.1", []string{"cast:p0:Battlegrowth"}, 0},
	{"Terrasymbiosis", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"Stocking the Pantry", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"Wildwood Scourge", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"Invisible Woman, Sue Storm", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"The Great Goblin", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"Stalwart Successor", "trigger#0.0", []string{"cast:p0:Battlegrowth"}, 0},
	{"Claim the Kingdom", "trigger#0.1", []string{"cast:p0:Steady Progress", "activate:p0:Karn's Bastion"}, 4},
	{"Death to Our Enemies", "trigger#0.1", []string{"cast:p0:Steady Progress", "activate:p0:Karn's Bastion"}, 4},
	{"Rewrite History", "trigger#0.1", []string{"cast:p0:Steady Progress", "activate:p0:Karn's Bastion"}, 4},
	{"Doom Reigns Supreme", "trigger#0.1", []string{"cast:p0:Steady Progress", "activate:p0:Karn's Bastion"}, 5},
	{"Construct a Cosmic Cube", "trigger#0.1", []string{"cast:p0:Steady Progress", "activate:p0:Karn's Bastion"}, 7},
}

// TestCounterAddedTriggerRecipesFire: each counter-added shape generates a
// scenario whose cause casts the expected probe, whose setup holds the
// expected plan counters (the PLAN rows), and whose card trigger reaches the
// stack.
func TestCounterAddedTriggerRecipesFire(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range counterAddedRows {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, levelb.CounterAddedSub)
			probe := ""
			for _, st := range it.Scenario.Steps {
				key := st.Op + ":" + st.Card
				for _, want := range tc.wantProbe {
					if key == want {
						probe = want
					}
				}
			}
			if probe == "" {
				t.Fatalf("precondition: scenario never runs %v: %+v", tc.wantProbe, it.Scenario.Steps)
			}
			if tc.plan > 0 {
				if got := it.Scenario.Setup["p0"].Counters[tc.name]["PLAN"]; got != int32(tc.plan-1) {
					t.Fatalf("precondition: setup plan counters = %d, want %d on %s: %+v",
						got, tc.plan-1, tc.name, it.Scenario.Setup["p0"].Counters)
				}
			} else if _, ok := it.Scenario.Setup["p0"].Counters[tc.name]; ok {
				t.Fatalf("precondition: a P1P1 row must not seed counters on %s: %+v",
					tc.name, it.Scenario.Setup["p0"].Counters)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !stackedAfterCause(t, reg, it.Scenario, tc.name, triggerSlot(tc.key)) {
				t.Fatalf("%s trigger %s never reaches the stack", tc.name, tc.key)
			}
		})
	}
}

// triggerSlot is the trigger index inside a "trigger#<face>.<slot>" key.
func triggerSlot(key string) string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '.' {
			return key[i+1:]
		}
	}
	return ""
}

// TestCounterAddedRecipeFailsWithoutTheRecipientProbe: the P1P1 recipe's
// choice of probe is real -- a recipient filter whose only candidate the
// engine rejects produces a named skip, not a bare "did not fire". The filter
// names a creature type no probe candidate has.
func TestCounterAddedRecipeFailsWithoutTheRecipientProbe(t *testing.T) {
	reg := loadGenRegistry(t)
	if _, ok := reg.Lookup("Grizzly Bears"); !ok {
		t.Fatal("precondition: Grizzly Bears missing from corpus")
	}
	// A Goblin filter is satisfied by Goblin Piker; a known filter no
	// candidate matches ("Creature.Zombie") is not. An unknown predicate
	// fails open by design (newFilterProbe leaves it undecided), so the
	// negative case must name a predicate the matcher knows.
	trig := &cards.Trigger{}
	if probe := counterAcceptedProbe(reg, "Creature.Goblin", trig, "Grizzly Bears"); probe != "Goblin Piker" {
		t.Fatalf("precondition: Goblin probe = %q, want Goblin Piker", probe)
	}
	if probe := counterAcceptedProbe(reg, "Creature.Zombie", trig, "Grizzly Bears"); probe != "" {
		t.Fatalf("unmatchable filter probe = %q, want none", probe)
	}
}

// TestCounterAddedNamesSelf pins the local filter-token helper the recipe uses
// in place of levelb's unexported one.
func TestCounterAddedNamesSelf(t *testing.T) {
	for _, tc := range []struct {
		filter string
		want   bool
	}{
		{"Card.Self", true},
		{"Card.Self+wasCastByYou", true},
		{"Creature.Self", true},
		{"Card.Other", false},
		{"Creature.YouCtrl,Card.Self", true},
		{"", false},
	} {
		if got := counterNamesSelf(tc.filter); got != tc.want {
			t.Errorf("counterNamesSelf(%q) = %v, want %v", tc.filter, got, tc.want)
		}
	}
}
