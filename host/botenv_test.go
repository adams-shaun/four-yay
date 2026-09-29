package host

import (
	"context"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// spyEnvSeat is a non-human seat that satisfies bots.EnvSeat without ever
// using an Env. BP-07 wires only the feed lifecycle: there is no Env dispatch
// yet (that is BP-08), so DecideEnv is deliberately unreachable and returns an
// error if the host ever calls it — a loud failure of this ticket's central
// claim (a match with an EnvSeat still answers through the Board/View path).
// Embedding *seat.Bot promotes Decide and DecideBoard, so the seat plays
// deterministically and projectNext takes its usual BoardSeat branch.
type spyEnvSeat struct {
	*seat.Bot
	decided int
}

func (s *spyEnvSeat) WantsEnv(d *decision.Decision) bool { return true }

func (s *spyEnvSeat) DecideEnv(_ context.Context, _ bots.Env, _ decision.Decision) (decision.Intent, error) {
	return decision.Intent{}, errNoEnvDispatch
}

// errNoEnvDispatch marks the not-yet-wired Env path (BP-08). A test that sees
// it knows an Env was built, which this ticket does not do.
var errNoEnvDispatch = &noEnvDispatchError{}

type noEnvDispatchError struct{}

func (*noEnvDispatchError) Error() string { return "BP-07: Env dispatch is not wired yet" }

// matchFor returns a finished match's engine and feeds after r.Wait. It is the
// in-package hook the feed tests use to inspect match-owned state that no
// Registry method exposes (the feeds are intentionally unexported and never
// reach the wire, §5.2).
func matchFor(t *testing.T, r *Registry, id TableID) (*match, *rules.Engine) {
	t.Helper()
	r.mu.RLock()
	tbl, ok := r.tables[id]
	r.mu.RUnlock()
	if !ok {
		t.Fatalf("table %s not found", id)
	}
	tbl.mu.RLock()
	defer tbl.mu.RUnlock()
	if len(tbl.history) == 0 {
		t.Fatalf("table %s kept no in-memory match history", id)
	}
	m := tbl.history[len(tbl.history)-1]
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m, m.e
}

// TestHostFeedEqualsRebuildFeed is BP-07's central assertion: a live match's
// host-owned feed, after every intent, equals searchseat.RebuildFeed over the
// same log and actor. That equality is what makes the feed derived state the
// host can rebuild after an undo (BP-09) and never persist (§5.2).
//
// The table seats a spy EnvSeat at slot 0 (so the host creates a feed) and a
// plain bot at slot 1, and it must reach a real game — asserted by the
// precondition block — or a comparison of two empty feeds would pass
// vacuously.
func TestHostFeedEqualsRebuildFeed(t *testing.T) {
	t.Parallel()
	opts := testOptions(t)
	const seed = uint64(20260928)
	opts.Seats = func(names []string, gotSeed uint64) []seat.Seat {
		return []seat.Seat{
			&spyEnvSeat{Bot: seat.NewBot(gotSeed ^ 1)},
			seat.NewBot(gotSeed ^ 2),
		}
	}
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cfg := TableConfig{ID: "t1", Name: "envfeeds", Seats: 2, Decks: []string{"a", "b"},
		Seed: seed, Pace: 0, Spectator: view.Public}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	m, engine := matchFor(t, r, "t1")

	// Precondition: the host built a feed for slot 0 and only slot 0, and the
	// game actually ran. Without this a mis-wired feed and a 0-intent game
	// would both compare equal and pass.
	if m.feeds == nil {
		t.Fatal("no feeds built for a table with an EnvSeat: Create is not wired")
	}
	if len(m.feeds.bySlot) != 2 {
		t.Fatalf("feeds slots = %d, want 2", len(m.feeds.bySlot))
	}
	if m.feeds.bySlot[0] == nil {
		t.Fatal("slot 0 (spy EnvSeat) has no feed")
	}
	if m.feeds.bySlot[1] != nil {
		t.Fatal("slot 1 (plain bot) has a feed; only EnvSeats must get one")
	}
	live := m.feeds.bySlot[0]
	if !live.Live() {
		t.Fatalf("live feed stopped: %s", live.StopReason())
	}
	if n := len(engine.L.Intents); n < 20 {
		t.Fatalf("match logged only %d intents; too short to compare feeds", n)
	}
	if live.Frames() < 20 {
		t.Fatalf("live feed captured only %d frames; Observe is not wired", live.Frames())
	}

	actor := state.PlayerID(0)
	rebuilt, err := searchseat.RebuildFeed(m.cfg, engine.L, len(engine.L.Intents), actor)
	if err != nil {
		t.Fatalf("RebuildFeed: %v", err)
	}
	if !reflect.DeepEqual(live.History(), rebuilt.History()) {
		lh, rh := live.History(), rebuilt.History()
		t.Fatalf("live feed != RebuildFeed(final): frames %d/%d answers %d/%d actors %d/%d",
			len(lh.Frames), len(rh.Frames), len(lh.Answers), len(rh.Answers), lh.Actor, rh.Actor)
	}
	if !reflect.DeepEqual(live.Collector().Clone(), rebuilt.Collector().Clone()) {
		t.Fatal("live feed collector state != RebuildFeed(final) collector state")
	}

	// The same equality must hold at every prefix, not only at the end: the
	// live feed truncated to boundary n is RebuildFeed(n).
	for _, n := range []int{1, len(engine.L.Intents) / 2, len(engine.L.Intents) - 1} {
		prefix, err := searchseat.RebuildFeed(m.cfg, engine.L, n, actor)
		if err != nil {
			t.Fatalf("RebuildFeed(%d): %v", n, err)
		}
		truncated := live.History()
		truncated.Frames = truncated.Frames[:n+1]
		truncated.Answers = map[int][]searchprobe.Action{}
		for frame, ans := range live.History().Answers {
			if frame < n {
				truncated.Answers[frame] = ans
			}
		}
		if !reflect.DeepEqual(prefix.History(), truncated) {
			t.Fatalf("prefix %d: RebuildFeed != live feed truncated (%d/%d frames)",
				n, len(prefix.History().Frames), len(truncated.Frames))
		}
	}
}

// TestNoEnvSeatNoFeed is BP-07's fast-path and no-regression gate: a table
// whose seats are all plain bots builds no feed at all (nil matchFeeds, no
// backing slice), and the nil-receiver Observe allocates nothing. Every match
// that existed before this ticket takes exactly this path, so its behaviour is
// unchanged (the Done-means TestHeads run pins the rules half of that).
func TestNoEnvSeatNoFeed(t *testing.T) {
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(fourSeatTable("t1", false)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	m, _ := matchFor(t, r, "t1")
	if m.feeds != nil {
		t.Fatalf("a table with no EnvSeat built feeds: %+v", m.feeds)
	}
	// The match genuinely ran, so the nil above is the fast path and not an
	// early bail (an unwired Observe would also leave feeds nil).
	m.mu.RLock()
	intents := m.intents
	m.mu.RUnlock()
	if intents < 20 {
		t.Fatalf("match logged only %d intents; the fast path was not exercised by a real game", intents)
	}

	// Allocation-free: the nil receiver returns before touching the engine, so
	// a nil *rules.Engine is safe to pass and proves nothing is built.
	var nilFeeds *matchFeeds
	if n := testing.AllocsPerRun(1000, func() { nilFeeds.observe(nil) }); n != 0 {
		t.Fatalf("nil matchFeeds.observe allocated %v per call, want 0", n)
	}
}
