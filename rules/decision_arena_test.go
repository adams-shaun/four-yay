package rules

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// arenaGame is a repo-deck game positioned at its first priority decision.
func arenaGame(t *testing.T) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := New(Config{Seed: 4242, Names: []string{"uw-tempo", "mono-red-prowess"},
		Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, "uw-tempo"), testutil.RepoDeck(t, reg, "mono-red-prowess")},
		Tokens: reg.Tokens})
	e.Advance()
	return e
}

// playArena plays n bot intents on e, returning every posed priority
// decision and a detached copy of its Options taken when it was posed.
func playArena(t *testing.T, e *Engine, n int) ([]*decision.Decision, [][]decision.Option) {
	t.Helper()
	bot := newTestBot(3)
	var ds []*decision.Decision
	var copies [][]decision.Option
	for i := 0; i < n && !e.G.Over && e.Pending() != nil; i++ {
		d := e.Pending()
		if d.Kind == decision.KPriority {
			ds = append(ds, d)
			copies = append(copies, append([]decision.Option(nil), d.Options...))
		}
		if err := e.Submit(bot.answer(e, d)); err != nil {
			t.Fatal(err)
		}
	}
	return ds, copies
}

// TestDecisionArenaIsInvisible pins the arena's contract: a game played
// with it on is event-for-event the game played with it off; every
// decision it posed keeps its Options intact for the engine's whole life
// (no later decision overwrites an earlier one's slots, and each Options
// slice is capped at its length); and after Release the next clone's arena
// reuses the cleared chunks.
func TestDecisionArenaIsInvisible(t *testing.T) {
	off, on := arenaGame(t), arenaGame(t)
	root := on.Clone()
	on.SetDecisionArena(true)
	_, _ = playArena(t, off, 600)
	ds, copies := playArena(t, on, 600)
	if off.L.Head() != on.L.Head() || len(off.L.Events) != len(on.L.Events) {
		t.Fatalf("arena game diverged: head %s vs %s", on.L.Head(), off.L.Head())
	}
	if len(ds) < 50 {
		t.Fatalf("precondition: only %d priority decisions", len(ds))
	}
	for i, d := range ds {
		if cap(d.Options) != len(d.Options) {
			t.Fatalf("decision %d: Options cap %d > len %d (an append would write a neighbour)", i, cap(d.Options), len(d.Options))
		}
		if !reflect.DeepEqual(d.Options, copies[i]) {
			t.Fatalf("decision %d's Options changed after it was answered", i)
		}
	}
	first := unsafe.Pointer(&on.decArena.opts.chunks[0][0])
	sp := on.Release()
	if sp.arena == nil || len(sp.arena.opts.chunks) == 0 || len(sp.arena.decs.chunks) == 0 {
		t.Fatal("Release dropped the decision arena")
	}
	for _, c := range sp.arena.opts.chunks {
		for i := range c {
			if !reflect.DeepEqual(c[i], decision.Option{}) {
				t.Fatal("a released arena chunk was not cleared")
			}
		}
	}
	next := root.CloneInto(&sp)
	next.SetDecisionArena(true)
	nds, _ := playArena(t, next, 50)
	// nds[0] is the clone's pending decision (Clone copies it); the first
	// decision the clone POSES is the arena's first.
	if len(nds) < 2 || nds[0] != root.Pending() && !reflect.DeepEqual(*nds[0], *root.Pending()) {
		t.Fatal("precondition: no posed priority decision on the recycled clone")
	}
	if unsafe.Pointer(&nds[1].Options[0]) != first {
		t.Fatal("the next clone's first decision did not reuse the recycled arena")
	}
	// Off (the default), a clone's decisions never come from its arena.
	plain := root.CloneInto(&Spare{})
	pds, _ := playArena(t, plain, 5)
	if len(pds) > 0 && plain.decArena != nil && len(plain.decArena.opts.chunks) > 0 {
		t.Fatal("an arena-off engine carved decisions from an arena")
	}
}
