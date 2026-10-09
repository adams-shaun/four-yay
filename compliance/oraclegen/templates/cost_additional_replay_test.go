package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// An own additional-cost probe is kept only when it plays through gorge: a
// cost grammar the generator cannot pay is a named skip, never a scenario
// whose precondition is false. Every one of these costs is now paid by a
// table rather than a per-card branch: Champions of the Perfect's
// BeholdExile<1/Elf> has an Elf fixture (elfBeholdFixture), and
// cost_raise_tokens.go pays Waterbend<N> (Benevolent River Spirit, Water
// Whip), Blight<X> (Soul Immolation) and Close Encounter's ChooseCard.
// Crashing Wave's Waterbend<X> casts at the pool-bound X = 0.
func TestCostStaticOwnAdditionalCostRequiresReplay(t *testing.T) {
	reg := loadGenRegistry(t)
	names := []string{
		"Champion of the Clachan", "Officious Interrogation", "Dragon's Prey",
		"Crashing Wave", "Champions of the Perfect",
		"Benevolent River Spirit", "Water Whip", "Soul Immolation", "Close Encounter",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			c, ok := reg.Lookup(name)
			if !ok {
				t.Fatalf("precondition: %s missing from corpus", name)
			}
			var req *levelb.Requirement
			for _, r := range levelb.Requirements(c) {
				if r.Key == "static#0.0" {
					r := r
					req = &r
				}
			}
			if req == nil || req.Gap != "" {
				t.Fatalf("precondition: %s static#0.0 = %+v, want served", name, req)
			}
			it, skip := GenerateB(reg, name, *req)
			if skip != nil {
				t.Fatalf("%s must be served, got skip %s", name, skip.Reason)
			}
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("%s: returned a scenario that does not play through gorge", name)
			}
		})
	}
}
