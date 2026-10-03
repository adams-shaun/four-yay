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
// F7): it is botpolicy.GameBot, the game-shaped adapter cmd/headdiff also
// answers the TestHeads acceptance game with, so the two cannot drift.
// Before botpolicy existed, this file carried a line-for-line copy of
// seat/bot.go's botDecide and clamp -- rules/fuzz_test.go is package rules,
// and importing seat -- which imports view -- runs the declared dependency
// order (cards -> state -> decision -> events -> effects -> botpolicy ->
// rules -> view -> seat -> replay -> cmd/*) backwards into the package under
// test. seat/integration_test.go's TestBotAdaptersAgree* pins the two
// halves to the same Board for the same game facts.
type testBot struct {
	gb *botpolicy.GameBot
	// r is a fresh rng seeded exactly as gb's, for the tests that call
	// botpolicy.Decide directly; answer never reads it.
	r *rand.Rand
}

func newTestBot(seed uint64) *testBot {
	return &testBot{gb: botpolicy.NewGameBot(seed), r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// answer is the call shape this package's harnesses have always used; the
// Board is built from the engine itself (e.G plus e's derived
// characteristics) and the policy is botpolicy.Decide's.
func (b *testBot) answer(e *Engine, d *decision.Decision) decision.Intent {
	return b.gb.Answer(e.G, e, d)
}

// acceptanceTestBot is PlayAcceptance's bot factory for this package's tests.
func acceptanceTestBot(seed uint64) Answerer { return newTestBot(seed).answer }

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
