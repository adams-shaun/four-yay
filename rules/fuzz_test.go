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

// TestInvariantsUnderSeedFuzz<k> is the rules core's acceptance gate: many
// games, every one terminating, invariants intact throughout. This is the
// test that found the stack double-push bug in the design spike.
//
// Ruling T25-b (fix round 1): this used to measure a game that never played
// Magic -- the bot wasted its whole pool tapping mana during the upkeep
// (where it holds priority for the trigger drain), so the pool was empty
// again by main 1 and no creature was ever affordable; combat therefore
// never happened in any of the 60 seeds. With that fixed (sampledecks.go's
// creature now costs one pip, and botDecide/answer only tap mana in a main
// phase), the loop below also asserts, IN AGGREGATE, that combat actually
// occurred -- so a future regression that empties the gate again fails
// loudly here instead of silently passing games of nothing.
//
// The 60 seeds are sharded into seedFuzzChunks independent tests of
// seedFuzzPerChunk seeds each (the operator's per-test budget, 2026-10-05:
// 2 GB, 2 vCPU, 1 min); chunk k plays seeds [k*seedFuzzPerChunk,
// (k+1)*seedFuzzPerChunk) and holds the combat aggregates over its own
// seeds, a stricter bar than the unsharded 60-seed aggregate.
func TestInvariantsUnderSeedFuzz0(t *testing.T)  { seedFuzzChunk(t, 0) }
func TestInvariantsUnderSeedFuzz1(t *testing.T)  { seedFuzzChunk(t, 1) }
func TestInvariantsUnderSeedFuzz2(t *testing.T)  { seedFuzzChunk(t, 2) }
func TestInvariantsUnderSeedFuzz3(t *testing.T)  { seedFuzzChunk(t, 3) }
func TestInvariantsUnderSeedFuzz4(t *testing.T)  { seedFuzzChunk(t, 4) }
func TestInvariantsUnderSeedFuzz5(t *testing.T)  { seedFuzzChunk(t, 5) }
func TestInvariantsUnderSeedFuzz6(t *testing.T)  { seedFuzzChunk(t, 6) }
func TestInvariantsUnderSeedFuzz7(t *testing.T)  { seedFuzzChunk(t, 7) }
func TestInvariantsUnderSeedFuzz8(t *testing.T)  { seedFuzzChunk(t, 8) }
func TestInvariantsUnderSeedFuzz9(t *testing.T)  { seedFuzzChunk(t, 9) }
func TestInvariantsUnderSeedFuzz10(t *testing.T) { seedFuzzChunk(t, 10) }
func TestInvariantsUnderSeedFuzz11(t *testing.T) { seedFuzzChunk(t, 11) }

// seedFuzzChunks chunks of seedFuzzPerChunk seeds: the 60 seeds.
const (
	seedFuzzChunks   = 12
	seedFuzzPerChunk = 5
)

func seedFuzzChunk(t *testing.T, k int) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	names, decks := testutil.SampleDecks(t, 4)
	first := uint64(k * seedFuzzPerChunk)
	results := make([]seedResult, seedFuzzPerChunk)
	// The outer t.Run returns only once every parallel child has completed, so
	// the aggregation below runs after all the chunk's games are done. Each
	// Engine is independent (New(Config{Seed: seed, ...})), so nothing is
	// shared except names/decks, which are read-only after SampleDecks.
	t.Run("seeds", func(t *testing.T) {
		for seed := first; seed < first+seedFuzzPerChunk; seed++ {
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
				r := &results[seed-first]
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
	turnLengths := make([]int, 0, seedFuzzPerChunk)
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
	if finished != seedFuzzPerChunk {
		t.Fatalf("%d of %d seeds [%d,%d) finished", finished, seedFuzzPerChunk, first, first+seedFuzzPerChunk)
	}

	// I-1 (Ruling T25-b): the gate must not go vacuous silently again. Every
	// one of these four was exactly 0 before the fix.
	if attackers == 0 {
		t.Fatal("0 attackers ever declared across the chunk's seeds -- combat never happened")
	}
	if blockPairs == 0 {
		t.Fatal("0 blockers ever declared across the chunk's seeds -- combat never happened")
	}
	if playerDamage == 0 {
		t.Fatal("0 combat-damage-to-a-player events across the chunk's seeds -- combat never connected")
	}
	if otherLosses == 0 {
		t.Fatalf("all %d eliminations across the chunk's seeds were deck-outs -- nobody ever died to damage", deckOuts)
	}

	sort.Ints(turnLengths)
	median := turnLengths[len(turnLengths)/2]
	t.Logf("seeds [%d,%d) finished, %d events, invariants held", first, first+seedFuzzPerChunk, totalEvents)
	t.Logf("combat: %d attack declarations (%d attackers total), %d block declarations (%d pairs total), %d player-damage events",
		attackDecls, attackers, blockDecls, blockPairs, playerDamage)
	t.Logf("eliminations: %d deck-out, %d by damage; turn length min=%d median=%d max=%d",
		deckOuts, otherLosses, turnLengths[0], median, turnLengths[len(turnLengths)-1])
}
