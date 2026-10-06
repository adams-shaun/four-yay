package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestTriggerETBProbeServesRequestedSlot: a card with two etb-other triggers
// is served, per row, by the probe its own trigger accepts. Kolodin, Triumph
// Caster has a Mount ETB (slot 0) and a Vehicle ETB (slot 1); a Smuggler's
// Copter (a Vehicle) fires only the second and must not serve the first.
func TestTriggerETBProbeServesRequestedSlot(t *testing.T) {
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup("Kolodin, Triumph Caster")
	if !ok {
		t.Fatal("Kolodin, Triumph Caster not in the corpus")
	}
	for _, tc := range []struct{ slot, typeWord string }{{"0", "Mount"}, {"1", "Vehicle"}} {
		t.Run(tc.typeWord, func(t *testing.T) {
			idx, _ := strconv.Atoi(tc.slot)
			if f := c.Faces[0].Triggers[idx].ParamStr(cards.PKValidCard); !strings.Contains(f, tc.typeWord) {
				t.Fatalf("precondition: slot %s ValidCard = %q, want %s", tc.slot, f, tc.typeWord)
			}
			it := triggerRequirement(t, reg, "Kolodin, Triumph Caster", "trigger#0."+tc.slot, "trigger.etb-other")
			probe := strings.TrimPrefix(it.Scenario.Steps[0].Card, "p0:")
			pc, ok := reg.Lookup(probe)
			if !ok || !strings.Contains(strings.Join(pc.Faces[0].Types, " "), tc.typeWord) {
				t.Fatalf("%s row served by %q, which is not a %s", tc.typeWord, probe, tc.typeWord)
			}
		})
	}
}

// TestETBUnservableFilter: only a filter every alternative of which is
// unservable by a probe is named as such.
func TestETBUnservableFilter(t *testing.T) {
	for _, tc := range []struct{ filter, want string }{
		{"Creature.token+YouCtrl", "token"},
		{"Creature.!token+YouCtrl", ""},
		{"Creature.YouCtrl,Land.OppCtrl", ""},
		{"Creature.OppCtrl,Land.OppCtrl", "OppCtrl"},
		{"Creature.faceDown+YouCtrl", "faceDown"},
	} {
		if got := etbUnservableFilter(tc.filter); got != tc.want {
			t.Errorf("etbUnservableFilter(%q) = %q, want %q", tc.filter, got, tc.want)
		}
	}
}
