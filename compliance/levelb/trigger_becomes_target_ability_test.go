package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestBecomesTargetAbilityShapeIsServed pins the classification of "a player
// or permanent becomes the target of an ability you control" (Loki, God of
// Mischief): the engine's becomesTargetSourceMatches answers the Ability base
// (an ability-shape predicate the object-filter grammar has no base word for)
// with the YouCtrl controller compare, and the targeted-ability cause (a
// probe's {T} ability at p1) fires it, so the shape is served as
// trigger.becomes-target-ability. The near-misses the served shape excludes --
// an OppCtrl ability (no opponent-side activation cause), Backup's keyword
// provenance, a host-card filter (Ability.Land+namedX), a value comparison
// (Ability.numTargets EQ1), and a spell source (the becomes-target recipes
// only cast at the card itself) -- are pinned so a later grammar widening
// reclassifies loudly. A plain SpellAbility.OppCtrl source stays served by the
// ordinary spell path.
func TestBecomesTargetAbilityShapeIsServed(t *testing.T) {
	creature := []string{"Creature"}
	for _, tc := range []struct {
		name, want string
		params     map[string]string
	}{
		{"an ability you control targets a player or permanent", "trigger.becomes-target-ability", map[string]string{"ValidSource": "Ability.YouCtrl", "ValidTarget": "Player,Permanent"}},
		{"an ability an opponent controls", "trigger.gap:BecomesTarget", map[string]string{"ValidSource": "Ability.OppCtrl", "ValidTarget": "Player,Permanent"}},
		{"a backup ability source", "trigger.gap:BecomesTarget", map[string]string{"ValidSource": "Ability.Backup", "ValidTarget": "Creature.inZoneBattlefield+YouCtrl"}},
		{"a land ability source", "trigger.gap:BecomesTarget", map[string]string{"ValidSource": "Ability.Land+YouCtrl", "ValidTarget": "Creature.Other"}},
		{"a spell you control", "trigger.gap:BecomesTarget", map[string]string{"ValidSource": "Spell.YouCtrl", "ValidTarget": "Player,Permanent"}},
		{"a plain SpellAbility.OppCtrl source stays served", "trigger.becomes-target", map[string]string{"ValidSource": "SpellAbility.OppCtrl", "ValidTarget": "Player,Permanent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: creature, Triggers: []cards.Trigger{trig("BecomesTarget", tc.params)}}))
			gap := got[0].Gap != ""
			wantGap := tc.want != "trigger.becomes-target" && tc.want != "trigger.becomes-target-ability"
			if len(got) != 1 || got[0].Sub != tc.want || gap != wantGap || got[0].CoveredByA {
				t.Fatalf("classification = %+v, want %s (gap=%v)", got, tc.want, wantGap)
			}
		})
	}
}
