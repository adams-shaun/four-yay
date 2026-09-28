package rules

import (
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// testBot drives the rules package's own fuzz and acceptance harnesses with
// the same policy seat.Bot uses, through the same botpolicy.Decide (Ruling
// F7). Before botpolicy existed, this file carried a line-for-line copy of
// seat/bot.go's botDecide and clamp -- rules/fuzz_test.go is package rules,
// and importing seat -- which imports view -- runs the declared dependency
// order (cards -> state -> decision -> events -> effects -> rules -> view
// -> seat -> replay -> cmd/*) backwards into the package under test. There
// is one policy now, in botpolicy, and this file is its game-shaped
// adapter: answer builds the botpolicy.Board straight off the engine
// (botpolicy.BoardFromGame, the same facts a seat.Bot would build from the
// projected View, IsMain from e.G.Step.IsMain). seat/integration_test.go's
// TestBotAdaptersAgree* pins the two halves to the same Board for the same
// game facts.
type testBot struct {
	r *rand.Rand
	// board is refilled per decision (botpolicy.BoardFromGameInto), the
	// host match loop's own reuse shape: Decide never retains a Board past
	// the call (botpolicy's TestBoardOwnership), so one Board per bot is
	// safe and spares a fresh set of maps per decision.
	board    botpolicy.Board
	hasBoard bool
}

func newTestBot(seed uint64) *testBot {
	return &testBot{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// answer is the fuzz-driver interface this package's callers have always
// used: the call shape is unchanged from the mirror it replaces, except
// that the Board is now built here from the engine itself rather than by
// the caller. The engine is the game-shaped analog of the projected View a
// seat.Bot builds its board from (botpolicy.BoardFromGame reads g and the
// engine's derived P/T/keywords; botpolicy.Decide's doc explains the
// equivalence), so seat/integration_test.go's TestBotAdaptersAgreeOverWholeGame
// can pin the two halves to the same Board for the same game facts. The
// policy behaviour itself -- and its doc, per case -- lives in
// botpolicy.Decide; see there for the rationale of each decision.Kind.
// Every access into d.Options is guarded against the list being empty
// there, and clamp (Ruling T25-c) is the last thing every return does, so
// the intent this forwards always validates against d for any Min/Max the
// wire format allows, not only today's shapes.
func (b *testBot) answer(e *Engine, d *decision.Decision) decision.Intent {
	if !b.hasBoard {
		b.board, b.hasBoard = botpolicy.NewBoard(len(e.G.Players)), true
	}
	return botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &b.board), d, b.r)
}

// TestTestBotDelegatesToBotPolicy pins answer's wiring: for representative
// shapes of the decisions the fuzz gate throws at it, answer must deliver
// exactly what botpolicy.Decide would -- same Board (built from the same
// engine), same rng stream, same consumption points. A real engine
// supplies the Board because answer now derives it from the engine itself;
// the decision shapes are synthetic on purpose, so the test exercises the
// forwarding, not the game. This is the successor of the mirror tests
// (TestTestBotChoosePolicy and friends), which died with the mirror: the
// policy's behaviour is now tested once, in botpolicy/combat_test.go and
// policy_test.go, and this file only has to prove it forwards unchanged.
func TestTestBotDelegatesToBotPolicy(t *testing.T) {
	t.Parallel()
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 99, Names: names, Decks: decks})
	e.Advance()
	shapes := []decision.Decision{
		{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
			Options: []decision.Option{
				{Index: 0, Kind: "activate", Obj: 1},
				{Index: 1, Kind: "pass"},
			}},
		// A KChoose shape and the two rng-consuming kinds (KTriggerOrder,
		// KTriggerOptional), so a regression that skips the rng forwarding --
		// or feeds the policy a different Board -- shows up here, not only in
		// a game.
		{Seq: 2, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1,
			Options: []decision.Option{
				{Index: 0, Kind: "x"},
				{Index: 1, Kind: "x"},
			}},
		{Seq: 3, Player: 0, Kind: decision.KTriggerOrder, Min: 3, Max: 3,
			Options: []decision.Option{
				{Index: 0, Kind: "trigger", Obj: 30},
				{Index: 1, Kind: "trigger", Obj: 31},
				{Index: 2, Kind: "trigger", Obj: 32},
			}},
		{Seq: 4, Player: 0, Kind: decision.KTriggerOptional, Min: 1, Max: 1,
			Options: []decision.Option{
				{Index: 0, Kind: "yes"}, {Index: 1, Kind: "no"},
			}},
		// A KBlockers shape: the choices come from the game's own board now,
		// but the forwarding contract is the same.
		{Seq: 5, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 2,
			Options: []decision.Option{
				{Index: 0, Kind: "block", Obj: 10, Attacker: 20},
				{Index: 1, Kind: "block", Obj: 11, Attacker: 20},
			}},
	}
	for _, d := range shapes {
		b := newTestBot(99)
		got := b.answer(e, &d)
		r := rand.New(rand.NewPCG(99, 99^0x9e3779b97f4a7c15))
		want := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), &d, r)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("kinds=%q: answer = %+v, botpolicy.Decide = %+v", d.Kind, got, want)
		}
		if err := d.Validate(got); err != nil {
			t.Errorf("kinds=%q: answer intents failed Validate: %v", d.Kind, err)
		}
	}
}
