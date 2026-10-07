package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestPhaseOtherClassification pins the Phase$ / ValidPlayer$ pairs the
// phase-other classifier admits, and that the pairs the original TriggerPhase
// case already serves (Main1/You, player-wide Upkeep/Draw) and every out-of-
// scope pair stay where they are.
func TestPhaseOtherClassification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]string
	}{
		{"second main phase for you", map[string]string{"Phase": "Main", "PhaseCount": "2", "ValidPlayer": "You"}},
		{"your end step if you descended", map[string]string{"Phase": "End of Turn", "ValidPlayer": "You.descended"}},
		{"each player's first main phase", map[string]string{"Phase": "Main1", "ValidPlayer": "Player"}},
		{"your end of combat", map[string]string{"Phase": "EndCombat", "ValidPlayer": "You"}},
		{"upkeep of enchanted creature's controller", map[string]string{"Phase": "Upkeep", "ValidPlayer": "Player.EnchantedController"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{trig("Phase", tc.params)}}))
			if len(got) != 1 {
				t.Fatalf("got %d requirements, want exactly one: %+v", len(got), got)
			}
			if got[0].Sub != PhaseOtherSub || got[0].Gap != "" || got[0].CoveredByA {
				t.Fatalf("classification = %+v, want one uncovered %s requirement", got[0], PhaseOtherSub)
			}
		})
	}

	// The shapes the original TriggerPhase case owns must still classify as
	// trigger.phase, not be stolen by the new classifier.
	for _, tc := range []struct {
		name   string
		params map[string]string
	}{
		{"your first main phase", map[string]string{"Phase": "Main1", "ValidPlayer": "You"}},
		{"your upkeep", map[string]string{"Phase": "Upkeep", "ValidPlayer": "You"}},
		{"every player's upkeep", map[string]string{"Phase": "Upkeep", "ValidPlayer": "Player"}},
		{"opponent's draw", map[string]string{"Phase": "Draw", "ValidPlayer": "Opponent"}},
		{"your end step", map[string]string{"Phase": "End of Turn", "ValidPlayer": "You"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{trig("Phase", tc.params)}}))
			if len(got) != 1 {
				t.Fatalf("got %d requirements, want exactly one: %+v", len(got), got)
			}
			if got[0].Sub != "trigger.phase" || got[0].Gap != "" {
				t.Fatalf("classification = %+v, want the original served trigger.phase", got[0])
			}
		})
	}

	for _, tc := range []struct {
		name   string
		params map[string]string
	}{
		{"second main phase for each player", map[string]string{"Phase": "Main", "PhaseCount": "2", "ValidPlayer": "Player"}},
		{"second main phase without a count", map[string]string{"Phase": "Main", "ValidPlayer": "You"}},
		{"end step for an opponent", map[string]string{"Phase": "End of Turn", "ValidPlayer": "Opponent"}},
		{"your untap step", map[string]string{"Phase": "Untap", "ValidPlayer": "You"}},
		{"static second main phase bookkeeping", map[string]string{"Phase": "Main", "PhaseCount": "2", "ValidPlayer": "You", "Static": "True"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{trig("Phase", tc.params)}}))
			if len(got) != 1 {
				t.Fatalf("got %d requirements, want exactly one: %+v", len(got), got)
			}
			if got[0].Sub == PhaseOtherSub {
				t.Fatalf("out-of-scope pair was admitted as %s: %+v", PhaseOtherSub, got[0])
			}
		})
	}
}
