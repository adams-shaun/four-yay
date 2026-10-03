package host

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// FrameType is protocol's own discriminator, used throughout this file.
type FrameType = protocol.FrameType

// frame builds an addressed frame; a marshal failure of our own structs is
// a bug, not a runtime condition.
func frame(t FrameType, tab *table, k int, seq uint64, body any) protocol.Frame {
	f, err := protocol.NewFrame(t, string(tab.cfg.ID), k, seq, body)
	if err != nil {
		panic("host: frame: " + err.Error())
	}
	return f
}

// head is the seq of the last event, or 0 for an empty log.
func head(m *match) uint64 {
	if n := len(m.e.L.Events); n > 0 {
		return uint64(n - 1)
	}
	return 0
}

// projectLive runs fn with m.mu held EXCLUSIVELY, and is the ONLY sanctioned
// way to project the LIVE engine. view.ProjectFor mutates the engine while it
// builds a View: rules.(*Engine).Derived appends to e.charsWalk.PTFrames and
// defers a slice truncation (rules/chars PT), writes e.charsWalk.Depth and
// the walk's scratch, and reuses the e.active()/activeBuf
// continuous-effect cache. Two concurrent projections therefore corrupt each
// other's in-progress layer-7 P/T frames — the intermittent module-gate panic
// in Derived/FilterDerivedPT that crashed a table mid-snapshot.
//
// A read lock is NOT enough for a live projection: the mutators above are
// writes regardless of the fact that the projection's result is read-only.
// Genuinely read-only frame assembly — eventBodiesFor's redaction/describe,
// widgetFrame's state reads (fanout/onMatchEnd) — still uses m.mu.RLock; only
// the ProjectFor callers must come through here. Every current caller is one
// of the three live-snapshot sites (Subscribe, onMatchStart, pushRewind); the
// historical replay path (host/viewat.go) projects a cloned engine and must
// NOT be routed through this.
//
// Lock order: Subscribe holds t.fanMu and then calls projectLive, so the
// order is t.fanMu -> m.mu. onMatchStart/pushRewind call projectLive and only
// acquire t.fanMu AFTER projectLive returns (their pushes are outside the
// closure), so they never invert it. Never call projectLive while holding
// t.mu (mint/registry table selection) and never acquire t.fanMu from inside
// fn.
func (r *Registry) projectLive(m *match, fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn()
}

// snapshotBody is the whole board at head in the table's visibility plus
// the turn starts — the body a snapshot frame carries. Split out of
// snapshotFrame so the rewind frame (host/undo.go) can carry the exact
// same body under its own envelope type: a client applies a rewind's
// snapshot exactly like a fresh subscription's.
//
// It projects the live engine, so every caller must reach it through
// projectLive (exclusive m.mu) — never directly under a read lock.
func (r *Registry) snapshotBody(t *table, m *match) protocol.Snapshot {
	v := view.ProjectFor(m.e.G, m.e, view.NoSeat, t.cfg.Spectator, nil)
	// The board clock reads v.Round, so it must be the EXACT round-trip count
	// (view.RoundOf, folded over the ordered event stream), not the
	// snapshot-only roundOf approximation that ProjectFor fills in (ui13): the
	// log is right here at head and holds the death times the snapshot lacks,
	// so a seat eliminated mid-round does not make the displayed round jump.
	v.Round = view.RoundOf(m.e.G, m.e.L.Events)
	// TurnStarts must never marshal as null on the wire (its protocol tag has
	// no omitempty, and web/src/protocol.ts types it as a plain number[]): a
	// nil slice would break every client before the first TurnChange — e.g.
	// a table with a London mulligan round, whose seats are decided before
	// turn 1 — because the DVR dereferences it as iterable. Appending onto an
	// empty literal (not a nil one) both copies m.turnStarts (no aliasing)
	// and keeps a zero-length TurnStarts a non-nil [] that marshals as []
	// rather than null.
	// Seats carries the live match's seat list on the snapshot itself, so a
	// subscriber that joins mid-game — which receives a snapshot but, from
	// host/session.go's Subscribe, never the match's match_start frame — can
	// still learn who is where in the CURRENT match. Without it the table
	// route's seat list stayed empty for the whole match (ui16). A copy, like
	// TurnStarts, so the frame never aliases m.seats.
	return protocol.Snapshot{
		View: v, TurnStarts: append([]uint64{}, m.turnStarts...), Head: head(m),
		Seats: append([]protocol.SeatInfo{}, m.seats...)}
}

