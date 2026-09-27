package host

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

const defaultMaxIntents = 400000

// DefaultMaxDecisionsPerTurn is the recommended value for
// Options.MaxDecisionsPerTurn — the one gorged installs for its served
// tables. The number is sized from measurement, not chosen round: the
// largest legitimate single turn measured across the gate population
// (sample-deck and repo-deck matches at 2/4/6/8 seats) is 244 decisions,
// and the observed policy stall ran ~10000 decisions per turn without the
// turn advancing, so 25000 clears the legitimate ceiling by ~100x and is
// swept by that stall class within three stall-turns. See the Options
// field's comment for the full reasoning, including why the unit is a
// count and never a duration.
//
// Note this is NOT installed when the field is 0: zero means "no guard",
// so every caller that does not opt in keeps exactly today's behaviour.
const DefaultMaxDecisionsPerTurn = 25000

// defaultExpectedEvents is the expected-size hint passed to a live match log's
// Reserve: just above the measured top of a real match's event count (~74k for
// the 4-seat repo-deck match), so a typical match never reallocates and the
// biggest observed one grows with at most one doubling. Sized from the profile,
// not from defaultMaxIntents: reserving at the intent cap would hold ~5x too
// much of a typical match's final length live.
const defaultExpectedEvents = 80000

// match is one game on a table: the engine, the intent boundaries and
// turn starts a view request needs, and the outcome. mu guards everything
// below cfg: the run loop holds it for the duration of each Submit and its
// bookkeeping; readers (ViewAt, Events, fan-out) hold it for reads only and
// never drive the engine.
type match struct {
	table *table
	k     int
	seed  uint64
	cfg   rules.Config
	seats []protocol.SeatInfo
	decks []string
	// slots is the actual []seat.Seat the current match built in play().
	// Registry methods that must reach a per-seat *HumanSeat (Pending,
	// SubmitIntent, Task M2b-2) go through it. Installed once at the top
	// of play(), never reassigned, so a resolved *HumanSeat stays stable
	// for the match's whole lifetime.
	slots []seat.Seat

	mu sync.RWMutex
	e  *rules.Engine
	// files is the live match's append-only logs; nil in memory mode and
	// after the match is archived (Task 12).
	files *matchFiles
	// persisted is the number of events confirmed appended to this match's
	// events file at the last successful persist; only that prefix is
	// durable, so a crash or kill records the head over it, never the
	// in-memory tail (fix round 1, burst atomicity). 0 in memory mode.
	persisted int
	// bounds[j] is len(e.L.Events) after j intents: the seq one past the
	// end of the j-th burst. bounds[0] is genesis plus the first Advance.
	bounds []uint64
	// turnStarts is the seq of every TurnChange so far — the DVR's ticks.
	turnStarts []uint64
	snaps      []snapshot // Task 11
	intents    int
	state      string // protocol.Match*
	result     string // "win", "draw" or ""
	winner     *uint8
	head       string
	reason     string // crash reason (Task 13)
	// undo queues accepted undo requests (Registry.Undo, host/undo.go) for the
	// play loop. Its size-one signal channel is only a wakeup: undoQueue's
	// pending count preserves every accepted click, rearming the wakeup after
	// each rewind until the queue is empty. Created at match build, never
	// reassigned, and dies with the match.
	undo *undoQueue
}

// snapshot is a cloned engine at an intent boundary that began a turn.
type snapshot struct {
	intent int
	seq    uint64 // Task 12: the persisted burst boundary this snapshot lines up with on disk.
	e      *rules.Engine
}

