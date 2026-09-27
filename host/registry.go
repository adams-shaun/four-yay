package host

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/seat"
)

// OnBurstFunc observes one recorded burst of a match's event chain: evs is
// that burst's events (the slice appended since the previous burst), in is
// the intent that drove it (nil for the genesis burst, which has no
// decision). t is the producing table and k the match number, so one sink
// can serve many tables and matches (Task M2c-1, FL-81: mtgserve runs the
// registry in-process and persists every match to SQLite through this
// hook).
//
// The callback runs on the match's own goroutine, while the table's match
// lock is held — so it must NOT call back into the Registry (that re-enters
// the same goroutine holding the lock and deadlocks), and it must not block
// on anything that could, in turn, wait on this goroutine. It should copy
// anything it wants to keep: evs is a fresh slice (a stable snapshot of this
// burst) but the events' guts and in are read-only references into the live
// match. Returning an error crashes the match exactly as a persist failure
// does (D15): the table halts and the event chain does not continue.
//
// A nil OnBurst is today's behaviour — nothing fires — so an embedder that
// does not set it pays no cost and sees no change.
type OnBurstFunc func(t TableID, k int, evs []events.Event, in *decision.Intent) error

// OnMatchEndFunc observes the terminal state of a finished, aborted or
// crashed match. It runs on the match's own goroutine after the match's
// final state has already been recorded, and like OnBurstFunc it must not
// call back into the Registry or block. Its error return cannot change that
// already-decided outcome and is only surfaceable by the embedder itself;
// a non-nil sink that needs an outcome to succeed should log/verify inside
// the callback.
type OnMatchEndFunc func(t TableID, k int, m protocol.MatchInfo) error

