package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
)

// TestZoneChangeETBHistoryProvenanceIsServed: a zone-history filter naming
// graveyard/exile provenance (ticket g17) is served by the probe that
// supplies it — Reanimate reanimates the creature from p0's graveyard,
// Flicker returns a battlefield creature from exile — mirroring
// TestTriggerETBProbeSpecialFilters' mechanism assertions: the served item
// names the probe, and the prelude puts the creature where the probe takes
// it from.
func TestZoneChangeETBHistoryProvenanceIsServed(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, probe, zone, zoneOf string }{
		{"Twilight Diviner", "Reanimate", "graveyard", "Graveyard"},
		{"Extraordinary Journey", "Flicker", "exile", "Battlefield"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s missing from corpus", tc.name)
			}
			f := c.Faces[0]
			if len(f.Triggers) < 2 {
				t.Fatalf("precondition: %s carries no second trigger", tc.name)
			}
			filter := levelb.ZoneChangeFilter(&f.Triggers[1])
			if !strings.Contains(strings.ToLower(filter), tc.zone) {
				t.Fatalf("precondition: history filter %q names no %s provenance", filter, tc.zone)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != "trigger#0.1" {
					continue
				}
				if r.Sub != "trigger.etb-other" {
					t.Fatalf("precondition: history trigger classified %s", r.Sub)
				}
				it, skip := GenerateB(reg, tc.name, r)
				if skip != nil {
					t.Fatalf("item=%s skip=%v, want the zone-history filter served by the provenance probe", it.ID, skip)
				}
				matched := false
				for _, st := range it.Scenario.Steps {
					matched = matched || strings.Contains(st.Card, tc.probe)
				}
				if !matched {
					t.Fatalf("no step names the %s provenance probe: steps = %+v", tc.probe, it.Scenario.Steps)
				}
				seat := it.Scenario.Setup["p0"]
				prelude := seat.Battlefield
				if tc.zoneOf == "Graveyard" {
					prelude = seat.Graveyard
				}
				found := false
				for _, n := range prelude {
					found = found || n == "Grizzly Bears"
				}
				if !found {
					t.Fatalf("p0 %s = %v, want the creature the %s probe returns", tc.zoneOf, prelude, tc.probe)
				}
				return
			}
			t.Fatal("precondition: trigger#0.1 absent")
		})
	}
	// A history-constrained branch must not block an unconstrained OR branch.
	tr := &cards.Trigger{Mode: "ChangesZoneAll"}
	if got := zoneETBHistorySkip(tr, "Creature.wasCastFromGraveyard,Creature.YouCtrl"); got != "" {
		t.Fatalf("unconstrained alternative was skipped: %s", got)
	}
}
