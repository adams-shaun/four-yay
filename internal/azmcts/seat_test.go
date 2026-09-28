package azmcts

import (
	"errors"
	"testing"

	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

func playAZ(t *testing.T, cfg rules.Config, az seat.Seat, maxIntents int) (string, error) {
	t.Helper()
	_, e, err := gbench.PlayGame(cfg, []seat.Seat{az, seat.NewBot(cfg.Seed ^ 2)}, 200, maxIntents, gbench.Hooks{})
	if e == nil {
		return "", err
	}
	return e.L.Head(), err
}

// With no simulations the seat is exactly the bot it wraps: same rng stream,
// same answers, same chain head (the delegation contract).
func TestSeatWithoutSimulationsIsTheBot(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.Search.Sims = 0
	az, err := NewSeat(cfg.Seed^1, nil, sc)
	if err != nil {
		t.Fatal(err)
	}
	azHead, err := playAZ(t, cfg, az, 400)
	if err != nil {
		t.Fatal(err)
	}
	botHead, err := playAZ(t, cfg, seat.NewBot(cfg.Seed^1), 400)
	if err != nil {
		t.Fatal(err)
	}
	if azHead != botHead {
		t.Fatalf("az with 0 sims head %s, bot head %s", azHead, botHead)
	}
}

// Spec §4 determinism, end to end: the same seeds replay the same game, and
// the search did run.
func TestSeatSearchesAndReplaysExactly(t *testing.T) {
	allowClairvoyantForTest(t)
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.Search.Sims = 4
	var diags []Diag
	prev := Watch
	Watch = func(d Diag) { diags = append(diags, d) }
	t.Cleanup(func() { Watch = prev })
	heads := make([]string, 2)
	for i := range heads {
		az, err := NewSeat(cfg.Seed^1, nil, sc)
		if err != nil {
			t.Fatal(err)
		}
		if heads[i], err = playAZ(t, cfg, az, 300); err != nil {
			t.Fatal(err)
		}
	}
	if heads[0] != heads[1] {
		t.Fatalf("replay diverged: %s vs %s", heads[0], heads[1])
	}
	searched := 0
	for _, d := range diags {
		if d.Searched {
			searched++
			if d.Stats.Simulations != 4 {
				t.Fatalf("a searched decision ran %d simulations, want 4", d.Stats.Simulations)
			}
		}
	}
	t.Logf("%d diags, %d searched over two replays", len(diags), searched)
	if searched == 0 {
		t.Fatal("the az seat never searched")
	}
}

func TestSeatRefusesClairvoyantByDefault(t *testing.T) {
	prev := clairvoyantAllowed.Load()
	clairvoyantAllowed.Store(false)
	t.Cleanup(func() { clairvoyantAllowed.Store(prev) })
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.Search.Sims = 4
	az, err := NewSeat(cfg.Seed^1, nil, sc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := playAZ(t, cfg, az, 300); !errors.Is(err, ErrClairvoyantRefused) {
		t.Fatalf("game error %v, want ErrClairvoyantRefused", err)
	}
}

func TestNewSeatValidates(t *testing.T) {
	sc := DefaultSeatConfig()
	sc.Search.CPUCT = 0
	if _, err := NewSeat(1, nil, sc); err == nil {
		t.Fatal("invalid options accepted")
	}
}
