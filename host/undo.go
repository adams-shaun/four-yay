package host

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Undo, dispatch fb-20260911T201015Z: a human seat's request to roll the
// live match back to the decision point at which that seat last submitted
// an intent — "undo my last action". The mechanism is a REWIND IN PLACE,
// never a fork: the engine is rebuilt at the rewind point with
// replay.ReplayTo (the same verified rebuild a DVR query and a restart
// already run) and swapped into the live match, whose id, seed and hash
// chain are unchanged. Everything AROUND the engine that assumed history
// only grows is repaired at the rewind: the match log is truncated to the
// replayed engine's log (in memory and on disk), the turn-start snapshots
// and DVR ticks past the new head are dropped, a `rewind` stream frame
// tells every connected client to discard state past the new head, and a
// distinct OnRewind persistence hook (never OnBurst, whose contract is one
// intent per burst) tells the embedder to truncate its own store.
//
// Bots' intents after the rewind point are discarded and the bots re-decide
// against the rewound state; the requester is left pending on the very
// decision their last intent answered, so pressing Undo repeatedly walks
// back one of their own actions at a time, to the start of the game.
//
// KNOWN, ACCEPTED INFORMATION EFFECT: the rewind replays the same seed
// through the same engine, so a shuffle that is rewound past re-orders the
// library identically — after an undo the player can know upcoming draws.
// That is acceptable for casual vs-bot play (Undo is refused on any table
// with a second human seat precisely because consent cannot be asked), and
// the alternative — reseeding on rewind — would fork the hash chain and
// break every replay guarantee the log carries. Do not reseed.
//
// Requests enter undoQueue, whose size-one channel is only a wakeup: a
// protected pending count preserves every accepted request instead of
// silently coalescing rapid clicks. After each rewind it rearms the wakeup
// for the next queued request, whose rewind point is recomputed from the
// now-truncated log. Admission reserves one extant human intent per request,
// so accepting more clicks than can be serviced is impossible. The wakeup is
// consumed at the next play-loop boundary or — if the loop is parked on the
// requester's decision — by that await's own select, whichever comes first.
// 204 means the request was queued, not that the rewind has landed; the
// client observes each rewind on the stream like any other board change.

// OnRewindFunc observes one truncation of a live match's log (see Undo).
// toIntent is the intent count the match was rewound TO (its log now holds
// exactly that many intents) and headSeq is the seq of the new chain head —
// the last event the match KEPT. An embedder that persists bursts through
// OnBurst must, on this hook, drop every stored event past headSeq and
// every stored intent past toIntent for (t, k): later OnBurst bursts
// legitimately RE-USE seq numbers at or below the old head (the replayed
// game re-decides the undone prefix), so the truncation is what keeps the
// sink's stream a prefix-consistent chain rather than a log with a
// rewritten tail.
//
// Exactly one OnRewind fires per undo, on the match goroutine with the
// match lock held (the same discipline as OnBurst), BEFORE any new burst —
// so the sink never sees a post-rewind burst it has not been told to make
// room for. A nil OnRewind is a no-op. A returned error crashes the match
// exactly as an OnBurst error does (D15): a sink that cannot record the
// truncation must not keep receiving a chain it has diverged from.
type OnRewindFunc func(t TableID, k int, toIntent int, headSeq uint64) error

// undoQueue turns a size-one wakeup channel into a lossless queue for the
// sole human requester's undo clicks. pending includes the request currently
// being serviced until complete is called under the match lock after its log
// truncation; Registry.Undo takes that same match lock while reserving an
// extant human intent, so a concurrent request cannot overbook the old log.
type undoQueue struct {
	mu      sync.Mutex
	signal  chan state.PlayerID
	pending int
	player  state.PlayerID
}

func newUndoQueue() *undoQueue {
	return &undoQueue{signal: make(chan state.PlayerID, 1)}
}

// request reserves one of available extant intents. The first request arms
// signal; later requests need no channel slot because complete rearms it.
func (q *undoQueue) request(player state.PlayerID, available int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pending >= available {
		return fmt.Errorf("host: seat %d has no unqueued submitted intent to undo", player)
	}
	if q.pending > 0 && q.player != player {
		return fmt.Errorf("host: undo already queued by seat %d", q.player)
	}
	q.player = player
	q.pending++
	if q.pending == 1 {
		q.signal <- player
	}
	return nil
}

