package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestSpareReuseIsInvisible pins Config.Spare's contract: a game built on a
// finished game's recycled storage (Engine.Release) plays byte-identically to
// the same game built fresh -- same chain head, same event and intent count
// -- whichever game the spare came from, and the spare is consumed by New.
func TestSpareReuseIsInvisible(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	all := testutil.LegacyDeckNames()
	cfgFor := func(seed uint64) Config {
		names := []string{all[int(seed)%len(all)], all[(int(seed)+1)%len(all)]}
		decks := [][]*cards.Card{testutil.RepoDeck(t, reg, names[0]), testutil.RepoDeck(t, reg, names[1])}
		return Config{Seed: seed, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Cards}
	}
	play := func(cfg Config) *Engine {
		e := New(cfg)
		b := newTestBot(cfg.Seed)
		e.Advance()
		for n := 0; !e.G.Over && e.Pending() != nil && n < 20000; n++ {
			if err := e.Submit(b.answer(e, e.Pending())); err != nil {
				t.Fatalf("seed %d, intent %d: %v", cfg.Seed, n, err)
			}
		}
		return e
	}
	type result struct {
		head            string
		events, intents int
	}
	summary := func(e *Engine) result {
		return result{head: e.L.Head(), events: len(e.L.Events), intents: len(e.L.Intents)}
	}
	seeds := []uint64{3, 4, 5}
	fresh := make(map[uint64]result, len(seeds))
	for _, s := range seeds {
		fresh[s] = summary(play(cfgFor(s)))
	}
	// Chain the spare through every game, so each reuses the previous (a
	// different game's) storage.
	spare := new(Spare)
	for round := 0; round < 2; round++ {
		for _, s := range seeds {
			cfg := cfgFor(s)
			cfg.Spare = spare
			e := play(cfg)
			if cfg.Spare.events != nil || cfg.Spare.objs != nil {
				t.Fatalf("seed %d: New did not consume the spare", s)
			}
			if got := summary(e); got != fresh[s] {
				t.Fatalf("seed %d round %d: with spare %+v, fresh %+v", s, round, got, fresh[s])
			}
			*spare = e.Release()
			if e.L.Events != nil || e.G.Objs != nil || e.L.Intents != nil {
				t.Fatalf("seed %d: Release left the engine's arrays in place", s)
			}
			// Release recycles the event array lazily: past evDirty it is
			// zero, and the consumer's clearDirty zeroes the spent history
			// (New's, below) so nothing of the finished game stays pinned.
			for i := spare.evDirty; i < len(spare.events); i++ {
				if spare.events[i].Text != "" || spare.events[i].IDs != nil {
					t.Fatalf("seed %d: event %d past the spent history is not zero", s, i)
				}
			}
			cleared := *spare
			cleared.clearDirty()
			for i := range cleared.events {
				if cleared.events[i].Text != "" || cleared.events[i].IDs != nil {
					t.Fatalf("seed %d: clearDirty did not clear event %d", s, i)
				}
			}
			for i := range cleared.objs {
				if cleared.objs[i].Card != nil || cleared.objs[i].ID != 0 {
					t.Fatalf("seed %d: clearDirty did not clear object %d", s, i)
				}
			}
		}
	}
}

// TestDecisionMadeTextMatchesSprintf pins the DecisionMade text to the
// fmt.Sprintf("%s:%v") form it replaced (the text is hash-chained).
func TestDecisionMadeTextMatchesSprintf(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		kind    decision.Kind
		choices []int
	}{
		{decision.KPriority, nil},
		{decision.KPriority, []int{}},
		{decision.KPriority, []int{0}},
		{decision.KTarget, []int{3, 17, 0}},
		{decision.KChoose, []int{-1, 1234567, 42}},
		{"", []int{9}},
	} {
		if got, want := decisionMadeText(c.kind, c.choices), fmt.Sprintf("%s:%v", c.kind, c.choices); got != want {
			t.Fatalf("decisionMadeText(%q, %v) = %q, want %q", c.kind, c.choices, got, want)
		}
	}
}
