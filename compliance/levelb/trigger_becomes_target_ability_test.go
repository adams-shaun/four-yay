package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestBecomesTargetAbilityShapeStaysGap pins the decision that "a player or
// permanent becomes the target of an ability you control" (Loki, God of
// Mischief) stays a named gap: the engine's becomesTargetMatches reads
// ValidSource$ through the ordinary filter grammar, which has no Ability
// base, so an Ability.YouCtrl source fails closed and the trigger can never
// fire. The recipe cause (a probe's targeted {T} ability at p1) is ready;
// serving the shape first needs the engine-side grammar. The near-misses the
// gap also covers are pinned so a later grammar widening reclassifies loudly.
func TestBecomesTargetAbilityShapeStaysGap(t *testing.T) {
	creature := []string{"Creature"}
	for _, tc := range []struct {
		name, want string
		params     map[string]string
	}{
		{"an ability you control targets a player or permanent", "trigger.gap:BecomesTarget", map[string]string{"ValidSource": "Ability.YouCtrl", "ValidTarget": "Player,Permanent"}},
		{"an ability an opponent controls", "trigger.gap:BecomesTarget", map[string]string{"ValidSource": "Ability.OppCtrl", "ValidTarget": "Player,Permanent"}},
		{"a backup ability source", "trigger.gap:BecomesTarget", map[string]string{"ValidSource": "Ability.Backup", "ValidTarget": "Creature.inZoneBattlefield+YouCtrl"}},
		{"a land ability source", "trigger.gap:BecomesTarget", map[string]string{"ValidSource": "Ability.Land+YouCtrl", "ValidTarget": "Creature.Other"}},
		{"a spell you control", "trigger.gap:BecomesTarget", map[string]string{"ValidSource": "Spell.YouCtrl", "ValidTarget": "Player,Permanent"}},
		{"a plain SpellAbility.OppCtrl source stays served", "trigger.becomes-target", map[string]string{"ValidSource": "SpellAbility.OppCtrl", "ValidTarget": "Player,Permanent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: creature, Triggers: []cards.Trigger{trig("BecomesTarget", tc.params)}}))
			gap := got[0].Gap != ""
			wantGap := tc.want != "trigger.becomes-target"
			if len(got) != 1 || got[0].Sub != tc.want || gap != wantGap || got[0].CoveredByA {
				t.Fatalf("classification = %+v, want %s (gap=%v)", got, tc.want, wantGap)
			}
		})
	}
}