// Options configures a Registry. LoadDeck is required so a caller can never
// forget that the host reads no files for decks itself.
type Options struct {
	Dir      string
	LoadDeck func(name string) (Deck, error)
	// Tokens is the token corpus the engine needs (rules.Config.Tokens):
	// the token definitions the decks in this match can create. Passed into
	// every rules.Config the host builds — live matches and the replays
	// that serve finished ones from disk — so a persisted match replays
	// with the same token definitions (Ruling FL-40).
	Tokens map[string]*cards.Card
	// NameUniverse is the compiled card corpus a NameCard decision ranges
	// over (rules.Config.NameUniverse): Pithing Needle names any card, a
	// land included, and this is the only source of names a seat has never
	// seen. Passed into every rules.Config the host builds, live and replay
	// alike, for the same reason Tokens is (the replay must offer the same
	// options the live match did).
	NameUniverse []*cards.Card
	// Sleep is the table's only clock read (PL-11): run calls it between
	// matches for Cooldown, and play calls it after every decision for
	// Pace. It must return once d elapses OR stop closes, whichever comes
	// first — that is what lets Close interrupt a table sitting in a long
	// cooldown or a slow pace (Ruling FL-18) instead of blocking up to d.
	// A nil Sleep gets defaultSleep, which does exactly that with a real
	// timer; it is the only place in the package that touches the clock,
	// so a caller wanting a faster-than-realtime test still goes through
	// this field rather than host reading time.Now/time.Sleep itself.
	Sleep func(d time.Duration, stop <-chan struct{})
	// Seats is an explicit embedder controller override. Nil selects the
	// table's named hosted policy through defaultSeats in play.
	Seats    func(names []string, seed uint64) []seat.Seat
	Sync     bool
	Ring     int
	Cooldown time.Duration
	// MaxIntents caps a whole match's total intent count (play's final
	// termination guard, 400000 by default — see match.go). It bounds a
	// match that will not terminate but says nothing about where it is
	// stuck, so it is a different question from MaxDecisionsPerTurn below.
	MaxIntents int
	// MaxDecisionsPerTurn is the host's progress guard for the stall that
	// keeps the SAME turn alive forever — the shape observed in the wild when
	// a bot re-offers one no-op action repeatedly (seed 175's 20000 intents
	// inside 2 turns; the measured 36 of 200 four-seat Commander games frozen
	// on main by a policy that kept re-activating an Equip on the creature
	// already carrying it). play counts decisions answered since the last
	// turn advance and, when the count reaches this limit with the turn
	// still unchanged, crashes the match with a stall reason and halts the
	// table through the ordinary crash path. Before this guard such a match
	// sat TableLive forever — the lobby showed LIVE, no match ever followed —
	// because Options.ThinkTimeout covers only a HumanSeat parked on a
	// decision, and a bot loop parks on nothing.
	//
	// The unit is a DECISIONS count, never a duration. A wall-clock watchdog
	// would make a match's outcome depend on how loaded the box was, which
	// destroys the determinism the whole engine is built on: the same seed
	// must produce the same event chain and the same result on every machine
	// and every replay (botbench's own watchdog is a -max-intents count for
	// the same reason). A turn that is merely LONG still passes: the counter
	// resets on every turn advance, and that reset is what distinguishes
	// "one very long turn" from "a turn that never ends". The measured
	// stall did ~10000 decisions per turn without the turn advancing; the
	// largest LEGITIMATE single turn across this repo's whole gate
	// population (2/4/6/8-seat sample-deck and legacy and Commander
	// repo-deck matches, 40+ games, measured for this task's sizing) is 244
	// decisions.
	//
	// DefaultMaxDecisionsPerTurn (25000) is the recommended production value
	// — 100x the measured legit ceiling above, and below the observed
	// stall's own ~10000-per-turn rate, so the stall class that froze the
	// demo trips within its third turn instead of freezing the table. 0 —
	// the zero value — means "no guard": every existing test and embedder
	// that never sets the field keeps exactly today's behaviour, so this
	// option is strictly opt-in. 0 ALSO disables the engine's own livelock
	// watcher (rules.Config.LoopGuard.Disabled, wired in match.go): the two
	// are the same non-terminating-loop protection at two levels, and the
	// engine aborting a game whose stall guard the embedder explicitly
	// turned off would defeat the opt-out.
	MaxDecisionsPerTurn int
	// ThinkTimeout is how long a HumanSeat parks on a decision before its
	// deterministic caretaker bot (the already-seeded bot for that slot) is
	// asked to answer in the player's place (Task M2b-3, D3). 0 — the default
	// — means "no timeout": the seat waits for a human SubmitIntent forever,
	// and only falls back to the caretaker when the table's context is
	// cancelled (a disconnected human seat must never wedge play, FL-17).
	// A non-zero value is a live clock read (host is the package allowed
	// time) that converts an unanswered decision into exactly the intent the
	// slot's bot would have produced, so a timed-out human game replays
	// byte-identically (the caretaker intent is committed to the log like any
	// other). The player may reconnect and answer later decisions via
	// SubmitIntent (D2).
	ThinkTimeout time.Duration
	// DefaultBotAutoPayMana is applied only while restoring a table written
	// before TableConfig recorded bot_auto_pay_mana. New tables always carry
	// their explicit setting, including false, so a restart preserves their
	// configured behaviour. gorged supplies its -bot-auto-mana startup flag
	// here to migrate an existing deployment when it first runs this build.
	DefaultBotAutoPayMana bool
	// MaxOnDemandTables bounds browser-created private tables retained by a
	// running process. 0 keeps the historical unlimited behaviour. A limit
	// refuses a new game instead of deleting a finished table: its private
	// join URL, feedback capture and replay routes remain valid until the
	// process restarts, when OnDemand's documented process-scoped cleanup
	// removes its config.
	MaxOnDemandTables int

	// OnBurst, when non-nil, is invoked after every recorded burst of every
	// match created by this registry, including the genesis burst, so an
	// embedder sees the whole chain from its first event (Task M2c-1). It is
	// the SQLite-persistence hook for mtgserve. See OnBurstFunc for the
	// contract — most importantly, the callback runs on the match goroutine
	// holding the match lock and must not re-enter the Registry.
	OnBurst OnBurstFunc
	// OnMatchEnd, when non-nil, is invoked once per match once it reaches a
	// terminal state (finished, aborted or crashed), with that match's final
	// MatchInfo (Task M2c-1). See OnMatchEndFunc.
	OnMatchEnd OnMatchEndFunc
	// OnRewind, when non-nil, is invoked exactly once per undo an undo-capable
	// match performs, with the truncation point the match rewound to. See
	// OnRewindFunc (host/undo.go) — a sink that persists bursts through OnBurst
	// MUST implement it, or its stored chain keeps events the live match has
	// undone while later bursts re-use the same seq numbers for different
	// events.
	OnRewind OnRewindFunc

	// beforeFinish is a test-only barrier immediately before play takes the
	// match lock that linearizes a natural finish against Undo admission. It
	// is deliberately unexported: production has no reason to delay this
	// boundary, while the race regression must force both lock orderings
	// without relying on scheduler timing.
	beforeFinish func()
}

