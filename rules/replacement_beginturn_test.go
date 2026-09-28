// The skip-an-extra-turn replacement class: R:Event$ BeginTurn |
// ExtraTurn$ True | Skip$ True (Trouble in Pairs, Stranglehold, Ugin's
// Nexus, Gerrard's Hourglass Pendant). Every test drives a REAL corpus
// extra-turn grant (Final Fortune) against a REAL corpus carrier moved onto
// the battlefield -- never a hand-authored stand-in. Time Vault's normal-turn
// optional skip (no ExtraTurn$) is deliberately out of scope and must stay
// inert; the matcher's ExtraTurn$ requirement is what keeps it out.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// extraTurnConsumptions returns every -1 ExtraTurn event of seat in log
// order, plus the index of the first.
func extraTurnConsumptions(e *Engine, seat state.PlayerID) ([]events.Event, int) {
	var out []events.Event
	first := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.ExtraTurn && ev.Player == seat && ev.Amount < 0 {
			if first < 0 {
				first = i
			}
			out = append(out, ev)
		}
	}
	return out, first
}

// turnChangeHoldersAfter returns the Player of every TurnChange after log
// index i.
func turnChangeHoldersAfter(e *Engine, i int) []state.PlayerID {
	var out []state.PlayerID
	for _, ev := range e.L.Events[i+1:] {
		if ev.Kind == events.TurnChange {
			out = append(out, ev.Player)
		}
	}
	return out
}

// TestTroubleInPairsSkipsAnOpponentsExtraTurn is the carrier leaf: seat 1's
// Trouble in Pairs skips seat 0's Final Fortune extra turn. The granted seat
// does NOT repeat -- the ordinary next seat takes the following turn, and the
// one after that is seat 0 again -- while the grant is still consumed (the
// fold's queue and count drain to zero) through a -1 consumption that does
// NOT carry Final Fortune's delayed-trigger rider (the granted turn never
// begins, so its "lose the game" end step must never register).
func TestTroubleInPairsSkipsAnOpponentsExtraTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Final Fortune")},
		[]*cards.Card{lookup(t, reg, "Trouble in Pairs")})
	moveByName(t, e, 1, "Trouble in Pairs", state.ZBattlefield)
	moveByName(t, e, 0, "Final Fortune", state.ZHand)
	addMana(t, e, 0, "RR")
	castNamed(t, e, "Final Fortune")
	passUntilStackEmpty(t, e, 40)
	grants := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ExtraTurn && ev.Player == 0 && ev.Amount > 0
	})
	if grants != 1 {
		t.Fatalf("want exactly one ExtraTurn grant, got %d", grants)
	}
	driveToStep(t, e, 4, 1, state.StepMain1)
	if len(e.G.ExtraTurnQueue) != 0 {
		t.Fatalf("the skipped grant was not consumed: queue %v", e.G.ExtraTurnQueue)
	}
	if e.G.ExtraTurns[0] != 0 {
		t.Fatalf("extra-turn count not consumed: %d", e.G.ExtraTurns[0])
	}
	consumptions, idx := extraTurnConsumptions(e, 0)
	if len(consumptions) != 1 {
		t.Fatalf("want exactly one -1 ExtraTurn consumption, got %d", len(consumptions))
	}
	if consumptions[0].Obj != 0 || consumptions[0].Counter != "" {
		t.Fatalf("the skipped consumption carried the Final-Fortune rider (Obj %d Counter %q)",
			consumptions[0].Obj, consumptions[0].Counter)
	}
	holders := turnChangeHoldersAfter(e, idx)
	if len(holders) < 2 {
		t.Fatalf("want the ordinary rotation after the skip, got holders %v", holders)
	}
	if holders[0] != 1 || holders[1] != 0 {
		t.Fatalf("granted seat repeated or rotation broke: holders after the skip are %v", holders)
	}
	replayCheck(t, e, cfg)
}

