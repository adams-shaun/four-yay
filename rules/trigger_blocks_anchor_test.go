package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestTriggerAnchoredAtBlockerAnchorSpellings pins the source-anchor spellings
// triggerAnchoredAtBlocker recognizes. The blocker half of Forge's Mode$
// AttackerBlockedByCreature is the half whose ValidBlocker$ carries the source
// anchor; the Effect-delivered spelling Card.EffectSource aliases Card.Self
// (effects/filter.go), and omitting it classified Goblin Flotilla's granted
// "ValidBlocker$ Card.EffectSource" line as attacker-side, capturing the wrong
// referent. This is a direct unit test of the helper: the granted path (DB$
// Effect + Triggers$) does not fire in the engine yet, so the spec read is
// asserted on its own rather than through a live trigger.
func TestTriggerAnchoredAtBlockerAnchorSpellings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		validCard   string
		validBlk    string
		wantBlocker bool
	}{
		{"Card.Self blocker half", "Creature", "Card.Self", true},
		{"Card.EffectSource blocker half", "Creature", "Card.EffectSource", true},
		{"attachment blocker half", "Creature", "Card.AttachedBy", true},
		{"Card.Self attacker half", "Card.Self", "Creature", false},
		{"Card.EffectSource attacker half", "Card.EffectSource", "Creature", false},
		{"no anchor", "Creature", "Creature", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := cards.Trigger{Params: map[string]string{
				"ValidCard":    tc.validCard,
				"ValidBlocker": tc.validBlk,
			}}
			if got := triggerAnchoredAtBlocker(tr); got != tc.wantBlocker {
				t.Fatalf("triggerAnchoredAtBlocker(ValidCard=%q ValidBlocker=%q) = %v, want %v",
					tc.validCard, tc.validBlk, got, tc.wantBlocker)
			}
		})
	}
}
