package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// An own additional-cost probe is kept only when it plays through gorge: a
// cost grammar the generator cannot pay is a named skip, never a scenario
// whose precondition is false.
func TestCostStaticOwnAdditionalCostRequiresReplay(t *testing.T) {
	reg := loadGenRegistry(t)
	unmodelled := map[string]bool{
		"Benevolent River Spirit":  true, // Waterbend<5>
		"Soul Immolation":          true, // Blight<X>
		"Close Encounter":          true, // ChooseCard<...>
		"Champions of the Perfect": true, // BeholdExile<1/Elf>, no fixture
	}
	names := []string{"Champion of the Clachan", "Officious Interrogation", "Dragon's Prey", "Crashing Wave"}
	for n := range unmodelled {
		names = append(names, n)
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
			if unmodelled[name] {
				if skip == nil || !strings.Contains(skip.Reason, "prerequisite fixture unavailable") {
					t.Fatalf("%s: item=%v skip=%+v, want named fixture-unavailable skip", name, it.Name, skip)
				}
				return
			}
			if skip != nil {
				return
			}
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("%s: returned a scenario that does not play through gorge", name)
			}
		})
	}
}
