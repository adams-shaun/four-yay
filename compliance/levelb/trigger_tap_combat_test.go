package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestTapCombatClassification(t *testing.T) {
	creature, artifact := []string{"Creature"}, []string{"Artifact", "Equipment"}
	for _, tc := range []struct {
		name   string
		types  []string
		mode   string
		params map[string]string
		want   string
	}{
		{"self taps", creature, "Taps", map[string]string{"ValidCard": "Card.Self"}, "trigger.tapped"},
		{"your creature taps", creature, "Taps", map[string]string{"ValidCard": "Creature.YouCtrl", "FirstTime": "True"}, "trigger.tapped"},
		{"bearer taps", artifact, "Taps", map[string]string{"ValidCard": "Creature.EquippedBy"}, "trigger.tapped"},
		{"you tap an opposing creature", creature, "Taps", map[string]string{"ValidCard": "Creature.OppCtrl", "ValidPlayer": "You"}, "trigger.tapped"},
		{"tap all", []string{"Enchantment"}, "TapAll", map[string]string{"ValidCards": "Creature.YouCtrl"}, "trigger.tapped"},
		{"bearer attacks", artifact, "Attacks", map[string]string{"ValidCard": "Card.EquippedBy"}, "trigger.attacks-attached"},
		{"self blocks", creature, "Blocks", map[string]string{"ValidCard": "Card.Self"}, "trigger.blocks"},
		{"your creature blocks", creature, "Blocks", map[string]string{"ValidCard": "Creature.YouCtrl"}, "trigger.blocks"},
		{"bearer blocks", []string{"Enchantment", "Aura"}, "Blocks", map[string]string{"ValidCard": "Card.AttachedBy"}, "trigger.blocks"},
		{"self blocks a flyer", creature, "AttackerBlocked", map[string]string{"ValidCard": "Creature.withFlying", "ValidBlocker": "Card.Self"}, "trigger.blocks"},
		{"self blocks a creature", creature, "AttackerBlockedByCreature", map[string]string{"ValidCard": "Creature", "ValidBlocker": "Card.Self"}, "trigger.blocks"},
		{"self becomes blocked", creature, "AttackerBlocked", map[string]string{"ValidCard": "Card.Self"}, "trigger.blocked"},
		{"your rat becomes blocked", creature, "AttackerBlocked", map[string]string{"ValidCard": "Rat.YouCtrl"}, "trigger.blocked"},
		{"self blocked by a creature", creature, "AttackerBlockedByCreature", map[string]string{"ValidCard": "Card.Self", "ValidBlocker": "Creature"}, "trigger.blocked"},
		{"attacked by an opponent", creature, "AttackersDeclared", map[string]string{"AttackedTarget": "You"}, "trigger.opponent-attacks"},
		{"attacked by an opponent with two", creature, "AttackersDeclared", map[string]string{"AttackedTarget": "You", "AttackingPlayer": "Opponent", "ValidAttackersAmount": "GE2"}, "trigger.opponent-attacks"},
		{"any player attacks with three", creature, "AttackersDeclared", map[string]string{"AttackingPlayer": "Player", "ValidAttackers": "Creature", "ValidAttackersAmount": "GE3"}, "trigger.attacks"},
		{"mana tap is a static mana ability", creature, "TapsForMana", map[string]string{"ValidCard": "Land", "Static": "True"}, TapsForManaSub},
		{"creature mana tap is a static mana ability", creature, "TapsForMana", map[string]string{"ValidCard": "Creature", "Static": "True"}, TapsForManaSub},
		{"artifact token mana tap has a token probe", creature, "TapsForMana", map[string]string{"ValidCard": "Artifact.token", "Static": "True"}, TapsForManaSub},
		{"you expend four", creature, "ManaExpend", map[string]string{"Amount": "4", "Player": "You"}, ManaExpendSub},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: tc.types, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 || got[0].Sub != tc.want || got[0].Gap != "" || got[0].CoveredByA {
				t.Fatalf("classification = %+v, want one uncovered %s requirement", got, tc.want)
			}
		})
	}
	// Shapes with no cause the recipes can build stay named gaps.
	for _, tc := range []struct {
		name   string
		types  []string
		mode   string
		params map[string]string
		want   string
	}{
		{"artifact token mana tap with a qualifier has no probe", creature, "TapsForMana", map[string]string{"ValidCard": "Artifact.token+withFlying", "Static": "True"}, "trigger.gap:TapsForMana"},
		{"artifact token mana tap producing G has no probe", creature, "TapsForMana", map[string]string{"ValidCard": "Artifact.token", "Produced": "G", "Static": "True"}, "trigger.gap:TapsForMana"},
		{"teamwork tap", creature, "Taps", map[string]string{"ValidCard": "Card.Self", "Teamwork": "True"}, "trigger.gap:Taps"},
		{"crewed vehicle taps", creature, "Taps", map[string]string{"ValidCard": "Card.CrewedBySource"}, "trigger.gap:Taps"},
		{"class level trigger", creature, "Taps", map[string]string{"ValidCard": "Card.Self", "ClassBand": "3"}, "trigger.gap:Taps"},
		{"a non-creature taps itself", artifact, "Taps", map[string]string{"ValidCard": "Card.Self"}, "trigger.gap:Taps"},
		{"a vehicle blocks", artifact, "Blocks", map[string]string{"ValidCard": "Card.Self"}, "trigger.gap:Blocks"},
		{"opposing land taps", creature, "Taps", map[string]string{"ValidCard": "Island.OppCtrl", "ValidPlayer": "You"}, "trigger.gap:Taps"},
		{"your artifact taps", creature, "Taps", map[string]string{"ValidCard": "Artifact.YouCtrl"}, "trigger.gap:Taps"},
		{"attacked by an opponent while monarch", creature, "AttackersDeclared", map[string]string{"AttackedTarget": "You", "AttackingPlayer": "Player.Opponent", "CheckDefinedPlayer": "You.isMonarch"}, "trigger.gap:AttackersDeclared"},
		{"a player attacks you or another player", creature, "AttackersDeclared", map[string]string{"AttackingPlayer": "Player", "AttackedTarget": "You"}, "trigger.gap:AttackersDeclared"},
		{"blocked by a restricted blocker", creature, "AttackerBlocked", map[string]string{"ValidCard": "Card.Self", "ValidBlocker": "Creature.withFlying"}, "trigger.gap:AttackerBlocked"},
	} {
		t.Run("gap "+tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: tc.types, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 || got[0].Sub != tc.want || got[0].Gap == "" {
				t.Fatalf("classification = %+v, want the gap %s", got, tc.want)
			}
		})
	}
}
