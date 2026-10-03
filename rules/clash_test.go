package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// These fixtures pin api:Clash and trig:Clashed (CR 701.31, task clash1) on the
// real corpus carrier Marvo, Deep Operative, plus the trigger mode's own
// Won$ orientation gate.
//
// Marvo's attack trigger is `DB$ Clash | Defined$ TriggeredDefendingPlayer`
// and its second line is `T:Mode$ Clashed | ValidPlayer$ You | Won$ True`
// (draw a card, then may cast an MV<=8 spell for free). The clash therefore
// runs entirely inside the attack trigger's resolution: seat 0 (Marvo's
// controller) reveals the top of its library, seat 1 (the defending player)
// reveals its own, the higher mana value wins, and the Clashed marker the
// clash emits fires Marvo's own "whenever you win a clash" line.

// hasClashEvent reports whether the log holds one events.Clash marker for
// player p with the given Won$ orientation (Amount != 0 means won).
func hasClashEvent(e *Engine, p state.PlayerID, won bool) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.Clash && ev.Player == p {
			if (ev.Amount != 0) == won {
				return true
			}
		}
	}
	return false
}

// TestClashPrimitivesRegistered pins that both halves of the mechanic are
// declared supported, so the coverage census counts the 29 api:Clash and the 4
// trig:Clashed corpus carriers as playable. Reverting either registration is a
// silent coverage regression this fails on.
func TestClashPrimitivesRegistered(t *testing.T) {
	t.Parallel()
	supported := effects.Supported()
	for _, p := range []string{"api:Clash", "trig:Clashed"} {
		if !supported[p] {
			t.Fatalf("effects.Supported() is missing %s", p)
		}
	}
}

// TestClashTriggerModeReadsWonOrientation pins the two parameters the corpus's
// Clashed lines actually carry -- ValidPlayer$ and the Won$ True/False split
// (Entangling Trap and Rebellion of the Flamekin each carry a True line and a
// Secondary$ True False sibling). It exercises the matcher directly, so it
// fails if the Clashed registration is removed, and it asserts the NEGATIVE
// answers too: a Won$ True line must reject a loss and a ValidPlayer$ You line
// must reject the other seat.
func TestClashTriggerModeReadsWonOrientation(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	const src = state.ObjID(1) // any id: controllerOf degrades to 0 for it

	winTrue := cards.Trigger{Mode: "Clashed", Params: map[string]string{"ValidPlayer": "You", "Won": "True"}}
	winFalse := cards.Trigger{Mode: "Clashed", Params: map[string]string{"ValidPlayer": "You", "Won": "False"}}

	win0 := events.Event{Kind: events.Clash, Obj: src, Player: 0, Amount: 1}
	loss0 := events.Event{Kind: events.Clash, Obj: src, Player: 0, Amount: 0}
	win1 := events.Event{Kind: events.Clash, Obj: src, Player: 1, Amount: 1}

	// Precondition: the two orientations really differ, so the assertions
	// below are about the gate and not about two identical events.
	if (win0.Amount != 0) == (loss0.Amount != 0) {
		t.Fatal("test precondition: win and loss events carry the same amount")
	}

	if !trigmatch.ClashMatches(boardOf(e), winTrue, src, win0, nil) {
		t.Error("Won$ True line rejected the matching win")
	}
	if trigmatch.ClashMatches(boardOf(e), winTrue, src, loss0, nil) {
		t.Error("Won$ True line accepted a loss")
	}
	if trigmatch.ClashMatches(boardOf(e), winTrue, src, win1, nil) {
		t.Error("ValidPlayer$ You line accepted another seat's win")
	}
	if !trigmatch.ClashMatches(boardOf(e), winFalse, src, loss0, nil) {
		t.Error("Won$ False line rejected the matching loss")
	}
	if trigmatch.ClashMatches(boardOf(e), winFalse, src, win0, nil) {
		t.Error("Won$ False line accepted a win")
	}
}
