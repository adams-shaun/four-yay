package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestCastFamilyOwnershipCauseFires pins the generic exile-and-grant cause of
// the ownership provenance family (Card.YouDontOwn / Spell.YouDontOwn): Nita,
// Forum Conciliator exiles a p1-owned Opt from p1's graveyard and grants p0
// the cast, so the trigger of each manifest row shows on the stack. The
// preconditions assert the setup the ownership predicate reads -- the granter
// on p0's battlefield, the probe in p1's graveyard, and a cast step naming the
// p1-owned card -- so a vacuous item cannot pass.
func TestCastFamilyOwnershipCauseFires(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key string }{
		{"Gonti, Night Minister", "trigger#0.0"},
		{"Vaan, Street Thief", "trigger#0.1"},
		{"Nita, Forum Conciliator", "trigger#0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, "trigger.spell-cast")
			p0, p1 := it.Scenario.Setup["p0"], it.Scenario.Setup["p1"]
			if !inZone(p0.Battlefield, "Nita, Forum Conciliator") {
				t.Fatalf("precondition: granter not on p0 battlefield: %v", p0.Battlefield)
			}
			if !inZone(p1.Graveyard, "Opt") {
				t.Fatalf("precondition: p1 graveyard does not hold the probe: %v", p1.Graveyard)
			}
			cast := false
			for _, step := range it.Scenario.Steps {
				if step.Op == "cast" && strings.Contains(step.Card, "p1:Opt") {
					cast = true
				}
			}
			if !cast {
				t.Fatalf("cause does not cast the p1-owned probe: %+v", it.Scenario.Steps)
			}
			// The cause's resolve step between the activate and the granted
			// cast is load-bearing, so the resolve-stripping
			// triggerShownOnStack helper cannot see the trigger: replay the
			// served scenario verbatim and look for the trigger's own slot.
			card, ok2 := reg.Lookup(tc.name)
			if !ok2 || len(card.Faces) == 0 {
				t.Fatalf("precondition: %s not in registry", tc.name)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			_, slot, _ := strings.Cut(strings.TrimPrefix(tc.key, "trigger#"), ".")
			if !ok || !abilityOnStack(res.Snapshots, stackSourceWants(reg, tc.name, card.Faces[0]), slot) {
				t.Fatalf("%s's ownership trigger never appears on stack (ok=%v)", tc.name, ok)
			}
		})
	}
}