// complete retires the request whose rewind just landed and rearms the
// wakeup when another accepted request remains. It is called under m.mu,
// after the live log has been truncated, preserving request's lock order
// (m.mu then q.mu) and its one-reservation-per-extant-intent invariant.
func (q *undoQueue) complete(player state.PlayerID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pending == 0 || q.player != player {
		panic("host: completed an undo that was not queued")
	}
	q.pending--
	if q.pending > 0 {
		q.signal <- q.player
	}
}

// undoRequest is the parked human's await outcome when the undo channel
// fires before any intent: it carries the requesting seat (the channel's
// payload) out of the seat and into play's rewind path. It is an error
// only so it can travel through the existing (Intent, error) await shape;
// play always matches it with errors.As and never surfaces it as a crash.
type undoRequest struct{ player state.PlayerID }

func (e undoRequest) Error() string {
	return fmt.Sprintf("host: undo requested by seat %d", e.player)
}

// asUndo reports whether err is an undo request and, when it is, the seat
// that asked for it.
func asUndo(err error) (state.PlayerID, bool) {
	var req undoRequest
	if errors.As(err, &req) {
		return req.player, true
	}
	return 0, false
}

// Undo requests the in-place rollback for the live match k of table id, as
// held by the human seat player. The table must have at least one human
// seat and EVERY human seat must be the requester — a vs-bot or solo table
// — because a rewind discards actions other people took and no consent
// flow exists (out of scope; a table with two or more distinct human seats
// is refused with that reason). The seat must be a real human seat of the
// live match, and the requester must have an unreserved logged intent. Each
// accepted request reserves one, so rapid repeated requests are all serviced
// and an excess request is explicitly rejected rather than silently dropped.
//
// 204 means the request was queued for the live loop; the rewind itself is
// observed on the stream (the rewind frame) or through Pending/ViewAt.
func (r *Registry) Undo(id TableID, k int, player state.PlayerID) error {
	r.mu.RLock()
	t, ok := r.tables[id]
	r.mu.RUnlock()
	if !ok {
		return ErrNotFound
	}
	humans := t.cfg.Humans
	if len(humans) == 0 {
		return fmt.Errorf("host: table %s runs bots only; undo needs a human seat", id)
	}
	for _, h := range humans {
		if h != int(player) {
			return fmt.Errorf("host: table %s has %d human seats (%v); undo is refused unless every human seat is the requester (seat %d) — a rewind discards other players' actions, and no consent flow exists",
				id, len(humans), humans, player)
		}
	}
	t.mu.RLock()
	m := t.cur
	t.mu.RUnlock()
	if m == nil || m.k != k {
		return fmt.Errorf("host: table %s: match %d is not the live match", id, k)
	}
	// humanSeat validates the range, the live state and that the seat is a
	// real HumanSeat — the same fence Pending/SubmitIntent use.
	if _, err := r.humanSeat(id, k, player); err != nil {
		return err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state != protocol.MatchLive {
		return fmt.Errorf("host: match %d is %s, nothing to undo", k, m.state)
	}
	// Hold m.mu across the reservation. rewindToLastIntent takes it for
	// writing and completes one reservation only after truncating the log,
	// so concurrent requests always count intents in the matching prefix.
	available := intentCountOf(m.e.L.Intents, player)
	if err := m.undo.request(player, available); err != nil {
		return fmt.Errorf("host: match %d: %w", k, err)
	}
	return nil
}

// lastIntentOf returns the index of the last intent player submitted, or
// ok false when they have none. Caretaker-substituted answers count as the
// human's: they are logged with the seat's own Player and were the seat's
// action in every observable sense.
func intentCountOf(ins []decision.Intent, p state.PlayerID) int {
	n := 0
	for i := range ins {
		if ins[i].Player == p {
			n++
		}
	}
	return n
}

func lastIntentOf(ins []decision.Intent, p state.PlayerID) (int, bool) {
	for i := len(ins) - 1; i >= 0; i-- {
		if ins[i].Player == p {
			return i, true
		}
	}
	return 0, false
}

// rewindToLastIntent truncates the live match in place so the requester's
// last submitted intent is pending again: the engine is rebuilt through the
// first n intents with replay.ReplayTo (n = the requester's last intent
// index), verified byte-for-byte against the recorded log, and swapped in.
// Every piece of the match's own bookkeeping that describes the discarded
// tail is re-derived from the truncated log: bounds, turn starts, DVR
// ticks, and the turn-start snapshots past the new head (the kept ones hold
// prefix logs of the SAME chain and stay valid read-side history). The
// persisted files are truncated to the same prefix and the OnRewind hook
// fires exactly once, before any new burst.
//
// Call on the match goroutine, under m.mu (the swap must not overlap a
// reader's projection or a burst); projectNext runs inside the same locked
// section so the caller can park the rewound decision before publishing
// anything. On success the returned parkedData is the rewound pending
// decision, which is guaranteed to exist and to belong to the requester.
func (r *Registry) rewindToLastIntent(t *table, m *match, seats []seat.Seat, brd *botpolicy.Board, player state.PlayerID) (*parkedData, error) {
	n, ok := lastIntentOf(m.e.L.Intents, player)
	if !ok {
		return nil, fmt.Errorf("host: seat %d has no submitted intent to undo in match %d", player, m.k)
	}
	if n >= len(m.bounds)-1 {
		return nil, fmt.Errorf("host: undo point %d is not a recorded burst boundary of match %d", n, m.k)
	}
	wantEvents := int(m.bounds[n])
	e, err := replay.ReplayTo(m.e.L.Clone(), m.cfg, n)
	if err != nil {
		return nil, fmt.Errorf("host: undo replay to intent %d: %w", n, err)
	}
	// The replay is verified event-for-event against the recorded log by
	// ReplayTo itself; the length and pending checks below state the same
	// fact from the match's own bookkeeping, so a future ReplayTo change
	// that weakened the compare cannot silently truncate to a wrong point.
	if got := len(e.L.Events); got != wantEvents {
		return nil, fmt.Errorf("host: undo replay reached %d events, recorded boundary for intent %d is %d", got, n, wantEvents)
	}
	d := e.Pending()
	if d == nil {
		return nil, fmt.Errorf("host: undo replay left match %d with no pending decision", m.k)
	}
	if d.Player != player {
		return nil, fmt.Errorf("host: undo replay left match %d pending on seat %d, not the requester %d", m.k, d.Player, player)
	}
	// The capacity hint newMatch gave the old log, again for the replayed
	// one: its log starts empty-capped and would otherwise climb through
	// the whole doubling series a second time.
	e.L.Reserve(defaultExpectedEvents)
	m.e = e
	m.intents = n
	m.bounds = append([]uint64(nil), m.bounds[:n+1]...)
	m.turnStarts = turnStartsBefore(m.turnStarts, uint64(len(e.L.Events)))
	m.snaps = snapsBefore(m.snaps, n)
	if err := r.truncatePersisted(t, m); err != nil {
		// The in-memory rewind has happened; a persistence failure at this
		// point is a crash (D15) — the files, the hook and the memory must
		// not disagree, and only the crash path stops the loop from
		// building on a half-truncated match.
		return nil, err
	}
	return projectNext(m, seats, brd), nil
}

// abandon clears the parked decision's answerable slot, if one was ever
// installed (a human seat's park): a decision an undo discards must stop
// being answerable BEFORE the match is rewound out from under it, or a
// submit racing the rewind would be validated against a decision the match
// no longer asks and silently dropped. A bot's already-resolved parked
// decision has nothing to abandon.
func (pd *parkedDecision) abandon() {
	if pd != nil && pd.hs != nil {
		pd.hs.abandon()
	}
}

// serviceUndo consumes a consumed undo request's rewind at a play-loop
// boundary: truncate in place (rewindToLastIntent), install the rewound
// pending decision's answerable slot (parkSeat — outside the match lock,
// the same discipline as every park), and only then publish the rewind
// frame (pushRewind — park-before-publish). Returns the re-parked decision
// the loop awaits next. Called on the match goroutine.
func (r *Registry) serviceUndo(ctx context.Context, t *table, m *match, seats []seat.Seat, brd *botpolicy.Board, req state.PlayerID) (*parkedDecision, error) {
	var data *parkedData
	err := m.locked(func() error {
		var err error
		data, err = r.rewindToLastIntent(t, m, seats, brd, req)
		if err != nil {
			return err
		}
		if data == nil {
			return fmt.Errorf("host: undo rewind left match %d with no pending decision", m.k)
		}
		// Retire this reservation only after the log reflects it. If another
		// accepted click is queued, complete rearms the size-one wakeup; its
		// next service recomputes the previous human intent from this prefix.
		m.undo.complete(req)
		return nil
	})
	if err != nil {
		return nil, err
	}
	parked := parkSeat(ctx, seats, data, m.undo.signal)
	r.pushRewind(t, m)
	return parked, nil
}

// turnStartsBefore keeps the DVR ticks at or before head.
func turnStartsBefore(ts []uint64, head uint64) []uint64 {
	out := make([]uint64, 0, len(ts))
	for _, s := range ts {
		if s < head {
			out = append(out, s)
		}
	}
	return out
}

// snapsBefore keeps the turn-start snapshots at or before intent boundary n.
// The kept snapshots hold frozen prefix engines of the SAME hash chain, so
// they remain exact read-side history for the truncated match.
func snapsBefore(snaps []snapshot, n int) []snapshot {
	out := make([]snapshot, 0, len(snaps))
	for _, s := range snaps {
		if s.intent <= n {
			out = append(out, s)
		}
	}
	return out
}

// truncatePersisted repairs every persistence channel for the rewind that
// rewindToLastIntent already applied to memory: the match's own files are
// truncated to the replayed log, the OnRewind hook fires exactly once, and
// the live sidecar is rewritten so its event count matches the truncated
// log. Called under m.mu, on the match goroutine — file writes under the
// match lock are the established persistBurst discipline. The order is the
// crash-safe one: the match's own files first (readLog's reconcileLog
// re-derives the same prefix from any interrupted intermediate state), then
// the hook, then the sidecar — so a crash mid-way always leaves a log that
// replays to the truncated prefix, never the discarded tail.
func (r *Registry) truncatePersisted(t *table, m *match) error {
	n := len(m.e.L.Events)
	if m.files != nil {
		if err := m.files.truncateTo(len(m.e.L.Intents)); err != nil {
			return fmt.Errorf("host: truncating match %d's persisted log: %w", m.k, err)
		}
		m.persisted = n
	}
	if r.opts.OnRewind != nil {
		if err := r.opts.OnRewind(t.cfg.ID, m.k, m.intents, uint64(n-1)); err != nil {
			return fmt.Errorf("host: OnRewind: %w", err)
		}
	}
	if r.opts.Dir != "" {
		if err := writeSidecar(r.opts.Dir, m.sidecar(), r.opts.Sync); err != nil {
			return fmt.Errorf("host: rewriting match %d's sidecar: %w", m.k, err)
		}
	}
	return nil
}

// pushRewind delivers the rewind frame to every focus subscriber and a
// re-derived widget to every overview one, after an in-place rewind. It
// runs on the match goroutine AFTER the rewound pending decision has been
// parked (the park-before-publish ordering), so the decision frame a
// subscriber needs is answerable before it is announced. The rewind frame's
// body is the full snapshot at the new head — the same shape a fresh focus
// subscription receives — and the widget's transcript line is re-derived
// from the truncated log, never carried from an undone event.
func (r *Registry) pushRewind(t *table, m *match) {
	ss, modes := r.sessionsFor(t.cfg.ID)
	if len(ss) == 0 {
		return
	}
	focus, overview := hasMode(modes, protocol.ModeFocus), hasMode(modes, protocol.ModeOverview)

	var rewind, decisionF *protocol.Frame
	var widget protocol.Frame
	// The rewind body is a live snapshot — the same projection a fresh focus
	// subscription makes — so it runs exclusively (projectLive): a concurrent
	// focused snapshot must not race this one through ProjectFor's engine
	// mutation. The push loop below stays outside the exclusive section: the
	// Subscribe path holds t.fanMu then takes m.mu, so m.mu must be dropped
	// before t.fanMu is taken here.
	if focus || overview {
		r.projectLive(m, func() {
			if focus {
				f := frame(protocol.TRewind, t, m.k, head(m), r.snapshotBody(t, m))
				rewind = &f
				if d := m.e.Pending(); d != nil {
					df := frame(protocol.TDecision, t, m.k, head(m), protocol.DecisionBody{Player: uint8(d.Player), Kind: string(d.Kind), Prompt: d.Prompt})
					decisionF = &df
				}
			}
			if overview {
				// The widget's Last is re-derived over the whole truncated log —
				// the same full-log scan onMatchStart runs — so an undone line can
				// never be carried into the rewound overview.
				bodies := eventBodiesFor(view.NoSeat, t.cfg.Spectator, m.e.G, m.e.L.Events)
				t.lastLine = lastLine(bodies, "")
				widget = r.widgetFrame(t, m, t.lastLine)
			}
		})
	}

	t.fanMu.Lock()
	for i, s := range ss {
		switch modes[i] {
		case protocol.ModeFocus:
			if rewind != nil && !s.push(*rewind) {
				break
			}
			if decisionF != nil {
				s.push(*decisionF)
			}
		case protocol.ModeOverview:
			s.setWidget(t.cfg.ID, widget)
		}
	}
	t.fanMu.Unlock()
	r.dropOverflowed(ss)
}
