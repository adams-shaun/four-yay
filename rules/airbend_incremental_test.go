package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// airbendGameDeck is a white airbend deck: every corpus airbend carrier this
// colour plays, two copies each, over Plains and Grizzly-Bears-sized filler,
// so a bot game airbends permanents into exile, recasts some and leaves
// others there -- every branch of the permission (granted, kept, ended by a
// later move, never granted).
func airbendGameDeck(t testing.TB, reg *cards.Registry) []*cards.Card {
	t.Helper()
	lookup := func(name string) *cards.Card {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus card %q missing", name)
		}
		return c
	}
	plains := lookup("Plains")
	bear := lookup("Grizzly Bears")
	var deck []*cards.Card
	for _, name := range []string{"Airbending Lesson", "Airbender's Reversal", "Airbender Ascension",
		"Aang, Airbending Master", "Monk Gyatso", "Avatar's Wrath", "Appa, Steadfast Guardian", "Glider Staff"} {
		c := lookup(name)
		deck = append(deck, c, c)
	}
	for i := 0; i < 17; i++ {
		deck = append(deck, plains)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	return deck
}

// airbendIndexMatchesScan fails t unless the engine's incremental index (as
// airbendCastAvailable reads it) equals a from-scratch rebuild for every
// object id the game has minted, and the literal backward scan for every
// card in an exile zone (the only ids the offer walk asks about).
func airbendIndexMatchesScan(t *testing.T, e *Engine, where string) (granted int) {
	t.Helper()
	ix := e.airbendIndex()
	full := buildAirbendExileIndex(e.L.Events)
	for id := state.ObjID(1); id < e.G.NextID; id++ {
		if got, rebuilt := ix.available(id), full.available(id); got != rebuilt {
			t.Fatalf("%s: obj %d at log %d: incremental %v, rebuild %v", where, id, len(e.L.Events), got, rebuilt)
		}
	}
	for p := range e.G.Players {
		for _, id := range e.G.Zone(state.ZExile, state.PlayerID(p)) {
			got, scan := ix.available(id), airbendScan(e.L.Events, id)
			if got != scan {
				t.Fatalf("%s: exiled obj %d at log %d: incremental %v, scan %v", where, id, len(e.L.Events), got, scan)
			}
			if got {
				granted++
			}
		}
	}
	return granted
}

// TestAirbendIncrementalIndexMatchesRebuildEveryDecision plays whole bot
// games of the airbend deck against itself and, at EVERY decision, checks the
// incremental airbend index against a full rebuild for every object and the
// literal scan for every exiled card; every sixteenth decision it also forks
// a clone (which carries the index and its watermark) and plays the clone on
// for a stretch under the same check, so a clone's shared copy-on-write words and the original's
// later folds are both exercised. It requires the permission to have been
// granted at some decision, so the equivalence is not vacuous.
func TestAirbendIncrementalIndexMatchesRebuildEveryDecision(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deck := airbendGameDeck(t, reg)
	grantedSeen, decisions := 0, 0
	for seed := uint64(1); seed <= 4; seed++ {
		e := New(Config{Seed: 7100 + seed, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{deck, deck}, Tokens: reg.Tokens})
		b := newTestBot(seed)
		e.Advance()
		for n := 0; !e.G.Over && e.Pending() != nil && n < 4000; n++ {
			grantedSeen += airbendIndexMatchesScan(t, e, "game")
			decisions++
			if n%16 == 0 {
				c := e.Clone()
				cb := newTestBot(seed*1000 + uint64(n))
				for k := 0; k < 40 && !c.G.Over && c.Pending() != nil; k++ {
					grantedSeen += airbendIndexMatchesScan(t, c, "clone")
					if err := c.Submit(cb.answer(c, c.Pending())); err != nil {
						t.Fatalf("seed %d clone at %d step %d: %v", seed, n, k, err)
					}
				}
				// The fork's folds must not have disturbed the original.
				airbendIndexMatchesScan(t, e, "original after clone")
			}
			if err := e.Submit(b.answer(e, e.Pending())); err != nil {
				t.Fatalf("seed %d intent %d: %v", seed, n, err)
			}
		}
	}
	if decisions < 500 {
		t.Fatalf("precondition: only %d decisions checked", decisions)
	}
	if grantedSeen == 0 {
		t.Fatal("precondition: no decision ever saw an airbend permission granted; the deck does not exercise the index")
	}
	t.Logf("%d decisions, %d granted-permission observations", decisions, grantedSeen)
}

// BenchmarkAirbendPermission prices one offer walk's airbend reads on a
// late-game airbend board: "incremental" is what the walk pays (bring the
// engine's index to the log head, answer every exiled card), "rebuild" a
// from-scratch fold of the whole log per walk.
func BenchmarkAirbendPermission(b *testing.B) {
	reg := testutil.CorpusRegistry(b)
	deck := airbendGameDeck(b, reg)
	e := New(Config{Seed: 7101, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, deck}, Tokens: reg.Tokens})
	bot := newTestBot(1)
	e.Advance()
	for n := 0; !e.G.Over && e.Pending() != nil && n < 1500; n++ {
		if err := e.Submit(bot.answer(e, e.Pending())); err != nil {
			b.Fatal(err)
		}
	}
	var exiled []state.ObjID
	for p := range e.G.Players {
		exiled = append(exiled, e.G.Zone(state.ZExile, state.PlayerID(p))...)
	}
	b.Logf("log %d events, %d exiled cards", len(e.L.Events), len(exiled))
	b.Run("incremental", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, id := range exiled {
				_ = e.airbendIndex().available(id)
			}
		}
	})
	b.Run("rebuild", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ix := buildAirbendExileIndex(e.L.Events)
			for _, id := range exiled {
				_ = ix.available(id)
			}
		}
	})
}