// snapshotFrame is snapshotBody under the TSnapshot envelope.
func (r *Registry) snapshotFrame(t *table, m *match) protocol.Frame {
	return frame(protocol.TSnapshot, t, m.k, head(m), r.snapshotBody(t, m))
}

// widgetFrame is the overview cell. It only reads live state (turn, step,
// life/lost) and never calls ProjectFor, so it is safe under either an
// m.mu.RLock (fanout/onMatchEnd) or the exclusive m.mu projectLive takes
// (onMatchStart/pushRewind build a widget inside its closure).
func (r *Registry) widgetFrame(t *table, m *match, last string) protocol.Frame {
	g := m.e.G
	w := protocol.Widget{Turn: g.Turn, Step: g.Step.String(), Phase: view.PhaseOf(g.Step),
		Active: uint8(g.Active), Priority: uint8(g.Priority), StackDepth: len(g.Stack), Last: last, State: m.state}
	for _, p := range g.Players {
		w.Life = append(w.Life, p.Life)
		w.Lost = append(w.Lost, p.Lost)
	}
	if w.Life == nil {
		w.Life, w.Lost = []int32{}, []bool{}
	}
	return frame(protocol.TWidget, t, m.k, head(m), w)
}

// eventBodiesFor redacts and describes evs against g for one viewer, the
// state that produced them (RedactEventsFor's convention): the viewer/vis
// pair is exactly what RedactEventsFor takes, so Events (spectator) and
// EventsSeat (seat) and the fan-out (the table's spectator) all share this
// one body and can only drift through that pair. Describe runs on the
// REDACTED event so a hidden card's name never reaches the line.
//
// Takes g and evs rather than a live (*table, *match) (fix round 1, FL-42)
// so a caller that cannot hold m.mu for the whole call — Events, over a
// caller-controlled since that can span the entire log — can copy g
// (Clone) and evs (a slice copy) under a brief read lock and format them
// afterwards. fanout/onMatchStart/onMatchEnd still call this under their
// own already-held lock, passing m.e.G and m.e.L.Events[from:] directly:
// no clone needed there, since they never release the lock mid-call.
func eventBodiesFor(viewer state.PlayerID, vis view.Visibility, g *state.Game, evs []events.Event) []protocol.EventBody {
	// One-pass body build: redact each event into its own protocol.EventBody
	// inline (view.RedactEventFor) instead of first materialising a full
	// len(evs) []events.Event via RedactEventsFor and copying that into this
	// output. The (viewer, vis) pair is exactly the same pair the burst
	// version applied to the whole slice, and Describe still runs on the
	// redacted event, so a hidden card's name never reaches the line.
	out := make([]protocol.EventBody, 0, len(evs))
	for _, ev := range evs {
		r := view.RedactEventFor(g, ev, viewer, vis)
		out = append(out, protocol.EventBody{Event: protocol.EventFrom(r), Line: view.Describe(g, r)})
	}
	return out
}

// lastLine is the transcript line of the most recent described event that
// has one, for widget.last; prev carries the previous value forward when
// nothing in bodies has a line.
func lastLine(bodies []protocol.EventBody, prev string) string {
	for i := len(bodies) - 1; i >= 0; i-- {
		if bodies[i].Line != "" {
			return bodies[i].Line
		}
	}
	return prev
}

