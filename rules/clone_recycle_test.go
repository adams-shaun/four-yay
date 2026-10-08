package rules

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestCloneWithRecyclesEngineStruct pins the heap-object POC cut that reuses a
// spent engine's own struct as the target of the next clone (Spare.engine):
// cloneWith must return that exact pointer (the allocation is what the cut
// removes), and a clone built on a struct carrying a *different* engine's
// later play must still be byte-identical to a plain Clone. The recycled
// struct is fully zeroed (*c = Engine{}) before cloneWith runs the same field
// assignments it runs for a fresh struct, so no stale field can survive --
// this test poisons the struct with donor state and asserts the sims agree.
func TestCloneWithRecyclesEngineStruct(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cfg := Config{Seed: 90000000, Names: []string{"uw-tempo", "mono-blue-tempo"},
		Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, "uw-tempo"), testutil.RepoDeck(t, reg, "mono-blue-tempo")},
		Tokens: reg.Tokens}
	root := New(cfg)
	root.Advance()
	bot := newTestBot(7)
	for n := 0; !root.G.Over && root.Pending() != nil && root.G.Turn < 5; n++ {
		if err := root.Submit(bot.answer(root, root.Pending())); err != nil {
			t.Fatal(err)
		}
	}
	if root.G.Over || root.Pending() == nil {
		t.Fatal("root game ended before turn 5")
	}
	// donor is a second engine played further than the root, so its struct
	// holds state (its own G, L, rng, watcher, caches) that the root's clone
	// must not inherit.
	donor := root.Clone()
	for n := 0; n < 30 && !donor.G.Over && donor.Pending() != nil; n++ {
		if err := donor.Submit(bot.answer(donor, donor.Pending())); err != nil {
			t.Fatal(err)
		}
	}
	play := func(c *Engine, steps int) (string, int, int) {
		r := rand.New(rand.NewPCG(1, 2))
		for s := 0; s < steps && !c.G.Over && c.Pending() != nil; s++ {
			d := c.Pending()
			if err := c.Submit(botpolicy.Decide(botpolicy.BoardFromGame(c.G, c, d.Player), d, r)); err != nil {
				t.Fatalf("step %d: %v", s, err)
			}
		}
		return c.L.Head(), len(c.L.Events), len(c.L.Intents)
	}
	for _, steps := range []int{0, 5, 40, 120} {
		fresh := root.Clone()
		recycled := root.cloneWith(Spare{engine: donor})
		if recycled != donor {
			t.Fatalf("steps %d: cloneWith did not reuse the spare's engine struct", steps)
		}
		if recycled == fresh {
			t.Fatalf("steps %d: the fresh and recycled clones are the same pointer", steps)
		}
		fh, fe, fi := play(fresh, steps)
		rh, re, ri := play(recycled, steps)
		if fh != rh || fe != re || fi != ri {
			t.Fatalf("steps %d: recycled clone %s/%d/%d, fresh %s/%d/%d", steps, rh, re, ri, fh, fe, fi)
		}
	}
}
