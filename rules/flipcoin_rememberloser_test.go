package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// fluxLoserSrc is Unleash the Flux's chain -- each player sacrifices a nonland
// permanent, then you flip a coin with RememberLoser$ True -- lifted onto an
// activated ability with a reader of the remembered loser chained after it.
//
// The real carrier cannot drive this test: Unleash the Flux is a Phenomenon
// whose T:Mode$ PlaneswalkedTo trigger can never fire in a build with no
// planar deck, and its repeat gate reads the count head
// PlayerCountRemembered$HasPropertyYou, which this build does not model, so
// the loop would not repeat even if the trigger fired. What IS this ticket's
// behaviour is the RememberLoser$ binding itself: a LOST flip must bind the
// flipper as Remembered (and a won flip must leave Remembered untouched),
// which is exactly what the real card's repeat gate goes on to count. The
// chained DB$ DealDamage | Defined$ Remembered makes that binding observable
// as life loss on the loser and no life loss on the winner.
const fluxLoserSrc = `Name:Flux Loser
ManaCost:2 R
Types:Creature
PT:1/1
A:AB$ Sacrifice | Defined$ Player | SacValid$ Permanent.nonLand | SubAbility$ DBCoin
SVar:DBCoin:DB$ FlipCoin | RememberLoser$ True | SubAbility$ DBLoserHit
SVar:DBLoserHit:DB$ DealDamage | Defined$ Remembered | NumDmg$ 3
Oracle:x
`

// TestFlipCoinRememberLoserBindsTheLosingFlipper is the behavioural test for
// RememberLoser$ (Unleash the Flux's parameter). It runs the card's own
// sacrifice-then-flip chain on the real engine and asserts BOTH branches: a
// lost flip remembers the flipper, so the chained Defined$ Remembered reader
// takes 3 off that player's life; a won flip remembers nobody, so no life
// moves. With the RememberLoser$ assignment removed, the loss branch also
// remembers nobody and the lost-flip seed loses no life -- which is what the
// "no unread note" coverage alone could not see.
func TestFlipCoinRememberLoserBindsTheLosingFlipper(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	loser := card(t, fluxLoserSrc)
	bear := lookup(t, reg, "Grizzly Bears")

	// run activates the chain on seat 0 and returns (flip won, seat 0's life
	// delta over the resolution, seat 0's creature count delta).
	run := func(seed uint64) (win bool, lifeLost int, sacrificed int) {
		t.Helper()
		e, _ := flipEngine(t, reg, seed, []*cards.Card{loser, bear}, []*cards.Card{bear})
		src := moveByName(t, e, 0, "Flux Loser", state.ZBattlefield)
		moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
		moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
		// Preconditions: the chain's sacrifice has something to take on both
		// seats, and seat 0 is at its full starting life so a 3-point hit is
		// unambiguous.
		if got := battlefieldCreatures(e, 0); got != 2 {
			t.Fatalf("precondition: seat 0 has %d creatures, want the Flux Loser plus a bear", got)
		}
		if got := battlefieldCreatures(e, 1); got != 1 {
			t.Fatalf("precondition: seat 1 has %d creatures, want 1 bear", got)
		}
		beforeLife := e.G.Players[0].Life
		beforeCreatures := battlefieldCreatures(e, 0)
		e.G.Obj(src).SummonSick = false
		e.priorityRound()
		activateAPIOption(t, e, src, "Sacrifice")
		drainCountingAsks(t, e, 400)

		flips := flipNotes(e)
		if len(flips) != 1 {
			t.Fatalf("seed %d precondition: the chain made %d flips, want exactly 1", seed, len(flips))
		}
		return flips[0], int(beforeLife - e.G.Players[0].Life),
			beforeCreatures - battlefieldCreatures(e, 0)
	}

	sawWin, sawLoss := false, false
	for seed := uint64(1); seed <= 40 && !(sawWin && sawLoss); seed++ {
		win, lifeLost, sacrificed := run(seed)
		if sacrificed != 1 {
			t.Fatalf("seed %d precondition: the chain's own Sacrifice took %d of seat 0's permanents, want 1", seed, sacrificed)
		}
		switch {
		case win:
			if sawWin {
				continue
			}
			sawWin = true
			if lifeLost != 0 {
				t.Fatalf("seed %d: the flip was WON, so RememberLoser$ must remember nobody, but seat 0 lost %d life to the chained Defined$ Remembered reader",
					seed, lifeLost)
			}
		default:
			if sawLoss {
				continue
			}
			sawLoss = true
			if lifeLost != 3 {
				t.Fatalf("seed %d: the flip was LOST, so RememberLoser$ must bind the flipper and the chained Defined$ Remembered reader must take 3 life; seat 0 lost %d",
					seed, lifeLost)
			}
		}
	}
	if !sawWin || !sawLoss {
		t.Fatalf("no seed range covered both branches: sawWin=%v sawLoss=%v", sawWin, sawLoss)
	}
}
