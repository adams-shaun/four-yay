package host

// fb-20260915T094418Z: a parked mid-burst decision made every view request
// at head return HTTP 500. The root cause was the host's burst-boundary
// model: a burst does not always end at its last DecisionAsk — Engine.
// Submit's post-ask continuation (the SBA pass and the step handler the
// ask interrupted) can still emit events after the ask that parks the
// decision — and boundsOf/viewAt treated the last ask as the end of the
// log. These tests pin the fixed model end to end on a deterministic
// commander fixture that reaches the reported shape: a burst whose SBA
// pass asks the CR 903.9 commander_zone replacement decision and then
// keeps sweeping lethal-damage creatures past it.
//
// The fixture is measured, not constructed: decks foundations-wretched-
// ranks vs foundations-reign-of-dragons at seed 1111 (match k=1, the slot
// takeMatchSlot reserves), burst 407 — the resolving spell deals damage to
// three creatures in one pass, its caster's priority returns, seat 1's
// commander_zone is asked (obj 214 died in the pass), and two more
// "lethal damage" sweeps land AFTER the ask. The park lands on that ask;
// replaying the recorded intents reproduces the tail byte for byte. The
// shape is re-derived from the live match below, so a corpus or engine
// move fails here loudly instead of testing nothing.
//
// Re-measured 2026-09-17 (rv2d static variable P/T): the previous measured
// shape (seed 1111, burst 407) stopped existing — the live course's burst
// 407 ask no longer parks (its overshoot tail moved), so seed 1111 now ends
// with no commander_zone overshoot burst to park on. Same decks, new
// measured seed (found by replaying the pure-bot course across seeds and
// taking the first whose burst carries seat 1's commander_zone ask with the
// post-ask lethal tail).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/internal/testutil/feedback"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

const (
	overshootDeckA = "foundations-wretched-ranks"
	overshootDeckB = "foundations-reign-of-dragons"
	overshootSeed  = 8
	// cli-20260922T225141Z-e771720d: the hosted game now opens with the CR
	// 103.1 winner-chooses ask, whose answer is the toss winner naming
	// itself -- one recorded intent at genesis -- so every later intent
	// (including the overshoot burst's own) sits one later in the count.
	// The Menace floor removed one forced-empty blockers ask at intent 194.
	overshootIntents = 378 // intents recorded when parked on the overshoot burst's pending ask
)

// gateSeat is a bot behind a test gate: every decision is signalled to the
// test, which releases it one at a time, so the match can be held at a
// known point. Same shape as cmd/repro's fixture generator uses. bot must
// be exactly the one defaultSeats would have built for the slot (seed ^
// slot+1), so the gated match replays the pure-bot game's prefix.
type gateSeat struct {
	bot     seat.Seat
	reached chan struct{}
	release chan struct{}
}

func (g *gateSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	select {
	case g.reached <- struct{}{}:
	case <-ctx.Done():
		return decision.Intent{}, ctx.Err()
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return decision.Intent{}, ctx.Err()
	}
	return g.bot.Decide(ctx, v, d)
}

