package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestCastFamilyClassification(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		params     map[string]string
		want       string
	}{
		{"own spell", "SpellCast", map[string]string{"ValidActivatingPlayer": "You"}, "trigger.spell-cast"},
		{"unqualified spell", "SpellCast", nil, "trigger.spell-cast"},
		{"opponent spell", "SpellCast", map[string]string{"ValidActivatingPlayer": "Opponent", "ValidCard": "Card.cmcLE2"}, "trigger.spell-cast-opponent"},
		{"opponent ownership condition remains a named skip", "SpellCast", map[string]string{"ValidActivatingPlayer": "Opponent", "ValidCard": "Card.YouDontOwn"}, "trigger.spell-cast-opponent"},
		{"self spell", "SpellCast", map[string]string{"ValidCard": "Card.Self", "Execute": "TrigDraw"}, "trigger.spell-cast-self-cast"},
		{"own crime", "CommitCrime", map[string]string{"ValidPlayer": "You"}, "trigger.commit-crime"},
		{"activated", "AbilityCast", map[string]string{"ValidActivatingPlayer": "You", "ValidSA": "Activated.Exhaust"}, "trigger.ability-activated"},
		{"nonmana spell-ability union", "AbilityCast", map[string]string{"ValidActivatingPlayer": "You", "ValidSA": "SpellAbility.!ManaAbility"}, "trigger.ability-activated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 || got[0].Sub != tc.want || got[0].Gap != "" || got[0].CoveredByA {
				t.Fatalf("classification = %+v, want one uncovered %s requirement", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mode   string
		params map[string]string
	}{
		{"level-A self cast", "SpellCast", map[string]string{"ValidCard": "Card.Self", "Execute": "TrigUntapAll"}},
		{"static cast", "SpellCast", map[string]string{"Static": "True", "ValidActivatingPlayer": "You"}},
		{"opponent crime", "CommitCrime", map[string]string{"ValidPlayer": "Opponent"}},
		{"loyalty stays event family", "AbilityCast", map[string]string{"ValidActivatingPlayer": "You", "ValidSA": "Activated.Loyalty"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 || got[0].Sub == "trigger.spell-cast-self-cast" || got[0].Sub == "trigger.commit-crime" || got[0].Sub == "trigger.ability-activated" {
				t.Fatalf("excluded shape incorrectly admitted: %+v", got)
			}
		})
	}
}