// tailFrom bounds a one-time end-of-match rescan to (at most) the last n
// events, so finding the closing transcript line costs a small constant
// regardless of how long the match ran, rather than redacting/describing
// the whole log again.
func tailFrom(m *match, n int) int {
	if total := len(m.e.L.Events); total > n {
		return total - n
	}
	return 0
}

// hasMode reports whether modes contains mode.
func hasMode(modes []string, mode string) bool {
	for _, m := range modes {
		if m == mode {
			return true
		}
	}
	return false
}

// fanout delivers one burst to every subscribed session: focus sessions
// get the events (and the pending decision, if any) as frames; overview
// sessions get a coalesced widget. It never blocks on a client.
//
// Building the per-event frames (evFrames) or the widget is gated on
// whether a subscriber of that mode actually exists, since a burst runs up
// to tens of thousands of times per match; eventBodies itself still runs
// whenever anyone is subscribed, focus or overview, since both need it
// (evFrames from it directly, the widget for its transcript line).
func (r *Registry) fanout(t *table, m *match, before int) {
	ss, modes := r.sessionsFor(t.cfg.ID)
	if len(ss) == 0 {
		return
	}
	focus, overview := hasMode(modes, protocol.ModeFocus), hasMode(modes, protocol.ModeOverview)

	m.mu.RLock()
	var evFrames []protocol.Frame
	var widget protocol.Frame
	bodies := eventBodiesFor(view.NoSeat, t.cfg.Spectator, m.e.G, m.e.L.Events[before:])
	if focus {
		evFrames = make([]protocol.Frame, 0, len(bodies))
		for _, b := range bodies {
			evFrames = append(evFrames, frame(protocol.TEvent, t, m.k, b.Event.Seq, b))
		}
	}
	t.lastLine = lastLine(bodies, t.lastLine)
	if overview {
		widget = r.widgetFrame(t, m, t.lastLine)
	}
	var decision *protocol.Frame
	if focus {
		if d := m.e.Pending(); d != nil {
			f := frame(protocol.TDecision, t, m.k, head(m), protocol.DecisionBody{Player: uint8(d.Player), Kind: string(d.Kind), Prompt: d.Prompt})
			decision = &f
		}
	}
	m.mu.RUnlock()

	// fanMu serialises this push loop against a focus Subscribe's own
	// snapshot build+push (Ruling FL-30) — see host/session.go's Subscribe
	// and host/table.go's fanMu doc. push never blocks, so this never
	// parks the match loop on a client.
	t.fanMu.Lock()
	for i, s := range ss {
		switch modes[i] {
		case protocol.ModeFocus:
			for _, f := range evFrames {
				if !s.push(f) {
					break
				}
			}
			if decision != nil {
				s.push(*decision)
			}
		case protocol.ModeOverview:
			s.setWidget(t.cfg.ID, widget)
		}
	}
	t.fanMu.Unlock()
	r.dropOverflowed(ss)
}

// onMatchStart announces a match to every subscriber and gives focus
// subscribers their first snapshot. Genesis events (before the first
// intent) are already inside that snapshot's head, so a focus client
// starts from the snapshot rather than replaying them as frames.
func (r *Registry) onMatchStart(t *table, m *match) {
	ss, modes := r.sessionsFor(t.cfg.ID)
	if len(ss) == 0 {
		return
	}
	focus, overview := hasMode(modes, protocol.ModeFocus), hasMode(modes, protocol.ModeOverview)

	m.mu.RLock()
	start := frame(protocol.TMatchStart, t, m.k, 0, protocol.MatchStart{Seats: m.seats, Seed: m.seed, Spectator: t.cfg.Spectator.String(), BotPolicy: t.cfg.BotPolicy})
	m.mu.RUnlock()
	// The focus snapshot projects the LIVE engine, so it runs exclusively:
	// ProjectFor mutates the engine's derived-P/T frames and caches, and a
	// second projection racing it under a read lock corrupts them (see
	// projectLive). The t.fanMu push loop below stays OUTSIDE this section —
	// Subscribe holds t.fanMu then takes m.mu, so taking t.fanMu under m.mu
	// here would invert the order and deadlock.
	var snap, widget protocol.Frame
	if focus || overview {
		r.projectLive(m, func() {
			if focus {
				snap = r.snapshotFrame(t, m)
			}
			if overview {
				bodies := eventBodiesFor(view.NoSeat, t.cfg.Spectator, m.e.G, m.e.L.Events)
				t.lastLine = lastLine(bodies, t.lastLine)
				widget = r.widgetFrame(t, m, t.lastLine)
			}
		})
	}

	t.fanMu.Lock()
	for i, s := range ss {
		s.push(start)
		if modes[i] == protocol.ModeFocus {
			s.push(snap)
		} else {
			s.setWidget(t.cfg.ID, widget)
		}
	}
	t.fanMu.Unlock()
	r.dropOverflowed(ss)
}

