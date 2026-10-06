package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// playBotGame plays one game of the harness bot to its end and returns the
// engine, so the test can read its log without importing a higher tier.
func playBotGame(t *testing.T, cfg Config) *Engine {
	t.Helper()
	e := New(cfg)
	b := newTestBot(cfg.Seed)
	e.Advance()
	for n := 0; !e.G.Over && e.Pending() != nil && n < 20000; n++ {
		if err := e.Submit(b.answer(e, e.Pending())); err != nil {
			t.Fatalf("seed %d, intent %d: %v", cfg.Seed, n, err)
		}
	}
	if !e.G.Over {
		t.Fatalf("seed %d: game did not finish", cfg.Seed)
	}
	return e
}

// TestConfigExpectedEventsPresizesTheLog pins the Config-to-log plumbing of
// the expected-size hint: a positive ExpectedEvents preallocates the engine's
// event log to at least that size, the whole game runs inside the reservation
// (the backing array never regrows past it), and the hint changes nothing a
// reader sees -- a hinted game and an unhinted game of the same seed have the
// same events and chain head.
func TestConfigExpectedEventsPresizesTheLog(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	all := testutil.LegacyDeckNames()
	name := all[0]
	deck := testutil.RepoDeck(t, reg, name)
	cfg := Config{Seed: 41, Names: []string{name}, Decks: [][]*cards.Card{deck}, Tokens: reg.Tokens}

	const hint = 24000
	hinted := cfg
	hinted.ExpectedEvents = hint
	e := playBotGame(t, hinted)
	if cap(e.L.Events) != hint {
		t.Fatalf("hinted game log cap = %d, want exactly %d (a regrow would have raised it)", cap(e.L.Events), hint)
	}

	plain := playBotGame(t, cfg)
	if cap(plain.L.Events) >= hint {
		t.Fatalf("precondition: the unhinted game's log grew to %d on its own; the hint proves nothing", cap(plain.L.Events))
	}
	if e.L.Head() != plain.L.Head() || len(e.L.Events) != len(plain.L.Events) {
		t.Fatalf("the hint changed the game: head %s/%s len %d/%d",
			e.L.Head(), plain.L.Head(), len(e.L.Events), len(plain.L.Events))
	}
}

// TestRecycledSpareRetainsCapacityThroughGenesis pins option 1 at the engine
// boundary: a finished game's spent event array keeps its grown capacity when
// the next game is built on it, even with no ExpectedEvents hint. The second
// engine's log starts at the spare's capacity, so the long-game regrowth the
// batch runner would otherwise repay never happens.
func TestRecycledSpareRetainsCapacityThroughGenesis(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	all := testutil.LegacyDeckNames()
	name := all[0]
	deck := testutil.RepoDeck(t, reg, name)
	// Grow the first game's log well past the default preallocation with a
	// hint, then release its array.
	const grown = 24000
	first := Config{Seed: 42, Names: []string{name}, Decks: [][]*cards.Card{deck}, Tokens: reg.Tokens, ExpectedEvents: grown}
	e1 := playBotGame(t, first)
	if cap(e1.L.Events) != grown {
		t.Fatalf("precondition: first log cap = %d, want %d", cap(e1.L.Events), grown)
	}
	spare := new(Spare)
	*spare = e1.Release()
	if got := cap(spare.events); got != grown {
		t.Fatalf("precondition: released spare cap = %d, want %d", got, grown)
	}

	// The next game asks for nothing special: the spare's grown capacity alone
	// must carry over.
	second := Config{Seed: 42, Names: []string{name}, Decks: [][]*cards.Card{deck}, Tokens: reg.Tokens, Spare: spare}
	e2 := New(second)
	if got := cap(e2.L.Events); got != grown {
		t.Fatalf("recycled engine log cap = %d, want the spare's %d", got, grown)
	}
}
