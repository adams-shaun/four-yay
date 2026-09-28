package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestFlipCoinForEachPlayerOpponentOrderIsControllerRelative pins the
// ForEachPlayer$ Opponent enumeration order (Mutalith Vortex Beast): the
// flipper walk must be controller-relative AliveFrom(c.Controller) order --
// the SAME order the shared Defined$ Opponent resolver uses -- not seat
// number order. In this 4-seat fixture the resolving controller is seat 1,
// so the flips (and the seeded RNG outcomes bound to them) must run for
// seats 2, 3, 0 in that order; the pre-fix AliveFrom(0) walk visited seats
// 0 and 2 instead (two flips, wrong order and wrong flippers).
func TestFlipCoinForEachPlayerOpponentOrderIsControllerRelative(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mutalith := lookup(t, reg, "Mutalith Vortex Beast")
	run := func(seed uint64) []effects.FlipResult {
		m, ok := reg.Lookup("Mountain")
		if !ok {
			t.Fatal("corpus fixture: Mountain missing")
		}
		fill := func(extras ...*cards.Card) []*cards.Card {
			deck := append([]*cards.Card{}, extras...)
			for len(deck) < 40 {
				deck = append(deck, m)
			}
			return deck
		}
		cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b", "c", "d"},
			Tokens: reg.Tokens,
			Decks:  [][]*cards.Card{fill(), fill(mutalith), fill(), fill()}})
		e := New(cfg)
		e.Advance()
		toMain1(t, e)
		// Precondition: seat 1 (the non-seat-0 controller) controls the
		// Mutalith whose ETB trigger flips for each of its opponents.
		moveByName(t, e, 1, "Mutalith Vortex Beast", state.ZBattlefield)
		e.priorityRound()
		passUntilStackEmpty(t, e, 200)
		return flipByPlayer(e)
	}

	var flips []effects.FlipResult
	for _, seed := range []uint64{1, 2, 3, 4, 5, 6, 7, 8} {
		fs := run(seed)
		if len(fs) != 3 {
			t.Fatalf("seed %d: three living opponents must each flip once, got %d flips (%v)",
				seed, len(fs), fs)
		}
		flips = fs
		break
	}
	if flips == nil {
		t.Fatal("no seed produced the three-flip shape")
	}
	want := []state.PlayerID{2, 3, 0}
	for i, w := range want {
		if flips[i].Player != w {
			t.Fatalf("flip %d went to seat %d, want seat %d (controller-relative AliveFrom(1) order = [2 3 0]); full order %v",
				i, flips[i].Player, w, flips)
		}
	}
}

// flipAskerSrc is a synthetic FlipUntilYouLose$ carrier whose WIN branch
// poses a real mid-resolution ask (DB$ ChoosePlayer: every living player is
// a legal choice, so the engine must pose the ask and suspend) and whose
// LOSE branch reads the accumulated flip memory through the Mana Clash
// consumer shape (DB$ DamageAll | ValidPlayers$ FlippedHeads). No corpus
// carrier asks inside a win branch, which is why the until-lose loop needs
// this synthetic: it exercises the real engine suspension/resume machinery,
// not a fake Host.
const flipAskerSrc = `Name:Flip Asker
ManaCost:2 R
Types:Creature
PT:1/1
A:AB$ FlipCoin | FlipUntilYouLose$ True | RememberResult$ True | WinSubAbility$ DBWinAsk | LoseSubAbility$ DBLoseHit
SVar:DBWinAsk:DB$ ChoosePlayer
SVar:DBLoseHit:DB$ DamageAll | ValidPlayers$ FlippedHeads | NumDmg$ 1
Oracle:x
`

// TestFlipCoinSuspendedWinBranchKeepsMemoryAcrossResume drives the REAL
// engine through a suspended until-lose loop, activated through the ordinary
// ability-offer path so the resolution runs from the stack (where a real
// FlipCoin always runs): the first flip wins, the win branch's ChoosePlayer
// ask suspends the resolution, the answer's resume walk re-enters effFlipCoin
// through the flip_rest continuation frame, and the losing flip's DamageAll
// reads ValidPlayers$ FlippedHeads. The pre-suspension winner must still be
// in the set: the continuation frame carries the resolution's shared flip
// memory (the pointer Engine.Ask captured), so a nil memory at resume --
// which lazily allocates a fresh one and loses every pre-suspension result --
// is a failure this test observes as the missing damage.
func TestFlipCoinSuspendedWinBranchKeepsMemoryAcrossResume(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	asker := card(t, flipAskerSrc)
	run := func(seed uint64) (flips []effects.FlipResult, hitsOn0 int, suspended bool) {
		e, _ := flipEngine(t, reg, seed, []*cards.Card{asker}, []*cards.Card{})
		src := moveByName(t, e, 0, "Flip Asker", state.ZBattlefield)
		e.G.Obj(src).SummonSick = false
		e.priorityRound() // a fresh priority decision that offers the ability
		flipAbilityOption(t, e, src)
		drainAll(t, e, 200)
		for _, ev := range e.L.Events {
			if ev.Kind == events.Damage && ev.Player == 0 && ev.Amount == 1 {
				hitsOn0++
			}
		}
		flips = flipByPlayer(e)
		// The suspension precondition reads the FIRST flip: only a heads-first
		// game exercised the suspended win branch at all.
		suspended = len(flips) > 0 && flips[0].Heads
		return flips, hitsOn0, suspended
	}

	for seed := uint64(1); seed <= 60; seed++ {
		flips, hits, suspended := run(seed)
		if !suspended {
			continue // first flip was tails: the win branch never asked
		}
		// The shape this test pins: the first flip won (the suspension was a
		// WIN-branch ask) and the next flip lost, so exactly one pre-suspension
		// win exists and the resumed loop's lose branch is the reader.
		if len(flips) != 2 || !flips[0].Heads || flips[1].Heads {
			continue
		}
		if hits != 1 {
			t.Fatalf("seed %d: FlippedHeads damage missed the pre-suspension winner: flips %v, %d hits on seat 0, want exactly 1",
				seed, flips, hits)
		}
		return
	}
	t.Fatal("no seed in 1..60 produced the heads-then-tails suspended shape")
}