// defaultSleep is installed when Options.Sleep is nil. It is the package's
// only time.Now/time.NewTimer read (PL-11's stated exception): everywhere
// else in host reaches the clock only through the Sleep field.
func defaultSleep(d time.Duration, stop <-chan struct{}) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-stop:
	}
}

// Registry owns the tables and the sessions watching them.
type Registry struct {
	opts Options

	mu       sync.RWMutex
	tables   map[TableID]*table
	sessions map[string]*Session
	nextSess int
	closed   bool
	done     chan struct{} // closed by Close once every table has been told to stop
	wg       sync.WaitGroup
}

// New validates Options and, when Dir is set, reads the registry back from
// disk (Task 12). Nothing starts running until Start/StartAll.
func New(o Options) (*Registry, error) {
	if o.LoadDeck == nil {
		return nil, fmt.Errorf("host: Options.LoadDeck is required")
	}
	if o.Sleep == nil {
		o.Sleep = defaultSleep
	}
	if o.Ring == 0 {
		o.Ring = 256
	}
	if o.MaxOnDemandTables < 0 {
		return nil, fmt.Errorf("host: MaxOnDemandTables %d, want >= 0", o.MaxOnDemandTables)
	}
	r := &Registry{opts: o, tables: map[TableID]*table{}, sessions: map[string]*Session{}, done: make(chan struct{})}
	if o.Dir != "" {
		if err := r.load(); err != nil { // Task 12
			return nil, err
		}
	}
	return r, nil
}

// AddTable registers (and persists) a table without starting it.
func (r *Registry) AddTable(c TableConfig) error {
	var err error
	c, err = c.validated(r.opts.LoadDeck)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("host: registry is closed")
	}
	if _, dup := r.tables[c.ID]; dup {
		return fmt.Errorf("host: table %s already exists", c.ID)
	}
	if c.OnDemand && r.opts.MaxOnDemandTables > 0 {
		n := 0
		for _, t := range r.tables {
			if t.cfg.OnDemand {
				n++
			}
		}
		if n >= r.opts.MaxOnDemandTables {
			return fmt.Errorf("host: on-demand table limit %d reached", r.opts.MaxOnDemandTables)
		}
	}
	r.tables[c.ID] = newTable(c)
	return r.saveLocked() // Task 12; a no-op in memory mode
}

// Start launches the table's goroutine; a second Start is a no-op.
func (r *Registry) Start(id TableID) error {
	r.mu.Lock()
	t, ok := r.tables[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("host: registry is closed")
	}
	if t.started {
		r.mu.Unlock()
		return nil
	}
	t.started = true
	r.wg.Add(1)
	r.mu.Unlock()
	go r.run(t)
	return nil
}

// StartAll starts every registered table, in ID order.
func (r *Registry) StartAll() error {
	for _, id := range r.ids() {
		if err := r.Start(id); err != nil {
			return err
		}
	}
	return nil
}

