//go:build fuzz

// Fuzz tests are opt-in: build tag `fuzz` (make fuzz). 60 seeded 4-seat games; minutes of CPU; they stay
// out of the default `go test ./...` and every pipeline gate.

package rules

import (
	"fmt"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// seedResult holds the per-seed accumulation of one fuzz game. Each parallel
// subtest writes only its own index, so no mutex or map is needed; the parent
// aggregates in index order afterwards, keeping the output deterministic.
type seedResult struct {
	finished     bool
	events       int
	turns        int
	attackDecls  int
	attackers    int
	blockDecls   int
	blockPairs   int
	playerDamage int
	deckOuts     int
	otherLosses  int
}

// TestInvariantsUnderSeedFuzz is the rules core's acceptance gate: many games,
// every one terminating, invariants intact throughout. This is the test that
// found the stack double-push bug in the design spike.
//
// Ruling T25-b (fix round 1): this used to measure a game that never played
// Magic -- the bot wasted its whole pool tapping mana during the upkeep
// (where it holds priority for the trigger drain), so the pool was empty
// again by main 1 and no creature was ever affordable; combat therefore
// never happened in any of the 60 seeds. With that fixed (sampledecks.go's
// creature now costs one pip, and botDecide/answer only tap mana in a main
// phase), the loop below also asserts, IN AGGREGATE across all 60 seeds,
// that combat actually occurred -- so a future regression that empties the
// gate again fails loudly here instead of silently passing 60 games of
// nothing.
func TestInvariantsUnderSeedFuzz(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	names, decks := testutil.SampleDecks(t, 4)
	results := make([]seedResult, 60)
	// The outer t.Run returns only once every parallel child has completed, so
	// the aggregation below runs after all 60 games are done. Each Engine is
	// independent (New(Config{Seed: seed, ...})), so nothing is shared except
	// names/decks, which are read-only after SampleDecks.
	t.Run("seeds", func(t *testing.T) {
		for seed := uint64(0); seed < 60; seed++ {
			seed := seed
			t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
				t.Parallel()
				e := New(Config{Seed: seed, Names: names, Decks: decks})
				// Ruling P6: e.L.NoHash = true here would panic -- events.Log.NoHash
				// is immutable after the first Append, and New already appended
				// GameStart before returning this Engine. Hashing every event is
				// cheap enough that there is nothing worth trading it away for.
				b := newTestBot(seed * 31)
				e.Advance()
				testutil.CheckInvariants(t, e.G, e.Pending(), "start")
				n := 0
				for !e.G.Over && e.Pending() != nil && n < 200000 {
					// Ruling T25-b: isMain is now answer's own business, computed
					// from the engine's step inside botpolicy.BoardFromGame --
					// the rules package's counterpart to seat.Bot reading v.Phase.
					if err := e.Submit(b.answer(e, e.Pending())); err != nil {
						t.Fatalf("seed %d intent %d: %v", seed, n, err)
					}
					if n%97 == 0 {
						testutil.CheckInvariants(t, e.G, e.Pending(), "mid")
					}
					n++
				}
				// M5: the game is over here (or the seed failed to terminate,
				// reported below), so Pending() is nil by construction and
				// invariants 3, 5 and 7 -- which need a decision -- are inert for
				// this call. Invariants 1, 2, 4 and 6 still run against e.G.
				testutil.CheckInvariants(t, e.G, e.Pending(), "end")
				if !e.G.Over {
					t.Errorf("seed %d did not terminate after %d intents (turn %d)", seed, n, e.G.Turn)
					return
				}
				r := &results[seed]
				r.finished = true
				r.events = len(e.L.Events)
				r.turns = int(e.G.Turn)
				for _, ev := range e.L.Events {
					switch ev.Kind {
					case events.DeclareAttackers:
						r.attackDecls++
						r.attackers += len(ev.IDs)
					case events.DeclareBlockers:
						r.blockDecls++
						r.blockPairs += len(ev.Pairs)
					case events.Damage:
						if ev.Obj == 0 {
							r.playerDamage++ // Obj == 0: this hit a player, not a permanent.
						}
					case events.PlayerLost:
						if ev.Text == "drew from an empty library" {
							r.deckOuts++
						} else {
							r.otherLosses++
						}
					}
				}
			})
		}
	})

	finished, totalEvents := 0, 0
	var attackDecls, attackers, blockDecls, blockPairs, playerDamage, deckOuts, otherLosses int
	turnLengths := make([]int, 0, 60)
	for _, r := range results {
		if r.finished {
			finished++
		}
		totalEvents += r.events
		if r.finished {
			turnLengths = append(turnLengths, r.turns)
		}
		attackDecls += r.attackDecls
		attackers += r.attackers
		blockDecls += r.blockDecls
		blockPairs += r.blockPairs
		playerDamage += r.playerDamage
		deckOuts += r.deckOuts
		otherLosses += r.otherLosses
	}
	if finished != 60 {
		t.Fatalf("%d of 60 seeds finished", finished)
	}

	// I-1 (Ruling T25-b): the gate must not go vacuous silently again. Every
	// one of these four was exactly 0 before the fix.
	if attackers == 0 {
		t.Fatal("0 attackers ever declared across 60 seeds -- combat never happened")
	}
	if blockPairs == 0 {
		t.Fatal("0 blockers ever declared across 60 seeds -- combat never happened")
	}
	if playerDamage == 0 {
		t.Fatal("0 combat-damage-to-a-player events across 60 seeds -- combat never connected")
	}
	if otherLosses == 0 {
		t.Fatalf("all %d eliminations across 60 seeds were deck-outs -- nobody ever died to damage", deckOuts)
	}

	sort.Ints(turnLengths)
	median := turnLengths[len(turnLengths)/2]
	t.Logf("60 seeds finished, %d events, invariants held", totalEvents)
	t.Logf("combat: %d attack declarations (%d attackers total), %d block declarations (%d pairs total), %d player-damage events",
		attackDecls, attackers, blockDecls, blockPairs, playerDamage)
	t.Logf("eliminations: %d deck-out, %d by damage; turn length min=%d median=%d max=%d",
		deckOuts, otherLosses, turnLengths[0], median, turnLengths[len(turnLengths)-1])
}
