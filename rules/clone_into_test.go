package rules

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestCloneIntoIsInvisible pins CloneInto's contract, the search-loop form of
// Clone: a simulation played on a clone built from a spent clone's recycled
// arrays (Release) is byte-identical to the same simulation on a plain Clone
// -- same chain head, event and intent counts -- and releasing every clone,
// including one released before it ever appended (its Events are still the
// root's shared prefix), never disturbs the root.
func TestCloneIntoIsInvisible(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cfg := Config{Seed: 90000000, Names: []string{"uw-tempo", "mono-blue-tempo"},
		Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, "uw-tempo"), testutil.RepoDeck(t, reg, "mono-blue-tempo")},
		Tokens: reg.Tokens}
	root := New(cfg)
	root.Advance()
	bot := newTestBot(7)
	for n := 0; !root.G.Over && root.Pending() != nil && root.G.Turn < 6; n++ {
		if err := root.Submit(bot.answer(root, root.Pending())); err != nil {
			t.Fatal(err)
		}
	}
	if root.G.Over || root.Pending() == nil {
		t.Fatal("root game ended before turn 6")
	}
	rootHead, rootEvents, rootIntents := root.L.Head(), len(root.L.Events), len(root.L.Intents)

	type result struct {
		head            string
		events, intents int
		objs            int
	}
	sim := func(c *Engine, i int, steps int) result {
		t.Helper()
		r := rand.New(rand.NewPCG(uint64(i), 1))
		for s := 0; s < steps && !c.G.Over && c.Pending() != nil; s++ {
			d := c.Pending()
			if err := c.Submit(botpolicy.Decide(botpolicy.BoardFromGame(c.G, c, d.Player), d, r)); err != nil {
				t.Fatalf("sim %d step %d: %v", i, s, err)
			}
		}
		if got, want := c.L.HeadAt(len(c.L.Events)), c.L.Head(); got != want {
			t.Fatalf("sim %d: chain desynced: HeadAt %s, Head %s", i, got, want)
		}
		return result{c.L.Head(), len(c.L.Events), len(c.L.Intents), len(c.G.Objs)}
	}
	// Step counts vary so a spare comes back both grown and ungrown. The
	// leading 0 builds its clone from an empty spare, so it shares the
	// root's prefix, and releases it before any append: the case Release
	// must not recycle (clearing it would zero the root's history).
	steps := []int{0, 40, 0, 5, 120, 0, 40, 400, 3}
	spare := new(Spare)
	if sp := root.Clone().Release(); sp.events != nil || sp.intents != nil {
		t.Fatal("Release recycled an unappended clone's shared log prefix")
	}
	for i, n := range steps {
		want := sim(root.Clone(), i, n)
		c := root.CloneInto(spare)
		if spare.events != nil || spare.objs != nil || spare.intents != nil {
			t.Fatalf("sim %d: CloneInto did not consume the spare", i)
		}
		if got := sim(c, i, n); got != want {
			t.Fatalf("sim %d (%d steps): CloneInto %+v, Clone %+v", i, n, got, want)
		}
		*spare = c.Release()
		if root.L.Head() != rootHead || len(root.L.Events) != rootEvents || len(root.L.Intents) != rootIntents ||
			root.L.HeadAt(rootEvents) != rootHead {
			t.Fatalf("sim %d: releasing a clone disturbed the root", i)
		}
	}
	// The hypothetical form draws from the spare the same way.
	h := root.CloneHypotheticalInto(5, spare)
	want := root.CloneHypothetical(5)
	for s := 0; s < 40 && !want.G.Over && want.Pending() != nil; s++ {
		d := want.Pending()
		in := botpolicy.Decide(botpolicy.BoardFromGame(want.G, want, d.Player), d, rand.New(rand.NewPCG(uint64(s), 2)))
		if err := want.SubmitHypothetical(in); err != nil {
			t.Fatal(err)
		}
		if err := h.SubmitHypothetical(in); err != nil {
			t.Fatal(err)
		}
	}
	if h.L.Head() != want.L.Head() {
		t.Fatalf("CloneHypotheticalInto head %s, CloneHypothetical %s", h.L.Head(), want.L.Head())
	}
	// And the root still replays to its recorded head.
	re, err := replayFor(cfg, root.L)
	if err != nil {
		t.Fatal(err)
	}
	if re.L.Head() != rootHead {
		t.Fatalf("root replay head %s, want %s", re.L.Head(), rootHead)
	}
}