// Wait blocks until the table's goroutine has exited: the table is idle,
// halted or the registry was closed. An unknown or never-started table
// returns at once.
func (r *Registry) Wait(id TableID) {
	r.mu.RLock()
	t, ok := r.tables[id]
	started := ok && t.started
	r.mu.RUnlock()
	if !started {
		return
	}
	<-t.done
}

// run is the table's goroutine: match after match while perpetual, until
// a non-perpetual match ends, the registry closes, or a crash halts it.
//
// ctx is derived once, here, for the table's whole lifetime — not once per
// match — and is the sole cancellation path play has into a Seat.Decide
// call that is already blocked when Close runs (Ruling FL-17): t.stop
// itself is only ever polled between decisions, so a seat that does not
// return on its own (a disconnected human, Task 25) would otherwise wedge
// this goroutine, and Close's wg.Wait with it, forever. The bridging
// goroutine below is the only consumer of ctx.Done() other than
// play/Decide; it exits as soon as either side fires, so it never outlives
// run.
func (r *Registry) run(t *table) {
	defer r.wg.Done()
	defer close(t.done)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-t.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	for {
		t.mu.Lock()
		k := t.k + 1
		t.mu.Unlock()
		m, err := r.newMatch(t, k)
		if err != nil {
			r.halt(t, k, err) // Task 13; sets state halted
			return
		}
		t.mu.Lock()
		t.k, t.cur, t.state = k, m, protocol.TableLive
		t.mu.Unlock()
		r.mu.Lock()
		r.saveLocked() // tables.json records the new match index before it plays
		r.mu.Unlock()
		r.onMatchStart(t, m) // Tasks 10, 12
		final := r.play(ctx, t, m)
		r.retire(t, m)
		switch final {
		case protocol.MatchCrashed:
			r.halt(t, k, fmt.Errorf("%s", m.reason))
			return
		case protocol.MatchAborted:
			t.setState(protocol.TableIdle)
			return
		}
		// Task M2c-2: a human-seated table stops at game over exactly like a
		// non-perpetual one; singleShot captures both. StartAll revives a
		// restored human table for a fresh single match, never an autoplay.
		if t.cfg.singleShot() {
			t.setState(protocol.TableIdle)
			return
		}
		t.setState(protocol.TableCooldown)
		r.opts.Sleep(r.opts.Cooldown, t.stop)
		select {
		case <-t.stop:
			t.setState(protocol.TableIdle)
			return
		default:
		}
	}
}

// retire takes a finished match off t.cur. In persistence mode the match
// is dropped from memory entirely — archive() has already recorded its
// sidecar in t.archived, so lookup serves it from disk — and the engine is
// released once any in-flight reader lets go. In memory mode the engine is
// the only copy, so the match joins t.history with its log trimmed to its
// length (the live log was reserved at defaultExpectedEvents), and history
// keeps only the last memoryHistoryLimit matches.
func (r *Registry) retire(t *table, m *match) {
	if r.opts.Dir == "" {
		m.mu.Lock()
		m.trimLog()
		m.mu.Unlock()
	}
	t.mu.Lock()
	t.cur = nil
	if r.opts.Dir == "" {
		t.history = append(t.history, m)
		if n := len(t.history) - memoryHistoryLimit; n > 0 {
			// Copy rather than reslice so the dropped matches do not stay
			// reachable through the backing array.
			t.history = append([]*match(nil), t.history[n:]...)
		}
	}
	t.mu.Unlock()
}

// trimLog gives a finished match's log a backing array exactly its length,
// releasing the spare capacity the live log was reserved with. Snapshots
// cloned from the live log share its backing array (Log.Clone truncates the
// capacity, not the array), so each one that does is re-pointed at the
// same prefix of the new array — the same events by memory identity. The
// snapshots are rebuilt, never mutated in place: a reader may be cloning a
// snapshot engine it copied out from under the read lock (viewAt).
// Called with m.mu held for writing, after the match's last event.
func (m *match) trimLog() {
	old := m.e.L.Events
	if len(old) == 0 || cap(old) == len(old) {
		return
	}
	evs := make([]events.Event, len(old)) // exact capacity; slices.Clone rounds up
	copy(evs, old)
	m.e.L.Events = evs
	snaps := make([]snapshot, len(m.snaps))
	for i, s := range m.snaps {
		snaps[i] = s
		se := s.e.L.Events
		if len(se) == 0 || len(se) > len(evs) || &se[0] != &old[0] {
			continue
		}
		ne := s.e.Clone()
		ne.L.Events = evs[:len(se):len(se)]
		snaps[i].e = ne
	}
	m.snaps = snaps
}