// parkedOvershootMatch builds a two-seat commander table with both seats
// gated, drives it to the measured overshoot burst, and holds it there:
// parked on the burst's pending commander_zone ask, with the burst's
// post-ask tail already on the log. A non-empty dir runs the match with
// persistence on (the archived-load tests); empty is memory mode.
func parkedOvershootMatch(t *testing.T, dir string) (*Registry, *table, *match, [2]*gateSeat) {
	t.Helper()
	takeMatchSlot(t)
	gates := [2]*gateSeat{
		{reached: make(chan struct{}), release: make(chan struct{})},
		{reached: make(chan struct{}), release: make(chan struct{})},
	}
	o := Options{
		LoadDeck: commanderDeckLoader(t),
		// Tokens must carry the live corpus token map, exactly as
		// cmd/repro's gatedFixtureRegistry does: without it r.opts.Tokens
		// is nil (host/match.go builds each match's rules.Config.Tokens
		// from r.opts.Tokens), so any token-minting ability the live match
		// hits falls back to the engine's "unimplemented API" Note stand-in
		// instead of actually minting. feedback.Load's own token resolution
		// falls back to the full corpus map when a capture records no
		// tokens, so a token-starved live capture replays differently
		// (Note vs a real token_create) the moment it is reloaded --
		// discovered regenerating fb-20260915T094418Z's committed fixture.
		Tokens:       testutil.CorpusRegistry(t).Tokens,
		NameUniverse: testutil.CorpusRegistry(t).Cards,
		Seats: func(names []string, seed uint64) []seat.Seat {
			// defaultSeats' exact bots: seed ^ slot+1. Written from the
			// match goroutine before the first park signals; the test never
			// reads bot, so there is no cross-goroutine read to race.
			gates[0].bot = seat.NewBot(seed ^ 1)
			gates[1].bot = seat.NewBot(seed ^ 2)
			return []seat.Seat{gates[0], gates[1]}
		},
		Sleep: func(time.Duration, <-chan struct{}) {},
	}
	if dir != "" {
		o.Dir = dir
	}
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if err := r.AddTable(TableConfig{ID: "t1", Name: "Table t1", Seats: 2,
		Decks: []string{overshootDeckA, overshootDeckB}, Seed: overshootSeed,
		Pace: 0, Spectator: view.Omniscient, Format: FormatCommander}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	tb := r.tables["t1"]
	r.mu.RUnlock()
	// Drive one gated decision at a time until intents == overshootIntents,
	// then hold: the pending park at that count is the overshoot burst's
	// own ask. The signal received when the count already names it is that
	// park (the submit that produced the count runs before its park), so
	// holding there parks the match exactly on the reported decision.
	for {
		select {
		case <-gates[0].reached:
		case <-gates[1].reached:
		case <-time.After(30 * time.Second):
			t.Fatal("the gated match never reached the overshoot burst")
		}
		tb.mu.RLock()
		m := tb.cur
		tb.mu.RUnlock()
		m.mu.RLock()
		n := m.intents
		m.mu.RUnlock()
		if n >= overshootIntents {
			return r, tb, m, gates
		}
		select {
		case gates[0].release <- struct{}{}:
		case gates[1].release <- struct{}{}:
		case <-time.After(30 * time.Second):
			t.Fatal("the gated match never accepted a release")
		}
	}
}

// assertOvershootShape re-derives the measured shape from the parked match,
// so a corpus or engine move that shifts it fails here with a name instead
// of silently exercising something else. Returns the parked ask's seq and
// the burst's recorded end (one past the log's last event).
func assertOvershootShape(t *testing.T, m *match) (askSeq uint64, burstEnd uint64) {
	t.Helper()
	if got := m.intents; got != overshootIntents {
		t.Fatalf("fixture parked at %d intents, want %d — the measured shape moved", got, overshootIntents)
	}
	if len(m.bounds) != overshootIntents+1 {
		t.Fatalf("fixture has %d burst boundaries, want %d", len(m.bounds), overshootIntents+1)
	}
	lo, hi := m.bounds[overshootIntents-1], m.bounds[overshootIntents]
	damages, lastAsk := 0, uint64(0)
	for s := lo; s < hi; s++ {
		switch m.e.L.Events[s].Kind {
		case events.Damage:
			damages++
		case events.DecisionAsk:
			lastAsk = s
		}
	}
	if lastAsk == 0 || hi <= lastAsk+1 {
		t.Fatalf("burst %d carries no overshoot tail (last ask %d, burst end %d) — the measured shape moved",
			overshootIntents, lastAsk, hi)
	}
	ask := m.e.L.Events[lastAsk]
	if ask.Text != "commander_zone" || ask.Player != 1 {
		t.Fatalf("overshoot ask at %d is %q for seat %d, want commander_zone for seat 1 — the measured shape moved",
			lastAsk, ask.Text, ask.Player)
	}
	lethal := 0
	for s := lastAsk + 1; s < hi; s++ {
		if ev := m.e.L.Events[s]; ev.Kind == events.MoveZone && ev.Text == "lethal damage" {
			lethal++
		}
	}
	if lethal == 0 {
		t.Fatal("no lethal-damage sweep landed after the ask — the measured shape moved")
	}
	if damages < 2 {
		t.Fatalf("burst carries %d damage events, want the shared-lethal pass's several", damages)
	}
	d := m.e.Pending()
	if d == nil || d.Kind != decision.KCommanderZone || d.Player != 1 {
		t.Fatalf("parked pending is %+v, want seat 1's commander_zone — the measured shape moved", d)
	}
	return lastAsk, hi
}

// TestParkedOvershootViewAtHeadServesTheDecision is the reported defect's
// regression: at head — and at every seq of the parked burst's tail, which
// the old model had no boundary for — the seat view must serve, and at head
// it must carry the pending commander_zone decision with its two options,
// because the decision reaches the client only through the seat-scoped view.
func TestParkedOvershootViewAtHeadServesTheDecision(t *testing.T) {
	t.Parallel()
	r, _, m, _ := parkedOvershootMatch(t, "")
	askSeq, burstEnd := assertOvershootShape(t, m)
	head := head(m)
	if head != burstEnd-1 {
		t.Fatalf("head %d, want the burst's recorded end %d", head, burstEnd-1)
	}

	// The seat's own view at head: the fetch that used to 500.
	v, err := r.ViewAtSeat("t1", m.k, head, 1)
	if err != nil {
		t.Fatalf("ViewAtSeat(head) failed: %v", err)
	}
	if v.Decision == nil || v.Decision.Kind != decision.KCommanderZone {
		t.Fatalf("head view carries %+v, want the commander_zone decision", v.Decision)
	}
	if len(v.Decision.Options) != 2 {
		t.Fatalf("commander_zone offers %d options, want 2", len(v.Decision.Options))
	}
	if v.Decision.Options[0].Kind != "command_zone" || v.Decision.Options[1].Kind != "leave" {
		t.Fatalf("commander_zone options are %q then %q, want command_zone then leave",
			v.Decision.Options[0].Kind, v.Decision.Options[1].Kind)
	}
	// The other seat's view at head serves too — it just carries no decision.
	other, err := r.ViewAtSeat("t1", m.k, head, 0)
	if err != nil {
		t.Fatalf("ViewAtSeat(head) for seat 0 failed: %v", err)
	}
	if other.Decision != nil {
		t.Fatalf("seat 0's view carries seat 1's decision")
	}

	// The ask's own seq and every seq of the tail: served. The decision
	// attaches only at head — a historical view never misrepresents what
	// was pending; the tail seqs' projection is the burst's end state, the
	// tolerance viewAt documents for the shape.
	for seq := askSeq; seq < burstEnd; seq++ {
		got, err := r.ViewAtSeat("t1", m.k, seq, 1)
		if err != nil {
			t.Fatalf("ViewAtSeat(%d) failed: %v", seq, err)
		}
		if seq != head && got.Decision != nil {
			t.Fatalf("ViewAtSeat(%d) carries a decision; only head does", seq)
		}
	}

	// The spectator board at head serves as well.
	if _, err := r.ViewAt("t1", m.k, head); err != nil {
		t.Fatalf("ViewAt(head) failed: %v", err)
	}

	// Seqs below the burst behave exactly as before: the previous burst's
	// last seq and its interior are served by the ordinary boundary path.
	prev := m.bounds[overshootIntents-1] - 1
	for _, seq := range []uint64{prev, prev - 1} {
		if _, err := r.ViewAtSeat("t1", m.k, seq, 1); err != nil {
			t.Fatalf("ViewAtSeat(%d) below the burst failed: %v", seq, err)
		}
	}

	// The two read paths (snapshot replay and from-genesis replay) must
	// agree at the tail seqs — the genesis path carries ReplayTo's own
	// byte-compare, the snapshot path viewAt's tail verification.
	for _, seq := range []uint64{askSeq, head} {
		fast, err := viewAt(m.cfg, m.e.L, m.snaps, seq, view.NoSeat, view.Omniscient, nil)
		if err != nil {
			t.Fatalf("viewAt(%d) snapshot path: %v", seq, err)
		}
		slow, err := viewAt(m.cfg, m.e.L, nil, seq, view.NoSeat, view.Omniscient, nil)
		if err != nil {
			t.Fatalf("viewAt(%d) genesis path: %v", seq, err)
		}
		if a, b := viewJSON(t, fast), viewJSON(t, slow); a != b {
			t.Fatalf("seq %d: snapshot path differs from the genesis replay at the overshoot tail", seq)
		}
	}
}

// TestParkedOvershootSnapshotForFeedbackIsComplete pins the feedback half:
// the capture must carry the seat view (the old capture lost it —
// "view unavailable: seq beyond head"), a log whose tail past the last ask
// is present, and a log that replays byte-exactly to its own recorded head.
func TestParkedOvershootSnapshotForFeedbackIsComplete(t *testing.T) {
	t.Parallel()
	r, _, m, _ := parkedOvershootMatch(t, "")
	askSeq, burstEnd := assertOvershootShape(t, m)
	seat1 := state.PlayerID(1)
	snap, err := r.SnapshotForFeedback("t1", &seat1)
	if err != nil {
		t.Fatalf("SnapshotForFeedback: %v", err)
	}
	if snap.ViewErr != "" {
		t.Fatalf("snapshot lost the seat view: %s", snap.ViewErr)
	}
	if snap.View == nil {
		t.Fatal("snapshot carries no view")
	}
	if snap.View.Decision == nil || snap.View.Decision.Kind != decision.KCommanderZone || len(snap.View.Decision.Options) != 2 {
		t.Fatalf("snapshot view carries %+v, want the two-option commander_zone decision", snap.View.Decision)
	}
	if got := len(snap.Log.Events); got != int(burstEnd) {
		t.Fatalf("captured log holds %d events, want the full %d — the tail must not be trimmed", got, burstEnd)
	}
	if snap.Log.Events[askSeq].Text != "commander_zone" {
		t.Fatalf("captured ask at %d is %q", askSeq, snap.Log.Events[askSeq].Text)
	}
	// The head is the chain over the captured events, and the log replays
	// byte-exactly to it — the contract cmd/repro verifies a capture with.
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()
	if want := snap.Log.HeadAt(len(snap.Log.Events)); snap.Log.Head != want {
		t.Fatalf("captured head %q, chain over its own events %q", snap.Log.Head, want)
	}
	e, err := replay.Replay(&snap.Log.Log, cfg)
	if err != nil {
		t.Fatalf("captured log does not replay: %v", err)
	}
	if len(e.L.Events) != len(snap.Log.Events) {
		t.Fatalf("replay produced %d events, the capture has %d", len(e.L.Events), len(snap.Log.Events))
	}
	if got := e.L.Head(); got != snap.Log.Head {
		t.Fatalf("replayed head %s, captured %s", got, snap.Log.Head)
	}
}

// TestUndoRewindsAcrossAnOvershootBurst pins the undo/rewind regression the
// brief calls out: rewindToLastIntent's rewind point is m.bounds — recorded
// by afterSubmit as the TRUE burst ends, overshoot tails included — so undo
// must keep working on a match whose history carries overshoot bursts, and
// resubmitting the undone intent must reproduce the overshoot burst byte
// for byte. The call is the internal one (not Registry.Undo) because the
// fixture's seats are gates, not HumanSeats; the rewind point arithmetic
// and the replay are the same code either way. The still-parked gate on the
// abandoned commander_zone ask is left blocked; the cleanup's Close cancels
// its ctx, exactly as the cmd/repro fixture generator tears its own park
// down.
func TestUndoRewindsAcrossAnOvershootBurst(t *testing.T) {
	t.Parallel()
	r, tb, m, _ := parkedOvershootMatch(t, "")
	assertOvershootShape(t, m)
	// The overshoot burst is intent overshootIntents-1's (537); the requester
	// is its own player.
	before := m.intents
	tailStart := m.bounds[before-1]
	saved := append([]events.Event(nil), m.e.L.Events[tailStart:]...)
	in := m.e.L.Intents[before-1]
	brd := botpolicy.NewBoard(2)
	var data *parkedData
	err := m.locked(func() error {
		var err error
		data, err = r.rewindToLastIntent(tb, m, m.slots, &brd, in.Player)
		return err
	})
	if err != nil {
		t.Fatalf("rewind across the overshoot burst: %v", err)
	}
	if m.intents != before-1 || len(m.bounds) != before {
		t.Fatalf("after rewind: %d intents, %d bounds; want %d and %d", m.intents, len(m.bounds), before-1, before)
	}
	if got := len(m.e.L.Events); got != int(tailStart) {
		t.Fatalf("rewound log holds %d events, want the undo boundary %d", got, tailStart)
	}
	if data == nil || data.dc.Player != in.Player {
		t.Fatalf("rewound pending is %+v, want seat %d's decision", data, in.Player)
	}
	// Resubmit the undone intent: the overshoot burst replays byte for byte.
	err = m.locked(func() error { return m.e.Submit(in) })
	if err != nil {
		t.Fatalf("resubmitting the undone intent: %v", err)
	}
	replayed := m.e.L.Events[tailStart:]
	if len(replayed) != len(saved) {
		t.Fatalf("resubmitted burst produced %d events, the original had %d", len(replayed), len(saved))
	}
	for i := range saved {
		if string(saved[i].Append(nil)) != string(replayed[i].Append(nil)) {
			t.Fatalf("resubmitted burst event %d differs from the original", tailStart+uint64(i))
		}
	}
	// The seat view at the rewound head serves, decision attached at head.
	if _, err := r.ViewAtSeat("t1", m.k, head(m), in.Player); err != nil {
		t.Fatalf("ViewAtSeat(head) after rewind: %v", err)
	}
}

// TestArchivedParkedTailMatchLoadsAfterRestart pins defect 3: a match
// parked (here: ended by Close) on an overshoot burst persists the full
// tail, readLog's reconcileLog trims it to the last ask at load, and the
// replay of the recorded intents runs past the trimmed end — which used to
// make matchForLog reject the whole match ("does not replay") and leave
// every view of the archived match dead. The tolerance (the sidecar's
// chain-verified head) must load it, and the views must serve the tail.
func TestArchivedParkedTailMatchLoadsAfterRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r, _, m, _ := parkedOvershootMatch(t, dir)
	askSeq, burstEnd := assertOvershootShape(t, m)
	k := m.k
	r.Close() // the restart's kill: the live match gets its terminal transition

	r2, err := New(Options{
		LoadDeck:     commanderDeckLoader(t),
		Tokens:       testutil.CorpusRegistry(t).Tokens,
		NameUniverse: testutil.CorpusRegistry(t).Cards,
		Sleep:        func(time.Duration, <-chan struct{}) {},
		Dir:          dir,
	})
	if err != nil {
		t.Fatalf("restarting on %s: %v", dir, err)
	}
	t.Cleanup(func() { r2.Close() })
	// The persisted stream is the full burst (tail included); the load
	// path trims and replays past the trim — the tolerated shape.
	sc, ok := r2.tables["t1"].archivedMatch(k)
	if !ok {
		t.Fatal("the restarted registry does not know the match")
	}
	if got := sc.Events; got != int(burstEnd) {
		t.Fatalf("sidecar records %d events, want the persisted %d", got, burstEnd)
	}
	if sc.Head == "" {
		t.Fatal("sidecar carries no head to verify the tail with")
	}
	head := uint64(sc.Events - 1)
	// The archived match's board serves — at head and inside the tail.
	for _, seq := range []uint64{askSeq, head} {
		if _, err := r2.ViewAt("t1", k, seq); err != nil {
			t.Fatalf("archived ViewAt(%d): %v", seq, err)
		}
		if _, err := r2.ViewAtSeat("t1", k, seq, 1); err != nil {
			t.Fatalf("archived ViewAtSeat(%d): %v", seq, err)
		}
	}
	// And the events endpoint serves the tail: the post-ask sweep is there.
	bodies, err := r2.Events("t1", k, askSeq)
	if err != nil {
		t.Fatalf("archived Events: %v", err)
	}
	found := false
	for _, b := range bodies {
		if b.Event.Kind == "move_zone" && b.Event.Text == "lethal damage" {
			found = true
		}
	}
	if !found {
		t.Fatal("the archived match's events lost the post-ask lethal-damage sweep")
	}
}