// newMatch resolves decks, seeds and builds the engine through genesis and
// the first Advance, so the returned match is at intent boundary 0. When
// persistence is on it also opens the match's files, writes the live
// sidecar and appends the genesis events; any error halts the table.
func (r *Registry) newMatch(t *table, k int) (*match, error) {
	c := t.cfg
	seed := MatchSeed(c.Seed, k)
	names := make([]string, c.Seats)
	decks := make([][]*cards.Card, c.Seats)
	sideboards := make([][]*cards.Card, c.Seats)
	deckNames := make([]string, c.Seats)
	// Display player names, independent of the deck: configured per seat (the
	// same slice index a match's seat uses, stable under the +k deck
	// rotation), else the deterministic "Player 1".."Player N". These reach
	// the wire as view.PlayerView.Name and protocol.SeatInfo.Name; the deck
	// identity stays on the engine's Names (event text, replay-hashed) and on
	// SeatInfo.Deck, so a player box can show a name that is a name, not a
	// deck stem.
	playerNames := make([]string, c.Seats)
	for i := 0; i < c.Seats; i++ {
		playerNames[i] = fmt.Sprintf("Player %d", i+1)
		if i < len(c.PlayerNames) && c.PlayerNames[i] != "" {
			playerNames[i] = c.PlayerNames[i]
		}
	}
	// cmds holds each seat's commander indices, parallel to decks: the
	// Deck the loader produced for that seat carries its own commanders,
	// so a commander table's seats get a command zone from their own deck
	// and a constructed table's seats never do.
	cmds := make([][]int, c.Seats)
	infos := make([]protocol.SeatInfo, c.Seats)
	for i := 0; i < c.Seats; i++ {
		dn := c.Decks[(i+k)%len(c.Decks)]
		d, err := r.opts.LoadDeck(dn)
		if err != nil {
			return nil, fmt.Errorf("host: table %s match %d: deck %q: %w", c.ID, k, dn, err)
		}
		if d.Name == "" {
			d.Name = dn
		}
		names[i], decks[i], sideboards[i], deckNames[i], cmds[i] = d.Name, d.Cards, d.Sideboard, dn, d.Commanders
		infos[i] = protocol.SeatInfo{Name: playerNames[i], Deck: d.Name, Colour: protocol.SeatColours[i%len(protocol.SeatColours)], DeckID: deckNames[i]}
		// Human marks the slots TableConfig.Humans seats with a real person:
		// the wire signal a client's undo control reads (protocol.SeatInfo's
		// doc).
		for _, h := range c.Humans {
			if h == i {
				infos[i].Human = true
				break
			}
		}
	}
	cfg := rules.Config{Seed: seed, Names: names, PlayerNames: playerNames, Decks: decks, Sideboards: sideboardConfig(sideboards), Tokens: r.opts.Tokens, NameUniverse: r.opts.NameUniverse, Mulligans: c.Mulligans}
	// The engine's own livelock watcher (rules/livelock.go) is the same
	// non-terminating-loop protection as this file's per-turn decision
	// guard, one level down: an embedder that opted out of the host guard
	// (MaxDecisionsPerTurn == 0, the supervised-infinite-loop shape the
	// stall tests drive) must not have the engine crash the match instead
	// at its own 400-event cycle threshold. Propagate the opt-out; every
	// Config with the host guard at its default keeps the watcher on.
	if r.opts.MaxDecisionsPerTurn == 0 {
		cfg.LoopGuard = &rules.LoopGuard{Disabled: true}
	}
	// The format the table was configured with is threaded into the engine
	// once, here, so the match's rules.Config is the single value both the
	// live game and its replay are built from (R-8.4). A commander table
	// also resolves its starting life — CR 903.6's 40 (TableConfig's
	// 0-means-format-default convention) — and its per-seat commanders
	// from the loaded decks; validate has already rejected a commander
	// table whose deck names no commander, so a seat can never silently
	// play without a command zone.
	switch c.Format {
	case FormatCommander:
		cfg.Format = rules.FormatCommander
		life := c.StartingLife
		if life == 0 {
			life = 40
		}
		cfg.StartingLife = life
		cfg.Commanders = make([][]int, c.Seats)
		for i := range cfg.Commanders {
			cfg.Commanders[i] = append([]int(nil), cmds[i]...)
		}
	default: // FormatConstructed: the zero rules.Config, every field stays unset.
	}
	e := rules.NewStartingPlayerChoice(cfg)
	// Events growEvents was the top allocator in ./host (2.87 GB of the test
	// binary's profile: every live match log reallocated ~2x its final length
	// on the way up). Preallocating the live log to just above a real match's
	// event count makes the common case grow without reallocating at all, and
	// the one make at Reserve time costs less than the doubling series it
	// replaces. The value is a hint, sized from the measured top of a real
	// match (the 4-seat repo-deck match runs ~74k events; stays under defaultMaxIntents);
	// a match that overruns it doubles once and is still correct. Reserve is a
	// pure capacity hint on the live log and cannot leak spare capacity to a
	// clone (Clone truncates to len), so the clone-sharing invariant is intact.
	e.L.Reserve(defaultExpectedEvents)
	// CR 103.1's second half: the toss winner chooses who takes the first
	// turn. Pose that choice BEFORE advancing so the play loop's parking
	// conveys it to the winner's seat; a caller with no decision channel
	// gets the deterministic fallback from Advance instead.
	e.AskStartingPlayer()
	e.Advance()
	m := &match{table: t, k: k, seed: seed, cfg: cfg, seats: infos, decks: deckNames, e: e, state: protocol.MatchLive,
		undo: newUndoQueue()}
	m.bounds = []uint64{uint64(len(e.L.Events))}
	m.turnStarts = turnStartsIn(e.L.Events, 0)
	m.snapshotGenesis()
	if r.opts.Dir != "" {
		var err error
		m.files, err = openMatchFiles(r.opts.Dir, t.cfg.ID, k, r.opts.Sync)
		if err != nil {
			return nil, fmt.Errorf("host: table %s match %d: %w", c.ID, k, err)
		}
		if err := writeSidecar(r.opts.Dir, m.sidecar(), r.opts.Sync); err != nil {
			m.files.close()
			return nil, fmt.Errorf("host: table %s match %d: %w", c.ID, k, err)
		}
		if err := m.files.append(e.L.Events, nil); err != nil {
			m.files.close()
			return nil, fmt.Errorf("host: table %s match %d: %w", c.ID, k, err)
		}
		m.persisted = len(e.L.Events) // genesis is durable once appended
	}
	return m, nil
}

