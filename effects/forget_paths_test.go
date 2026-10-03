package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestChooseCardForgetChosenFallbackAndRandom(t *testing.T) {
	for _, random := range []bool{false, true} {
		name := "fallback"
		body := "DB$ ChooseCard | Defined$ You | Amount$ 1 | Choices$ Creature.IsRemembered | ChoiceZone$ Exile | Mandatory$ True | ForgetChosen$ True"
		if random {
			name = "random"
			body += " | AtRandom$ True"
		}
		t.Run(name, func(t *testing.T) {
			h, src, cards := forgetFixtureHost(t, "Picked", "Retained")
			picked, retained := cards[0], cards[1]
			h.g.SetZone(state.ZExile, 0, []state.ObjID{picked.ID, retained.ID})
			picked.Zone, retained.Zone = state.ZExile, state.ZExile
			seedRemembered(h, src, picked.ID, retained.ID)
			c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: picked.ID}, {Obj: retained.ID}}}
			if picked.ID == retained.ID || picked.Zone != state.ZExile || !sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{picked.ID, retained.ID}) {
				t.Fatal("precondition: distinct exiled remembered candidates")
			}
			effChooseCard(h, c, sa(t, body))
			if len(c.Chosen) != 1 || c.Chosen[0].Obj != picked.ID {
				t.Fatalf("selected %v, want %d", c.Chosen, picked.ID)
			}
			if got := rememberedIDs(c.Remembered); !sameIDs(got, []state.ObjID{retained.ID}) {
				t.Errorf("ctx remembered %v", got)
			}
			if got := rememberedIDs(h.g.Obj(src.ID).Remembered); !sameIDs(got, []state.ObjID{retained.ID}) {
				t.Errorf("persistent remembered %v", got)
			}
		})
	}
}
