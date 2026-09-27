package host

// Task M2c-3 pins the contract mtgserve's match_events adapter will rely
// on: an embedder that persists every OnBurst into its own store must be
// able to serve a finished match's EventsSeat/ViewAtSeat from that store
// exactly as the live match served them. In gorge terms this test is that
// guarantee with the host's own files never consulted:
//
//   (a) the sink's data alone replays (replay.Replay over a log rebuilt
//       from the sink, seed included) to the live chain head;
//   (b) the same log, built into a read-side match by the host's own build
//       (matchForLog — the code loadArchived runs for disk), serves
//       EventsSeat(t,k,0,p) and ViewAtSeat(t,k,head,p) byte-identically
//       to the live projection, through the real Registry read path.
//
// (b) is the half that catches a divergence between the persisted and live
// read paths, so it must not compare a projection to itself: the served
// match is the sink-built one (proven by lookup identity), the live side is
// taken before it is installed, and positive content — opponents' hands
// redacted and the seat's own secret draws surfacing in the persisted stream
// — is asserted outright so an all-empty run cannot pass.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestObserverPersistedLogServesIdenticallyToTheLiveMatch is Task M2c-3's
// one acceptance test. The table is a real human-seated one
// (TableConfig.Humans, seat 0 answered through Pending/SubmitIntent) — the
// configuration mtgserve will actually run — played to a finished, archived
// match on a disk registry while OnBurst captured every burst. Like the
// package's other human-driven integration test it is deliberately not
// t.Parallel: it is a full repo-deck match plus two full replays, and the
// sequential phase keeps it out of the match-slot fight.
func TestObserverPersistedLogServesIdenticallyToTheLiveMatch(t *testing.T) {
	names := testutil.RepoDeckNames()
	if len(names) < 2 {
		t.Skip("testutil has fewer than 2 repo decks")
	}
	decks := []string{names[0], names[1]}

	// The sink: every burst the match produced, plus the finished MatchInfo.
	var (
		bursts  []burstObs
		endInfo protocol.MatchInfo
	)
	o := diskOptions(t, t.TempDir())
	o.LoadDeck = repoDeckLoader(t)
	o.OnBurst = func(_ TableID, _ int, evs []events.Event, in *decision.Intent) error {
		bursts = append(bursts, burstObs{evs: append([]events.Event(nil), evs...), in: in})
		return nil
	}
	// The finished match is captured here, while it is still the table's
	// current match: in persistence mode a finished match is not retained
	// in memory (it is served from disk), and half (b) needs the live
	// engine itself as the reference.
	var (
		r    *Registry
		live *match
	)
	o.OnMatchEnd = func(id TableID, _ int, m protocol.MatchInfo) error {
		endInfo = m
		if r != nil {
			r.mu.RLock()
			tb := r.tables[id]
			r.mu.RUnlock()
			tb.mu.RLock()
			live = tb.cur
			tb.mu.RUnlock()
		}
		return nil
	}
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cfg := TableConfig{ID: "t1", Name: "observer-persisted", Seats: 2, Decks: decks,
		Seed: 99, Pace: 0, Spectator: view.Omniscient, Humans: []int{0}, Perpetual: false}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	driveHumanSeat(t, r, "t1") // answers every decision asked of seat 0
	r.Wait("t1")

	r.mu.RLock()
	tb := r.tables["t1"]
	r.mu.RUnlock()
	tb.mu.Lock()
	m := live
	var sc sidecar
	for _, a := range tb.archived {
		if a.Match == 1 {
			sc = a
		}
	}
	// Serve the live match through the real lookup path (the single-slot
	// archived cache), so the "live" projections below come from the live
	// engine rather than a disk rebuild.
	tb.loaded = m
	tb.mu.Unlock()
	if m == nil || m.state != protocol.MatchFinished {
		t.Fatalf("the human-driven match did not finish: %+v", m)
	}
	if sc.Match != 1 {
		t.Fatal("match 1 was never archived; there is no sidecar to serve a rebuilt match against")
	}
	head := uint64(len(m.e.L.Events) - 1)

	// The sink's own record of the finish agrees with the live match, and
	// the sink saw the whole chain: genesis plus one burst per decision.
	if endInfo.State != protocol.MatchFinished || endInfo.Match != 1 {
		t.Fatalf("OnMatchEnd info %+v, want the finished match 1", endInfo)
	}
	if endInfo.Head != m.e.L.Head() {
		t.Fatalf("OnMatchEnd head %s, live head %s", endInfo.Head, m.e.L.Head())
	}
	var sinkEvs []events.Event
	var sinkIns []decision.Intent
	for _, b := range bursts {
		sinkEvs = append(sinkEvs, b.evs...)
		if b.in != nil {
			sinkIns = append(sinkIns, *b.in)
		}
	}
	if len(bursts) != 1+len(m.e.L.Intents) {
		t.Fatalf("saw %d bursts, want genesis + %d intents", len(bursts), len(m.e.L.Intents))
	}
	if len(sinkEvs) != len(m.e.L.Events) {
		t.Fatalf("sink saw %d events, the match log has %d", len(sinkEvs), len(m.e.L.Events))
	}
	if sinkEvs[0].Seq != 0 || sinkEvs[len(sinkEvs)-1].Kind != events.GameOver {
		t.Fatalf("sink stream does not span seq 0..GameOver (first seq %d, last kind %v)",
			sinkEvs[0].Seq, sinkEvs[len(sinkEvs)-1].Kind)
	}

	// Half (a): the sink's own data — not the host's files — replays to the
	// live chain head. The log is rebuilt from the sink alone: burst events
	// in order, burst intents in order, and the seed from the sink's own
	// MatchInfo. The replay config is exactly the live match's (configuration,
	// not log data).
	l := events.NewLog(endInfo.Seed)
	for _, ev := range sinkEvs {
		l.Append(ev)
	}
	l.Intents = sinkIns
	rep, err := replay.Replay(l, m.cfg)
	if err != nil {
		t.Fatalf("sink log does not replay: %v", err)
	}
	if got, want := rep.L.Head(), m.e.L.Head(); got != want {
		t.Fatalf("sink replay head %s, live head %s", got, want)
	}

	// The live seat projections, taken before the sink-built match is
	// installed: this is what the persisted read path must reproduce.
	var liveEvs0, liveEvs1 []protocol.EventBody
	for _, p := range []state.PlayerID{0, 1} {
		evs, err := r.EventsSeat("t1", 1, 0, p)
		if err != nil {
			t.Fatalf("live EventsSeat(seat %d): %v", p, err)
		}
		if p == 0 {
			liveEvs0 = evs
		} else {
			liveEvs1 = evs
		}
		if len(evs) != len(m.e.L.Events) {
			t.Fatalf("live seat %d stream holds %d bodies, want the whole %d-event log", p, len(evs), len(m.e.L.Events))
		}
	}
	liveViews := make([]view.View, 2)
	for p := 0; p < 2; p++ {
		v, err := r.ViewAtSeat("t1", 1, head, state.PlayerID(p))
		if err != nil {
			t.Fatalf("live ViewAtSeat(seat %d): %v", p, err)
		}
		liveViews[p] = v
	}
	// Both opponent views redact the hand. CR 800.4a legitimately empties a
	// departed seat's zones, so positive secret content is asserted against a
	// historical draw below rather than assuming either final hand is nonempty.
	if liveViews[0].Players[1].Hand != nil || liveViews[1].Players[0].Hand != nil {
		t.Fatal("a live view reads the opponent's hand")
	}

	// Half (b): serve the same match from the sink. matchForLog is the
	// host's own read-side build — the code loadArchived runs for disk —
	// fed the sink-rebuilt log; installing the result as the archived
	// match makes EventsSeat/ViewAtSeat serve it through the real lookup
	// path, exactly as a cold instance would serve a rebuilt log.
	sm, err := r.matchForLog(tb, sc, l)
	if err != nil {
		t.Fatalf("sink log does not build into a servable match: %v", err)
	}
	tb.mu.Lock()
	tb.loaded = sm
	tb.mu.Unlock()
	if _, got, err := r.lookup("t1", 1); err != nil || got != sm {
		t.Fatalf("the serve path reached the live match (%p), not the sink-built one (%p): %v — half (b) would compare a projection to itself",
			got, sm, err)
	}

	var sinkEvs0 []protocol.EventBody
	for _, p := range []state.PlayerID{0, 1} {
		evs, err := r.EventsSeat("t1", 1, 0, p)
		if err != nil {
			t.Fatalf("sink EventsSeat(seat %d): %v", p, err)
		}
		v, err := r.ViewAtSeat("t1", 1, head, p)
		if err != nil {
			t.Fatalf("sink ViewAtSeat(seat %d): %v", p, err)
		}
		if p == 0 {
			sinkEvs0 = evs
		}
		wantEvs, wantView := liveEvs0, liveViews[0]
		if p == 1 {
			wantEvs, wantView = liveEvs1, liveViews[1]
		}
		if !reflect.DeepEqual(evs, wantEvs) {
			if i := firstBodyDiff(evs, wantEvs); i >= 0 {
				t.Fatalf("seat %d: sink-served events differ from the live projection at body %d:\n%+v\nvs\n%+v",
					p, i, evs[i], wantEvs[i])
			}
			t.Fatalf("seat %d: sink-served events: %d bodies, live has %d", p, len(evs), len(wantEvs))
		}
		if !reflect.DeepEqual(v, wantView) {
			t.Fatalf("seat %d: sink-served head view differs from the live projection", p)
		}
		// The persisted side projects every seat's own hand, and nobody else's.
		if v.Players[p].Hand == nil {
			t.Fatalf("seat %d: sink view redacts the seat's own hand", p)
		}
		for i := range v.Players {
			if pv := v.Players[i]; pv.ID != p && pv.Hand != nil {
				t.Fatalf("seat %d: sink view reads seat %d's hand", p, pv.ID)
			}
		}
	}

	// The seat redaction, positively, on the persisted side: seat 0's latest
	// secret draw surfaces in its own sink stream with the card. It need not
	// remain in hand: CR 800.4a removes a departed owner's cards at game end.
	g := sm.e.G
	var last events.Event
	for i := len(sm.e.L.Events) - 1; i >= 0; i-- {
		ev := sm.e.L.Events[i]
		if ev.Kind == events.Draw && ev.Player == 0 {
			last = ev
			break
		}
	}
	if last.Obj == 0 {
		t.Fatal("seat 0 has no logged draw; the redaction assertion would be vacuous")
	}
	bySeq := make(map[uint64]protocol.EventBody, len(sinkEvs0))
	for _, b := range sinkEvs0 {
		bySeq[b.Event.Seq] = b
	}
	b, ok := bySeq[last.Seq]
	if !ok {
		t.Fatalf("seat 0's own draw at seq %d is missing from its sink stream", last.Seq)
	}
	if b.Event.Obj != uint32(last.Obj) {
		t.Fatalf("seat 0's sink stream lost the card it drew at seq %d: obj %d, want %d", last.Seq, b.Event.Obj, last.Obj)
	}
	if name := g.Obj(last.Obj).Face().Name; !strings.Contains(b.Line, name) {
		t.Fatalf("seat 0's sink stream does not name its own drawn card %q: %q", name, b.Line)
	}
}

// firstBodyDiff is reflect.DeepEqual with a location: the index of the
// first body where a and b differ, or -1 when equal or when only the
// lengths differ. It lets the equality assertion name the divergence
// instead of printing two multi-thousand-element slices.
func firstBodyDiff(a, b []protocol.EventBody) int {
	if len(a) != len(b) {
		return -1
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return i
		}
	}
	return -1
}