// turnStartsIn lists the seq of every TurnChange in evs[from:].
func turnStartsIn(evs []events.Event, from int) []uint64 {
	var out []uint64
	for _, ev := range evs[from:] {
		if ev.Kind == events.TurnChange {
			out = append(out, ev.Seq)
		}
	}
	return out
}

// locked runs fn under the write lock and releases it even if fn panics —
// a panicking Submit must not leave the mutex held, or crash (which takes
// the lock to record the failure) would deadlock the table.
func (m *match) locked(fn func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn()
}

// afterSubmit records the burst that a successful Submit just produced.
// Called with m.mu held.
func (m *match) afterSubmit(before int) {
	m.intents++
	m.bounds = append(m.bounds, uint64(len(m.e.L.Events)))
	m.turnStarts = append(m.turnStarts, turnStartsIn(m.e.L.Events, before)...)
}

// info is the sidecar/wire summary. Called with m.mu held for reading.
func (m *match) info() protocol.MatchInfo {
	return protocol.MatchInfo{Table: string(m.table.cfg.ID), Match: m.k, Seed: m.seed, Seats: m.seats,
		State: m.state, Result: m.result, Winner: m.winner, Head: m.head,
		Events: len(m.e.L.Events), Turns: m.e.G.Turn, BotPolicy: m.table.cfg.BotPolicy}
}

// sidecar is the on-disk summary of the match. Called with m.mu held.
// For a crashed match the summary reflects the persisted prefix, not the
// in-memory tail: crash() recorded m.head over it, and Events here is the
// persisted count (fix round 1).
func (m *match) sidecar() sidecar {
	events := len(m.e.L.Events)
	if m.files != nil && m.state == protocol.MatchCrashed {
		events = m.persisted
	}
	return sidecar{Table: string(m.table.cfg.ID), Match: m.k, Seed: m.seed, Seats: m.seats, Names: m.cfg.Names,
		PlayerNames: m.cfg.PlayerNames, Decks: m.decks, Spectator: m.table.cfg.Spectator.String(), State: m.state, Result: m.result, Winner: m.winner,
		Head: m.head, Events: events, Turns: m.e.G.Turn, Reason: m.reason, Mulligans: m.cfg.Mulligans,
		NameUniverse:      len(m.cfg.NameUniverse) > 0,
		NameUniverseNames: append([]string(nil), m.e.G.NameUniverseNames...),
		Format:            Format(m.cfg.Format), StartingLife: m.cfg.StartingLife, Commanders: m.cfg.Commanders, BotPolicy: m.table.cfg.BotPolicy}
}

// defaultSeats is PL-14: one bot per seat, seeded from the match seed.
func defaultSeats(policy string, names []string, seed uint64) []seat.Seat {
	return defaultSeatsWithAutoPayMana(policy, false, names, seed)
}