// halt is D15's second half for the table: it stops and stays stopped,
// recording why. k is the match number the caller was building or had just
// finished — not necessarily t.k, which the newMatch-failure path never
// bumps, so a first-boot halt is addressed to the match that actually
// failed, not match 0. Task 13 adds the crash report file.
func (r *Registry) halt(t *table, k int, err error) {
	reason := err.Error()
	t.mu.Lock()
	t.state = protocol.TableHalted
	t.reason = reason
	t.mu.Unlock()
	r.sendHalted(t, k, reason)
}

// Tables lists every table, sorted by ID.
func (r *Registry) Tables() []protocol.TableInfo {
	out := make([]protocol.TableInfo, 0)
	for _, id := range r.ids() {
		r.mu.RLock()
		t := r.tables[id]
		r.mu.RUnlock()
		out = append(out, t.info())
	}
	return out
}

// LobbyTables lists the long-lived tables that belong in the public lobby.
// A play-vs-bot table is entered only through the unguessable join URL
// returned by POST /api/games; advertising it here would leak an abandoned
// private game into every visitor's lobby (and used to render one card for
// every game restored from old persistence). It remains addressable through
// its table-scoped routes for the lifetime of this process.
func (r *Registry) LobbyTables() []protocol.TableInfo {
	out := make([]protocol.TableInfo, 0)
	for _, id := range r.ids() {
		r.mu.RLock()
		t := r.tables[id]
		r.mu.RUnlock()
		t.mu.RLock()
		onDemand := t.cfg.OnDemand
		t.mu.RUnlock()
		if !onDemand {
			out = append(out, t.info())
		}
	}
	return out
}

// Matches lists a table's matches in ascending order; the live one last.
// Finished matches known only from disk (archived sidecars) come first,
// then in-memory history entries whose match index is not already
// archived (memory mode only: persistence mode retains no history), then
// the live match.
func (r *Registry) Matches(id TableID) ([]protocol.MatchInfo, error) {
	r.mu.RLock()
	t, ok := r.tables[id]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	t.mu.RLock()
	out := make([]protocol.MatchInfo, 0, len(t.archived)+len(t.history)+1)
	archived := make(map[int]bool, len(t.archived))
	for _, sc := range t.archived {
		out = append(out, sc.info())
		archived[sc.Match] = true
	}
	ms := append([]*match(nil), t.history...)
	if t.cur != nil {
		ms = append(ms, t.cur)
	}
	t.mu.RUnlock()
	for _, m := range ms {
		if archived[m.k] {
			continue
		}
		m.mu.RLock()
		out = append(out, m.info())
		m.mu.RUnlock()
	}
	return out, nil
}

// Close stops every table, aborts in-progress matches, closes sessions and
// waits for the goroutines. Idempotent.
func (r *Registry) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	for _, t := range r.tables {
		close(t.stop)
	}
	close(r.done)
	r.mu.Unlock()
	r.wg.Wait()
	r.closeSessions() // Task 10
	return nil
}

// Done is closed once Close has signalled every table to stop — before Close
// waits for them — so a Sleep hook that triggers Close can wait for the
// signal without deadlocking on its own goroutine.
func (r *Registry) Done() <-chan struct{} { return r.done }

// ids is the sorted table list every enumeration walks, so no map order
// ever reaches a frame or a file.
func (r *Registry) ids() []TableID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]TableID, 0, len(r.tables))
	for id := range r.tables {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
