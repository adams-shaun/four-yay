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
			if !triggerShownOnStack(t, reg, it.Scenario, tc.name) {
				t.Fatalf("no snapshot shows an ability on the stack sourced by %s", tc.name)
			}
			t.Logf("%s: %s %s", tc.name, steps[0].Op, probe)
		})
	}
}

// TestTriggerETBProbeNamedSkip: a filter no probe can satisfy by construction
// (a token, a face-down permanent, a chosen type, an opponent's permanent)
// skips with the filter named, never the bare "did not fire".
func TestTriggerETBProbeNamedSkip(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, want string }{
		{"Belladonna Took", "trigger#0.0", "trigger no recipe: etb filter token"},
		{"Cryptid Inspector", "trigger#0.0", "trigger no recipe: etb filter faceDown"},
		{"Dawn-Blessed Pennant", "trigger#0.0", "trigger no recipe: etb filter ChosenType"},
		{"Gideon the Oathless", "trigger#0.0", "trigger no recipe: etb filter OppCtrl"},
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
				_, skip := GenerateB(reg, tc.name, r)
				if skip == nil || !strings.HasPrefix(skip.Reason, tc.want) {
					t.Fatalf("skip = %v, want prefix %q", skip, tc.want)
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
	if !triggerShownOnStack(t, reg, it.Scenario, "Bloodghast") {
		t.Fatal("Bloodghast's graveyard trigger never reaches the stack")
	}
}