// defaultSeatsWithAutoPayMana builds every table bot with the persisted
// auto-payment setting. Keeping the legacy wrapper preserves embedders and
// tests that intentionally exercise the historical manual-mana policy.
func defaultSeatsWithAutoPayMana(policy string, autoPayMana bool, names []string, seed uint64) []seat.Seat {
	out := make([]seat.Seat, len(names))
	for i := range names {
		bot, err := NewBotPolicySeatWithAutoPayMana(policy, seed^uint64(i+1), autoPayMana)
		if err != nil {
			panic(err) // policy was normalized before the table was registered.
		}
		out[i] = bot
	}
	return out
}

// parkedDecision is the outcome of installing (parking) the seat that owns one
// pending decision, done — as the loop's structure now requires — before that
// decision is ever published. For a bot seat the Decision runs synchronously
// in parkSeat and in/err are already resolved; for a human seat park installed
// the answerable slot and answer() blocks until a SubmitIntent (or ctx/timeout
// caretaker) arrives. p is the decision's owner seat, kept for the crash line.
type parkedDecision struct {
	p   state.PlayerID
	hs  *parking
	in  decision.Intent
	err error
}

// answer returns the parked decision's intent: immediately for a bot, after
// blocking on the human seat's parked slot otherwise — the same intent/error
// pair the old loop got straight out of seat.S Decide.
func (pd *parkedDecision) answer() (decision.Intent, error) {
	if pd.hs != nil {
		in, err := pd.hs.await()
		return decision.CloneIntent(in), err
	}
	return decision.CloneIntent(pd.in), pd.err
}

// parkedData is the projected shape of one pending decision, split out of the
// park step so the projection — which touches the live engine — can be held
// under the match's exclusive lock while the seat step runs without it.
// Exactly one of v (the projected View) and brd (the botpolicy.Board) is set:
// a BoardSeat gets brd built from the engine under the same lock view.Project
// would occupy, and no View is projected for it at all.
type parkedData struct {
	p       state.PlayerID
	v       view.View
	dc      decision.Decision
	brd     botpolicy.Board
	isBoard bool
}

// projectNext reads the engine's current pending decision and projects the
// board for it, copying options exactly as the old loop did (Ruling FL-19
// minor: *d aliases the engine's pending, and dc.Options a slice header into
// the same backing array, so the copy is required). It touches only the
// engine — never a seat — so the loop can hold it under m.mu.Lock. That is
// the fix's lock discipline: view.Project mutates the engine's Derived cache
// (rules/layers.go Engine.active), so projecting the live engine must not run
// concurrently with any other live projection — a focus subscriber's own
// snapshot build now also takes m.mu exclusively (through projectLive, see
// fanout.go), so the two can never overlap; running it inside the Submit's
// exclusive section keeps this projection on the same side of the rule.
//
// seats lets it type-assert the deciding seat: a seat that implements
// seat.BoardSeat gets a botpolicy.Board built from the engine (under the same
// exclusive lock) instead of a projected View, so a bot seat never pays for
// cardViews' string round-trip. The deciding seat is stable for the match, so
// this agrees with parkSeat's own assertion on the same seat.
//
// brd is the per-match botpolicy.Board the host loop keeps and refills for
// every BoardSeat decision (Task d2): BoardFromGameInto clears and reuses its
// three maps instead of allocating them per call — the measured bulk of
// BoardFromGame's cumulative in ./host. The ownership contract is in
// botpolicy.BoardFromGameInto's doc comment: brd's maps are built here under
// the match lock, consumed by the answer's DecideBoard outside it, and never
// read after the next decision refills them.
//
// Returns nil when there is no pending decision (game over, or a stall the
// caller resolves via G.Over). Call on the match goroutine, under m.mu.
func projectNext(m *match, seats []seat.Seat, brd *botpolicy.Board) *parkedData {
	d := m.e.Pending()
	if d == nil {
		return nil
	}
	dc := *d.Clone()
	// Payment plans are an opt-in human interface. Keep the engine's pending
	// decision intact for replay and independently configured bots, but never
	// publish the extension to a human seat when this table has it disabled.
	// Options are untouched, so this is precisely the legacy manual path.
	_, isHuman := seats[d.Player].(*HumanSeat)
	if isHuman && !m.table.cfg.AutoMana {
		dc.PaymentActions = nil
	}
	// A BoardSeat answers from a botpolicy.Board and needs no projected View:
	// build the Board from the engine the way BoardFromGame reads it (same
	// zones, same derived P/T and keywords the View would carry) and skip
	// view.Project entirely — cardViews' string encode/parse round-trip is the
	// measured bulk of the suite's allocations, and a bot is the seat that
	// does it per decision.
	//
	// The HumanSeat test comes FIRST, and must, because parkSeat tests in that
	// order too: it hands a HumanSeat pd.v before it ever considers BoardSeat.
	// *HumanSeat does not implement BoardSeat today, so the two orders agree
	// either way — but if one ever gained a DecideBoard method, the opposite
	// order here would build only the Board and then park the human on a zero
	// View, blanking a live player's board with nothing failing. Testing the
	// same thing first in both places makes that unrepresentable rather than
	// merely unlikely.
	if _, ok := seats[d.Player].(seat.BoardSeat); ok && !isHuman {
		return &parkedData{
			p:       d.Player,
			dc:      dc,
			brd:     botpolicy.BoardFromGameInto(m.e.G, m.e, d.Player, brd),
			isBoard: true,
		}
	}
	v := view.Project(m.e.G, m.e, d.Player, &dc)
	// The seat's view is built at head, so its round is the exact round-trip
	// count (view.RoundOf over the live log), not the snapshot-only roundOf
	// approximation Project fills in (ui13). A human seat renders this view,
	// so it must agree with the board clock the same match fans out.
	v.Round = view.RoundOf(m.e.G, m.e.L.Events)
	return &parkedData{p: d.Player, v: v, dc: dc}
}

