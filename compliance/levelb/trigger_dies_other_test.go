package levelb_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestDiesOtherRealCardSubFamilies: every non-self Battlefield->Graveyard
// trigger is dies-other, whichever side, type or qualifier its filter names,
// and ChangesZoneAll reads its ValidCards$ filter. Each row asserts the
// trigger really is that zone change (a vacuous row fails loudly) and that
// the requirement carries no gap.
func TestDiesOtherRealCardSubFamilies(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key string
		mode      cards.TriggerMode
	}{
		{"Massacre Wurm", "trigger#0.1", cards.TriggerChangesZone},       // Creature.OppCtrl
		{"Vein Ripper", "trigger#0.0", cards.TriggerChangesZone},         // Creature
		{"Ashiok's Reaper", "trigger#0.0", cards.TriggerChangesZone},     // Enchantment.YouCtrl
		{"Great Fierce Bee", "trigger#0.0", cards.TriggerChangesZoneAll}, // Creature.Other
		{"Chainsaw", "trigger#0.1", cards.TriggerChangesZoneAll},         // Creature
		{"Lead Pipe", "trigger#0.0", cards.TriggerChangesZone},           // Card.EquippedBy
		{"Boggart Cursecrafter", "trigger#0.0", cards.TriggerChangesZone},
		{"Krenko, Baron of Tin Street", "trigger#0.0", cards.TriggerChangesZone}, // Artifact
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s is not in the corpus", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				slot, err := strconv.Atoi(r.Slot)
				if err != nil {
					t.Fatalf("precondition: slot %q: %v", r.Slot, err)
				}
				tr := &c.Faces[r.Face].Triggers[slot]
				if tr.ModeKind() != tc.mode || !strings.EqualFold(tr.ParamStr(cards.PKOrigin), "Battlefield") ||
					!strings.EqualFold(tr.ParamStr(cards.PKDestination), "Graveyard") || levelb.ZoneChangeFilter(tr) == "" {
					t.Fatalf("precondition: %s %s is not a Battlefield->Graveyard %v trigger with a filter: %v", tc.name, tc.key, tc.mode, tr.Params)
				}
				if r.Sub != "trigger.dies-other" || r.Gap != "" {
					t.Fatalf("%s %s: sub=%q gap=%q, want trigger.dies-other with no gap", tc.name, tc.key, r.Sub, r.Gap)
				}
				return
			}
			t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
		})
	}
}
