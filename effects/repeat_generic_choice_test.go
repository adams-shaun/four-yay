package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestRepeatGenericChoiceRunsForEveryIteration(t *testing.T) {
	h, c := fixtureHost(t)
	const opponent = 1
	before := h.Game().Players[opponent].Life
	if before <= 6 {
		t.Fatalf("precondition: opponent life = %d, need room to observe two losses", before)
	}
	c.SVars = map[string]string{
		"DBChoice": "DB$ GenericChoice | Defined$ Opponent | Choices$ A",
		"A":        "DB$ LoseLife | Defined$ Opponent | LifeAmount$ 3",
	}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Repeat", Params: map[string]string{
		"RepeatSubAbility": "DBChoice",
		"MaxRepeat":        "2",
	}})
	got := h.Game().Players[opponent].Life
	if got == before {
		t.Fatalf("precondition: repeated body made no change (life %d); GenericChoice must resolve", got)
	}
	if want := before - 6; got != want {
		t.Fatalf("opponent life = %d after Repeat, want %d (two GenericChoice losses)", got, want)
	}
}
