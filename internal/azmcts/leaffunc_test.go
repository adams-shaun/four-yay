package azmcts

import (
	"math"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// TestLeafFuncNilIsTheOldSearch is the seam's off path: Options.Leaf nil must
// give the Result the package gave before the field existed. The existing
// determinism and node-cache tests pin that against their own goldens
// (unmodified); this one pins the seam's two paths against each other -- a
// Leaf that IS the heuristic leaf gives a byte-identical Result to nil -- so
// the seam adds nothing to a search but the function call.
func TestLeafFuncNilIsTheOldSearch(t *testing.T) {
	cfg := testConfig(t, "uw-tempo", "mono-red-prowess", testSeed)
	for _, kind := range []decision.Kind{decision.KPriority, ""} {
		e, d, bot := botPosition(t, cfg, kind, 5, 4000)
		opts := DefaultOptions()
		opts.Sims, opts.Seed = 40, 7
		off := searchAt(t, e, d, bot, nil, opts)
		opts.Leaf = heuristicLeafValue
		on := searchAt(t, e, d, bot, nil, opts)
		if !reflect.DeepEqual(off, on) {
			t.Fatalf("%q: a Leaf equal to the heuristic leaf changed the Result:\noff %+v\non  %+v", kind, off, on)
		}
		if off.Stats.Completed != 40 {
			t.Fatalf("%q: %d of 40 simulations completed", kind, off.Stats.Completed)
		}
	}
}

// TestLeafFuncReplacesTheLeaf: with Options.Leaf set every non-terminal leaf
// (the root's own evaluation included) is the function's value for the
// searching seat, clamped into [0,1] with NaN read as 0.5, and neither the
// heuristic nor a network is consulted.
func TestLeafFuncReplacesTheLeaf(t *testing.T) {
	cfg := testConfig(t, "uw-tempo", "mono-red-prowess", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 3, 4000)
	for _, tc := range []struct {
		name string
		ret  float64
		want float64
	}{{"constant", 0.7, 0.7}, {"above one", 3, 1}, {"below zero", -2, 0}, {"NaN", math.NaN(), 0.5}} {
		calls := 0
		opts := DefaultOptions()
		opts.Sims, opts.Seed = 30, 7
		opts.Leaf = func(w *rules.Engine, actor state.PlayerID) float64 {
			calls++
			if actor != d.Player {
				t.Fatalf("leaf asked for seat %d, the searching seat is %d", actor, d.Player)
			}
			if w == e {
				t.Fatal("leaf was handed the real engine, not a world")
			}
			return tc.ret
		}
		res := searchAt(t, e, d, bot, nil, opts)
		if res.Stats.Completed != 30 {
			t.Fatalf("%s: %d of 30 simulations completed (%+v)", tc.name, res.Stats.Completed, res.Stats)
		}
		// Every non-terminal value is tc.want; a terminal leaf is 0, 0.5 or 1.
		if res.Stats.Terminal == 0 && math.Abs(res.RootValue-tc.want) > 1e-12 {
			t.Fatalf("%s: root value %v, want %v", tc.name, res.RootValue, tc.want)
		}
		if want := 1 + res.Stats.Completed - res.Stats.Terminal; calls != want {
			t.Fatalf("%s: %d leaf calls, want %d (the root plus one per non-terminal simulation)", tc.name, calls, want)
		}
	}
}
