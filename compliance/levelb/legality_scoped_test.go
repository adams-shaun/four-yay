package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestScopedLegalityStaticShapes pins which bearer- and filter-scoped
// combat-legality statics are recognized and that every sibling carrying a
// parameter, qualifier or mode the template does not read stays the named
// "legality static" gap.
func TestScopedLegalityStaticShapes(t *testing.T) {
	classify := func(types []string, st cards.Static) (string, string) {
		f := &cards.Face{Types: types, Statics: []cards.Static{st}}
		return classifyStatic(f, &f.Statics[0])
	}
	creature, equipment, aura, land := []string{"Creature"}, []string{"Artifact", "Equipment"}, []string{"Enchantment", "Aura"}, []string{"Land"}
	for _, tc := range []struct {
		name  string
		types []string
		st    cards.Static
		sub   string // "" = the legality static gap
	}{
		{"equipped can't be blocked", equipment, stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.EquippedBy", "Secondary": "True"}), "static.cant-block-by-bearer"},
		{"enchanted can't be blocked", aura, stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.EnchantedBy"}), "static.cant-block-by-bearer"},
		{"bearer CantBlockBy with an unread gate", equipment, stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.EquippedBy", "IsPresent": "Card.Other"}), ""},
		{"equipped Max 1", equipment, stat("MinMaxBlocker", map[string]string{"ValidCard": "Creature.EquippedBy", "Max": "1", "Secondary": "True"}), "static.max-blockers-bearer"},
		{"enchanted Max 1", aura, stat("MinMaxBlocker", map[string]string{"ValidCard": "Creature.EnchantedBy", "Max": "1"}), "static.max-blockers-bearer"},
		{"bearer Max with a non-literal bound", equipment, stat("MinMaxBlocker", map[string]string{"ValidCard": "Creature.EquippedBy", "Max": "X"}), ""},
		{"bearer Max with an unread Min", equipment, stat("MinMaxBlocker", map[string]string{"ValidCard": "Creature.EquippedBy", "Max": "1", "Min": "1"}), ""},
		{"Max over a power filter", creature, stat("MinMaxBlocker", map[string]string{"ValidCard": "Creature.YouCtrl+powerGE4", "Max": "1"}), "static.max-blockers-filter"},
		{"Max over a counters filter", creature, stat("MinMaxBlocker", map[string]string{"ValidCard": "Creature.YouCtrl+HasCounters", "Max": "1"}), "static.max-blockers-filter"},
		{"Max over a type filter", creature, stat("MinMaxBlocker", map[string]string{"ValidCard": "Boar.YouCtrl", "Max": "1"}), "static.max-blockers-filter"},
		{"Max over an unmodelled qualifier", creature, stat("MinMaxBlocker", map[string]string{"ValidCard": "Creature.YouCtrl+cmcGE4", "Max": "1"}), ""},
		{"Max self stays the self family", creature, stat("MinMaxBlocker", map[string]string{"ValidCard": "Card.Self", "Max": "1"}), "static.max-blockers"},
		{"attacker and blocker filters", creature, stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.YouCtrl+powerLE2", "ValidBlocker": "Creature.powerGE3"}), "static.cant-block-by-filter"},
		{"type attacker, keyword blocker", equipment, stat("CantBlockBy", map[string]string{"ValidAttacker": "Spider.YouCtrl", "ValidBlocker": "Creature.withDefender", "Secondary": "True"}), "static.cant-block-by-filter"},
		{"self attacker, token blocker", creature, stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.Self", "ValidBlocker": "Permanent.token"}), "static.cant-block-by-filter"},
		{"bearer blocker list", aura, stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.Detective", "ValidBlocker": "Creature.EnchantedBy+nonDetective,Creature.EnchantedBy+YouDontCtrl", "Secondary": "True"}), "static.cant-block-by-filter"},
		{"blocker filter with an unmodelled qualifier", creature, stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.YouCtrl", "ValidBlocker": "Creature.tapped"}), ""},
		{"filter with an unread parameter", creature, stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.YouCtrl", "ValidBlocker": "Creature.withFlying", "Condition": "Threshold"}), ""},
		{"land can't be blocked", land, stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.Self"}), "static.cant-block-by-animated-self"},
		{"flier can't attack you", creature, stat("CantAttack", map[string]string{"ValidCard": "Creature.withFlying", "Target": "You"}), "static.cant-attack-filter"},
		{"enchanted can't attack you or walkers", creature, stat("CantAttack", map[string]string{"ValidCard": "Creature.EnchantedBy Aura.YouCtrl", "Target": "You,Planeswalker.YouCtrl"}), "static.cant-attack-bearer"},
		{"CantAttack with an unread target", creature, stat("CantAttack", map[string]string{"ValidCard": "Creature.withFlying", "Target": "Player.Opponent"}), ""},
		{"planeswalker can't be attacked", []string{"Legendary", "Artifact", "Planeswalker", "Equipment"}, stat("CantAttack", map[string]string{"ValidCard": "Creature", "Target": "Card.Self+AttachedTo Creature"}), "static.cant-be-attacked-attached"},
	} {
		sub, gap := classify(tc.types, tc.st)
		if tc.sub != "" && (sub != tc.sub || gap != "") {
			t.Errorf("%s: classified %q gap %q, want %q served", tc.name, sub, gap, tc.sub)
		}
		if tc.sub == "" && (sub != "static.combat" || gap != "legality static") {
			t.Errorf("%s: classified %q gap %q, want the legality static gap", tc.name, sub, gap)
		}
	}
	// A Vehicle's own gated "can't be blocked" (a face that is no creature
	// until crewed) is the crewed shape; the same line on a plain artifact
	// is not.
	gated := stat("CantBlockBy", map[string]string{"ValidAttacker": "Card.Self", "IsPresent": "Permanent.YouOwn", "PresentZone": "Graveyard", "PresentCompare": "GE8"})
	if sub, gap := classify([]string{"Artifact", "Vehicle"}, gated); sub != "static.cant-block-by-crewed" || gap != "" {
		t.Errorf("Vehicle gated unblockable: %q gap %q, want static.cant-block-by-crewed", sub, gap)
	}
	if sub, gap := classify([]string{"Artifact"}, gated); sub != "static.combat" || gap != "legality static" {
		t.Errorf("non-Vehicle artifact gated unblockable: %q gap %q, want the legality static gap", sub, gap)
	}
}
