package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestTriggerETBProbe: an "a <filter> you control enters" trigger is served by
// a probe the filter accepts, not the Grizzly Bears every etb-other cause used
// to be. Each row names the card, the probe's op, and a type word the probe
// card must carry; the item must play through gorge and show the card's
// trigger on the stack (so gorge's own matcher, not this table, decided the
// probe satisfies the filter).
func TestTriggerETBProbe(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, op, typeWord string }{
		{"Eusocial Engineering", "trigger#0.0", "play", "Land"},
		{"Balemurk Leech", "trigger#0.0", "cast", "Enchantment"},
		{"Mechan Assembler", "trigger#0.0", "cast", "Artifact"},
		{"Champion of the Path", "trigger#0.0", "cast", "Elemental"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, "trigger.etb-other")
			steps := it.Scenario.Steps
			if len(steps) == 0 || steps[0].Op != tc.op {
				t.Fatalf("first step = %+v, want a %s", steps, tc.op)
			}
			probe := strings.TrimPrefix(steps[0].Card, "p0:")
			if probe == bearsProbe || probe == "" {
				t.Fatalf("probe = %q, want a card the trigger's filter accepts, not the generic Bears", probe)
			}
			c, ok := reg.Lookup(probe)
			if !ok || !strings.Contains(strings.Join(c.Faces[0].Types, " "), tc.typeWord) {
				t.Fatalf("probe %q is not a %s", probe, tc.typeWord)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerProbeSlotOnStack(t, reg, it.Scenario, tc.name, "0") {
				t.Fatalf("no snapshot shows an ability on the stack sourced by %s", tc.name)
			}
			t.Logf("%s: %s %s", tc.name, steps[0].Op, probe)
		})
	}
}

// TestTriggerETBProbeSpecialFilters: a filter the cast/played probe walk
// cannot satisfy by construction (a token, a face-down permanent, a chosen
// type, an opponent's permanent) is served by its dedicated cause, and the
// scenario's probe names the mechanism: a token maker, a face-down cast, a
// cast of the chosen type's creature, and a p1 cast for the opponent's
// permanent (ticket g17).
func TestTriggerETBProbeSpecialFilters(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, probe string }{
		{"Belladonna Took", "trigger#0.0", "Sprout"},
		{"Cryptid Inspector", "trigger#0.0", "disguised"},
		{"Dawn-Blessed Pennant", "trigger#0.0", "Arc Runner"},
		{"Gideon the Oathless", "trigger#0.0", "p1:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				if r.Sub != "trigger.etb-other" {
					t.Fatalf("precondition: %s %s classified %s", tc.name, tc.key, r.Sub)
				}
				it, skip := GenerateB(reg, tc.name, r)
				if skip != nil {
					t.Fatalf("%s %s: %s", tc.name, tc.key, skip.Reason)
				}
				matched := false
				for _, st := range it.Scenario.Steps {
					matched = matched || strings.Contains(st.Card, tc.probe) || st.CastMode == tc.probe
				}
				if !matched {
					t.Fatalf("no step names the %s mechanism: steps = %+v", tc.probe, it.Scenario.Steps)
				}
				if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
					t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
				}
				return
			}
			t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
		})
	}
}

// TestTriggerETBProbeGraveyardZone: a trigger with TriggerZones$ Graveyard
// only works with its card in the graveyard, so the scenario puts it there and
// not on the battlefield.
func TestTriggerETBProbeGraveyardZone(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Bloodghast", "trigger#0.0", "trigger.etb-other")
	p0 := it.Scenario.Setup["p0"]
	inGrave, onField := false, false
	for _, n := range p0.Graveyard {
		inGrave = inGrave || n == "Bloodghast"
	}
	for _, n := range p0.Battlefield {
		onField = onField || n == "Bloodghast"
	}
	if !inGrave || onField {
		t.Fatalf("Bloodghast graveyard=%v battlefield=%v, want it in the graveyard only", p0.Graveyard, p0.Battlefield)
	}
	if !triggerProbeSlotOnStack(t, reg, it.Scenario, "Bloodghast", "0") {
		t.Fatal("Bloodghast's graveyard trigger never reaches the stack")
	}
}