// parkSeat installs the answerable slot for a projected decision: for a
// HumanSeat it installs the slot without blocking, for any other seat (a bot,
// or an embedder's blocking seat) it calls Decide — a BoardSeat via
// DecideBoard, matching the half projectNext built. It is NEVER called under
// m.mu — a blocking seat must not hold the match mutex across a Decide — but
// by the time play calls it the next decision is already fully projected, and
// the caller publishes (fanout) only after it returns, so publish-outranks-park
// stays closed. The decision's owner seat is stable, so the BoardSeat/HumanSeat
// assertions here match projectNext's, and exactly the field that was built is
// consumed.
func parkSeat(ctx context.Context, seats []seat.Seat, pd *parkedData, undo <-chan state.PlayerID) *parkedDecision {
	if hs, ok := seats[pd.p].(*HumanSeat); ok {
		return &parkedDecision{p: pd.p, hs: hs.park(ctx, pd.v, pd.dc, undo)}
	}
	if bs, ok := seats[pd.p].(seat.BoardSeat); ok && pd.isBoard {
		in, err := bs.DecideBoard(ctx, pd.brd, pd.dc)
		return &parkedDecision{p: pd.p, in: in, err: err}
	}
	in, err := seats[pd.p].Decide(ctx, pd.v, pd.dc)
	return &parkedDecision{p: pd.p, in: in, err: err}
}

