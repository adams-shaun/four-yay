package mbtest

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// censusMaxTurns and censusMaxIntents are the harness watchdogs: a smoke
// census run must terminate quickly even if a policy loops (repeatedly
// activating a mana ability with nowhere useful to spend it, for instance),
// and a stalled game is not itself a census failure -- only an unmapped
// decision or a rejected first-legal answer is.
const (
	censusMaxTurns   = 60
	censusMaxIntents = 20000
	// censusGamesPerSeatCount keeps the whole run smoke-sized (box rule:
	// "one go test at a time", "smoke-sized"): 10 games at 2 seats + 10 at 4
	// seats = 20 total, the ticket's "at most 20" cap.
	censusGamesPerSeatCount = 10
)

// playCensusGame builds and plays one game at the given seat count and seed,
// through TranslatingSeat/MockClient, folding decisions into census. It
// returns the finished engine (nil on a hard error) so callers needing the
// log/config for a follow-on check (replay equivalence) can reuse it.
func playCensusGame(t *testing.T, reg *cards.Registry, seats int, seed uint64, client *MockClient, census *Census) (rules.Config, *rules.Engine) {
	t.Helper()
	names := testutil.LegacyDeckNames()
	playerNames := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := 0; i < seats; i++ {
		playerNames[i] = names[(int(seed)+i)%len(names)]
		decks[i] = testutil.RepoDeck(t, reg, playerNames[i])
	}
	cfg := rules.Config{Seed: seed, Names: playerNames, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Cards}
	table := "census"
	seats_ := make([]seat.Seat, seats)
	for i := range seats_ {
		seats_[i] = NewTranslatingSeat(table, int64(seed), client, census)
	}
	outcome, e, err := bench.PlayGame(cfg, seats_, censusMaxTurns, censusMaxIntents, bench.Hooks{})
	if err != nil {
		t.Fatalf("seats=%d seed=%d: PlayGame: %v", seats, seed, err)
	}
	census.addGame()
	if bench.IsAbort(outcome.StallOn) {
		t.Fatalf("seats=%d seed=%d: engine abort (%s): %s", seats, seed, outcome.StallOn, outcome.Livelock)
	}
	return cfg, e
}

// TestManaBrewCensusNoUnmapped plays a smoke-sized set of seeded repo-deck
// games, one at a time, through the ManaBrew mapping (scoping spec §8 item
// 4) and asserts the census recorded no unmapped decision: every decision
// gorge's engine posed across every one of these games built a prompt
// through internal/manabrew's Translator.
func TestManaBrewCensusNoUnmapped(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	census := NewCensus()
	client := NewFirstLegalClient()
	for _, seats := range []int{2, 4} {
		for g := 0; g < censusGamesPerSeatCount; g++ {
			seed := uint64(seats)*1000 + uint64(g)
			playCensusGame(t, reg, seats, seed, client, census)
		}
	}
	t.Logf("census: %s", census.Summary())
	if census.Games == 0 {
		t.Fatal("census recorded zero games -- the .cards corpus is not reachable in this worktree")
	}
	if n := census.TotalUnmapped(); n > 0 {
		t.Fatalf("census recorded %d unmapped decision(s) across %d games: %v", n, census.Games, census.Unmapped)
	}
}

// TestFirstLegalNeverRefused plays the same smoke-sized set with the
// first-legal client and asserts the census recorded no rejected response:
// every answer this deterministic client produced was drawn from the
// prompt's own advertised options, so a rejection would mean the
// translator's Prompt and TranslateResponse halves disagree about what is
// legal (scoping spec §8 item 4's "the test fails ... on any rejected count
// for the first-legal client").
func TestFirstLegalNeverRefused(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	census := NewCensus()
	client := NewFirstLegalClient()
	for _, seats := range []int{2, 4} {
		for g := 0; g < censusGamesPerSeatCount; g++ {
			seed := uint64(seats)*2000 + uint64(g)
			playCensusGame(t, reg, seats, seed, client, census)
		}
	}
	t.Logf("census: %s", census.Summary())
	if census.Games == 0 {
		t.Fatal("census recorded zero games -- the .cards corpus is not reachable in this worktree")
	}
	if n := census.TotalRejected(); n > 0 {
		t.Fatalf("first-legal client was refused %d time(s) across %d games: %v", n, census.Games, census.Rejected)
	}
}
