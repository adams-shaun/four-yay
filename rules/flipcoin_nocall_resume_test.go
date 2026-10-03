package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// noCallAskerSrc is a synthetic NoCall$ True carrier whose per-head branch
// poses a real mid-resolution ask (DB$ ChoosePlayer: every living player is
// a legal choice, so the engine must suspend) before a 1-life gain, and whose
// per-tail branch costs 1 life. Neither branch reads X, so each runs once
// per matching flip (Urza Academy Headmaster's literal one-turn-per-head
// shape). The 11 corpus NoCall$ lines never pair an asking branch with a
// count above one (Odds' tails copy asks, but Odds flips one coin), which is
// why the deferred-outcome loop needs this synthetic: it drives the real
// suspension/resume machinery, not a fake Host.
const noCallAskerSrc = `Name:NoCall Asker
ManaCost:2 R
Types:Creature
PT:1/1
A:AB$ FlipCoin | Amount$ 4 | NoCall$ True | HeadsSubAbility$ DBHeadsAsk | TailsSubAbility$ DBTailsLose
SVar:DBHeadsAsk:DB$ ChoosePlayer | SubAbility$ DBHeadsGain
SVar:DBHeadsGain:DB$ GainLife | Defined$ You | LifeAmount$ 1
SVar:DBTailsLose:DB$ LoseLife | Defined$ You | LifeAmount$ 1
Oracle:x
`

// TestNoCallOutcomeResumesAfterAnAskingCall pins the NoCall$ deferred-outcome
// repetition across a suspension: the per-head branch asks, and once that ask
// is answered the REMAINING head calls and every tail call must still run.
// Before the fix resolveOutcome returned on the first suspended call with no
// cursor, so the enclosing chain fell through to the FlipCoin's Sub and the
// later heads and the whole tails side were dropped: seat 0 gained 1 life
// however many heads came up and never lost any.
func TestNoCallOutcomeResumesAfterAnAskingCall(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	asker := card(t, noCallAskerSrc)
	for seed := uint64(1); seed <= 60; seed++ {
		e, _ := flipEngine(t, reg, seed, []*cards.Card{asker}, []*cards.Card{})
		src := moveByName(t, e, 0, "NoCall Asker", state.ZBattlefield)
		e.G.Obj(src).SummonSick = false
		before := e.G.Players[0].Life
		e.priorityRound()
		flipAbilityOption(t, e, src)
		drainAll(t, e, 200)
		heads, tails := 0, 0
		for _, w := range flipNotes(e) {
			if w {
				heads++
			} else {
				tails++
			}
		}
		if heads+tails != 4 {
			t.Fatalf("seed %d: precondition: %d flips, want 4", seed, heads+tails)
		}
		// The shape this test pins: at least two asking head calls (so a
		// dropped LATER head call is visible) and at least one tail call
		// after them.
		if heads < 2 || tails < 1 {
			continue
		}
		got := e.G.Players[0].Life - before
		if want := int32(heads - tails); got != want {
			t.Fatalf("seed %d: %d heads / %d tails moved seat 0's life by %d, want %d (one +1 per head after its ask, one -1 per tail)",
				seed, heads, tails, got, want)
		}
		return
	}
	t.Fatal("no seed in 1..60 produced two or more heads and a tail")
}
