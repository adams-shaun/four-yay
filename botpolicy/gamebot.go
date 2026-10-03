package botpolicy

import (
	"math/rand/v2"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// GameBot answers decisions straight off a live game: it builds the Board
// from state.Game and the engine's derived characteristics
// (BoardFromGameInto, the same facts a seat.Bot builds from the projected
// View) and forwards to Decide with its own seeded rng. It is the bot of the
// rules package's fuzz and acceptance harnesses (rules/testbot_test.go's
// testBot wraps it) and of cmd/headdiff, which must answer the TestHeads
// acceptance game exactly as the tests do; seat/integration_test.go's
// TestBotAdaptersAgree* pins this half and the View half to the same Board
// for the same game facts.
type GameBot struct {
	r *rand.Rand
	// board is refilled per decision (BoardFromGameInto), the host match
	// loop's own reuse shape: Decide never retains a Board past the call
	// (TestBoardOwnership), so one Board per bot is safe and spares a fresh
	// set of maps per decision.
	board    Board
	hasBoard bool
}

// NewGameBot returns a GameBot whose rng is PCG(seed, seed^0x9e3779b97f4a7c15).
func NewGameBot(seed uint64) *GameBot {
	return &GameBot{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// Answer returns the intent for d in g, with ch the engine's derived
// characteristics. The policy and its per-kind rationale live in Decide.
func (b *GameBot) Answer(g *state.Game, ch Chars, d *decision.Decision) decision.Intent {
	if !b.hasBoard {
		b.board, b.hasBoard = NewBoard(len(g.Players)), true
	}
	return Decide(BoardFromGameInto(g, ch, d.Player, &b.board), d, b.r)
}
