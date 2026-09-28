package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestGenericChoiceTempRememberRestoresOuterPlayers(t *testing.T) {
	h, c := fixtureHost(t)
	outer := state.Target{Player: 0, IsPlayer: true}
	c.Remembered = []state.Target{outer}
	c.SVars = map[string]string{
		"Gain": "DB$ GainLife | Defined$ Remembered | LifeAmount$ 2",
	}
	before0, before1 := h.Game().Players[0].Life, h.Game().Players[1].Life
	Resolve(h, c, &cards.SA{Kind: "DB", API: "GenericChoice", Params: map[string]string{
		"Defined": "Opponent", "TempRemember": "Chooser", "Choices": "Gain",
	}})
	if got := h.Game().Players[1].Life; got != before1+2 {
		t.Fatalf("chooser life = %d, want %d (temporary Remembered must bind opponent)", got, before1+2)
	}
	if got := h.Game().Players[0].Life; got != before0 {
		t.Fatalf("outer remembered player's life = %d, want %d", got, before0)
	}
	if len(c.Remembered) != 1 || c.Remembered[0] != outer {
		t.Fatalf("Remembered after GenericChoice = %v, want restored outer set [%v]", c.Remembered, outer)
	}
}
