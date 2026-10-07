package rules

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// knownUnsupported is the M1 coverage RATCHET (Ruling P12/D2-a): the exact
// card -> missing-primitive set TestEveryRepoDeckIsFullySupported measures
// over the repo decks. It lives one file per card,
// testdata/known-unsupported/<slug>.json, {"card": "<Card>", "labels":
// [...]} (spec 2026-10-03-rules-engine-lasagna-design.md W2): a shared
// table made every ticket that retired an entry conflict with every other.
// The test asserts the MEASURED set equals the files EXACTLY, in both
// directions -- a newly-missing card is a regression, a file whose card is
// now fully supported is stale and must be deleted -- so, for a fixed deck
// catalogue, the set only ever shrinks, and only by implementing a real
// primitive (Ruling W2: this project does not grow the "supported" set just
// to make the ratchet green). A newly imported deck can extend it; its
// entries must be measured from the compiled corpus, never marked supported
// without an implementation. Which task retired an entry goes in the commit
// message (git log -- rules/acceptance_test.go holds the history to
// 2026-10-03).
func knownUnsupported(t *testing.T) map[string][]string {
	t.Helper()
	return loadPerCardLabels(t, filepath.Join("testdata", "known-unsupported"))
}

// TestKnownUnsupportedFilesWellFormed holds testdata/known-unsupported to
// its schema without a corpus.
func TestKnownUnsupportedFilesWellFormed(t *testing.T) {
	t.Parallel()
	knownUnsupported(t)
}

// TestEveryRepoDeckIsFullySupported is the M1 coverage ratchet: every card
// across every deck file (the 12 Legacy decks and the m38 commander decks)
// is either fully playable, or is named in
// knownUnsupported with the exact primitives it is missing. A card missing
// something knownUnsupported does not list is a regression (fails); a card
// knownUnsupported lists that is now fully supported is a stale entry that
// must be deleted from the table (also fails) -- the table can drift out of
// sync in either direction as the engine grows, and both directions are a
// bug in the ratchet, not something to silently tolerate.
func TestEveryRepoDeckIsFullySupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	measured := map[string][]string{}
	total := 0
	seen := map[string]bool{}
	for _, name := range testutil.RepoDeckNames() {
		for _, c := range testutil.RepoDeck(t, reg, name) {
			cardName := c.Faces[0].Name
			if seen[cardName] {
				continue
			}
			seen[cardName] = true
			total++
			if m := reg.Unsupported(c, supported); len(m) > 0 {
				measured[cardName] = m
			}
		}
	}
	knownUnsupported := knownUnsupported(t)
	t.Logf("ratchet: %d of %d distinct cards across the repo decks are not fully supported",
		len(measured), total)

	for card, gotPrims := range measured {
		want, ok := knownUnsupported[card]
		if !ok {
			t.Errorf("%s needs %v, which is not in knownUnsupported -- new gap, add it to the ratchet table", card, gotPrims)
			continue
		}
		if !sameSet(want, gotPrims) {
			t.Errorf("%s: knownUnsupported says %v, measured %v -- update the ratchet table to match", card, want, gotPrims)
		}
	}
	for card, want := range knownUnsupported {
		if _, stillMissing := measured[card]; !stillMissing {
			t.Errorf("%s is fully supported now (was missing %v) -- delete it from knownUnsupported", card, want)
		}
	}
}

// sameSet reports whether a and b hold the same strings, order-independent
// and duplicate-independent -- cards.Card.Primitives (and therefore
// Registry.Unsupported) already returns a sorted, deduplicated slice, so in
// practice this only ever needs to reject a genuine content difference, not
// tolerate reordering, but checking it this way keeps the comparison
// correct even if that guarantee ever loosens.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[string]int{}
	for _, s := range a {
		am[s]++
	}
	for _, s := range b {
		am[s]--
	}
	for _, n := range am {
		if n != 0 {
			return false
		}
	}
	return true
}

// playAcceptance plays one deterministic acceptance game -- seats seats,
// testutil.AcceptanceDecks's seating, rules.AcceptanceConfig and
// rules.PlayAcceptance (rules/acceptance_game.go, the non-test construction
// cmd/headdiff shares) answered by testBot (botpolicy.GameBot) -- to
// completion, replays it from its own recorded
// (Config, Log) through the package-local replayFor helper, and Fatals if
// the two chain Heads disagree. step is PlayAcceptance's per-checkpoint hook.
//
// TestRepoDecksPlayAtEverySeatCount and acceptanceHead (rules/heads_test.go's
// TestHeads) both call this, so the games the invariant/replay guarantees
// are checked against and the games the chain-head goldens pin can never
// silently drift apart from each other.
//
// The pool is LegacyDeckNames, never RepoDeckNames (which also lists the
// five interim commander decks, Ruling M38-P): the golden heads are
// byte-identical games over the 12 constructed decks, and seating them by
// index over a directory that grew would move every head for no behavioural
// reason.
func playAcceptance(t *testing.T, reg *cards.Registry, seats int, step func(e *Engine, n int)) string {
	t.Helper()
	names, decks, err := testutil.AcceptanceDecks(reg, seats)
	if err != nil {
		t.Fatalf("%v", err)
	}
	cfg := AcceptanceConfig(reg, names, decks)
	e, _, err := PlayAcceptance(cfg, acceptanceTestBot, step)
	if err != nil {
		t.Fatalf("%d seats: %v", seats, err)
	}
	re, err := replayFor(cfg, e.L)
	if err != nil {
		t.Fatalf("%d seats: %v", seats, err)
	}
	if re.L.Head() != e.L.Head() {
		t.Fatalf("%d seats: chain %s, replay %s", seats, e.L.Head(), re.L.Head())
	}
	return e.L.Head()
}