// onMatchEnd sends match_end (any final state) to every subscriber and a
// final widget to overview ones. The match is archived to disk first
// (Task 12), so the end frame and the sidecar agree.
func (r *Registry) onMatchEnd(t *table, m *match) {
	r.archive(t, m)
	ss, modes := r.sessionsFor(t.cfg.ID)
	if len(ss) == 0 {
		return
	}
	overview := hasMode(modes, protocol.ModeOverview)

	m.mu.RLock()
	end := frame(protocol.TMatchEnd, t, m.k, head(m), protocol.MatchEnd{Result: m.result, Winner: m.winner, Head: m.head})
	var widget protocol.Frame
	if overview {
		bodies := eventBodiesFor(view.NoSeat, t.cfg.Spectator, m.e.G, m.e.L.Events[tailFrom(m, 64):])
		t.lastLine = lastLine(bodies, t.lastLine)
		widget = r.widgetFrame(t, m, t.lastLine)
	}
	m.mu.RUnlock()

	t.fanMu.Lock()
	for i, s := range ss {
		s.push(end)
		if modes[i] == protocol.ModeOverview {
			s.setWidget(t.cfg.ID, widget)
		}
	}
	t.fanMu.Unlock()
	r.dropOverflowed(ss)
}

// sendHalted delivers table_halted (spec D15) to every subscriber of a
// table the run loop has stopped for good.
func (r *Registry) sendHalted(t *table, k int, reason string) {
	ss, _ := r.sessionsFor(t.cfg.ID)
	for _, s := range ss {
		s.push(frame(protocol.TTableHalted, t, k, 0, protocol.TableHaltedBody{Reason: reason}))
	}
	r.dropOverflowed(ss)
}

// archive writes the final sidecar, closes the files, and drops the
// snapshots: a finished match is served from disk (spec: snapshots dropped
// when the match finishes). The engine itself is released by retire, which
// does not keep a persistence-mode match once play returns (onMatchEnd still
// reads it after archive). In memory mode the engine is kept so ViewAt
// still works.
//
// The sidecar indexed in t.archived omits NameUniverseNames: the list is
// ~24k labels per name-universe match, only a disk rebuild needs it, and
// loadArchived reads it back from the match's own N.json.
func (r *Registry) archive(t *table, m *match) {
	if r.opts.Dir == "" {
		return
	}
	m.mu.Lock()
	sc := m.sidecar()
	m.files.close()
	m.files = nil
	m.snaps = nil
	m.mu.Unlock()
	if err := writeSidecar(r.opts.Dir, sc, r.opts.Sync); err != nil {
		m.mu.Lock()
		m.reason = "sidecar: " + err.Error()
		m.mu.Unlock()
	}
	sc.NameUniverseNames = nil
	t.mu.Lock()
	t.archived = append(t.archived, sc)
	t.mu.Unlock()
	r.mu.Lock()
	r.saveLocked()
	r.mu.Unlock()
}