// TestCrashedMatchFeedbackCaptureStillTrims pins the other half of the
// feedback change: the reconcile is skipped only for a match whose log is
// provably complete under the read lock (every non-crashed state). A
// CRASHED match's log can carry an orphan tail and poison intent (D15),
// and its capture must still come back trimmed to the replayable prefix —
// the same reconcileLog the file-path tests pin. The state is forced on
// the parked fixture because no seat can make the ENGINE panic mid-Submit
// from a test; the conditional reads m.state and nothing else.
func TestCrashedMatchFeedbackCaptureStillTrims(t *testing.T) {
	t.Parallel()
	r, _, m, _ := parkedOvershootMatch(t, "")
	_, burstEnd := assertOvershootShape(t, m)
	seat1 := state.PlayerID(1)
	m.mu.Lock()
	m.state = protocol.MatchCrashed
	m.mu.Unlock()
	snap, err := r.SnapshotForFeedback("t1", &seat1)
	if err != nil {
		t.Fatalf("SnapshotForFeedback: %v", err)
	}
	if got := len(snap.Log.Events); got >= int(burstEnd) {
		t.Fatalf("crashed capture holds %d events, want the trimmed prefix (below the burst end %d)", got, burstEnd)
	}
	last := snap.Log.Events[len(snap.Log.Events)-1]
	if last.Kind != events.DecisionAsk {
		t.Fatalf("crashed capture ends with %s, want the DecisionAsk boundary", last.Kind)
	}
}

