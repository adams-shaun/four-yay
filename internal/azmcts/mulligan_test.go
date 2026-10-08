package azmcts

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/seat"
)

// SeatConfig.Mulligan reaches the seat's own bot, which answers the London
// round (never a searched kind): with no simulations the seat is exactly
// seat.NewBot(seed).WithMulligan(rule), chain head for chain head, in a game
// that runs the round.
func TestSeatMulliganRuleIsTheBots(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	cfg.Mulligans = 2
	for _, rule := range []botpolicy.MulliganRule{botpolicy.MulliganLands, botpolicy.MulliganNever} {
		sc := DefaultSeatConfig()
		sc.Search.Sims = 0
		sc.Source = testSeatSource
		sc.Mulligan = rule
		az, err := NewSeat(cfg.Seed^1, nil, sc)
		if err != nil {
			t.Fatal(err)
		}
		azHead, err := playAZ(t, cfg, az, 400)
		if err != nil {
			t.Fatal(err)
		}
		botHead, err := playAZ(t, cfg, seat.NewBot(cfg.Seed^1).WithMulligan(rule), 400)
		if err != nil {
			t.Fatal(err)
		}
		if azHead != botHead {
			t.Fatalf("rule %d: az with 0 sims head %s, bot head %s", rule, azHead, botHead)
		}
	}
}

// An az seat configured never to mulligan keeps every opening hand, where
// the default seat (the bot's 1/3 coin) mulligans some.
func TestSeatNeverMulligans(t *testing.T) {
	count := func(rule botpolicy.MulliganRule) (mulls int) {
		for k := uint64(0); k < 20; k++ {
			cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed+k)
			cfg.Mulligans = 2
			sc := DefaultSeatConfig()
			sc.Search.Sims = 0
			sc.Source = testSeatSource
			sc.Mulligan = rule
			az, err := NewSeat(cfg.Seed^1, nil, sc)
			if err != nil {
				t.Fatal(err)
			}
			hooks := gbench.Hooks{Decision: func(seatIdx int, d *decision.Decision, in decision.Intent, _ *botpolicy.Board) error {
				if seatIdx == 0 && d.Kind == decision.KMulligan {
					if c := d.Chosen(in); len(c) == 1 && c[0].Kind == "mulligan" {
						mulls++
					}
				}
				return nil
			}}
			// A short cap: only the pregame round matters here.
			if _, _, err := gbench.PlayGame(cfg, []seat.Seat{az, seat.NewBot(cfg.Seed ^ 2)}, 4, 40, hooks); err != nil {
				t.Fatal(err)
			}
		}
		return mulls
	}
	if n := count(botpolicy.MulliganNever); n != 0 {
		t.Errorf("MulliganNever seat mulliganed %d times in 20 games", n)
	}
	if n := count(botpolicy.MulliganCoin); n == 0 {
		t.Errorf("the default seat never mulliganed in 20 games: the comparison is vacuous")
	}
}
