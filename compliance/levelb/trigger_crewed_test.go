package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestCrewedTriggerClassification pins the routing the crew/saddle-cause
// ticket (agent-20261009T174759Z-80a5bc9a) added: a Mode$ Crewed / Mode$
// Saddled row whose ValidCrew$ admits Card.Self now names a servable
// sub-family (the recipe activates a probe Vehicle/Mount's own Crew/Saddle
// ability electing the source), and Balthier and Fran's crew-by-source
// attack trigger is served the same way round-tripped. An opponent-turn gate
// the turn-1 cause cannot satisfy stays a gap.
func TestCrewedTriggerClassification(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Canyon Vaulter", "trigger#0.0", levelb.SaddledSub},
		{"Canyon Vaulter", "trigger#0.1", levelb.CrewedSub},
		{"Reckless Velocitaur", "trigger#0.0", levelb.SaddledSub},
		{"Reckless Velocitaur", "trigger#0.1", levelb.CrewedSub},
		{"Balthier and Fran", "trigger#0.0", levelb.AttacksCrewedVehicleSub},
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s not in the corpus", tc.name)
		}
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Key != tc.key {
				continue
			}
			found = true
			if r.Sub != tc.sub {
				t.Fatalf("%s %s classified %s, want %s", tc.name, tc.key, r.Sub, tc.sub)
			}
			if r.Gap != "" {
				t.Fatalf("%s %s carries gap %q, want served", tc.name, tc.key, r.Gap)
			}
		}
		if !found {
			t.Fatalf("%s carries no requirement %s", tc.name, tc.key)
		}
	}
}
