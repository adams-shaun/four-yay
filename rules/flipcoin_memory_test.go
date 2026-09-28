package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// flipByPlayer returns the flips recorded in the log as (flipper, heads)
// pairs, in order, so a test can assert WHICH player's coin landed where --
// the per-player memory RememberResult$ records and Defined$ FlippedTails
// reads.
func flipByPlayer(e *Engine) []effects.FlipResult {
	var out []effects.FlipResult
	for _, ev := range e.L.Events {
		if p, win, ok := effects.FlipNoteResult(ev); ok {
			out = append(out, effects.FlipResult{Player: p, Heads: win})
		}
	}
	return out
}

// battlefieldCreatures counts seat p's battlefield creatures.
func battlefieldCreatures(e *Engine, p state.PlayerID) int {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		for _, typ := range o.Face().Types {
			if typ == "Creature" {
				n++
				break
			}
		}
	}
	return n
}

// drainAll drives the stack to empty, answering any mid-resolution ask a
// triggered flip chain poses (the FlippedTails sacrifice picker) with its
// first option and passing priority otherwise. Used by the each-player flip
// leaves whose chain asks (Goblin Assassin's sacrifice).
func drainAll(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 {
				e.priorityRound()
				if e.Pending() == nil && len(e.G.Stack) == 0 {
					return
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
				return
			}
			passOnce(t, e)
		case decision.KChoose, decision.KTarget:
			if len(d.Options) == 0 {
				// A Min 0 "up to" ask: answer with nothing.
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
	t.Fatal("drainAll did not converge")
}

// TestFlipCoinGoblinAssassinFlippedTails is the RememberResult$ /
// Defined$ FlippedTails leaf (real corpus Goblin Assassin): its ETB makes
// EACH player flip a coin (Flipper$ Player), and the chained SubAbility$
// sacrifices a creature for each player whose coin came up tails. Before the
// fix FlippedTails resolved to the EMPTY set, so no one sacrificed.
func TestFlipCoinGoblinAssassinFlippedTails(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(seed uint64) (int, int, []effects.FlipResult, map[state.PlayerID]int) {
		e, _ := flipEngine(t, reg, seed,
			[]*cards.Card{lookup(t, reg, "Goblin Assassin"), lookup(t, reg, "Grizzly Bears")},
			[]*cards.Card{lookup(t, reg, "Grizzly Bears")})
		// Precondition: each seat starts with exactly one creature so a
		// sacrifice is observable as a decrement.
		moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
		moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
		// Goblin Assassin's ETB fires the each-player flip. Capture the
		// battlefield counts with the Assassin already in play, before the
		// trigger resolves, so a sacrifice shows as a decrement.
		moveByName(t, e, 0, "Goblin Assassin", state.ZBattlefield)
		before := map[state.PlayerID]int{0: battlefieldCreatures(e, 0), 1: battlefieldCreatures(e, 1)}
		if before[0] != 2 || before[1] != 1 {
			t.Fatalf("precondition: want Assassin+Bear on seat 0 and a Bear on seat 1, got %v", before)
		}
		e.priorityRound()
		drainAll(t, e, 200)
		return battlefieldCreatures(e, 0), battlefieldCreatures(e, 1), flipByPlayer(e), before
	}

	seeds := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
	sawHeads, sawTails := false, false
	for _, seed := range seeds {
		c0, c1, flips, before := run(seed)
		if len(flips) != 2 {
			t.Fatalf("seed %d: want 2 flips (one per player), got %v", seed, flips)
		}
		for _, fr := range flips {
			if fr.Heads {
				sawHeads = true
			} else {
				sawTails = true
			}
		}
		// Each losing player must have sacrificed exactly one creature; each
		// winning player keeps theirs.
		for _, fr := range flips {
			start := before[fr.Player]
			var got int
			if fr.Player == 0 {
				got = c0
			} else {
				got = c1
			}
			want := start
			if !fr.Heads {
				want = start - 1
			}
			if got != want {
				t.Fatalf("seed %d: player %d (heads=%v) has %d creatures, want %d (before=%d)",
					seed, fr.Player, fr.Heads, got, want, start)
			}
		}
	}
	if !sawTails {
		t.Fatal("precondition: no losing flip in the seed set, so FlippedTails was never exercised")
	}
	if !sawHeads {
		t.Fatal("precondition: no winning flip in the seed set")
	}
}

// TestFlipCoinMutalithForEachPlayer is the ForEachPlayer$ leaf (real corpus
// Mutalith Vortex Beast): "flip a coin for each opponent you have. For each
// flip you lose, deals 3 damage to that player." It flips once per living
// opponent (seat 1), not once total, and the lose branch's Defined$
// Remembered names the opponent whose flip lost.
func TestFlipCoinMutalithForEachPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(seed uint64) ([]effects.FlipResult, int) {
		e, _ := flipEngine(t, reg, seed,
			[]*cards.Card{lookup(t, reg, "Mutalith Vortex Beast")}, []*cards.Card{})
		life1 := e.G.Players[1].Life
		moveByName(t, e, 0, "Mutalith Vortex Beast", state.ZBattlefield)
		e.priorityRound()
		passUntilStackEmpty(t, e, 120)
		hits := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.Damage && ev.Obj == 0 && ev.Player == 1 && ev.Amount == 3 {
				hits++
			}
		}
		_ = life1
		return flipByPlayer(e), hits
	}

	seeds := []uint64{4, 1, 2, 3, 5, 6}
	sawHeads, sawTails := false, false
	for _, seed := range seeds {
		flips, hits := run(seed)
		if len(flips) != 1 {
			t.Fatalf("seed %d: want exactly one flip (one opponent), got %v", seed, flips)
		}
		if flips[0].Player != 1 {
			t.Fatalf("seed %d: the flipper must be the opponent (seat 1), got %v", seed, flips[0])
		}
		if flips[0].Heads {
			sawHeads = true
			if hits != 0 {
				t.Fatalf("seed %d: a winning flip dealt 3 damage %d times, want 0", seed, hits)
			}
		} else {
			sawTails = true
			if hits != 1 {
				t.Fatalf("seed %d: a losing flip must deal exactly one 3-damage hit to the flipper, got %d", seed, hits)
			}
		}
	}
	if !sawHeads || !sawTails {
		t.Fatalf("precondition: seeds did not cover both outcomes (heads=%v tails=%v)", sawHeads, sawTails)
	}
}

