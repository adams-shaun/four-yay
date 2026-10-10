package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestOpponentCauseClassification pins the opponent-side event shapes the
// opponent-cause recipes serve, and the near-misses that must stay gaps. The
// classifier is a pure function of the IR, so every row is one trigger on one
// face and a reclassification shows as a changed Sub.
func TestOpponentCauseClassification(t *testing.T) {
	artifact := []string{"Artifact"}
	for _, tc := range []struct {
		name, mode string
		types      []string
		params     map[string]string
		want       string
	}{
		{"an opponent searches their own library", "SearchedLibrary", []string{"Creature"},
			map[string]string{"ValidPlayer": "Player.Opponent", "SearchOwnLibrary": "True"}, "trigger.searched-library"},
		{"a player searches", "SearchedLibrary", []string{"Creature"},
			map[string]string{"ValidPlayer": "Player", "SearchOwnLibrary": "True"}, "trigger.searched-library"},
		{"an opponent discards a land", "Discarded", []string{"Creature"},
			map[string]string{"ValidCard": "Land.OppOwn"}, "trigger.discarded-opponent"},
		{"an opponent discards anything", "DiscardedAll", []string{"Creature"},
			map[string]string{"ValidCard": "Card.OppOwn"}, "trigger.discarded-opponent"},
		{"an opponent discards, player-wide", "Discarded", []string{"Creature"},
			map[string]string{"ValidCard": "Creature.OppCtrl", "ValidPlayer": "Opponent"}, "trigger.discarded-opponent"},
		{"an opponent gains control of a p0 permanent", "ChangesController", []string{"Creature"},
			map[string]string{"ValidCard": "Card.OppCtrl", "ValidOriginalController": "You"}, "trigger.changes-controller"},
		{"any opponent gain of control, no original filter", "ChangesController", []string{"Creature"},
			map[string]string{"ValidCard": "Permanent.OppCtrl"}, "trigger.changes-controller"},
		{"opponent activates an artifact ability", "AbilityCast", artifact,
			map[string]string{"ValidCard": "Artifact.inZoneBattlefield", "ValidSA": "Activated.OppCtrl", "ValidSAonCard": "Activated.YouCtrl"},
			"trigger.ability-activated-opponent"},
		{"exiled while activating a craft ability", "Exiled", []string{"Artifact", "Creature"},
			map[string]string{"Origin": "Battlefield", "ValidCard": "Card.Self", "WhileKeyword": "Ability.Craft"},
			"trigger.exiled-craft"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: tc.types, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 || got[0].Sub != tc.want || got[0].Gap != "" || got[0].CoveredByA {
				t.Fatalf("classification = %+v, want one uncovered %s requirement", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name, mode string
		types      []string
		params     map[string]string
		want       string
	}{
		{"another player's library", "SearchedLibrary", []string{"Creature"},
			map[string]string{"ValidPlayer": "Player.Other", "SearchOwnLibrary": "True"}, "trigger.gap:SearchedLibrary"},
		{"a search without the own-library gate", "SearchedLibrary", []string{"Creature"},
			map[string]string{"ValidPlayer": "Opponent"}, "trigger.gap:SearchedLibrary"},
		{"a narrowed search card", "SearchedLibrary", []string{"Creature"},
			map[string]string{"ValidPlayer": "Opponent", "SearchOwnLibrary": "True", "ValidCard": "Card.OppOwn"},
			"trigger.gap:SearchedLibrary"},
		{"a you-gated opponent-owned discard", "Discarded", []string{"Creature"},
			map[string]string{"ValidCard": "Card.OppOwn", "ValidPlayer": "You"}, "trigger.gap:Discarded"},
		{"losing control of itself", "ChangesController", []string{"Creature"},
			map[string]string{"ValidCard": "Card.Self"}, "trigger.gap:ChangesController"},
		{"gaining control from another player", "ChangesController", []string{"Creature"},
			map[string]string{"ValidCard": "Card.Self", "ValidOriginalController": "Player.Other"},
			"trigger.gap:ChangesController"},
		{"exiled without the craft keyword", "Exiled", []string{"Creature"},
			map[string]string{"Origin": "Battlefield", "ValidCard": "Card.Self"}, "trigger.gap:Exiled"},
		{"an opponent's craft exile of another card", "Exiled", []string{"Creature"},
			map[string]string{"Origin": "Battlefield", "ValidCard": "Creature.Other", "WhileKeyword": "Ability.Craft"},
			"trigger.gap:Exiled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: tc.types, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 || got[0].Sub != tc.want || got[0].Gap == "" {
				t.Fatalf("classification = %+v, want the gap %s", got, tc.want)
			}
		})
	}
}