// play drives m to completion, abort or crash on the table's goroutine and
// returns the final match state. A panic anywhere in a decision or Submit
// is a crash (spec D15), never a dead goroutine.
//
// ctx is the table's own context (run derives it once from t.stop, over the
// table's whole lifetime, not per match): it is the only cancellation path
// into a Seat.Decide call once the loop is blocked inside one, since t.stop
// itself is polled only between decisions (Ruling FL-17). A bot ignores ctx
// and never blocks; a disconnected human seat is expected to select on it.
func (r *Registry) play(ctx context.Context, t *table, m *match) (final string) {
	defer func() {
		if p := recover(); p != nil {
			final = r.crash(t, m, fmt.Errorf("panic: %v\n%s", p, debug.Stack()))
		}
	}()
	// Task M2c-1: if an embedder observes bursts, deliver the genesis burst
	// first so the sink sees the whole chain from its first event — genesis
	// goes through the same observeBurst as every Submit burst, on the match
	// goroutine under m.mu (genesis has no intent, so in is nil). A genesis
	// observation error crashes the match exactly as a per-burst one would
	// (D15).
	if r.opts.OnBurst != nil {
		if err := m.locked(func() error { return r.observeBurst(t, m, 0) }); err != nil {
			return r.crash(t, m, err)
		}
	}
	// AutoMana is the table-level feature gate. A disabled table must keep the
	// pre-payment-plan behaviour for every participant, including bots and a
	// human's timeout caretaker; -bot-auto-mana only takes effect when the
	// feature itself is enabled for the table.
	autoPayMana := t.cfg.autoPayManaEnabled()
	seats := defaultSeatsWithAutoPayMana(t.cfg.BotPolicy, autoPayMana, m.cfg.Names, m.seed)
	if r.opts.Seats != nil {
		seats = r.opts.Seats(m.cfg.Names, m.seed)
	}
	// Task M2c-2: honor the TableConfig.Humans plan — every listed slot is a
	// real person, so replace the bot that Options.Seats built for it (by
	// default defaultSeats, one bot per seat) with a fresh HumanSeat. The
	// remaining slots stay exactly what Options.Seats produced, so a pure-bot
	// table (Humans nil) is byte-identical to today. Each HumanSeat is armed
	// with its deterministic caretaker a few lines below, exactly like the
	// M2b-5 seats an embedder builds itself through the Seats option.
	for _, h := range t.cfg.Humans {
		seats[h] = NewHumanSeat()
	}
	m.mu.Lock()
	m.slots = seats
	m.mu.Unlock()
	// Task M2b-3: arm every human seat with its think budget and its
	// deterministic caretaker bot — the one defaultSeats would have built
	// for that slot (seed ^ slot+1), so a timed-out human decision is
	// answered by exactly the intent a pure-bot game would have logged for
	// that seat, keeping the replay byte-identical (D3). Done here, once, on
	// the match goroutine before the loop, so it never races a Decide.
	for i, s := range seats {
		if hs, ok := s.(*HumanSeat); ok {
			caretaker, err := NewBotPolicySeatWithAutoPayMana(t.cfg.BotPolicy, m.seed^uint64(i+1), autoPayMana)
			if err != nil {
				return r.crash(t, m, err)
			}
			hs.configure(r.opts.ThinkTimeout, caretaker)
		}
	}
	maxIntents := r.opts.MaxIntents
	if maxIntents == 0 {
		maxIntents = defaultMaxIntents
	}
	// Task HW1: the per-turn progress guard. lastTurn is the turn the game
	// was in when the previous decision was answered; decisionsThisTurn is
	// how many decisions have been answered since the last turn advance. A
	// turn that ADVANCES resets the count — a legitimately long turn of
	// hundreds of decisions keeps passing — while a turn whose decisions
	// never advance it (seed 175's 20000-intent equip loop) accumulates
	// until it trips the limit and the match crashes with a stall reason,
	// halting the table through run()'s ordinary MatchCrashed path. The
	// limit is a count of DECISIONS, never a duration: see
	// Options.MaxDecisionsPerTurn for why the determinism the engine is
	// built on forbids a wall clock here. 0 — the zero value — disables the
	// guard entirely.
	lastTurn := m.e.G.Turn
	decisionsThisTurn := 0
	perTurnLimit := r.opts.MaxDecisionsPerTurn
	// parked is the decision currently awaiting its answer (nil before the
	// first live iteration). It is parked — installed, accept-ready — before
	// any fan-out that could publish it, so no decision is ever visible before
	// its seat can accept an answer. The first decision is parked on the first
	// live iteration, after the stop/over checks, so that if parking it blocks
	// (a seat that never answers, ctx-cancelled by Close) and that seat then
	// errors, the error is surfaced as a crash rather than masked by a stop
	// abort that raced the first park.
	var parked *parkedDecision
	var before int
	// Task d2 (buffered reuse): one botpolicy.Board for the whole match,
	// refilled for every decision a BoardSeat answers. projectNext fills it
	// under the match's exclusive lock via BoardFromGameInto (shared maps,
	// cleared-and-reused, no per-decision map allocation); parkSeat consumes
	// it in DecideBoard outside the lock; it is never read after the next
	// decision's refill (botpolicy.BoardFromGameInto's doc comment states the
	// contract, and botpolicy.TestBoardOwnership pins that Decide never
	// retains the Board). Sized to len(m.e.G.Players) like BoardFromGame's own
	// Life map, once per match — three small maps, nothing per decision.
	brd := botpolicy.NewBoard(len(m.e.G.Players))
	for n := 0; ; n++ {
		select {
		case <-t.stop:
			return r.abort(m)
		default:
		}
		// Atomically choose an already-accepted undo before committing a
		// natural end. Undo reserves its request under this same match lock,
		// so exactly one side wins: a request queued first is serviced even
		// when the preceding burst ended the game; a finish committed first
		// makes Undo return a conflict instead of 204-then-nothing. This check
		// is the first operation at every post-burst loop boundary, including
		// the boundary after the pace sleep.
		req, undo, final := r.settleNaturalEndOrUndo(t, m)
		if final != "" {
			return final
		}
		if undo {
			// The rewind itself is host/undo.go's in-place truncate. A parked
			// decision this path abandons must stop being answerable BEFORE the
			// game is rebuilt, or a submit racing the rewind could be validated
			// against a decision the match no longer asks and silently dropped.
			parked.abandon()
			var err error
			parked, err = r.serviceUndo(ctx, t, m, seats, &brd, req)
			if err != nil {
				return r.crash(t, m, err)
			}
			// continue executes the for-loop post statement (n++), so seed n
			// one behind the rewound intent count. At the next body entry n once
			// again equals the number of intents already submitted.
			n, lastTurn, decisionsThisTurn = m.intents-1, m.e.G.Turn, 0
			continue
		}
		if n >= maxIntents {
			return r.crash(t, m, fmt.Errorf("did not terminate after %d intents (turn %d)", n, m.e.G.Turn))
		}
		// Park the decision if this is the first live iteration, or the
		// previous iteration's end-of-loop park produced nothing because the
		// game just ended (the Over check above would already have caught
		// that; a non-over nil here is the old "engine stalled" crash). The
		// projection runs under the exclusive lock so a focus subscriber
		// cannot project the live engine concurrently; parkSeat — which may
		// call a blocking Decide — runs without holding m.mu.
		if parked == nil {
			var data *parkedData
			err := m.locked(func() error {
				data = projectNext(m, seats, &brd)
				return nil
			})
			if err != nil {
				return r.crash(t, m, err)
			}
			if data == nil {
				return r.crash(t, m, fmt.Errorf("engine stalled: game not over and no decision pending"))
			}
			parked = parkSeat(ctx, seats, data, m.undo.signal)
		}
		// Await the answer to the parked decision (parked at the first live
		// iteration or at the end of the previous one). A bot seat resolved
		// synchronously in parkSeat and returns immediately; a human seat
		// parks here on its slot until a SubmitIntent arrives or ctx/timeout
		// fires the caretaker.
		in, err := parked.answer()
		if err != nil {
			// The parked human's await saw the undo signal before any intent:
			// rewind exactly as the top-of-loop check would. The await's own
			// defer has already cleared the seat's slot.
			if req, isUndo := asUndo(err); isUndo {
				var uerr error
				parked, uerr = r.serviceUndo(ctx, t, m, seats, &brd, req)
				if uerr != nil {
					return r.crash(t, m, uerr)
				}
				n, lastTurn, decisionsThisTurn = m.intents-1, m.e.G.Turn, 0
				continue
			}
			return r.crash(t, m, fmt.Errorf("seat %d: %w", parked.p, err))
		}
		var next *parkedDecision
		var nextData *parkedData
		err = m.locked(func() error {
			before = len(m.e.L.Events)
			if err := m.e.Submit(in); err != nil {
				return fmt.Errorf("intent %d rejected: %w", n, err)
			}
			m.afterSubmit(before)
			if err := r.afterBurst(t, m, before); err != nil { // Tasks 11, 12
				return fmt.Errorf("persist: %w", err)
			}
			// Still exclusive: project the engine's NEXT decision (nil when the
			// game just ended) so a focus subscriber, which also projects the live
			// engine through projectLive's exclusive m.mu, can never run its own
			// projection concurrently with this one (view.Project/view.ProjectFor
			// write the Derived cache). The old loop got the same serialization
			// because its projection immediately preceded this Submit; this keeps
			// it now that the park happens after the Submit.
			nextData = projectNext(m, seats, &brd)
			return nil
		})
		if err != nil {
			return r.crash(t, m, err)
		}
		// Task HW1: advance the per-turn progress counter now that the
		// Submit's burst is applied. G.Turn only ever increases, so a turn
		// greater than lastTurn means this very decision carried the game
		// into a new turn — reset the count. Otherwise count the decision
		// and, once the whole turn's unanswered decisions reach the limit,
		// crash with a reason naming the stall, the count and the turn (the
		// monospace crash report and the table_halted frame carry it, so an
		// operator sees what happened rather than a generic failure). Read
		// without the lock like the top-of-loop Over check: the loop is the
		// only writer, and readers holding RLock see a stable boundary
		// either side of this section.
		if perTurnLimit > 0 {
			if m.e.G.Turn > lastTurn {
				lastTurn = m.e.G.Turn
				decisionsThisTurn = 0
			} else {
				decisionsThisTurn++
				if decisionsThisTurn >= perTurnLimit {
					return r.crash(t, m, fmt.Errorf("stalled: %d decisions answered with no turn advance in turn %d (limit %d)", decisionsThisTurn, m.e.G.Turn, perTurnLimit))
				}
			}
		}
		// Install the next decision's answerable slot OUTSIDE the match lock:
		// parkSeat may call a blocking Decide (an embedder's seat), and the
		// match mutex must never be held across one. Publishing happens only
		// after this, so the park-before-publish ordering still holds.
		if nextData != nil {
			next = parkSeat(ctx, seats, nextData, m.undo.signal)
		}
		// Park the engine's NEXT decision BEFORE publishing it: the seat that
		// owns it is now accept-ready, so the fan-out below cannot expose a
		// decision no seat is waiting to answer — the race defect B fixes.
		// When the game just ended, next stays nil and the top-of-loop Over
		// check finishes. This ordering, on the match goroutine, holds for
		// every consumer of the published state.
		parked = next
		r.fanout(t, m, before) // Task 10
		r.opts.Sleep(t.cfg.Pace, t.stop)
	}
}

