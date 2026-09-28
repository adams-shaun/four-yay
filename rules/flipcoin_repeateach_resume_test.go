package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// flipLoopAskerSrc is Mana Clash's shape with one addition: the ask sits
// BETWEEN the RepeatEach loop and the flip-result reader. The loop flips once
// per player (Flipper$ Remembered binds each iteration's subject), the
// enclosing SubAbility$ poses a real mid-resolution ask (DB$ ChoosePlayer;
// every living player is a legal choice, so the engine suspends), and the
// answer's resumed walk is where the Defined$ FlippedHeads reader runs. No
// corpus carrier has that ordering, which is why this is synthetic: the
// point is to exercise the real engine's suspend/resume seam, not a fake Host.
const flipLoopAskerSrc = `Name:Flip Loop Asker
ManaCost:2 R
Types:Creature
PT:1/1
A:AB$ RepeatEach | RepeatPlayers$ Player | RepeatSubAbility$ DBFlip | SubAbility$ DBAsk
SVar:DBFlip:DB$ FlipCoin | Flipper$ Remembered | NoCall$ True | RememberResult$ True
SVar:DBAsk:DB$ ChoosePlayer | SubAbility$ DBHit
SVar:DBHit:DB$ DamageAll | ValidPlayers$ FlippedHeads | NumDmg$ 1
Oracle:x
`

// activateAPIOption submits the pending priority option that activates obj's
// ability whose API is api. The sibling flipAbilityOption is FlipCoin-only.
func activateAPIOption(t *testing.T, e *Engine, obj state.ObjID, api string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision to activate %s from: %+v", api, d)
	}
	face := e.G.Obj(obj).Face()
	idx := -1
	for _, o := range d.Options {
		if o.Kind != "ability" || o.Obj != obj || face == nil ||
			o.Ability < 0 || o.Ability >= len(face.Abilities) {
			continue
		}
		if face.Abilities[o.Ability].API == api {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("%s ability not offered: %+v", api, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit %s activation: %v", api, err)
	}
}

// drainCountingAsks is drainAll with a count of the mid-resolution asks it
// answered, so a test can assert its own suspension precondition.
func drainCountingAsks(t *testing.T, e *Engine, limit int) int {
	t.Helper()
	asks := 0
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 {
				e.priorityRound()
				if e.Pending() == nil && len(e.G.Stack) == 0 {
					return asks
				}
			}
			d = e.Pending()
			if d == nil {
				t.Fatalf("no decision with stack depth %d", len(e.G.Stack))
			}
		}
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				return asks
			}
			passOnce(t, e)
		case decision.KChoose, decision.KTarget:
			asks++
			if len(d.Options) == 0 {
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
					t.Fatalf("submit empty answer: %v", err)
				}
				continue
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit first option: %v", err)
			}
		default:
			t.Fatalf("unexpected decision %v while draining a flip chain", d.Kind)
		}
	}
	t.Fatal("drainCountingAsks did not converge")
	return asks
}

// TestFlipCoinRepeatEachPostLoopAskKeepsMemoryAcrossResume is the regression
// for the RepeatEach republication seam. Each iteration of a RepeatEach body
// resolves on a Ctx COPY, and effects.Resolve publishes/restores the engine's
// resolvingFlipMemory around every walk -- so the iteration that lazily
// allocated the flip memory restores the engine slot to nil on its way out.
// Retaining the pointer on the outer Ctx alone is not enough: the ask the
// enclosing SubAbility$ poses after the loop captures the ENGINE slot onto
// its resume point, and a nil there means the resumed walk rebuilds a fresh
// context with no flip memory and the Defined$ FlippedHeads reader sees the
// empty set. Observed here as the missing DamageAll damage on every flipper
// whose coin came up heads.
func TestFlipCoinRepeatEachPostLoopAskKeepsMemoryAcrossResume(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	asker := card(t, flipLoopAskerSrc)

	for seed := uint64(1); seed <= 40; seed++ {
		e, _ := flipEngine(t, reg, seed, []*cards.Card{asker}, nil)
		src := moveByName(t, e, 0, "Flip Loop Asker", state.ZBattlefield)
		if got := e.G.Obj(src).Face().Name; got != "Flip Loop Asker" {
			t.Fatalf("precondition: fixture card = %q", got)
		}
		e.G.Obj(src).SummonSick = false
		e.priorityRound()
		activateAPIOption(t, e, src, "RepeatEach")
		asks := drainCountingAsks(t, e, 200)

		flips := flipByPlayer(e)
		if len(flips) != 2 {
			t.Fatalf("seed %d precondition: the RepeatEach over both seats made %d flips, want 2 (%v)",
				seed, len(flips), flips)
		}
		var heads []state.PlayerID
		for _, f := range flips {
			if f.Heads {
				heads = append(heads, f.Player)
			}
		}
		if len(heads) == 0 {
			continue // no heads: the reader has nothing to hit either way
		}
		// Precondition: the post-loop DB$ ChoosePlayer really suspended the
		// resolution, so the reader below ran on a RESUMED walk.
		if asks == 0 {
			t.Fatalf("seed %d precondition: the post-loop ChoosePlayer posed no ask, so no resume was exercised", seed)
		}
		got := map[state.PlayerID]int{}
		for _, ev := range e.L.Events {
			if ev.Kind == events.Damage && ev.Obj == 0 && ev.Amount == 1 {
				got[ev.Player]++
			}
		}
		for _, p := range heads {
			if got[p] != 1 {
				t.Fatalf("seed %d: flips %v; the resumed Defined$ FlippedHeads reader dealt %v, seat %d took %d and wants 1 (the loop's flip memory was lost across the ask)",
					seed, flips, got, p, got[p])
			}
		}
		if len(got) != len(heads) {
			t.Fatalf("seed %d: flips %v; FlippedHeads damage %v hit %d seats, want exactly the %d heads flippers",
				seed, flips, got, len(got), len(heads))
		}
		return
	}
	t.Fatal("no seed in 1..40 produced a two-flip round with at least one heads")
}
