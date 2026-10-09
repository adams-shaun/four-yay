package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// An own additional-cost probe is kept only when it plays through gorge: a
// cost grammar the generator cannot pay is a named skip, never a scenario
// whose precondition is false. Champions of the Perfect's BeholdExile<1/Elf>
// now has an Elf fixture (elfBeholdFixture), so it is served; the other three
// costs (Waterbend<5>, Blight<X>, ChooseCard) have no fixture and stay named.
func TestCostStaticOwnAdditionalCostRequiresReplay(t *testing.T) {
	reg := loadGenRegistry(t)
	unmodelled := map[string]string{
		"Benevolent River Spirit": "Waterbend<5>",
		"Soul Immolation":         "Blight<X>",
		"Close Encounter":         "ChooseCard<",
	}
	names := []string{"Champion of the Clachan", "Officious Interrogation", "Dragon's Prey", "Crashing Wave", "Champions of the Perfect"}
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
			if cost, ok := unmodelled[name]; ok {
				if skip == nil || !strings.Contains(skip.Reason, cost) {
					t.Fatalf("%s: item=%v skip=%+v, want a named skip naming %q", name, it.Name, skip, cost)
				}
				return
			}
			if skip != nil {
				t.Fatalf("%s must be served, got skip %s", name, skip.Reason)
			}
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("%s: returned a scenario that does not play through gorge", name)
			}
		})
	}
}
