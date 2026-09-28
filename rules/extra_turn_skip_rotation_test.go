package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestUginsNexusSkippedOpponentExtraTurnPreservesOrdinaryRotation drives a
// real Ugin's Nexus through the whole extra-turn consumer. A pending turn for
// seat 1 is skipped at seat 0's turn-1 cleanup, then seat 1 takes its normal
// turn 2 and seat 0 must take ordinary turn 3. The skipped consumption and
// normal turn have the same holder, which rotationBase cannot infer from an
// unmarked -1 ExtraTurn event.
func TestUginsNexusSkippedOpponentExtraTurnPreservesOrdinaryRotation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Ugin's Nexus")}, nil)
	nexus := moveByName(t, e, 0, "Ugin's Nexus", state.ZBattlefield)
	if e.G.Obj(nexus).Zone != state.ZBattlefield {
		t.Fatal("precondition: Ugin's Nexus must be on the battlefield")
	}

	// This grant is pending while seat 0's ordinary turn is still active. The
	// cleanup consumer below is therefore the exact skipped-then-normal-turn
	// order that exposed the rotation ambiguity.
	e.emit(events.Event{Kind: events.ExtraTurn, Player: 1, Amount: 1})
	if len(e.G.ExtraTurnQueue) != 1 || e.G.ExtraTurnQueue[0].Player != 1 {
		t.Fatalf("precondition: seat 1 extra turn is not pending: %v", e.G.ExtraTurnQueue)
	}
	if skipped, _ := e.extraTurnSkipped(1); !skipped {
		t.Fatal("precondition: Ugin's Nexus did not skip seat 1's extra turn")
	}

	// This crosses the skipped consumption, normal turn 2 for seat 1, and its
	// cleanup's ordinary-rotation decision. The required holders are 0,1,0.
	driveToStep(t, e, 3, 0, state.StepMain1)
	var holders []state.PlayerID
	var skipped bool
	for _, ev := range e.L.Events {
		if ev.Kind == events.TurnChange {
			holders = append(holders, ev.Player)
		}
		if ev.Kind == events.ExtraTurn && ev.Player == 1 && ev.Amount == -1 && ev.Text == events.ExtraTurnSkippedText {
			skipped = true
		}
	}
	if !skipped {
		t.Fatal("Ugin's Nexus consumption was not marked skipped")
	}
	if len(holders) != 3 || holders[0] != 0 || holders[1] != 1 || holders[2] != 0 {
		t.Fatalf("turn holders = %v, want ordinary rotation 0,1,0", holders)
	}
	replayCheck(t, e, cfg)
}
