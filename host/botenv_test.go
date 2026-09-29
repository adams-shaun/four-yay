package host

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// spyEnvSeat is a non-human seat that satisfies bots.EnvSeat and records
// exactly what the host handed each DecideEnv call — the evidence BP-08's
// tests assert on. It plays the wrapped bot from env.Board, the same fallback
// every real adapter plays when the root is refused, so a match it seats runs
// identically to one driven through the plain Board path (that is what makes
// TestHostFeedEqualsRebuildFeed's live-vs-rebuilt comparison still hold: the
// Env changes nothing the engine can see).
// Embedding *seat.Bot promotes Decide and DecideBoard, so the seat plays
// deterministically.
type spyEnvSeat struct {
	*seat.Bot
	decided int
	envs    []envRecord
}

// envRecord is what one DecideEnv call was handed and answered.
type envRecord struct {
	seq       uint64
	rootRef   string        // env.RootRefused (empty when the root built)
	engine    *rules.Engine // env.Search.Engine; nil when the root was refused
	g         *state.Game   // the root's own game; never the live engine's
	feed      *searchseat.Feed
	board     botpolicy.Board
	view      view.View
	actorHand []string // the ROOT's hand for the deciding seat (slot 0)
	oppHand   []string // the ROOT's hand for the opponent (slot 1)
	actorLib  []string // the ROOT's library for slot 0, in order
	oppLib    []string // the ROOT's library for slot 1, in order
	in        decision.Intent
}

func (s *spyEnvSeat) WantsEnv(d *decision.Decision) bool { return true }

func (s *spyEnvSeat) DecideEnv(ctx context.Context, env bots.Env, d decision.Decision) (decision.Intent, error) {
	s.decided++
	rec := envRecord{
		seq: d.Seq, rootRef: env.RootRefused, feed: env.Search.Feed,
		board: env.Board, view: env.View,
	}
	if e := env.Search.Engine; e != nil {
		rec.engine, rec.g = e, e.G
		rec.actorHand = zoneNames(e.G.Zone(state.ZHand, 0), e)
		rec.oppHand = zoneNames(e.G.Zone(state.ZHand, 1), e)
		rec.actorLib = zoneNames(e.G.Zone(state.ZLibrary, 0), e)
		rec.oppLib = zoneNames(e.G.Zone(state.ZLibrary, 1), e)
	}
	in, err := s.Bot.DecideBoard(ctx, env.Board, d)
	rec.in = in
	s.envs = append(s.envs, rec)
	return in, err
}

// recorded returns the env record of the decision with the given seq, or nil.
func (s *spyEnvSeat) recorded(seq uint64) *envRecord {
	for i := range s.envs {
		if s.envs[i].seq == seq {
			return &s.envs[i]
		}
	}
	return nil
}

// zoneNames names the objects of one zone, in zone order. Face-down or
// face-less objects contribute nothing rather than panicking.
func zoneNames(ids []state.ObjID, e *rules.Engine) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if o := e.G.Obj(id); o != nil && o.Card != nil && len(o.Card.Faces) > 0 && o.Card.Faces[0] != nil {
			out = append(out, o.Card.Faces[0].Name)
		}
	}
	return out
}

