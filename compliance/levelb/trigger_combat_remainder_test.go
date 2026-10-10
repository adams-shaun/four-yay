package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestCombatRemainderClassification pins the combat/keyword remainder shapes
// the new recipes serve, and the near-misses that must stay gaps. Every row
// is one trigger on one face, so a reclassification shows as a changed Sub
// with the requirement count still 1.
func TestCombatRemainderClassification(t *testing.T) {
	creature := []string{"Creature"}
	equipment := []string{"Artifact", "Equipment"}
	vehicle := []string{"Artifact", "Vehicle"}
	for _, tc := range []struct {
		name, mode string
		types      []string
		params     map[string]string
		want       string
	}{
		{"self attaches to a creature", "Attached", equipment, map[string]string{"ValidSource": "Card.Self", "ValidTarget": "Creature"}, "trigger.attached"},
		{"an Aura you control attaches", "Attached", creature, map[string]string{"ValidSource": "Aura.YouCtrl", "ValidTarget": "Creature.YouCtrl"}, "trigger.attached"},
		{"an Aura attaches to the source", "Attached", creature, map[string]string{"ValidSource": "Aura", "ValidTarget": "Card.Self"}, "trigger.attached"},
		{"you untap during your untap step", "UntapAll", creature, map[string]string{"ValidPlayer": "You", "Phase": "Untap", "ValidCards": "Permanent"}, "trigger.untap-all"},
		{"excess noncombat damage to opponents", "ExcessDamageAll", creature, map[string]string{"ValidTarget": "Creature.OppCtrl", "CombatDamage": "False"}, "trigger.excess-damage"},
		{"opponent attacks with two", "AttackersDeclared", creature, map[string]string{"AttackedTarget": "You", "AttackingPlayer": "Opponent", "ValidAttackersAmount": "GE2"}, "trigger.opponent-attacks"},
		{"your opponent is attacked", "AttackersDeclared", creature, map[string]string{"AttackedTarget": "Opponent", "ValidAttackers": "Creature"}, "trigger.attacks-opponent"},
		{"a Vehicle blocks", "Blocks", vehicle, map[string]string{"ValidCard": "Card.Self"}, "trigger.blocks-vehicle"},
		{"opponent loyalty activation", "AbilityCast", creature, map[string]string{"ValidSA": "Activated.Loyalty+OppCtrl"}, "trigger.ability-activated-opponent"},
		{"an attacker's own trigger fires", "AbilityTriggered", creature, map[string]string{"TriggeredOwnAbility": "True", "ValidMode": "Attacks,AttackersDeclared", "ValidSource": "Creature.YouCtrl"}, "trigger.ability-triggered"},
		{"you solve a Case", "CaseSolved", creature, map[string]string{"ValidPlayer": "You", "ValidCard": "Case"}, "trigger.case-solved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: tc.types, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 || got[0].Sub != tc.want || got[0].Gap != "" || got[0].CoveredByA {
				t.Fatalf("classification = %+v, want one uncovered %s requirement", got, tc.want)
			}
		})
	}
	// Tomik, Wielder of Law's attacker-count gate is a CheckSVar$ whose body
	// names the attacking-you count; the recipe supplies the SVarCompare$
	// floor's attackers.
	t.Run("opponent attacks with a CheckSVar count", func(t *testing.T) {
		f := &cards.Face{
			Types: []string{"Creature"},
			SVars: map[string]string{"X": "Count$ValidAll Creature.attackingYouOrYourPWLKI"},
			Triggers: []cards.Trigger{trig("AttackersDeclared", map[string]string{
				"AttackingPlayer": "Player.Opponent", "CheckSVar": "X", "SVarCompare": "GE2",
			})},
		}
		got := Requirements(cardOf(f))
		if len(got) != 1 || got[0].Sub != "trigger.opponent-attacks" || got[0].Gap != "" {
			t.Fatalf("classification = %+v, want one uncovered trigger.opponent-attacks requirement", got)
		}
	})
	// An attached trigger whose effect targets TriggeredTarget (Blade of
	// Shared Souls) is servable now that the filter predicate resolves
	// against the trigger context the push-time ask binds.
	t.Run("an attached effect targeting TriggeredTarget", func(t *testing.T) {
		f := &cards.Face{
			Types: []string{"Artifact", "Equipment"},
			SVars: map[string]string{"TrigCopy": "DB$ Clone | ValidTgts$ Creature.YouCtrl+!TriggeredTarget"},
			Triggers: []cards.Trigger{trig("Attached", map[string]string{
				"ValidSource": "Card.Self", "ValidTarget": "Creature", "Execute": "TrigCopy",
			})},
		}
		got := Requirements(cardOf(f))
		if len(got) != 1 || got[0].Sub != "trigger.attached" || got[0].Gap != "" {
			t.Fatalf("classification = %+v, want one uncovered trigger.attached requirement", got)
		}
	})
	for _, tc := range []struct {
		name, mode string
		types      []string
		params     map[string]string
		want       string
	}{
		{"an Aura with no bearer filter (Eriette)", "Attached", creature, map[string]string{"ValidSource": "Aura.YouCtrl"}, "trigger.gap:Attached"},
		{"a Static attached line", "Attached", equipment, map[string]string{"ValidSource": "Card.Self", "ValidTarget": "Creature", "Static": "True"}, "trigger.gap:Attached"},
		{"an opponent-active cast", "SpellCast", creature, map[string]string{"ValidActivatingPlayer": "Player.Opponent+Active", "ValidCard": "Card"}, "trigger.gap:SpellCast"},
		{"an opponent attacks another opponent", "AttackersDeclared", creature, map[string]string{"AttackingPlayer": "Player.Opponent", "AttackedTarget": "Opponent"}, "trigger.gap:AttackersDeclared"},
		{"combat excess damage", "ExcessDamageAll", creature, map[string]string{"ValidTarget": "Creature.OppCtrl", "CombatDamage": "True"}, "trigger.gap:ExcessDamageAll"},
		{"a non-Vehicle blocks itself", "Blocks", []string{"Artifact"}, map[string]string{"ValidCard": "Card.Self"}, "trigger.gap:Blocks"},
		{"an opponent's untap step", "UntapAll", creature, map[string]string{"ValidPlayer": "Opponent", "Phase": "Untap", "ValidCards": "Permanent"}, "trigger.gap:UntapAll"},
		{"an ability-triggered line with a destination", "AbilityTriggered", creature, map[string]string{"TriggeredOwnAbility": "True", "ValidMode": "Attacks", "ValidSource": "Creature.YouCtrl", "ValidDestination": "Graveyard"}, "trigger.gap:AbilityTriggered"},
		{"an opponent solves a Case", "CaseSolved", creature, map[string]string{"ValidPlayer": "Opponent", "ValidCard": "Case"}, "trigger.gap:CaseSolved"},
	} {
		t.Run("gap "+tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: tc.types, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 || got[0].Sub != tc.want || got[0].Gap == "" {
				t.Fatalf("classification = %+v, want the gap %s", got, tc.want)
			}
		})
	}
}