// committedCaptureRel is the REAL fb-20260915T094418Z capture, committed as
// a fixture beside cmd/repro's fixture set (testdata — a fixture, not a
// production package; cmd/repro's Go sources stay untouched, per the task's
// host-only scope). The capture was recorded by the pre-fix host: its
// reconcileLog cut the parked burst's overshoot tail (the two real events
// after the commander_zone ask — the "lethal damage" sweep and the
// end-combat step change) from log.json while the head field stayed the
// chain hash over the FULL 1326-event stream. The committed copy carries
// that tail RECONSTRUCTED: the recorded intents' replay reproduced it
// byte for byte and the chain proves it (the trimmed prefix chains to
// d5b4fa8fa12b16e7, the full stream to the recorded 7e35c9a40dedff68 —
// measured at repair time), so the fixture verifies under plain
// replay.Replay with no cut-tail tolerance anywhere and cmd/repro on it
// exits 0. The test pins both, so an engine or corpus move that breaks the
// capture fails loudly here instead of leaving a stale fixture behind.
//
// rv2a integration (toss Note before the deal): the same repair was applied
// once more. The rv2a engine moves the genesis toss Note from its post-deal
// position to sequence 1 and shortens its text ("X won the toss"), so the
// recorded stream diverged at event 1. The recorded intents' replay under
// the rv2a engine reproduces the identical 1326-event stream except for
// exactly that one Note's position and text (measured by diffing the rebuilt
// stream against the recording: one changed event, every other event byte-identical,
// and the game facts at the capture point — turn, step, priority, active —
// unchanged), so log.json's events and head were regenerated the same way the
// tail was: same match, engine-current bytes. The recorded Intents are untouched.
//
// sacrifice/unless integration (2026-09-16, full re-record): the recorded
// intents stopped replaying altogether. Main's mass-effects merge registered
// the DB$ Shuffle primitive AFTER this capture's last regeneration (822502e4
// is not an ancestor of 679883dc), so event 76 — Gamble's mid-burst "then
// shuffle" — became a real Secret Shuffle where the recording kept the
// "unimplemented API Shuffle" Note the pre-Shuffle engine emitted; a real
// shuffle changes the library order, and no recorded intent after the draw
// it feeds can survive that. The stale recording also failed on plain main
// tip (the divergence is exactly the registered-primitive move, measured by
// replaying the capture there), so it was re-recorded rather than patched:
// a live gated match over the SAME deck pair (foundations-wretched-ranks vs
// foundations-reign-of-dragons, commander, corpus tokens attached — a
// capture must replay, so its registry carries Tokens, which the live
// fixture's does not need) parked on the same reported shape: seat 0's
// commander_zone ask (a dying Grave Titan, turn 15, declare-blockers) with
// the post-ask "lethal damage" sweep and end-combat step change on the log
// (2150 events, 396 intents, head 023ef9e6f0175524). report.json keeps the
// original report's provenance fields; its snapshot line names the
// re-record.
//
// event-802 re-record (2026-09-17): that re-record was itself made by
// parkedOvershootMatch BEFORE host.Options.Tokens was wired (see this
// file's parkedOvershootMatch) — its live match ran token-starved, so a
// token-minting ability it hit recorded the engine's "unimplemented API"
// Note stand-in in place of the real mint. feedback.Load falls back to the
// full corpus token map when a capture carries no token text, so replaying
// this same capture today actually mints the token, diverging at event 802
// (recorded move_zone [the Note's text], replayed choose). Fixed at the
// source and re-recorded via TestGenerateOvershootCapture
// (REPRO_REGEN_FIXTURE=1) with Tokens now attached to the live match:
// 2238 events, 407 intents, head d8cf3b6dc00a07d9.
//
// Discard Mode$ Hand re-record (2026-09-19): the pre-fix capture (the
// seed-1111 game: MatchSeed(1111,1) = 1671561686018727072, 2238 events,
// 407 intents, head d8cf3b6dc00a07d9, 10 discard events) names Reforge the
// Soul, and the Discard Mode$ Hand wheel fix (DiscardEffect's Mode$ Hand
// arm, pinned on the real corpus card in rules/reforge_the_soul_wheel_test.go)
// made it diverge at event 836 under the post-fix engine: the pre-fix
// engine kept one front-card events.Discard where the post-fix engine
// emits one events.Discard per card in hand order. Re-recorded via
// TestGenerateOvershootCapture (REPRO_REGEN_FIXTURE=1) — but NOT over that
// old game: 4987fe2e (2026-09-17) had already re-measured the live
// fixture to table seed 8 (MatchSeed(8,1) = 11409396526365357622, burst
// 378 — overshootIntents as it stands) WITHOUT re-recording the capture,
// so the regen mechanism (which records the CURRENT fixture's game) taped
// a different game than the one the pre-fix capture held: toss to
// foundations-reign-of-dragons, 14 turns, 2112 events, 378 intents, head
// fd33d9db389d28e3. That game contains NO discard events at all, so this
// gate does not exercise the wheel fix's emission shape — that fix's
// defence remains rules/reforge_the_soul_wheel_test.go, and a future diff
// against 2238 → 2112 is the seed re-measure at 4987fe2e plus this
// re-record, not a discard-shape change.
//
// trig:SearchedLibrary re-record (2026-09-23): the completed-library-search
// marker (events.SearchedLibrary, emitted by effects/applyLibrarySearch) is
// one new event in this game's stream — Cultivate's search at the burst —
// so the pre-marker capture diverged at event 173 (recorded shuffle,
// replayed the new marker). Re-recorded via TestGenerateOvershootCapture
// (REPRO_REGEN_FIXTURE=1): 2113 events (2112 + the one marker), 378 intents
// (the overshootIntents shape unchanged), head 3a31833838e13145.
// Starting-player election and Artifact mana provenance (2026-09-23):
// the current live fixture records the starting_player ask at event 20
// (previously no ask), and the first artifact-produced mana is tagged at
// the old event 265. Re-recorded through TestGenerateOvershootCapture;
// the parked commander-zone ask and lethal-damage tail are unchanged.
// 2117 events, 379 intents, head 9f32a260ba20e345.
//
// Menace block-count floor (2026-09-23): at turn 8 Undead Augur attacks
// with Lord of the Accursed's menace grant while seat 0 has one untapped
// creature (Lathliss), so no legal blocking declaration exists and the
// engine no longer poses the empty blockers ask (CR 509.1a/702.111b; the
// Menace floor is now part of blockPairScopeFor's impossible-bound check).
// The recorded stream loses exactly that ask/answer pair (events 1052-1053)
// and intent 194; every other event and intent is identical. Re-recorded via
// TestGenerateOvershootCapture: 2115 events, 378 intents, head
// dbba928d76d290b2; overshootIntents 379 -> 378.
//
// Game-long damage provenance (2026-09-23): every landed Damage event now
// emits a DamageProvenance fact through Engine.emit, so the re-recorded
// stream carries 23 more events (one per point of damage the match dealt)
// and a new head; no intent moved (provenance changes no decision).
// Re-recorded via TestGenerateOvershootCapture: 2138 events, 378 intents,
// head 55f5cbb8b801acb7.
// lifeLost1: effLoseLife now publishes AFLifeLost on its source after each
// instruction. Four StoreSVar events (seqs 179, 1695, 1722, 2086) are the
// only difference from that stream apart from shifted seqs/head. Re-recorded
// via TestGenerateOvershootCapture: 2142 events, 378 intents, head
// 5775daf7a611571c.
//
// Source damage-history re-record (2026-10-05): DamageProvenance's text now
// carries the source's colours at the hit and the recipient's derived types
// (Count$ damage-history heads), so the capture diverged at its first
// damage_provenance event. Re-recorded via TestGenerateOvershootCapture:
// 2142 events, 378 intents (unchanged), head f8d6151ddd8d7d50.
const committedCaptureRel = "../cmd/repro/testdata/feedback/20260915T094418Z-e484f1db"

