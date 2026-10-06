package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestTriggerSpellCastProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, probe string
		steps       int
	}{
		{"Cosmogrand Zenith", "Shock", 2},
		{"Gnarlback Rhino", growthProbe, 1},
		{"Spinerock Tyrant", "Shock", 1},
		{"Helga, Skittish Seer", "Air Elemental", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 || len(card.Faces[0].Triggers) == 0 {
				t.Fatalf("precondition: %s trigger is unavailable", tc.name)
			}
			trigger := &card.Faces[0].Triggers[0]
			causes, why := triggerRecipe(reg, card.Faces[0], tc.name, trigger, "trigger.spell-cast")
			if why != "" {
				t.Fatalf("spell-cast recipe returned %q", why)
			}
			if len(causes) == 0 {
				t.Fatal("precondition: spell-cast handler produced no candidate")
			}
			cause := causes[0]
			if len(cause.steps) != tc.steps || len(cause.hand) != tc.steps {
				t.Fatalf("cause has %d steps and %d hand probes; want %d", len(cause.steps), len(cause.hand), tc.steps)
			}
			for i, step := range cause.steps {
				want := "p0:" + tc.probe
				if i == 1 {
					want += "#2"
				}
				if step.Op != "cast" || step.Card != want {
					t.Fatalf("step %d = %+v, want cast %s", i, step, want)
				}
				if tc.name == "Spinerock Tyrant" && len(step.Targets) != 1 {
					t.Fatalf("single-target filter probe has targets %v", step.Targets)
				}
			}
		})
	}
}