// settleNaturalEndOrUndo is the linearization point between an accepted undo
// and a natural match finish. Registry.Undo reserves its request while holding
// m.mu too. Therefore a request that wins the lock is consumed here before the
// G.Over transition, while a finish that wins records MatchFinished before
// Undo can return and causes that request to be rejected. An HTTP 204 can never
// be followed by match_end without the corresponding rewind being serviced.
//
// The test-only barrier runs before this lock only when the game is over; it
// lets the race regression deterministically pause finish while Undo queues.
// The match goroutine is the sole engine writer, so the preliminary G.Over read
// is stable until this function either services an undo or records the finish.
func (r *Registry) settleNaturalEndOrUndo(t *table, m *match) (state.PlayerID, bool, string) {
	if m.e.G.Over && r.opts.beforeFinish != nil {
		r.opts.beforeFinish()
	}

	m.mu.Lock()
	select {
	case req := <-m.undo.signal:
		m.mu.Unlock()
		return req, true, ""
	default:
	}
	if !m.e.G.Over {
		m.mu.Unlock()
		return 0, false, ""
	}
	m.state = protocol.MatchFinished
	m.head = m.e.L.Head()
	if m.e.G.Draw {
		m.result = "draw"
	} else {
		m.result = "win"
		w := uint8(m.e.G.Winner)
		m.winner = &w
	}
	m.mu.Unlock()

	r.onMatchEnd(t, m)      // Tasks 10, 12
	r.observeMatchEnd(t, m) // Task M2c-1
	return 0, false, protocol.MatchFinished
}

