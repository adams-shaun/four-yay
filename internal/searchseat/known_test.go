package searchseat_test

// The feed-owned known-card tracker (BP-04): the projection a seat reads back
// must equal the whole-history fold at every decision the driver hands the
// seat its own feed, which is what lets azmcts and sbsearch stop keeping
// private copies of the tracker.

import (
	"context"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	ss "github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// knownCheckSeat reads the feed the driver handed it at every one of its own
// decisions and checks the feed's own projection against a fresh whole-history
// fold of the same frames. It delegates the answer to the wrapped search bot
// so the game (and the feed's frame stream) plays exactly as usual.
type knownCheckSeat struct {
	*ss.SearchBot
	checks int
	t      *testing.T
}

func (s *knownCheckSeat) DecideSearch(ctx context.Context, env ss.Env, d decision.Decision) (decision.Intent, error) {
	s.t.Helper()
	if env.Feed == nil || !env.Feed.Live() || env.Feed.Frames() == 0 {
		s.t.Fatalf("seat asked without a live feed")
	}
	got, err := env.Feed.Known()
	if err != nil {
		s.t.Fatalf("Feed.Known: %v", err)
	}
	want, err := searchprobe.ProjectKnownCards(env.Feed.History())
	if err != nil {
		s.t.Fatalf("ProjectKnownCards: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		s.t.Fatalf("frame %d: feed projection %+v, whole fold %+v", env.Feed.Frames(), got, want)
	}
	s.checks++
	return s.SearchBot.DecideSearch(ctx, env, d)
}

// TestFeedKnownMatchesTheSeatTracker drives a bench game with a search seat
// that checks, at every decision of its own, that Feed.Known equals the
// whole-history projection of the very frames the feed holds -- the
// incremental fold the tracker moved out of the seats must be identical to
// the one-shot fold.
//
// The check is asserted at least ten times (a vacuous run with fewer seat
// decisions must fail, not pass silently).
func TestFeedKnownMatchesTheSeatTracker(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	cfg := rules.Config{Seed: 21, Names: names, Decks: decks}
	opts := ss.Defaults()
	opts.Worlds, opts.Attempts = 2, 4
	checker := &knownCheckSeat{SearchBot: ss.NewSearchBot(21^1, opts), t: t}
	if _, _, err := gbench.PlayGame(cfg, []seat.Seat{checker, seat.NewBot(21 ^ 2)}, 200, 200, gbench.Hooks{}); err != nil {
		t.Fatalf("play: %v", err)
	}
	if checker.checks < 10 {
		t.Fatalf("only %d seat decisions checked; the comparison is vacuous", checker.checks)
	}
	t.Logf("checked %d seat decisions", checker.checks)
}