// TestTroubleInPairsLeavesItsControllersOwnExtraTurnAlone is the
// ValidPlayer$ Opponent gate's flip side: seat 0's own Trouble in Pairs does
// not skip seat 0's own Final Fortune extra turn. The seat repeats as usual,
// and the consumption keeps the delayed-trigger rider (seat 0 loses the game
// at the extra turn's end step) -- proving the skip is the only shape that
// drops it.
func TestTroubleInPairsLeavesItsControllersOwnExtraTurnAlone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Final Fortune"), lookup(t, reg, "Trouble in Pairs")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Trouble in Pairs", state.ZBattlefield)
	moveByName(t, e, 0, "Final Fortune", state.ZHand)
	addMana(t, e, 0, "RR")
	castNamed(t, e, "Final Fortune")
	passUntilStackEmpty(t, e, 40)
	// The extra turn IS taken: turn 2 belongs to seat 0 again (the extra
	// turn lands right after the turn that created it).
	driveToStep(t, e, 2, 0, state.StepMain1)
	if len(e.G.ExtraTurnQueue) != 0 || e.G.ExtraTurns[0] != 0 {
		t.Fatalf("extra-turn accounting wrong: queue %v count %d",
			e.G.ExtraTurnQueue, e.G.ExtraTurns[0])
	}
	consumptions, _ := extraTurnConsumptions(e, 0)
	if len(consumptions) != 1 || consumptions[0].Obj == 0 || consumptions[0].Counter == "" {
		t.Fatalf("the taken turn's consumption lost the rider: %+v", consumptions)
	}
	passAll(t, e, 4000)
	if !e.G.Over {
		t.Fatal("the game did not end with the lost extra turn")
	}
	lost := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.PlayerLost && ev.Player == 0 {
			lost = true
		}
	}
	if !lost {
		t.Fatal("the extra turn's end step did not cost seat 0 the game")
	}
	replayCheck(t, e, cfg)
}

// TestUginsNexusSkipsItsControllersOwnExtraTurn covers the no-ValidPlayer$
// carriers: Ugin's Nexus skips ANY player's extra turn, its controller's own
// included (a ValidPlayer$-gated carrier would not).
func TestUginsNexusSkipsItsControllersOwnExtraTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Final Fortune"), lookup(t, reg, "Ugin's Nexus")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Ugin's Nexus", state.ZBattlefield)
	moveByName(t, e, 0, "Final Fortune", state.ZHand)
	addMana(t, e, 0, "RR")
	castNamed(t, e, "Final Fortune")
	passUntilStackEmpty(t, e, 40)
	driveToStep(t, e, 4, 1, state.StepMain1)
	if len(e.G.ExtraTurnQueue) != 0 || e.G.ExtraTurns[0] != 0 {
		t.Fatalf("the skipped grant was not consumed: queue %v count %d",
			e.G.ExtraTurnQueue, e.G.ExtraTurns[0])
	}
	consumptions, idx := extraTurnConsumptions(e, 0)
	if len(consumptions) != 1 || consumptions[0].Obj != 0 || consumptions[0].Counter != "" {
		t.Fatalf("the skipped consumption is wrong: %+v", consumptions)
	}
	holders := turnChangeHoldersAfter(e, idx)
	if len(holders) < 2 || holders[0] != 1 || holders[1] != 0 {
		t.Fatalf("rotation broke after the skip: holders %v", holders)
	}
	replayCheck(t, e, cfg)
}

// TestBeginTurnSkipLeavesNormalTurnsAlone is the table-wide guard: with
// Trouble in Pairs live, the ordinary turn rotation proceeds untouched -- no
// ExtraTurn consumption, no repeated seat. The matcher only ever fires on
// the synthetic begin-turn event the extra-turn consumption poses, so a
// normal turn never consults it at all; this test pins that end to end.
func TestBeginTurnSkipLeavesNormalTurnsAlone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{},
		[]*cards.Card{lookup(t, reg, "Trouble in Pairs")})
	moveByName(t, e, 1, "Trouble in Pairs", state.ZBattlefield)
	driveToStep(t, e, 4, 1, state.StepMain1)
	holders := []state.PlayerID{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.TurnChange {
			holders = append(holders, ev.Player)
		}
	}
	// Turns 1..4 rotate 0, 1, 0, 1: no seat repeated.
	if len(holders) != 4 || holders[0] != 0 || holders[1] != 1 || holders[2] != 0 || holders[3] != 1 {
		t.Fatalf("normal rotation disturbed under Trouble in Pairs: %v", holders)
	}
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ExtraTurn
	}); n != 0 {
		t.Fatalf("normal turns emitted %d ExtraTurn events", n)
	}
	replayCheck(t, e, cfg)
}
