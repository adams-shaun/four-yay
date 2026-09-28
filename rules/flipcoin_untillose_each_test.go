package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// flipUntilLoseEachSrc is a synthetic three-seat carrier: its losing branch
// poses a real ChoosePlayer ask, which suspends the resolution. It isolates
// the cursor transition after a loss; no corpus FlipUntilYouLose$ carrier has
// a loss branch that asks.
const flipUntilLoseEachSrc = `Name:Flip Until Lose Each
ManaCost:2 R
Types:Creature
PT:1/1
A:AB$ FlipCoin | ForEachPlayer$ Opponent | FlipUntilYouLose$ True | LoseSubAbility$ LoseAsk
SVar:LoseAsk:DB$ ChoosePlayer
Oracle:x
`

// TestFlipCoinEachUntilLoseLossResumeAdvancesFlipper drives the real engine
// through a ForEachPlayer$ + FlipUntilYouLose$ loss-branch ask. A loss ends
// that player's loop, so answering its ask must resume with the NEXT opponent;
// retaining PlayerIndex with Iter+1 re-flips the loser because until-lose does
// not use Iter as a bound.
func TestFlipCoinEachUntilLoseLossResumeAdvancesFlipper(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	carrier := card(t, flipUntilLoseEachSrc)
	mountain := lookup(t, reg, "Mountain")
	run := func(seed uint64) []effects.FlipResult {
		fill := func(extra ...*cards.Card) []*cards.Card {
			deck := append([]*cards.Card{}, extra...)
			for len(deck) < 40 {
				deck = append(deck, mountain)
			}
			return deck
		}
		cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b", "c"}, Tokens: reg.Tokens,
			Decks: [][]*cards.Card{fill(carrier), fill(), fill()}})
		e := New(cfg)
		e.Advance()
		toMain1(t, e)
		src := moveByName(t, e, 0, "Flip Until Lose Each", state.ZBattlefield)
		if o := e.G.Obj(src); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
			t.Fatalf("precondition: carrier %d is not a seat-0 battlefield object: %+v", src, o)
		}
		e.G.Obj(src).SummonSick = false
		e.priorityRound()
		flipAbilityOption(t, e, src)
		drainAll(t, e, 200)

		return flipByPlayer(e)
	}

	for seed := uint64(1); seed <= 60; seed++ {
		flips := run(seed)
		// This is the losing branch which poses the suspension-bearing ask;
		// recover it from the logged deterministic result, never from a seed
		// assumption.
		if len(flips) == 0 || flips[0].Player != 1 || flips[0].Heads {
			continue
		}
		// The first player repeats only on the old cursor bug. A later flip is
		// also required: it proves the loss branch really suspended and resumed.
		if len(flips) < 2 {
			t.Fatalf("seed %d: loss branch did not resume to another flipper: %v", seed, flips)
		}
		for i, result := range flips[1:] {
			if result.Player != 2 {
				t.Fatalf("seed %d: flip %d repeated/lost order at seat %d after seat-1 loss; want only next opponent seat 2: %v", seed, i+1, result.Player, flips)
			}
		}
		return
	}
	t.Fatal("no seed in 1..60 produced the seat-1 loss-branch suspension shape")
}
