package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestSpareRecycledGameIsLiveArena pins enableLiveArena: a game New builds on
// a Spare (enginebench / internal/bench recycling) must carry the arena ON in
// live mode, not the switched-off arena adoptArena leaves a clone, and its
// arena must reuse the finished game's chunks.
func TestSpareRecycledGameIsLiveArena(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg := Config{Seed: 4242, Names: []string{"uw-tempo", "mono-red-prowess"},
		Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, "uw-tempo"), testutil.RepoDeck(t, reg, "mono-red-prowess")},
		Tokens: reg.Tokens}
	first := New(cfg)
	first.Advance()
	bot := newTestBot(3)
	for i := 0; i < 50 && !first.G.Over && first.Pending() != nil; i++ {
		if err := first.Submit(bot.answer(first, first.Pending())); err != nil {
			t.Fatal(err)
		}
	}
	a0 := first.activeArena()
	if a0 == nil || len(a0.gens[0].opts.chunks) == 0 {
		t.Fatal("precondition: the first game carved no arena chunks")
	}
	chunk := &a0.gens[0].opts.chunks[0][0]
	spare := first.Release()
	if spare.arena == nil {
		t.Fatal("precondition: Release returned no arena in the Spare")
	}
	cfg.Spare = &spare
	second := New(cfg)
	a := second.activeArena()
	if a == nil {
		t.Fatal("a game built on a Spare has its arena off")
	}
	if !a.live || a.res {
		t.Fatalf("a recycled live game's arena is live=%v res=%v, want live and no resolution slabs", a.live, a.res)
	}
	if len(a.gens[0].opts.chunks) == 0 || &a.gens[0].opts.chunks[0][0] != chunk {
		t.Fatal("the recycled game did not reuse the finished game's option chunk")
	}
}