// abort records a match cut short by Close.
func (r *Registry) abort(m *match) string {
	m.mu.Lock()
	m.state = protocol.MatchAborted
	m.head = m.e.L.Head()
	m.mu.Unlock()
	r.onMatchEnd(m.table, m)
	r.observeMatchEnd(m.table, m) // Task M2c-1
	return protocol.MatchAborted
}

// crash is spec D15's first half: the match is marked crashed with its
// reason. Task 13 adds the crash report, the table halt frame and tests.
// Fix round 1 (burst atomicity): the head recorded here is over the
// persisted prefix, not the in-memory log — the in-memory log can be ahead
// of disk because the crashed Submit's events never fully reached the
// files, and a sidecar naming a head/count that was never written would
// make a restart serve a log that cannot replay. m.persisted is the last
// fully-appended boundary.
func (r *Registry) crash(t *table, m *match, err error) string {
	m.mu.Lock()
	m.state = protocol.MatchCrashed
	m.reason = err.Error()
	if m.files != nil {
		m.head = m.e.L.HeadAt(m.persisted)
	} else {
		m.head = m.e.L.Head()
	}
	m.mu.Unlock()
	r.writeCrashReport(t, m, err.Error())
	r.onMatchEnd(t, m)
	r.observeMatchEnd(t, m) // Task M2c-1
	return protocol.MatchCrashed
}
