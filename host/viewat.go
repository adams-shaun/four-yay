package host

import (
	"errors"
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// ErrBeyondHead says a requested seq is past the match's last event; Head
// is the last valid seq, which the http layer returns with a 409.
type ErrBeyondHead struct{ Head uint64 }

func (e ErrBeyondHead) Error() string { return fmt.Sprintf("host: seq beyond head %d", e.Head) }

// ViewAt is the board as of event seq (inclusive) in the table's
// visibility. For a live match it uses the turn-start snapshots; a finished
// match (Task 12) replays from its files.
//
// The replay viewAt does (up to a turn's worth of Submit calls) can run
// long enough to matter, so this takes the read lock only to copy what it
// needs — the log (m.e.L.Events/Intents are live, appended-to slices the
// match loop still owns, so a bare pointer would race; Clone gives an
// independent copy) and the (append-only, never-mutated-in-place) snapshot
// list — then computes outside the lock, never blocking a concurrent
// Submit for longer than those two copies take.
func (r *Registry) ViewAt(id TableID, k int, seq uint64) (view.View, error) {
	t, m, err := r.lookup(id, k)
	if err != nil {
		return view.View{}, err
	}
	m.mu.RLock()
	// The whole log, not a prefix to seq: Log.Clone (Task 1) is a flat
	// whole-log deep copy with no truncation parameter, and computing a
	// correct prefix here would mean re-doing Clone's own per-event
	// IDs/Pairs copying by hand instead of reusing it, for a saving that
	// is usually small (most ViewAt calls land near head anyway) and is
	// freed the moment this call returns — unlike a stored snapshot
	// (FL-44), this copy never outlives the request.
	l := m.e.L.Clone()
	snaps := append([]snapshot(nil), m.snaps...)
	cfg, vis := m.cfg, t.cfg.Spectator
	m.mu.RUnlock()
	return viewAt(cfg, l, snaps, seq, view.NoSeat, vis, nil)
}

// ViewAtSeat is one seat's own view of the board as of event seq
// (inclusive): their hand and mana pool — every other seat's hidden zones
// are counts only — and, when the projection is exactly at the live head
// and the engine is parked on that seat, the decision asked of them (Task
// M2b-4, FL-73). It shares viewAt with ViewAt; the two differ only in the
// (viewer, vis, decision) triple they hand it, i.e. only in how the common
// replay calls view.ProjectFor.
//
// Same lock discipline as ViewAt: read the shared fields under the read
// lock — the log, the snapshots, the config, and a COPY of the pending
// decision (never the engine's own pointer, which play replaces between
// bursts) — then project outside it.
func (r *Registry) ViewAtSeat(id TableID, k int, seq uint64, player state.PlayerID) (view.View, error) {
	t, m, err := r.lookup(id, k)
	if err != nil {
		return view.View{}, err
	}
	m.mu.RLock()
	l := m.e.L.Clone()
	snaps := append([]snapshot(nil), m.snaps...)
	cfg := m.cfg
	// The decision belongs in the view only when the board is projected
	// exactly at the live head. At an earlier seq the decision that went
	// with that moment cannot be reconstructed here — the replayed engine
	// sits at the burst's start, where the decision about to be asked (or
	// already answered) is a different one than the board shows — so it
	// stays nil: a historical view never misrepresents what was pending.
	var d *decision.Decision
	if seq == head(m) {
		if p := m.e.Pending(); p != nil {
			cp := *p.Clone()
			if !t.cfg.AutoMana {
				cp.PaymentActions = nil
			}
			d = &cp
		}
	}
	m.mu.RUnlock()
	return viewAt(cfg, l, snaps, seq, player, view.Seat, d)
}

// Events is every redacted, described event from since (inclusive) to the
// head, in chain order. Redaction is against the state at head (PL-15),
// which holds because g is cloned at the tail's own head — RedactEventsFor
// and Describe read "the state as of the last event in evs".
//
// since is caller-controlled and since=0 on a long match is the whole log,
// so (fix round 1, FL-42) this takes the read lock only long enough to
// clone g (state.Game.Clone) and copy the events tail, then formats
// outside the lock — the same discipline as ViewAt, now that eventBodies
// (host/fanout.go) takes a state/events pair instead of a live match.
func (r *Registry) Events(id TableID, k int, since uint64) ([]protocol.EventBody, error) {
	t, m, err := r.lookup(id, k)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	n := uint64(len(m.e.L.Events))
	if n == 0 {
		m.mu.RUnlock()
		return nil, ErrBeyondHead{Head: 0}
	}
	if since >= n {
		head := n - 1
		m.mu.RUnlock()
		return nil, ErrBeyondHead{Head: head}
	}
	g := m.e.G.Clone()
	evs := append([]events.Event(nil), m.e.L.Events[since:]...)
	vis := t.cfg.Spectator
	m.mu.RUnlock()
	return eventBodiesFor(view.NoSeat, vis, g, evs), nil
}

// EventsSeat is every event from since (inclusive) to the head, redacted
// for player as a seat (Task M2b-4): their own secret draws keep their
// cards, a card that entered any other seat's hidden zone never surfaces.
// Mirrors Events — the only difference is the redaction mode, Seat instead
// of the table's spectator visibility, i.e. only the viewer/visibility pair
// handed to the common eventBodiesFor.
func (r *Registry) EventsSeat(id TableID, k int, since uint64, player state.PlayerID) ([]protocol.EventBody, error) {
	_, m, err := r.lookup(id, k)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	n := uint64(len(m.e.L.Events))
	if n == 0 {
		m.mu.RUnlock()
		return nil, ErrBeyondHead{Head: 0}
	}
	if since >= n {
		head := n - 1
		m.mu.RUnlock()
		return nil, ErrBeyondHead{Head: head}
	}
	g := m.e.G.Clone()
	evs := append([]events.Event(nil), m.e.L.Events[since:]...)
	m.mu.RUnlock()
	return eventBodiesFor(player, view.Seat, g, evs), nil
}

// lookup finds a table's live or retained match, loading a finished match
// from disk when only its sidecar is known (Task 12). The last loaded
// match is cached per table so a DVR session stepping through a finished
// match does not replay it per request.
func (r *Registry) lookup(id TableID, k int) (*table, *match, error) {
	r.mu.RLock()
	t, ok := r.tables[id]
	r.mu.RUnlock()
	if !ok {
		return nil, nil, ErrNotFound
	}
	// The fast path — a live or in-memory match, or an already-loaded
	// archived one — runs under the read lock only. Only an actual
	// load-from-disk upgrades to the write lock (see below), so the common
	// ViewAt/Events request never contends with the other lock holders.
	for {
		t.mu.RLock()
		if t.cur != nil && t.cur.k == k {
			c := t.cur // copy under the lock; run reassigns t.cur outside our RUnlock window
			t.mu.RUnlock()
			return t, c, nil
		}
		if t.loaded != nil && t.loaded.k == k {
			ld := t.loaded
			t.mu.RUnlock()
			return t, ld, nil
		}
		for _, m := range t.history {
			if m.k == k {
				t.mu.RUnlock()
				return t, m, nil
			}
		}
		t.mu.RUnlock()

		// The requested match is not in memory or already loaded. It can only
		// be an archived match known from disk, so check that before
		// committing to a write-locked load. Load under the write lock and
		// re-check t.loaded after the upgrade: two concurrent readers that
		// both miss in the read pass must not both rebuild the match, so the
		// lock is held for the whole (slow) disk load — the second waiter
		// blocks here, finds t.loaded already set, and reuses it.
		sc, ok := t.archivedMatch(k)
		if !ok {
			return nil, nil, ErrNotFound
		}
		t.mu.Lock()
		if t.loaded != nil && t.loaded.k == k {
			t.mu.Unlock()
			return t, t.loaded, nil
		}
		m, err := r.loadArchived(t, sc)
		if err != nil {
			t.mu.Unlock()
			return nil, nil, err
		}
		t.loaded = m // under the write lock: never a bare read-lock write
		t.mu.Unlock()
		return t, m, nil
	}
}

// archivedMatch returns the sidecar for archived match k on t, or ok false
// when k is not known from disk. archived is append-only after archive()
// (it is only ever appended to under t.mu, never mutated in place), so a
// reader taking the read lock is fine.
func (t *table) archivedMatch(k int) (sidecar, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, sc := range t.archived {
		if sc.Match == k {
			return sc, true
		}
	}
	return sidecar{}, false
}

// loadArchived rebuilds enough of a finished match to serve ViewAt/Events:
// the log from its files, then matchForLog for everything else. It is the
// on-disk entry to the read side; an embedder's persisted observer log
// (Task M2c-3) takes the same matchForLog with the log already in hand,
// so both sources are served through the exact same shape.
//
// sc is t.archived's index entry, which carries no NameUniverseNames (see
// archive); a name-universe match's exact label list is read back from its
// own sidecar file here.
func (r *Registry) loadArchived(t *table, sc sidecar) (*match, error) {
	if sc.NameUniverse && len(sc.NameUniverseNames) == 0 {
		full, err := readSidecar(r.opts.Dir, t.cfg.ID, sc.Match)
		if err != nil {
			return nil, err
		}
		sc.NameUniverseNames = full.NameUniverseNames
	}
	l, err := readLog(r.opts.Dir, t.cfg.ID, sc.Match)
	if err != nil {
		return nil, err
	}
	return r.matchForLog(t, sc, l)
}

// matchForLog builds the read-side shape of a finished match whose log is
// already in hand — the readLog-equivalent for a non-file source, e.g. an
// embedder's persisted observer log (Task M2c-3). Only the log's origin
// differs from loadArchived: a Config is rebuilt from the sidecar's names
// and decks and the log is replayed to its end state, so ViewAt/EventsSeat
// serve a sink-sourced match exactly as a live or on-disk one. head views
// are exact and mid views take the genesis path of viewAt (no snapshots on
// disk). replay.Replay is the gate: a log that does not reproduce the
// engine's own event stream is refused here, before it can be served.
func (r *Registry) matchForLog(t *table, sc sidecar, l *events.Log) (*match, error) {
	decks := make([][]*cards.Card, len(sc.Decks))
	sideboards := make([][]*cards.Card, len(sc.Decks))
	for i, dn := range sc.Decks {
		d, err := r.opts.LoadDeck(dn)
		if err != nil {
			return nil, fmt.Errorf("host: %s/%d: deck %q: %w", t.cfg.ID, sc.Match, dn, err)
		}
		decks[i] = d.Cards
		sideboards[i] = d.Sideboard
	}
	cfg := rules.Config{Seed: sc.Seed, Names: sc.Names, PlayerNames: sc.PlayerNames, Decks: decks, Sideboards: sideboardConfig(sideboards), Tokens: r.opts.Tokens, Mulligans: sc.Mulligans}
	// NameUniverse is a match MODE, not just data, and NameUniverseNames pins
	// the exact ordered labels it offered. A pre-feature sidecar (no field)
	// gets neither and therefore takes the legacy no-universe path, so a log
	// that recorded no name decision still reproduces (R-8.4).
	if sc.NameUniverse {
		cfg.NameUniverse = r.opts.NameUniverse
		cfg.NameUniverseNames = append([]string(nil), sc.NameUniverseNames...)
	}
	// A persisted Commander match carries its format, the opening life it
	// played with and its per-seat commander indices, so the replay
	// reproduces the live match branch for branch (R-8.4: the replay Config
	// is rebuilt from this sidecar alone, and the engine builds a command
	// zone — and a 40-life start — off exactly those values). An old
	// sidecar's zero Format is constructed, which needs none of this.
	if sc.Format == FormatCommander {
		cfg.Format = rules.FormatCommander
		cfg.StartingLife = sc.StartingLife
		cfg.Commanders = sc.Commanders
	}
	e, err := replay.Replay(l, cfg)
	if err != nil {
		// fb-20260915T094418Z: readLog's reconcileLog trims the file to the
		// last burst's ask boundary (the crash-cut protection, kept — see
		// reconcileLog), but a burst may legitimately extend past its last
		// DecisionAsk, so a match parked or ended on such a burst persists a
		// tail the trim cut, and the replay of the recorded intents runs
		// past the trimmed end. That is the tolerated shape, and the
		// reproduced tail is verified the strong way: the sidecar's Head is
		// the chain hash over the FULL stream the match held at its terminal
		// transition, so a replayed log reaching the same head — and the
		// same recorded event count — reproduced the cut tail byte for byte.
		// Anything else stays the rejection it was.
		if !(cutTailReplayed(err, e, sc.Head) && len(e.L.Events) == sc.Events) {
			return nil, fmt.Errorf("host: %s/%d does not replay: %w", t.cfg.ID, sc.Match, err)
		}
		// Accepted: the engine's own log is the full, chain-verified stream,
		// so the bookkeeping below derives from it rather than from the
		// trimmed recording.
		l = e.L
	}
	return &match{table: t, k: sc.Match, seed: sc.Seed, cfg: cfg, seats: sc.Seats, decks: sc.Decks, e: e,
		bounds: boundsOf(l.Events), turnStarts: turnStartsIn(l.Events, 0), state: sc.State, result: sc.Result,
		winner: sc.Winner, head: sc.Head, reason: sc.Reason}, nil
}

// cutTailReplayed reports whether err — a *replay.Divergence that
// replay.Replay returned — is the one shape a recording CUT AT A BURST
// TAIL produces, and the cut tail is nonetheless verified: every recorded
// event reproduced byte for byte (the incremental compare passed the whole
// recording), the replay then running past the recording's end (Missing)
// because the last recorded burst's own post-ask continuation — the SBA
// pass and step handler a mid-burst DecisionAsk does not stop (rules
// Engine.Submit) — legitimately emitted events the trimmed recording does
// not carry (fb-20260915T094418Z), and the reproduced stream —
// reconstructed tail included — chaining to the sidecar's full-stream head.
// The chain comparison is the whole proof: a head taken over the FULL
// stream matches only a byte-exact reproduction of that stream, so an
// accepted tail is exactly what the live engine logged. A corpus or engine
// change that alters any event, recorded or tail, breaks the chain.
//
// e is the engine replay.Replay returned alongside err; recordedHead is
// the caller's own full-stream head. An empty recordedHead matches
// nothing — a caller with no trusted full-stream head has no way to verify
// a tail it cannot see, and must keep treating the divergence as the error
// it is.
func cutTailReplayed(err error, e *rules.Engine, recordedHead string) bool {
	if err == nil || e == nil || recordedHead == "" {
		return false
	}
	var div *replay.Divergence
	if !errors.As(err, &div) || !div.Missing {
		return false
	}
	return e.L.Head() == recordedHead
}

// viewAt is PL-1: find the last intent boundary j with bounds[j] <= seq+1,
// reach it from the nearest snapshot (or genesis) by re-submitting the
// recorded intents, apply the rest of that burst's events onto the clone's
// own game, and project. Zones, life, damage, counters and the stack are
// exact at every seq; derived P/T from continuous effects and the pending
// tray are as of the burst's start (at most one resolution stale) — and a seq
// strictly inside a burst's overshoot tail (fb-20260915T094418Z) is as of the
// tail's end, at most the one Submit's continuation ahead, the same tolerance.
//
// A reader must never crash (D15's philosophy, extended to readers by
// fix round 1, FL-43): boundsOf already keeps a crashed match's poison
// intent (the one Submit recorded right before a panic, spec D15) out of
// the replay range, but the deferred recover below is the backstop for
// any other way replaying a hand-built, corrupted, or future-buggy log
// could panic — it turns that into a plain error instead.
func viewAt(cfg rules.Config, l *events.Log, snaps []snapshot, seq uint64, viewer state.PlayerID, vis view.Visibility, d *decision.Decision) (v view.View, err error) {
	n := uint64(len(l.Events))
	if n == 0 {
		return view.View{}, ErrBeyondHead{Head: 0}
	}
	if seq >= n {
		return view.View{}, ErrBeyondHead{Head: n - 1}
	}
	defer func() {
		if p := recover(); p != nil {
			v, err = view.View{}, fmt.Errorf("host: viewAt(seq=%d) panicked: %v", seq, p)
		}
	}()
	bounds := boundsOf(l.Events)
	if len(bounds) == 0 {
		return view.View{}, fmt.Errorf("host: no intent boundaries found in a %d-event log", n)
	}
	j := 0
	for j+1 < len(bounds) && bounds[j+1] <= seq+1 {
		j++
	}
	// Nearest snapshot at or before boundary j; snaps are in ascending order.
	var e *rules.Engine
	from := 0
	for i := len(snaps) - 1; i >= 0; i-- {
		if snaps[i].intent <= j {
			e = snaps[i].e.Clone()
			from = snaps[i].intent
			break
		}
	}
	if e == nil {
		var err error
		e, err = replay.ReplayTo(l, cfg, j)
		if err != nil {
			return view.View{}, fmt.Errorf("host: replay to boundary %d: %w", j, err)
		}
		from = j
	}
	for i := from; i < j && i < len(l.Intents); i++ {
		if err := e.Submit(l.Intents[i]); err != nil {
			return view.View{}, fmt.Errorf("host: replay intent %d: %w", i, err)
		}
	}
	// fb-20260915T094418Z: a burst does not always end at its last
	// DecisionAsk. Engine.Submit runs handle, then — whether or not handle
	// just asked a fresh decision — the SBA pass and the step handler's
	// continuation can still emit events before Submit returns, so the
	// replayed engine can legitimately stop PAST bounds[j]: the extra
	// events are the same burst's overshoot tail, which the recording
	// carries (the live log, and a file whose tail reconcileLog chose not
	// to cut — see matchForLog). got < bounds[j] stays a real error: a
	// replay that stops SHORT of the boundary is an engine change or a
	// broken log, and the incremental compare inside ReplayTo has already
	// rejected the divergent shapes it can see.
	got := uint64(len(e.L.Events))
	if got < bounds[j] {
		return view.View{}, fmt.Errorf("host: replay reached seq %d, boundary %d is %d", got, j, bounds[j])
	}
	if got > bounds[j] {
		// The overshoot tail. The engine has already applied those events,
		// so the apply loop below must not re-apply them (it starts at got);
		// but first the tail is verified against the recording — the manual
		// snapshot path above submits intents without ReplayTo's own
		// compare, and this check is what makes serving the tail from the
		// engine's state trustworthy rather than assumed. A recording that
		// stops short of the replay (a log cut mid-burst) cannot verify a
		// tail it does not carry and stays an error.
		if got > n {
			return view.View{}, fmt.Errorf("host: replay produced %d events, the recording has %d", got, n)
		}
		for i := bounds[j]; i < got; i++ {
			if string(e.L.Events[i].Append(nil)) != string(l.Events[i].Append(nil)) {
				return view.View{}, fmt.Errorf("host: replay tail event %d differs from the recording", i)
			}
		}
	}
	for s := got; s <= seq; s++ {
		events.Apply(e.G, l.Events[s])
	}
	v = view.ProjectFor(e.G, e, viewer, vis, d)
	// The round at seq is the exact round-trip count as of that moment, not
	// the snapshot-only roundOf approximation ProjectFor fills in (ui13): fold
	// the logged events up to and including seq. This is what makes the DVR's
	// round match the live board clock across a mid-round elimination.
	v.Round = view.RoundOf(e.G, l.Events[:seq+1])
	return v, nil
}