// requireCommittedCapture skips when the worktree has no .cards/ corpus:
// the capture replays against it (tokens resolve through the gitignored
// sync dir when present, else the corpus at the same FORGE_REF — both
// paths measured to replay). Skipping without a corpus matches
// testutil.CorpusRegistry's convention; a green run without the corpus
// proves nothing.
func requireCommittedCapture(t *testing.T) {
	t.Helper()
	root, err := feedback.Root()
	if err != nil {
		t.Skipf("no repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".cards")); err != nil {
		t.Skip("no .cards/ corpus present — the committed capture cannot replay")
	}
}

// TestCommittedOvershootCaptureReplaysToTheParkedAsk is the committed
// capture's gate: the REAL reported match, parked on seat 1's commander_zone
// ask with its overshoot tail on the log, verifies under plain
// replay.Replay (no tolerance, host or otherwise) and the rebuilt engine is
// parked on that ask — the state cmd/repro prints and a -emit-test
// skeleton starts from.
func TestCommittedOvershootCaptureReplaysToTheParkedAsk(t *testing.T) {
	requireCommittedCapture(t)
	l, cfg, meta, err := feedback.Load(committedCaptureRel)
	if err != nil {
		t.Fatalf("feedback.Load: %v", err)
	}
	if n := len(l.Events); n != 2142 {
		t.Fatalf("capture carries %d events, want the full 2142-event stream (re-recorded)", n)
	}
	e, err := replay.Replay(l, cfg)
	if err != nil {
		t.Fatalf("replay.Replay: %v — the committed capture no longer verifies", err)
	}
	if got, want := e.L.Head(), meta.Head; got != want {
		t.Fatalf("replayed head %q, want the recorded %q", got, want)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KCommanderZone || d.Player != 1 {
		t.Fatalf("pending is %+v, want seat 1's commander_zone ask", d)
	}
	// The tail events are on the rebuilt log, after the ask that parked it.
	// event-802 re-record (2026-09-17): with Tokens now attached, the token
	// creatures the burst's lethal-damage sweep hits are real permanents,
	// so their own SBA cleanup ("ceased to exist") now lands after the
	// sweep — the pre-fix, token-starved capture never had real tokens to
	// clean up, so its tail ended one step earlier (the step change).
	last := l.Events[len(l.Events)-1]
	if last.Kind != events.MoveZone || last.Text != "ceased to exist" {
		t.Fatalf("capture ends with %+v, want a token's post-sweep cleanup", last)
	}
	if prev := l.Events[len(l.Events)-2]; prev.Kind != events.MoveZone || prev.Text != "ceased to exist" {
		t.Fatalf("second-to-last event is %+v, want a token's post-sweep cleanup", prev)
	}
	sweep := false
	for _, ev := range l.Events[len(l.Events)-6:] {
		if ev.Kind == events.MoveZone && ev.Text == "lethal damage" {
			sweep = true
		}
	}
	if !sweep {
		t.Fatal("no lethal-damage sweep found in the capture's tail")
	}
}
