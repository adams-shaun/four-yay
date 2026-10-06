package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestSelfCastCoverageIsLimitedToLevelASettledShape(t *testing.T) {
	for _, tc := range []struct {
		name    string
		execute string
		covered bool
	}{
		{name: "Emrakul untap trigger", execute: "TrigUntapAll", covered: true},
		{name: "Ulamog targeted trigger", execute: "TrigChange"},
		{name: "World Breaker targeted trigger", execute: "TrigChange"},
		{name: "Hope-Ender Coatl targeted trigger", execute: "TrigChange"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			face := &cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{
				trig("SpellCast", map[string]string{
					"Execute": tc.execute, "ValidCard": "Card.Self",
					"TriggerDescription": "When you cast this spell.",
				}),
			}}
			got := Requirements(cardOf(face))
			if len(got) != 1 {
				t.Fatalf("got %d requirements, want exactly one: %+v", len(got), got)
			}
			if got[0].CoveredByA != tc.covered {
				t.Fatalf("CoveredByA=%v, want %v (requirement: %+v)", got[0].CoveredByA, tc.covered, got[0])
			}
			if tc.covered && got[0].Sub != "trigger.spell-cast-self" {
				t.Fatalf("covered Emrakul shape has Sub=%q, want trigger.spell-cast-self", got[0].Sub)
			}
			if !tc.covered && got[0].Sub != "trigger.gap:SpellCast" {
				t.Fatalf("unsettled targeted trigger has Sub=%q, want trigger.gap:SpellCast", got[0].Sub)
			}
		})
	}
}

func TestLevelBServedOpponentAndPlayerWidePhasesArePinned(t *testing.T) {
	for _, tc := range []struct {
		name, phase, player string
	}{
		{name: "opponent draw", phase: "Draw", player: "Opponent"},
		{name: "opponent upkeep", phase: "Upkeep", player: "Opponent"},
		{name: "any player's upkeep", phase: "Upkeep", player: "Player"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{
				Types: []string{"Enchantment"},
				Triggers: []cards.Trigger{trig("Phase", map[string]string{
					"Phase": tc.phase, "ValidPlayer": tc.player,
				})},
			}))
			if len(got) != 1 {
				t.Fatalf("got %d requirements, want exactly one: %+v", len(got), got)
			}
			if got[0].Sub != "trigger.phase" || got[0].Gap != "" {
				t.Fatalf("got Sub=%q Gap=%q, want served trigger.phase", got[0].Sub, got[0].Gap)
			}
		})
	}
}
