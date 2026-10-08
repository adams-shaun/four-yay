package bench_test

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// refuseFirstSeat answers every decision from a wrapped bot, except that its
// FIRST answer is a deliberately out-of-range choice the engine refuses. It
// implements bots.RefusalAnswerer, so PlayGame's refusal ladder asks it to
// re-answer; the game must survive instead of dying on the rejected Submit.
type refuseFirstSeat struct {
	inner   seat.Seat
	refused bool
}

func (s *refuseFirstSeat) Decide(_ context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in, err := s.inner.Decide(context.Background(), v, d)
	if err == nil && !s.refused {
		s.refused = true
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{len(d.Options)}}, nil
	}
	return in, err
}

func (s *refuseFirstSeat) AnswerRefused(v view.View, d decision.Decision, _ decision.Intent) decision.Intent {
	in, _ := s.inner.Decide(context.Background(), v, d)
	return in
}

// TestPlayGameReanswersRefusedDecision: a Submit the engine refuses leaves
// the pending decision intact, and PlayGame feeds the rejection back to the
// seat (the refusal ladder) rather than returning a fatal error, so the game
// reaches its natural end.
func TestPlayGameReanswersRefusedDecision(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, "mono-black-aggro")
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, "mono-red-prowess")
	if err != nil {
		t.Fatal(err)
	}
	const seed = 424242
	cfg := rules.Config{
		Seed: seed, Names: []string{"mono-black-aggro", "mono-red-prowess"},
		Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens,
	}
	naughty := &refuseFirstSeat{inner: seat.NewBot(seed ^ 1)}
	seats := []seat.Seat{naughty, seat.NewBot(seed ^ 2)}
	_, e, err := bench.PlayGame(cfg, seats, 60, 4000, bench.Hooks{})
	if err != nil {
		t.Fatalf("game died on a refused intent instead of re-asking: %v", err)
	}
	if !naughty.refused {
		t.Fatal("the naughty seat never produced a refused intent; the test is vacuous")
	}
	if e == nil || !e.G.Over {
		t.Fatalf("game did not finish after the re-ask (over=%v)", e != nil && e.G.Over)
	}
}
