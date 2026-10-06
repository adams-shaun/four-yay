package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestZoneChangeTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Spirit Mascot", "trigger#0.0", "trigger.leaves-graveyard"},
		{"Ninja Teen", "trigger#0.0", "trigger.ltb-other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			if len(it.Scenario.Steps) == 0 {
				t.Fatal("precondition: generated scenario has no cause step")
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerProbeSlotOnStack(t, reg, it.Scenario, tc.name, "0") {
				t.Fatalf("precondition: %s trigger slot 0 never reaches the stack", tc.name)
			}
			c, found := reg.Lookup(tc.name)
			if !found {
				t.Fatalf("precondition: %s absent from corpus", tc.name)
			}
			for _, req := range levelb.Requirements(c) {
				if req.Key == tc.key && req.Sub != tc.sub {
					t.Fatalf("precondition: %s classified %s, want %s", tc.name, req.Sub, tc.sub)
				}
			}
		})
	}
}

func TestZoneChangeResiduesHaveNamedSkips(t *testing.T) {
	for _, tc := range []struct {
		filter, origin, destination, want string
	}{
		{"ChosenCardStrict", "Any", "Graveyard", "zone-change filter ChosenCardStrict"},
		{"Land", "Library", "Graveyard", "library to graveyard"},
		{"Card", "Any", "Graveyard", "put into graveyard from anywhere"},
	} {
		got := zoneChangeSkip(tc.filter, &cards.Trigger{Params: map[string]string{"Origin": tc.origin, "Destination": tc.destination}})
		if got != tc.want {
			t.Errorf("skip(%q, %q -> %q) = %q, want %q", tc.filter, tc.origin, tc.destination, got, tc.want)
		}
	}
}

func TestTokenChangesZoneETBProbeIsOffered(t *testing.T) {
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup("Woodland Champion")
	if !ok || len(c.Faces) == 0 || len(c.Faces[0].Triggers) == 0 {
		t.Fatal("precondition: Woodland Champion token-entry trigger is absent")
	}
	causes, why := etbProbeCauses(reg, c.Faces[0].Name, &c.Faces[0].Triggers[0])
	if why != "" {
		t.Fatalf("token entry probe selection failed: %s", why)
	}
	for _, cause := range causes {
		for _, step := range cause.steps {
			if step.Op == "cast" && step.Card == "p0:Raise the Alarm" {
				return
			}
		}
	}
	t.Fatal("handler ran but did not offer Raise the Alarm for a token-entry filter")
}