// sortedEqual compares two name lists as multisets.
func sortedEqual(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// hiddenWorldsEqual reports whether two engines hold the same hidden card
// arrangement: hand name MULTISETS and library name ORDERS per player. It is
// the comparison both uses below are built on: the leak test's roots must
// satisfy it name-for-name, and the swap fixture's two live engines must NOT.
func hiddenWorldsEqual(a, b *rules.Engine) bool {
	for pl := state.PlayerID(0); int(pl) < len(a.G.Players); pl++ {
		if !sortedEqual(zoneNames(a.G.Zone(state.ZHand, pl), a), zoneNames(b.G.Zone(state.ZHand, pl), b)) {
			return false
		}
		if !slices.Equal(zoneNames(a.G.Zone(state.ZLibrary, pl), a), zoneNames(b.G.Zone(state.ZLibrary, pl), b)) {
			return false
		}
	}
	return true
}

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

// envTestTable is the 2-seat table every BP-08 test seats: a spy EnvSeat at
// slot 0, a plain bot at slot 1, fixed seed and fixed repo decks.
func envTestTable(id TableID, seed uint64) TableConfig {
	return TableConfig{ID: id, Name: "env" + string(id), Seats: 2, Decks: []string{"a", "b"},
		Seed: seed, Pace: 0, Spectator: view.Public}
}

// driveEnvStep advances one decision through the REAL host path — projectNext
// under the match's exclusive lock (which observes the feeds and builds the
// Env), parkSeat outside it. beforeProject, when non-nil, runs under the lock
// right before projectNext whenever the pending decision is the EnvSeat's
// (slot 0): the swap fixture and the feed-stop use it to act at a boundary the
// host path is about to read.
func driveEnvStep(t *testing.T, m *match, seats []seat.Seat, brd *botpolicy.Board,
	beforeProject func(m *match, d *decision.Decision)) (*parkedData, *parkedDecision) {
	t.Helper()
	m.mu.Lock()
	if beforeProject != nil {
		if d := m.e.Pending(); d != nil && d.Player == 0 {
			beforeProject(m, d)
		}
	}
	data := projectNext(m, seats, brd)
	m.mu.Unlock()
	if data == nil {
		return nil, nil
	}
	pd := parkSeat(context.Background(), seats, data, m.undo.signal, nil, nil)
	if pd.err != nil {
		t.Fatalf("seat %d: DecideEnv/Decide error: %v", pd.p, pd.err)
	}
	return data, pd
}

// submitEnvStep submits one parked answer through the same exclusive section
// play's attempt uses (minus persistence and the refusal ladder: the spy plays
// the wrapped bot's own answer, which the engine accepts).
func submitEnvStep(t *testing.T, r *Registry, tbl *table, m *match, pd *parkedDecision) {
	t.Helper()
	err := m.locked(func() error {
		before := len(m.e.L.Events)
		d := m.e.Pending()
		if e := m.e.Submit(pd.in); e != nil {
			return e
		}
		m.afterSubmit(before)
		m.feeds.record(d, pd.in)
		return r.afterBurst(tbl, m, before)
	})
	if err != nil {
		t.Fatalf("submit at seq %d: %v", pd.in.Seq, err)
	}
}

// swapOpponentHidden is the swap fixture of TestRedealWorldsIgnoreTheRealHiddenCards
// (internal/azmcts/redeal_test.go), applied to a LIVE host engine: every
// unpinned player-1 hand card is exchanged with a same-named-DIFFERENT library
// card, then player 1's library is reversed — all through SECRET events, so
// the public projection the actor's feed captures is byte-identical while the
// hidden arrangement is different. It returns the number of hand cards
// swapped; zero means nothing could be swapped (a fixture failure the caller
// must report).
func swapOpponentHidden(t *testing.T, e *rules.Engine, feed *searchseat.Feed) int {
	t.Helper()
	pinned := map[string]bool{}
	if feed != nil {
		known, err := feed.Known()
		if err != nil {
			t.Fatalf("known projection: %v", err)
		}
		for _, kh := range known.Hands {
			if kh.Player == 1 {
				for _, c := range kh.Cards {
					pinned[c.Name] = true
				}
			}
		}
	}
	hand, lib := e.G.Zone(state.ZHand, 1), e.G.Zone(state.ZLibrary, 1)
	swapped := 0
	used := map[state.ObjID]bool{}
	for _, h := range append([]state.ObjID(nil), hand...) {
		hn := e.G.Obj(h).Card.Faces[0].Name
		if pinned[hn] {
			continue
		}
		for _, l := range lib {
			if used[l] || e.G.Obj(l).Card.Faces[0].Name == hn {
				continue
			}
			used[l] = true
			events.Emit(e.G, e.L, events.Event{Kind: events.MoveZone, Player: 1, Obj: h, From: state.ZHand, To: state.ZLibrary, Secret: true})
			events.Emit(e.G, e.L, events.Event{Kind: events.MoveZone, Player: 1, Obj: l, From: state.ZLibrary, To: state.ZHand, Secret: true})
			swapped++
			break
		}
	}
	if swapped == 0 {
		return 0
	}
	cur := e.G.Zone(state.ZLibrary, 1)
	rev := make([]state.ObjID, len(cur))
	for i, id := range cur {
		rev[len(cur)-1-i] = id
	}
	events.Emit(e.G, e.L, events.Event{Kind: events.LibraryOrder, Player: 1, IDs: rev, Secret: true})
	return swapped
}

// TestHostedEnvSeatsNeverSeeTheLiveEngine is BP-08's central claim (spec §5.4),
// asserted through a full live host match: every engine an EnvSeat is handed
// across a real game is the honest root — never the live engine, never a
// pointer that aliases it, never reused across DecideEnv calls — and the
// root's opponent hand differs from the live opponent hand at the same
// boundary at least once, which is what proves a REDEAL happened (a plain
// clone of the live engine would pass every pointer check and still leak).
// The deciding seat's own hand, which the seat is entitled to, must match the
// live hand at every boundary: the redeal keeps what the seat knows.
func TestHostedEnvSeatsNeverSeeTheLiveEngine(t *testing.T) {
	t.Parallel()
	var spy *spyEnvSeat
	opts := testOptions(t)
	const seed = uint64(20260928)
	opts.Seats = func(names []string, gotSeed uint64) []seat.Seat {
		spy = &spyEnvSeat{Bot: seat.NewBot(gotSeed ^ 1)}
		return []seat.Seat{spy, seat.NewBot(gotSeed ^ 2)}
	}
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(envTestTable("t1", seed)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	m, engine := matchFor(t, r, "t1")

	// Precondition: the game ran and the Env dispatch actually engaged — a
	// spy that was never parked on DecideEnv would prove nothing here.
	m.mu.RLock()
	intents := m.intents
	feeds := m.feeds
	m.mu.RUnlock()
	if intents < 20 {
		t.Fatalf("match logged only %d intents; too short to assert on", intents)
	}
	if spy.decided < 20 {
		t.Fatalf("DecideEnv answered only %d of %d intents; the Env dispatch never engaged", spy.decided, intents)
	}
	if len(spy.envs) != spy.decided {
		t.Fatalf("env records %d != DecideEnv calls %d", len(spy.envs), spy.decided)
	}
	if feeds == nil || feeds.bySlot[0] == nil {
		t.Fatal("the EnvSeat has no feed; newMatchFeeds is not wired")
	}

	seen := map[*rules.Engine]bool{}
	roots := 0
	for _, rec := range spy.envs {
		if rec.engine == nil {
			t.Fatalf("env decision seq %d built no root (refused %q); the honest path failed on these decks", rec.seq, rec.rootRef)
		}
		if rec.rootRef != "" {
			t.Fatalf("env decision seq %d: root built but RootRefused %q", rec.seq, rec.rootRef)
		}
		if rec.engine == engine {
			t.Fatalf("env decision seq %d: the host handed the EnvSeat the LIVE engine", rec.seq)
		}
		if rec.g == engine.G {
			t.Fatalf("env decision seq %d: the root's game aliases the live engine's", rec.seq)
		}
		if seen[rec.engine] {
			t.Fatalf("env decision seq %d: a root was REUSED after an earlier DecideEnv returned", rec.seq)
		}
		seen[rec.engine] = true
		if rec.feed != feeds.bySlot[0] {
			t.Fatalf("env decision seq %d: Search.Feed is not the actor's own feed", rec.seq)
		}
		if rec.view.Round == 0 {
			t.Fatalf("env decision seq %d: env.View carries no Round", rec.seq)
		}
		roots++
	}
	if roots < 10 {
		t.Fatalf("only %d roots built across %d Env decisions", roots, spy.decided)
	}

	// The live opponent hand at every slot-0 decision boundary, rebuilt by
	// deterministic replay of the finished log.
	liveOpp, liveActor := map[uint64][]string{}, map[uint64][]string{}
	if _, err := replay.Walk(engine.L, m.cfg, len(engine.L.Intents), func(e *rules.Engine, i int) error {
		if d := e.Pending(); d != nil && d.Player == 0 {
			liveOpp[d.Seq] = zoneNames(e.G.Zone(state.ZHand, 1), e)
			liveActor[d.Seq] = zoneNames(e.G.Zone(state.ZHand, 0), e)
		}
		return nil
	}); err != nil {
		t.Fatalf("replay walk: %v", err)
	}

	same, diff := 0, 0
	for _, rec := range spy.envs {
		live, ok := liveOpp[rec.seq]
		if !ok {
			t.Fatalf("no replay boundary for env decision seq %d", rec.seq)
		}
		if sortedEqual(rec.oppHand, live) {
			same++
		} else {
			diff++
		}
		if !sortedEqual(rec.actorHand, liveActor[rec.seq]) {
			t.Fatalf("env decision seq %d: the root redealt the deciding seat's OWN hand (%v, live %v)",
				rec.seq, rec.actorHand, liveActor[rec.seq])
		}
	}
	if diff == 0 {
		t.Fatalf("the root's opponent hand equalled the live opponent hand at all %d Env decisions; no redeal happened", len(spy.envs))
	}
	t.Logf("roots: %d, live-equal opponent hands: %d, redealt-different: %d", roots, same, diff)
}

// TestHostedEnvSeatsIgnoreTheRealHiddenCards is the seat-level leak test
// (spec §5.3): two live matches built from the same table config and seed
// differ ONLY in hidden cards after the swap fixture is applied to the second
// match's live engine (every unpinned opponent hand card exchanged with a
// same-named-different library card, that library reversed — all secret
// events, public state untouched). Each is driven through the REAL host Env
// path — projectNext under the lock, parkSeat outside it — and the decision
// played at the swapped boundary must be answered identically from roots
// whose hidden worlds are name-identical: what an honest root deals is a
// function of the seat's observation and the seed alone, never of the real
// hidden cards. A host that handed a plain clone of the live engine instead
// of the redeal would pass the pointer checks and fail exactly here.
func TestHostedEnvSeatsIgnoreTheRealHiddenCards(t *testing.T) {
	t.Parallel()
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	const seed = uint64(20260928)
	if err := r.AddTable(envTestTable("t1", seed)); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	tbl := r.tables["t1"]
	r.mu.RUnlock()

	build := func() (*match, *spyEnvSeat, []seat.Seat) {
		spy := &spyEnvSeat{Bot: seat.NewBot(seed ^ 1)}
		seats := []seat.Seat{spy, seat.NewBot(seed ^ 2)}
		m, err := r.newMatch(tbl, 0)
		if err != nil {
			t.Fatal(err)
		}
		m.mu.Lock()
		m.slots = seats
		m.feeds = newMatchFeeds(seats)
		m.mu.Unlock()
		return m, spy, seats
	}
	m1, spy1, seats1 := build()
	m2, spy2, seats2 := build()

	swapped := false
	var targetSeq uint64
	swapFn := func(m *match, d *decision.Decision) {
		if swapped {
			return
		}
		if n := swapOpponentHidden(t, m.e, m.feeds.bySlot[0]); n > 0 {
			swapped = true
			targetSeq = d.Seq
		}
	}

	brd1, brd2 := botpolicy.NewBoard(2), botpolicy.NewBoard(2)
	steps := 0
	for ; steps < 20000; steps++ {
		_, pd1 := driveEnvStep(t, m1, seats1, &brd1, nil)
		if pd1 == nil {
			t.Fatal("match 1 ended before the swapped decision was reached")
		}
		_, pd2 := driveEnvStep(t, m2, seats2, &brd2, swapFn)
		if pd2 == nil {
			t.Fatal("match 2 ended before the swapped decision was reached")
		}
		submitEnvStep(t, r, tbl, m1, pd1)
		submitEnvStep(t, r, tbl, m2, pd2)
		if rec1, rec2 := spy1.recorded(targetSeq), spy2.recorded(targetSeq); swapped && rec1 != nil && rec2 != nil {
			break
		}
	}
	if steps >= 20000 || !swapped {
		t.Fatal("fixture: no opponent hand card could be swapped at any Env decision")
	}

	t.Logf("swap target: seq %d at drive step %d; roots real %v, swapped %v", targetSeq, steps, len(spy1.envs), len(spy2.envs))
	rec1, rec2 := spy1.recorded(targetSeq), spy2.recorded(targetSeq)
	if rec1 == nil || rec2 == nil {
		t.Fatalf("the swapped decision (seq %d) was never parked on DecideEnv (records %d/%d)",
			targetSeq, len(spy1.envs), len(spy2.envs))
	}

	// Fixture precondition: the two live positions genuinely differ in hidden
	// cards — the swap did its work and nothing public moved.
	if hiddenWorldsEqual(m1.e, m2.e) {
		t.Fatal("fixture: the two live positions hold the same hidden cards")
	}
	// The Env carried the honest root, not the live engine, in both matches.
	for i, rec := range []*envRecord{rec1, rec2} {
		if rec.engine == nil {
			t.Fatalf("match %d: root refused at the swapped boundary (%q)", i+1, rec.rootRef)
		}
	}
	if rec1.engine == m1.e || rec2.engine == m2.e {
		t.Fatal("the host handed a live engine to an EnvSeat")
	}
	// Central claim: the two roots are name-identical in every hidden zone,
	// and the decision the seat answers is identical in both worlds.
	if !hiddenWorldsEqual(rec1.engine, rec2.engine) {
		t.Fatalf("two roots for the same seed and feed depend on the real hidden cards:\nreal hand %v swapped %v\nreal lib %v swapped %v",
			rec1.oppHand, rec2.oppHand, rec1.oppLib, rec2.oppLib)
	}
	if !reflect.DeepEqual(rec1.in, rec2.in) {
		t.Fatalf("the two matches answered the swapped decision differently: %+v vs %+v", rec1.in, rec2.in)
	}
	// The deciding seat's own zones are untouched by the fixture and must
	// match in the roots too (the redeal keeps what the seat knows).
	if !sortedEqual(rec1.actorHand, rec2.actorHand) {
		t.Fatal("the two roots dealt the actor's own hand differently")
	}
}

// TestEnvRootRefusalPlaysFallback drives a refusal through the REAL host path
// (spec §5.1): once the actor's feed is stopped — the host-side state a failed
// capture or a failed Record leaves (§5.2) — the seat must still be ASKED,
// with env.Search.Engine nil and env.RootRefused set to HonestRoot's
// "no live observation feed" reason, and must still answer (its DecideBoard
// fallback), so the match never wedges. A healthy root at the first Env
// decision is asserted first, so the nil engine below is the refusal and not
// the norm, and the counter is asserted to have counted exactly the refusal.
func TestEnvRootRefusalPlaysFallback(t *testing.T) {
	t.Parallel()
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	const seed = uint64(20260929)
	if err := r.AddTable(envTestTable("t1", seed)); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	tbl := r.tables["t1"]
	r.mu.RUnlock()
	spy := &spyEnvSeat{Bot: seat.NewBot(seed ^ 1)}
	seats := []seat.Seat{spy, seat.NewBot(seed ^ 2)}
	m, err := r.newMatch(tbl, 0)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.slots = seats
	m.feeds = newMatchFeeds(seats)
	m.mu.Unlock()

	healthySeen := false
	var refused *parkedData
	brd := botpolicy.NewBoard(2)
	for steps := 0; steps < 20000 && refused == nil; steps++ {
		stopFeed := healthySeen
		data, pd := driveEnvStep(t, m, seats, &brd, func(m *match, d *decision.Decision) {
			if stopFeed && m.feeds != nil && m.feeds.bySlot[0] != nil {
				// The state §5.2 leaves behind after a failed capture or
				// Record: the feed is dead for the rest of the game.
				m.feeds.stopped[0] = true
			}
		})
		if data == nil {
			t.Fatal("match ended before a refused root was answered")
		}
		if data.env == nil {
			submitEnvStep(t, r, tbl, m, pd) // slot 1's decision; keep driving
			continue
		}
		if !healthySeen {
			if data.env.Search.Engine == nil || data.env.RootRefused != "" {
				t.Fatalf("the FIRST Env decision (seq %d) built no healthy root: %q", data.dc.Seq, data.env.RootRefused)
			}
			healthySeen = true
			submitEnvStep(t, r, tbl, m, pd)
			continue
		}
		refused = data
		// The refusal is HonestRoot's canonical dead-feed reason, routed
		// through the host-side stopped bit (see envData).
		if data.env.RootRefused != "no live observation feed" {
			t.Fatalf("refused root reason %q, want the canonical dead-feed reason", data.env.RootRefused)
		}
		// §5.2: "the seat plays its fallback from then on" — the stopped feed
		// is not handed to the seat at all (a live fd would let an adapter
		// read a History that misses every future recorded answer).
		if data.env.Search.Feed != nil {
			t.Fatal("the refused Env handed the seat a stopped feed")
		}
		t.Logf("refused root at env decision seq %d (%q) after %d steps", data.dc.Seq, data.env.RootRefused, steps)
		// The fallback proof: the parked answer is exactly the wrapped bot's
		// DecideBoard answer from a freshly built copy of the same board.
		var want decision.Intent
		err := m.locked(func() error {
			fresh := botpolicy.NewBoard(2)
			want, _ = spy.Bot.DecideBoard(context.Background(), botpolicy.BoardFromGameInto(m.e.G, m.e, 0, &fresh), data.dc)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pd.in, want) {
			t.Fatalf("the refused root's seat did not play its DecideBoard fallback: %+v vs %+v", pd.in, want)
		}
		submitEnvStep(t, r, tbl, m, pd)
	}
	if !healthySeen {
		t.Fatal("no healthy root was ever built; the refusal half proves nothing")
	}
	if refused == nil {
		t.Fatal("the stopped feed never produced a refused root")
	}
	if refused.env.Search.Engine != nil {
		t.Fatal("a root was built from a dead feed")
	}
	if refused.env.RootRefused != "no live observation feed" {
		t.Fatalf("a refused root carried %q, not the fail-closed reason", refused.env.RootRefused)
	}
	if refused.env.Search.Feed != nil {
		t.Fatal("the refused Env did carry the stopped feed")
	}
	m.mu.RLock()
	rootRefusals := m.rootRefusals
	m.mu.RUnlock()
	if rootRefusals != 1 {
		t.Fatalf("rootRefusals = %d, want 1", rootRefusals)
	}
	m.mu.RLock()
	sc := m.sidecar()
	m.mu.RUnlock()
	if sc.RootRefusals != 1 {
		t.Fatalf("sidecar RootRefusals = %d, want 1", sc.RootRefusals)
	}
}
