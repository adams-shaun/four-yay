package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestZoneChangeTriggerFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, mode, want string
		params           map[string]string
	}{
		{"graveyard departure", "ChangesZone", "trigger.leaves-graveyard", map[string]string{"Origin": "Graveyard", "Destination": "Any", "ValidCard": "Creature.YouOwn"}},
		{"other leaves", "ChangesZone", "trigger.ltb-other", map[string]string{"Origin": "Battlefield", "Destination": "Hand", "ValidCard": "Creature.YouCtrl"}},
		{"exiled other", "ChangesZone", "trigger.ltb-other", map[string]string{"Origin": "Battlefield", "Destination": "Exile", "ValidCard": "Creature.YouCtrl"}},
		{"all enters plural filter", "ChangesZoneAll", "trigger.etb-other", map[string]string{"Origin": "Any", "Destination": "Battlefield", "ValidCards": "Creature.YouCtrl"}},
		{"self ltb remains gap", "ChangesZone", "trigger.gap:ChangesZone", map[string]string{"Origin": "Battlefield", "Destination": "Hand", "ValidCard": "Card.Self"}},
		{"dies delegated", "ChangesZone", "trigger.dies-other", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard", "ValidCard": "Creature.YouCtrl"}},
		{"all dies is dies-other", "ChangesZoneAll", "trigger.dies-other", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard", "ValidCards": "Creature.YouCtrl"}},
		{"other changes-zone-all shape is named", "ChangesZoneAll", "trigger.zone-change-residue", map[string]string{"Origin": "Library", "Destination": "Exile", "ValidCards": "Card.YouOwn"}},
		{"chosen-card residue is named", "ChangesZone", "trigger.zone-change-residue", map[string]string{"Origin": "Any", "Destination": "Graveyard", "ValidCard": "ChosenCardStrict"}},
		{"library residue is named", "ChangesZone", "trigger.zone-change-residue", map[string]string{"Origin": "Library", "Destination": "Graveyard", "ValidCard": "Land"}},
		{"any-to-graveyard residue is named", "ChangesZone", "trigger.zone-change-residue", map[string]string{"Origin": "Any", "Destination": "Graveyard", "ValidCard": "Card"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 {
				t.Fatalf("precondition: got %d requirements, want 1", len(got))
			}
			if got[0].Sub != tc.want {
				t.Fatalf("sub = %q, want %q", got[0].Sub, tc.want)
			}
		})
	}
}
