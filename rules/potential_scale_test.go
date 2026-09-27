package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// potentialScaleBoard is seat 0's board for the PotentialMana scaling pins:
// n vanilla creature tokens (no mana ability), one Forest, and one Continuous
// static on the battlefield -- the shape of the cardfuzz hang seed
// 6181111140895991800, where a Krenko doubling left ~8k-16k goblins beside
// ordinary anthem-style statics.
func potentialScaleBoard(t testing.TB, n int) *Engine {
	t.Helper()
	e := layerEngine(t)
	e.G.SetZone(state.ZHand, 0, nil)
	onBoard(t, e, 0, "Name:Anthem Stand-in\nTypes:Enchantment\n"+
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | Description$ Creatures you control get +1/+0.\nOracle:x\n")
	onBoard(t, e, 0, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")
	for i := 0; i < n; i++ {
		onBoard(t, e, 0, fmt.Sprintf("Name:Goblin %d\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", i))
	}
	return e
}

// TestPotentialManaStaticScanIsNotPerObject pins the cardfuzz hang
// 6181111140895991800 root cause: PotentialMana's fixpoint asks every
// battlefield object for its mana abilities, and that walk reads the board's
// Continuous statics (the AddAbility$ grant scan). Outside a memo scope
// activeStatics rescans the WHOLE battlefield per call, so one PotentialMana
// was O(permanents^2) -- at 8k tokens every priority offer build spent about
// a second there, and the game overran the 90s hang budget. The scan must be
// shared across the objects of one PotentialMana call: the per-call work
// (measured here as allocations, each rescan appending a fresh view slice)
// must not grow with the square of the board.
func TestPotentialManaStaticScanIsNotPerObject(t *testing.T) {
	allocs := func(n int) float64 {
		e := potentialScaleBoard(t, n)
		want := e.PotentialMana(0)
		if want[state.MG] != 1 {
			t.Fatalf("n=%d: potential = %v, want the Forest's {G}", n, want)
		}
		// The first call above ran in the test binary's walk-cache verify
		// mode (every cache hit recomputed and compared); the budget below
		// measures the production path, so verify is off for it.
		return allocsWithoutWalkCacheVerify(5, func() {
			if got := e.PotentialMana(0); got != want {
				t.Fatalf("n=%d: PotentialMana not stable: %v vs %v", n, got, want)
			}
		})
	}
	small, large := allocs(50), allocs(400)
	// Eight times the objects. A per-object whole-board rescan allocates at
	// least one view slice per object per fixpoint pass (>= 8x growth); a
	// shared scan leaves only the constant per-call work.
	if large-small > 50 {
		t.Fatalf("PotentialMana allocations grow with the board: %.0f at 50 tokens, %.0f at 400 -- the static scan is per object", small, large)
	}
}

// TestWindowManaWalksStaticScanIsNotPerObject is the same pin for the
// payment-window membership walks the hang's profile charged next:
// windowManaUnits (the unless/attack/payment-plan unit census) and
// attackManaSources (the attack-tax tap list) each ask every battlefield
// permanent for its window mana abilities, one whole-board Continuous-static
// rescan apiece outside a memo scope.
func TestWindowManaWalksStaticScanIsNotPerObject(t *testing.T) {
	for _, tc := range []struct {
		name string
		walk func(e *Engine) int
	}{
		{"windowManaUnits", func(e *Engine) int { return len(e.windowManaUnits(0)) }},
		{"attackManaSources", func(e *Engine) int { return len(e.attackManaSources(0)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allocs := func(n int) float64 {
				e := potentialScaleBoard(t, n)
				want := tc.walk(e) // verify mode: every cache hit recomputed
				if want != 1 {
					t.Fatalf("n=%d: %s found %d units, want the Forest's one", n, tc.name, want)
				}
				return allocsWithoutWalkCacheVerify(5, func() {
					if got := tc.walk(e); got != want {
						t.Fatalf("n=%d: %s not stable: %d vs %d", n, tc.name, got, want)
					}
				})
			}
			small, large := allocs(50), allocs(400)
			if large-small > 50 {
				t.Fatalf("%s allocations grow with the board: %.0f at 50 tokens, %.0f at 400 -- the static scan is per object", tc.name, small, large)
			}
		})
	}
}

func BenchmarkPotentialManaLargeBoard(b *testing.B) {
	for _, n := range []int{500, 2000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			e := potentialScaleBoard(b, n)
			benchWithoutVerify(b)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = e.PotentialMana(0)
			}
		})
	}
}

// benchWithoutVerify turns the rules test binary's verify modes off for the
// rest of a benchmark (they recompute every cache hit and fast path, which is
// exactly the work the benchmarks measure the removal of), restoring them at
// cleanup. Benchmarks run sequentially, never beside a Parallel test.
func benchWithoutVerify(b *testing.B) {
	prevMemo, prevWalk, prevL4, prevInert := derivedMemoVerify, walkCacheVerify, layer4PrecheckVerify, layerInertVerify
	derivedMemoVerify, walkCacheVerify, layer4PrecheckVerify, layerInertVerify = false, false, false, false
	b.Cleanup(func() {
		derivedMemoVerify, walkCacheVerify, layer4PrecheckVerify, layerInertVerify = prevMemo, prevWalk, prevL4, prevInert
	})
}