// TestFlipCoinCrazedFirecatWinsCounter pins the RememberNumber$ Wins read: a
// FlipUntilYouLose$ loop's win branch puts a +1/+1 counter per flip won
// (CounterNum$ Wins). Before the fix Wins was unbound, so Crazed Firecat
// always entered with zero counters.
func TestFlipCoinCrazedFirecatWinsCounter(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(seed uint64) (int, []bool) {
		e, _ := flipEngine(t, reg, seed,
			[]*cards.Card{lookup(t, reg, "Crazed Firecat")}, []*cards.Card{})
		moveByName(t, e, 0, "Crazed Firecat", state.ZBattlefield)
		id := firstCreature(t, e, 0)
		e.priorityRound()
		passUntilStackEmpty(t, e, 120)
		notes := flipNotes(e)
		return int(e.G.Obj(id).Counter("P1P1")), notes
	}

	// Seed 21: three heads then a tail. Seed 1: tails first.
	multi, multiFlips := run(21)
	if len(multiFlips) != 4 {
		t.Fatalf("seed 21 precondition: want 4 flips, got %v", multiFlips)
	}
	wins := 0
	for _, w := range multiFlips {
		if w {
			wins++
		}
	}
	if wins == 0 {
		t.Fatal("seed 21 precondition: expected at least one win")
	}
	if multi != wins {
		t.Fatalf("seed 21: Crazed Firecat has %d +1/+1 counters, want one per win (%d)", multi, wins)
	}
	zero, zeroFlips := run(1)
	if len(zeroFlips) != 1 || zeroFlips[0] {
		t.Fatalf("seed 1 precondition: want one losing flip, got %v", zeroFlips)
	}
	if zero != 0 {
		t.Fatalf("seed 1: losing opener left %d counters, want 0", zero)
	}
}
