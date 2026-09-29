package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestWardPromptRendersCostAsEnglish pins the ward ask's player-facing text:
// the raw UnlessCost$ (PayLife<2>, Sac<...>) must never reach the prompt or
// the pay label verbatim (client-table UI rework spec §4; the repo-deck
// scan in rules/prompt_text_test.go caught "Pay PayLife<2> for ward?").
func TestWardPromptRendersCostAsEnglish(t *testing.T) {
	for _, tc := range []struct{ cost, prompt, label string }{
		{"PayLife<2>", "Pay 2 life for ward?", "Pay 2 life"},
		{"Sac<1/Creature.Other/another creature>", "Sacrifice another creature for ward?", "Sacrifice another creature"},
		{"Sac<1/Land>", "Sacrifice a land for ward?", "Sacrifice a land"},
		{"Sac<1/Artifact>", "Sacrifice an artifact for ward?", "Sacrifice an artifact"},
		{"Sac<2/Creature>", "Pay the cost for ward?", "Pay the cost"},
		{"Sac<1/Creature.Other>", "Pay the cost for ward?", "Pay the cost"},
		{"Discard<1/Card>", "Pay the cost for ward?", "Pay the cost"},
		{"2", "Pay 2 for ward?", "Pay 2"},
	} {
		h := &chooseColorHost{}
		h.g = state.NewGame(names(2))
		spell := h.g.AddObject(mkCard(t, "Name:Spell\nTypes:Instant\nOracle:x\n"), 1)
		spell.Zone = state.ZStack // fixture setup; effects never mutate game state directly.
		ward := &cards.SA{API: "Ward", Params: map[string]string{"UnlessCost": tc.cost}}
		c := &Ctx{Controller: 0}
		c.TriggerStack = spell.ID
		effWard(h, c, ward)
		if len(h.asks) != 1 {
			t.Fatalf("%s: asked %d decisions, want 1", tc.cost, len(h.asks))
		}
		d := h.asks[0]
		if d.Prompt != tc.prompt || d.Options[0].Label != tc.label {
			t.Errorf("%s: prompt %q label %q, want %q / %q", tc.cost, d.Prompt, d.Options[0].Label, tc.prompt, tc.label)
		}
		if strings.Contains(d.Prompt, "<") {
			t.Errorf("%s: prompt %q carries raw cost syntax", tc.cost, d.Prompt)
		}
	}
}