// acceptanceHead is playAcceptance with no per-checkpoint hook: just the
// finished game's chain Head, for rules/heads_test.go's TestHeads to pin.
func acceptanceHead(t *testing.T, reg *cards.Registry, seats int) string {
	return playAcceptance(t, reg, seats, nil)
}

// TestRepoDecksPlayAtEverySeatCount is the M1 acceptance gate: the 12 repo
// decks, round-robined across 2, 4, 6 and 8 seats, must each play a
// complete game -- termination under budget, every invariant holding
// throughout -- with the cards knownUnsupported lists still shuffled in and
// simply inert (Ruling: this task is about the engine surviving real Forge
// data at scale, not about every card's fidelity, which is M4's separate
// worklist).
func TestRepoDecksPlayAtEverySeatCount(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	for _, seats := range []int{2, 4, 6, 8} {
		seenStart := false
		playAcceptance(t, reg, seats, func(e *Engine, n int) {
			label := fmt.Sprintf("%d-seat mid", seats)
			switch {
			case !seenStart:
				label = fmt.Sprintf("%d-seat start", seats)
				seenStart = true
			case e.G.Over:
				label = fmt.Sprintf("%d-seat end", seats)
				// Ruling P14: Draw before Winner -- Winner's zero value is
				// seat 0, a real seat, so reading it unconditionally would
				// misreport a drawn game as "seat 0 won".
				result := "draw"
				if !e.G.Draw {
					result = e.G.Players[e.G.Winner].Name
				}
				t.Logf("%d seats: %6d intents, %6d events, %3d turns, winner=%s, chain=%s",
					seats, n, len(e.L.Events), e.G.Turn, result, e.L.Head())
			}
			testutil.CheckInvariants(t, e.G, e.Pending(), label)
		})
	}
}

// TestRepoDeckGamesReplayExactly ties acceptance to the replay guarantee:
// five seeded 4-seat games over the pinned Legacy decks (the same 12
// LegacyDeckNames the golden seat-count games and heads use, Ruling M38-P),
// each re-run from its own recorded (Config, Log) through the package-local
// replayFor helper, must reach the same chain Head as the original run.
func TestRepoDeckGamesReplayExactly(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	all := testutil.LegacyDeckNames()
	for seed := uint64(0); seed < 5; seed++ {
		names := make([]string, 4)
		decks := make([][]*cards.Card, 4)
		for i := 0; i < 4; i++ {
			names[i] = all[(int(seed)+i)%len(all)]
			decks[i] = testutil.RepoDeck(t, reg, names[i])
		}
		cfg := Config{Seed: seed, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.AllCards(),
			// R-8.4: Mulligans must travel in the same Config replay is handed;
			// this replay-exactness test exercises the round so a concession of
			// mutating it silently would be caught here (M2d-1).
			Mulligans: 1}
		e := New(cfg)
		b := newTestBot(seed)
		e.Advance()
		n := 0
		for !e.G.Over && e.Pending() != nil && n < 400000 {
			if err := e.Submit(b.answer(e, e.Pending())); err != nil {
				t.Fatalf("seed %d, intent %d: %v", seed, n, err)
			}
			n++
		}
		if !e.G.Over {
			t.Fatalf("seed %d did not terminate after %d intents (turn %d)", seed, n, e.G.Turn)
		}
		re, err := replayFor(cfg, e.L)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if re.L.Head() != e.L.Head() {
			t.Fatalf("seed %d chain %s, replay %s", seed, e.L.Head(), re.L.Head())
		}
		t.Logf("seed %d: %d intents, chain %s, replay OK", seed, n, e.L.Head())
	}
}

// replayFor is a thin, package-local mirror of replay.Replay (Task 24):
// this test file is package rules and the replay package imports rules, so
// importing it here would be a cycle (Ruling in the supplement's ¶0). It
// reruns l's recorded Intents against a fresh Engine built from cfg -- the
// same (Config, Log) contract replay.Replay documents -- and returns the
// resulting engine; the replay package's own exported API and its own
// tests are what every other caller uses.
func replayFor(cfg Config, l *events.Log) (*Engine, error) {
	cfg.Seed = l.Seed
	// CR 103.1's winner-chooses ask is conditional: a log that recorded it
	// carries a DecisionAsk for the kind, so mirror replay.Replay's own
	// conditional constructor here (this is the package-local mirror, per
	// the doc above).
	var e *Engine
	asked := startingPlayerAsked(l)
	if asked {
		e = NewStartingPlayerChoice(cfg)
	} else {
		e = New(cfg)
	}
	if asked {
		// Re-pose the recorded ask, before Advance's R-9 fallback would
		// default it, so the recorded choice Intent lands on the decision it
		// answered.
		if e.AskStartingPlayer() == nil {
			return e, fmt.Errorf("replayFor: log recorded a starting_player ask but none is available")
		}
	}
	e.Advance()
	for i := 0; i < len(l.Intents); i++ {
		if e.G.Over {
			break
		}
		if e.Pending() == nil {
			return e, fmt.Errorf("replayFor: no decision pending at intent %d", i)
		}
		if err := e.Submit(l.Intents[i]); err != nil {
			return e, fmt.Errorf("replayFor: intent %d rejected: %w", i, err)
		}
	}
	return e, nil
}
