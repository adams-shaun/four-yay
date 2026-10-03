package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
)

const redistributeSA = "SP$ SetLife | PlayerChoices$ Player | ChoiceAmount$ Any | ChoicePrompt$ Choose any number of players | Redistribute$ True"

func TestSetLifeRedistributeNoHost(t *testing.T) {
	h := newHost(t, 3)
	h.g.Players[0].Life, h.g.Players[1].Life, h.g.Players[2].Life = 20, 5, 2
	if h.g.Players[0].Life == h.g.Players[1].Life {
		t.Fatal("precondition: distinct totals required")
	}
	Resolve(h, &Ctx{Controller: 0}, sa(t, redistributeSA))
	if h.askCount != 1 {
		t.Fatalf("SetLife handler did not ask: ask count=%d", h.askCount)
	}
	if h.g.Players[0].Life != 20 || h.g.Players[1].Life != 5 || h.g.Players[2].Life != 2 {
		t.Fatalf("no-host changed life: %+v", h.g.Players)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note || ev.Kind == events.LifeChange {
			t.Fatalf("unexpected no-host event: %+v", ev)
		}
	}
}
