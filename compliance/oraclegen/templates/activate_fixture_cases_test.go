package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// activateFixtureCases are level-B activate rows that the activation template
// now serves with a fixture beyond the generic board list: a spell held on the
// stack (Kitsa, Diversion Unit), an artifact token made by a prelude (Worldwalker
// Helm), a subtype-qualified creature (Pirate Hat), a turn-history SVar gate
// (Lilypad Village, Matzalantli), a mana combination ask (Flamebraider), a
// zero-toughness source (Marketback Walker) and an ETB-counter shortfall
// (Flitterwing Nuisance). Each item must generate AND gorge must replay it:
// the fixture the generator names is the one the scenario settles with.
var activateFixtureCases = []struct {
	card, key, why string
}{
	{"Kitsa, Otterball Elite", "activate#0.1", "targets Instant.YouCtrl,Sorcery.YouCtrl@Stack"},
	{"Diversion Unit", "activate#0.0", "targets Instant,Sorcery@Stack"},
	{"Worldwalker Helm", "activate#0.0", "targets Artifact.YouCtrl+token"},
	{"Pirate Hat", "activate#0.0", "targets Creature.Pirate+YouCtrl"},
	{"Lilypad Village", "activate#0.2", "ThisTurnEntered_Battlefield gate"},
	{"Matzalantli, the Great Door", "activate#0.1", "CardTypesPermanent delirium gate"},
	{"Flamebraider", "activate#0.0", "mana combination ask"},
	{"Marketback Walker", "activate#0.0", "0-toughness source"},
	{"Flitterwing Nuisance", "activate#0.0", "ETB counter shortfall"},
}

func TestActivateFixturesGenerateAndPlay(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range activateFixtureCases {
		t.Run(tc.card, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok {
				t.Fatalf("%s not in corpus", tc.card)
			}
			var req levelb.Requirement
			found := false
			for _, r := range levelb.Requirements(c) {
				if r.Key == tc.key {
					req, found = r, true
					break
				}
			}
			if !found {
				t.Fatalf("%s: requirement %s not offered by the census", tc.card, tc.key)
			}
			if req.Gap != "" {
				t.Fatalf("%s: census gap %q, want a servable requirement", tc.card, req.Gap)
			}
			item, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("%s (%s): skip %q", tc.card, tc.why, skip.Reason)
			}
			if item.ID == "" {
				t.Fatalf("%s: empty item", tc.card)
			}
			// Precondition: the scenario must exercise the ability itself, not
			// merely build a board; a generator that emitted only setup steps
			// would pass the replay below while testing nothing.
			activated := false
			for _, st := range item.Scenario.Steps {
				if st.Op == "activate" && st.Card == "p0:"+tc.card {
					activated = true
					break
				}
			}
			if !activated {
				t.Fatalf("%s: scenario has no activate step for the source (%d steps)", tc.card, len(item.Scenario.Steps))
			}
			if _, ok := oraclegen.PlaysThrough(reg, item.Scenario); !ok {
				t.Fatalf("%s (%s): generated scenario does not replay", tc.card, tc.why)
			}
		})
	}
}
