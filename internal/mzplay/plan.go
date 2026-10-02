package mzplay

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/cards"
)

// The run's game plan: which seed and which two decks each game plays. It is
// fixed before any game starts, from the run seed and the game's index
// alone, so the games of a run are the same whatever order the game threads
// finish in (ParallelDataGenerator draws a fresh random seed per game and
// its deck from java.util.Random(seed ^ 0x5DEECE66D); here the seed is
// derived, and the deck stream is a PCG seeded from it).

// MinDeckSize is the size below which upstream refuses a deck
// (ParallelDataGenerator.createLocalPlayer).
const MinDeckSize = 40

// GameSeed is game index's seed in a run seeded runSeed.
func GameSeed(runSeed uint64, index int) uint64 {
	return splitmix(runSeed ^ splitmix(uint64(index)+0x67616d65))
}

// GamePlan is one game's seed and deck files.
type GamePlan struct {
	Index        int
	Seed         uint64
	DeckA, DeckB string // .dck paths
}

// PlanGames lays out cfg.Games games. poolA and poolB are the players' deck
// pools (ReadPool), or a one-entry list holding deckPath when the player has
// no pool. The two decks of a game are drawn A first, then B, from one
// stream, as upstream draws them.
func PlanGames(cfg Config, runSeed uint64, poolA, poolB []string) ([]GamePlan, error) {
	if len(poolA) == 0 || len(poolB) == 0 {
		return nil, fmt.Errorf("mzplay: empty deck pool")
	}
	out := make([]GamePlan, cfg.Games)
	for i := range out {
		seed := GameSeed(runSeed, i)
		rng := rand.New(rand.NewPCG(seed^0x5DEECE66D, splitmix(seed)))
		out[i] = GamePlan{Index: i, Seed: seed,
			DeckA: ChooseDeck(poolA, cfg.A.DeckPoolMode, i, rng.IntN),
			DeckB: ChooseDeck(poolB, cfg.B.DeckPoolMode, i, rng.IntN)}
	}
	return out, nil
}

// ResolvedDeck is a .dck file resolved against the corpus.
type ResolvedDeck struct {
	Path  string
	Stem  string
	Cards []*cards.Card
}

// LoadDeck parses and resolves one .dck file. A deck naming a card the
// corpus does not hold, or with fewer than MinDeckSize cards, is an error:
// it is never played short.
func LoadDeck(path string, reg CardLookup) (ResolvedDeck, error) {
	list, err := LoadDCK(path)
	if err != nil {
		return ResolvedDeck{}, err
	}
	deck, missing := list.Resolve(reg)
	if len(missing) > 0 {
		return ResolvedDeck{}, fmt.Errorf("%s: %d card name(s) the corpus does not resolve: %q", path, len(missing), missing)
	}
	if len(deck) < MinDeckSize {
		return ResolvedDeck{}, fmt.Errorf("%s: couldn't load deck, deck size=%d", path, len(deck))
	}
	return ResolvedDeck{Path: path, Stem: DeckStem(path), Cards: deck}, nil
}

// Summary is the GAME_SUMMARY record of one finished game
// (ParallelDataGenerator.logGameSummary; parsed by draft-zero's
// stats.parse_summaries): the game's number in completion order, its seed,
// the two deck stems, who went first, the winner ("A", "B" or "draw"), the
// turn count, and every card each player drew.
type Summary struct {
	Game   int      `json:"game"`
	Seed   int64    `json:"seed"`
	DeckA  string   `json:"deck_a"`
	DeckB  string   `json:"deck_b"`
	First  string   `json:"first"`
	Winner string   `json:"winner"`
	Turns  int      `json:"turns"`
	DrawnA []string `json:"drawn_a"`
	DrawnB []string `json:"drawn_b"`
}

// SummaryOf builds a game's summary; number is its 1-based completion
// number. The seed is printed as a Java long would be.
func SummaryOf(number int, r GameResult, deckA, deckB string) Summary {
	s := Summary{Game: number, Seed: int64(r.Seed), DeckA: deckA, DeckB: deckB, First: "A", Winner: "draw", Turns: r.Turns,
		DrawnA: r.Drawn[0], DrawnB: r.Drawn[1]}
	if r.First == 1 {
		s.First = "B"
	}
	switch r.Winner {
	case 0:
		s.Winner = "A"
	case 1:
		s.Winner = "B"
	}
	if s.DrawnA == nil {
		s.DrawnA = []string{}
	}
	if s.DrawnB == nil {
		s.DrawnB = []string{}
	}
	return s
}

// SummaryTag starts a summary line's payload.
const SummaryTag = "GAME_SUMMARY "

// Line is the text upstream logs: the tag and the JSON object.
func (s Summary) Line() string {
	b, err := json.Marshal(s)
	if err != nil {
		return SummaryTag + "{}"
	}
	return SummaryTag + string(b)
}
