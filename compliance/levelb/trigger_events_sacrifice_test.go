package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestSacrificeTriggerSubFamilies classifies the sacrifice-filter shapes the
// recipe serves and the near-misses that must stay mode gaps. Every row is one
// trigger on a creature face, so a reclassification shows as a changed Sub with
// the requirement count still 1.
func TestSacrificeTriggerSubFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		params     map[string]string
		wantSub    string
		wantGap    bool
	}{
		{"creature other", "Sacrificed", map[string]string{"ValidCard": "Creature.Other", "ValidPlayer": "You"}, SacrificeSub, false},
		{"permanent other", "Sacrificed", map[string]string{"ValidCard": "Permanent.Other", "ValidPlayer": "You"}, SacrificeSub, false},
		{"artifact", "Sacrificed", map[string]string{"ValidCard": "Artifact", "ValidPlayer": "You"}, SacrificeSub, false},
		{"opponent artifact", "Sacrificed", map[string]string{"ValidCard": "Artifact.OppCtrl"}, SacrificeSub, false},
		{"self", "Sacrificed", map[string]string{"ValidCard": "Card.Self", "ValidPlayer": "You"}, SacrificeSub, false},
		{"self or another artifact", "Sacrificed", map[string]string{"ValidCard": "Card.Self,Artifact.Other", "ValidPlayer": "You"}, SacrificeSub, false},
		{"creature or artifact other", "Sacrificed", map[string]string{"ValidCard": "Creature.Other,Artifact.Other", "ValidPlayer": "You"}, SacrificeSub, false},
		{"player-wide creature", "Sacrificed", map[string]string{"ValidCard": "Creature.Other", "ValidPlayer": "Player"}, SacrificeSub, false},
		{"food token", "SacrificedOnce", map[string]string{"ValidCard": "Food", "ValidPlayer": "You"}, SacrificeSub, false},
		{"clue token", "Sacrificed", map[string]string{"ValidCard": "Clue.YouCtrl"}, SacrificeSub, false},
		{"any permanent token", "Sacrificed", map[string]string{"ValidCard": "Permanent.token+YouCtrl", "ValidPlayer": "You"}, SacrificeSub, false},
		{"enchantment stays a gap", "Sacrificed", map[string]string{"ValidCard": "Enchantment", "ValidPlayer": "You"}, "trigger.gap:Sacrificed", true},
		{"land stays a gap", "Sacrificed", map[string]string{"ValidCard": "Land", "ValidPlayer": "You"}, "trigger.gap:Sacrificed", true},
		{"opponent sacrifice stays a gap", "Sacrificed", map[string]string{"ValidCard": "Artifact", "ValidPlayer": "Opponent"}, "trigger.gap:Sacrificed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 {
				t.Fatalf("got %d requirements, want exactly 1 (a reclassification never adds or drops one): %+v", len(got), got)
			}
			r := got[0]
			if r.Sub != tc.wantSub || (r.Gap != "") != tc.wantGap {
				t.Fatalf("got Sub=%q Gap=%q, want Sub=%q gap=%v", r.Sub, r.Gap, tc.wantSub, tc.wantGap)
			}
		})
	}
}
