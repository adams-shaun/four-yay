package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestTriggerTargetFixtures: a trigger whose effect has a mandatory target is
// removed from the stack when nothing is legal (CR 603.3d), so the scenario
// must hold the target the effect needs. Each card below fires only with it.
func TestTriggerTargetFixtures(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	creature := func(n string) bool { c, ok := reg.Lookup(n); return ok && c.Faces[0].IsCreature() }
	land := func(n string) bool { c, ok := reg.Lookup(n); return ok && c.Faces[0].IsLand() }
	any := func(string) bool { return true }
	cases := []struct {
		card string
		key  string
		// zone picks the setup list that must hold a fixture card other
		// than the card itself, and want the kind of card it must be.
		zone func(oraclegen.Seat) []string
		want func(string) bool
		what string
	}{
		{"Desperate Futurescribe", "trigger#0.0", func(s oraclegen.Seat) []string { return s.Battlefield }, creature, "another creature you control"},
		{"Glister Bairn", "trigger#0.0", func(s oraclegen.Seat) []string { return s.Battlefield }, creature, "another creature you control"},
		{"Toph, the First Metalbender", "trigger#0.0", func(s oraclegen.Seat) []string { return s.Battlefield }, land, "a land you control"},
		{"Carnivorous Cultivator", "trigger#0.0", func(s oraclegen.Seat) []string { return s.Graveyard }, any, "a card in a graveyard"},
	}
	for _, tc := range cases {
		t.Run(tc.card, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.card)
			}
			var req levelb.Requirement
			for _, r := range levelb.Requirements(c) {
				if r.Family == "trigger" && r.Key == tc.key {
					req = r
				}
			}
			if req.Key == "" {
				t.Fatalf("%s has no %s requirement", tc.card, tc.key)
			}
			f := c.Faces[0]
			idx := 0
			if req.Slot != "0" {
				t.Fatalf("test assumes trigger slot 0, got %q", req.Slot)
			}
			tr := &f.Triggers[idx]
			if len(oraclegen.AbilitySlotSpecs(f, tr.Effect)) == 0 {
				t.Fatalf("%s: effect has no target slot; the test is vacuous", tc.card)
			}
			// Precondition: with only the empty fixture the trigger never
			// reaches the stack, so the target fixture is what serves it.
			causes, why := triggerRecipe(reg, f, tc.card, tr, req.Sub)
			if why != "" || len(causes) == 0 {
				t.Fatalf("%s: no recipe (%q)", tc.card, why)
			}
			for _, cause := range causes {
				if _, _, fired := triggerWith(reg, f, tc.card, req, cause, []oraclegen.Fixture{{}}); fired {
					t.Fatalf("%s fires with no target fixture; the fixture is not what serves it", tc.card)
				}
			}
			item, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("%s not served: %s", tc.card, skip.Reason)
			}
			p0, ok := item.Scenario.Setup["p0"]
			if !ok || !containsString(p0.Battlefield, tc.card) {
				t.Fatalf("trigger source is not on p0's battlefield: %+v", item.Scenario.Setup)
			}
			found := false
			// These targets must be supplied on the controller's side, not
			// merely somewhere on the board (YouCtrl / YourGraveyard).
			for _, n := range tc.zone(p0) {
				if n != tc.card && tc.want(n) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s scenario holds no fixture for %s: %+v", tc.card, tc.what, item.Scenario.Setup)
			}
		})
	}
}
