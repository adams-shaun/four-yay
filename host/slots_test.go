package host

// BP-10 (spec 2026-09-28-hosted-bot-packages §7, §11): the FIFO search-slot
// gate, the abort-on-close of a queued decision, and AddTable's search-table
// admission. The spy entry is a bots.Register'd entry with Search set — the
// gate, the wait and the admission check all key on the TABLE's policy entry,
// exactly as production will when the real search policies arrive (BP-11+).

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// bp10SpyPolicy is the test-only hosted policy the BP-10 tests seat. It is
// registered once per test binary with Search set; its factory delegates to
// whichever harness the running test installed.
const bp10SpyPolicy = "bp10-search-spy"

// bp10Harness is the coordination point every BP-10 spy seat talks to. The
// tests are serial (no t.Parallel): each installs itself as the current
// harness before it starts any table, so no other test observes its signals.
type bp10Harness struct {
	mu          sync.Mutex
	blockN      int  // the first blockN entered decisions wait for a permit
	open        bool // every decision proceeds without waiting
	tagSeq      int  // per-match labels A, B, C... assigned by the Seats override
	holders     int  // spies currently inside DecideEnv
	maxHolders  int  // high-water mark of holders
	violations  int  // times two spies were inside DecideEnv at once
	entrySeq    int  // monotonic DecideEnv entry counter
	entered     []string
	permits     map[string]chan struct{}
	enteredC    chan string
	wantC       chan string
	parallelism int64 // bots.Options.SearchParallelism last seen by the factory
}

func newBP10Harness(blockN int) *bp10Harness {
	return &bp10Harness{
		blockN:   blockN,
		permits:  map[string]chan struct{}{},
		enteredC: make(chan string, 64),
		wantC:    make(chan string, 64),
	}
}

var (
	bp10Once    sync.Once
	bp10Current atomic.Pointer[bp10Harness]
)

// registerBP10Spy registers the spy policy once per binary and points it at h.
// The factory must keep working between tests (a stale harness simply builds
// plain bots), so it reads the current harness atomically.
func registerBP10Spy(t *testing.T, h *bp10Harness) {
	t.Helper()
	bp10Current.Store(h)
	bp10Once.Do(func() {
		bots.Register(bots.Entry{Info: bots.Info{
			Name: bp10SpyPolicy, Label: "BP-10 spy search", Tier: bots.Experimental,
			Description: "test-only search entry for the BP-10 host slot tests",
			Env:         true, Search: true, Formats: []string{"constructed"}, MaxSeats: 8,
		}, New: func(o bots.Options) (seat.Seat, error) {
			if cur := bp10Current.Load(); cur != nil {
				return cur.spyFactory(o)
			}
			return seat.NewBot(o.Seed), nil
		}})
	})
	if _, ok := bots.Lookup(bp10SpyPolicy); !ok {
		t.Fatal("spy search policy was not registered")
	}
}

func (h *bp10Harness) spyFactory(o bots.Options) (seat.Seat, error) {
	atomic.StoreInt64(&h.parallelism, int64(o.SearchParallelism))
	return seat.NewBot(o.Seed), nil
}

// seatBuilder is an opts.Seats override: it tags each match's spies A, B, C...
// in play order so the test can tell the two tables' decisions apart.
func (h *bp10Harness) seatBuilder(t *testing.T) func(names []string, seed uint64) []seat.Seat {
	t.Helper()
	return func(names []string, seed uint64) []seat.Seat {
		h.mu.Lock()
		tag := string(rune('A' + h.tagSeq))
		h.tagSeq++
		h.mu.Unlock()
		out := make([]seat.Seat, len(names))
		for i := range names {
			out[i] = &bp10SpySeat{Bot: seat.NewBot(seed ^ uint64(i+1)), h: h, tag: tag}
		}
		return out
	}
}

func (h *bp10Harness) setOpen() {
	h.mu.Lock()
	h.open = true
	h.mu.Unlock()
}

// permitBlocked unblocks whichever decision is waiting for its permit and
// returns its label.
func (h *bp10Harness) permitBlocked() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for label, ch := range h.permits {
		close(ch)
		delete(h.permits, label)
		return label
	}
	return ""
}

func (h *bp10Harness) enteredLabels() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.entered...)
}

// bp10SpySeat is a bots.EnvSeat that records its wants and DecideEnv entries
// through the harness, blocks the harness's first blockN decisions until
// permitted (or ctx is done), and otherwise answers exactly like the wrapped
// bot — so a match the spies seat runs identically to a plain-bot match and
// the slotting cannot change its intents.
type bp10SpySeat struct {
	*seat.Bot
	h   *bp10Harness
	tag string
}

func (s *bp10SpySeat) WantsEnv(d *decision.Decision) bool {
	s.h.mu.Lock()
	s.h.signal(s.h.wantC, s.tag)
	s.h.mu.Unlock()
	return true
}

func (s *bp10SpySeat) DecideEnv(ctx context.Context, env bots.Env, d decision.Decision) (decision.Intent, error) {
	h := s.h
	h.mu.Lock()
	idx := h.entrySeq
	h.entrySeq++
	label := s.tag + "#" + itoa(idx)
	h.entered = append(h.entered, label)
	h.holders++
	if h.holders > h.maxHolders {
		h.maxHolders = h.holders
	}
	if h.holders > 1 {
		h.violations++
	}
	var permit chan struct{}
	if idx < h.blockN && !h.open {
		permit = make(chan struct{})
		h.permits[label] = permit
	}
	h.mu.Unlock()
	h.signal(h.enteredC, label) // DecideEnv entry signal, awaited by the tests
	if permit != nil {
		select {
		case <-permit:
		case <-ctx.Done():
			h.exitBP10(label)
			return decision.Intent{}, ctx.Err()
		}
	}
	in, err := s.Bot.DecideBoard(ctx, env.Board, d)
	h.exitBP10(label)
	return in, err
}

func (h *bp10Harness) exitBP10(label string) {
	h.mu.Lock()
	h.holders--
	delete(h.permits, label)
	h.mu.Unlock()
}

// signal records an observable event on ch; the buffer is far larger than
// any test consumes, and a full buffer drops rather than blocks (the signals
// are a convenience observability aid, never a correctness input).
func (h *bp10Harness) signal(ch chan string, s string) {
	select {
	case ch <- s:
	default:
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for ; i > 0; i /= 10 {
		p--
		b[p] = byte('0' + i%10)
	}
	return string(b[p:])
}

// bp10Tag returns the table tag of a harness label: an entry label is
// "<tag>#<n>", a want signal is the bare tag.
func bp10Tag(label string) string {
	if i := strings.IndexByte(label, '#'); i >= 0 {
		return label[:i]
	}
	return label
}

func bp10Idx(label string) int {
	i := 0
	for _, c := range label[strings.IndexByte(label, '#')+1:] {
		i = i*10 + int(c-'0')
	}
	return i
}

func awaitBP10(t *testing.T, ch <-chan string, what string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return ""
	}
}

// bp10WaitQueued waits until exactly want decisions are queued on the
// registry's search-slot gate. It replaces a fixed sleep: a sleeper races
// Close — under load the second table's goroutine can reach acquire only
// after the Close-driven release freed the slot, so it walks straight in
// and the "never queued" assertions fail spuriously. Being in the waiters
// queue is the deterministic state the assertions below actually need.
func bp10WaitQueued(t *testing.T, r *Registry, want int) {
	t.Helper()
	if r.searchGate == nil {
		t.Fatal("the registry has no search-slot gate; the test's bounded setup is wrong")
	}
	deadline := time.After(10 * time.Second)
	for {
		r.searchGate.mu.Lock()
		queued := len(r.searchGate.waiters)
		r.searchGate.mu.Unlock()
		if queued >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("only %d of %d wanted decisions queued on the search-slot gate", queued, want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// bp10Tables is the two-table config every BP-10 slot test plays: 2 seats,
// fixed seeds, fixed repo decks, the spy search policy.
func bp10Tables(policy string) []TableConfig {
	return []TableConfig{
		{ID: "s1", Name: "bp10 s1", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260928,
			Pace: 0, Spectator: view.Public, BotPolicy: policy},
		{ID: "s2", Name: "bp10 s2", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260929,
			Pace: 0, Spectator: view.Public, BotPolicy: policy},
	}
}

func addAndStartBP10(t *testing.T, r *Registry, cfgs []TableConfig) {
	t.Helper()
	for _, c := range cfgs {
		if err := r.AddTable(c); err != nil {
			t.Fatalf("AddTable(%s): %v", c.ID, err)
		}
		if err := r.Start(c.ID); err != nil {
			t.Fatalf("Start(%s): %v", c.ID, err)
		}
	}
}

// bp10Snapshot captures one finished match's outcome and full log.
func bp10Snapshot(t *testing.T, r *Registry, id TableID) (protocol.MatchInfo, *events.Log) {
	t.Helper()
	m, _ := matchFor(t, r, id)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.info(), m.e.L.Clone()
}

// compareBP10Logs asserts two runs of the same table produced the identical
// event chain and identical intents (§7: slotting must never change intents).
func compareBP10Logs(t *testing.T, id TableID, bounded, unbounded *events.Log) {
	t.Helper()
	if bounded.Head() != unbounded.Head() {
		t.Fatalf("%s: bounded head %s, unbounded head %s (slotting changed intents)", id, bounded.Head(), unbounded.Head())
	}
	if bounded.Seed != unbounded.Seed || len(bounded.Events) != len(unbounded.Events) || len(bounded.Intents) != len(unbounded.Intents) {
		t.Fatalf("%s: log shapes differ: seed %d/%d events %d/%d intents %d/%d", id,
			bounded.Seed, unbounded.Seed, len(bounded.Events), len(unbounded.Events), len(bounded.Intents), len(unbounded.Intents))
	}
	for i := range bounded.Events {
		if !reflect.DeepEqual(bounded.Events[i], unbounded.Events[i]) {
			t.Fatalf("%s: event %d differs between the bounded and unbounded runs", id, i)
		}
	}
	for i := range bounded.Intents {
		if !reflect.DeepEqual(bounded.Intents[i], unbounded.Intents[i]) {
			t.Fatalf("%s: intent %d differs between the bounded and unbounded runs", id, i)
		}
	}
}

// TestSearchSlotsQueueFIFOAndNeverChangeIntents is BP-10's central assertion:
// two spy search tables contending for ONE slot (SearchSlots 1) run with
// exactly one search inside DecideEnv at any moment, the second decision
// queues FIFO behind the held slot and is served the moment it is released,
// and the two matches' logs are identical to the same two tables run
// unbounded — the slot changes latency, never intents.
func TestSearchSlotsQueueFIFOAndNeverChangeIntents(t *testing.T) {
	boundedLogs := func() map[TableID]*events.Log {
		h := newBP10Harness(1)
		registerBP10Spy(t, h)
		opts := testOptions(t)
		opts.SearchSlots = 1
		opts.Seats = h.seatBuilder(t)
		r, err := New(opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		addAndStartBP10(t, r, bp10Tables(bp10SpyPolicy))

		// The first search decision enters DecideEnv, holds the only slot and
		// blocks on the harness permit.
		first := awaitBP10(t, h.enteredC, "the first search decision to enter DecideEnv")
		// The other table reaches WantsEnv (the projection immediately
		// precedes the slot wait), so it is deterministically about to queue.
		other := ""
		deadline := time.After(10 * time.Second)
		for other == "" || bp10Tag(other) == bp10Tag(first) {
			select {
			case w := <-h.wantC:
				if bp10Tag(w) != bp10Tag(first) {
					other = w
				}
			case <-deadline:
				t.Fatalf("the second search table never reached WantsEnv (wants seen: %v)", h.enteredLabels())
			}
		}
		// Wait until the second table's goroutine is DETERMINISTICALLY queued
		// on the gate, then prove it is QUEUED, not deciding: no second entry
		// while the slot is held. A broken (or missing) gate enters here within
		// microseconds of the want, so this assertion is what fails.
		bp10WaitQueued(t, r, 1)
		select {
		case l := <-h.enteredC:
			t.Fatalf("a second search entered DecideEnv while the slot was held: %s", l)
		default:
		}
		// Release the holder: the queued decision must be served FIFO, at
		// once, before anything else takes the slot.
		h.permitBlocked()
		second := awaitBP10(t, h.enteredC, "the queued search decision to enter DecideEnv after the release")
		if bp10Tag(second) != bp10Tag(other) {
			t.Fatalf("the queued table %s did not get the slot first; %s entered instead", other, second)
		}
		if bp10Idx(second) != 1 {
			t.Fatalf("the queued decision entered as entry #%d, want #1 (FIFO queue skipped or reordered)", bp10Idx(second))
		}
		h.setOpen()
		r.Wait("s1")
		r.Wait("s2")

		// Precondition: the games really ran, and the slot actually
		// serialized them — exactly one holder at any time.
		for _, id := range []TableID{"s1", "s2"} {
			_, log := bp10Snapshot(t, r, id)
			if len(log.Intents) < 20 {
				t.Fatalf("%s: match logged only %d intents; too short to compare", id, len(log.Intents))
			}
		}
		if h.violations != 0 {
			t.Fatalf("two searches ran inside DecideEnv at once %d times; the slot did not serialize", h.violations)
		}
		if h.maxHolders != 1 {
			t.Fatalf("slot holder high-water mark %d, want 1", h.maxHolders)
		}
		out := map[TableID]*events.Log{}
		for _, id := range []TableID{"s1", "s2"} {
			_, out[id] = bp10Snapshot(t, r, id)
		}
		return out
	}()

	// The control: the same two tables, same seeds, same spies, unbounded.
	unbounded := func() map[TableID]*events.Log {
		h := newBP10Harness(0)
		registerBP10Spy(t, h)
		h.setOpen()
		opts := testOptions(t)
		opts.Seats = h.seatBuilder(t)
		r, err := New(opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		addAndStartBP10(t, r, bp10Tables(bp10SpyPolicy))
		r.Wait("s1")
		r.Wait("s2")
		out := map[TableID]*events.Log{}
		for _, id := range []TableID{"s1", "s2"} {
			info, log := bp10Snapshot(t, r, id)
			if info.State != protocol.MatchFinished {
				t.Fatalf("%s: unbounded match state %s, want finished", id, info.State)
			}
			out[id] = log
		}
		return out
	}()

	for _, id := range []TableID{"s1", "s2"} {
		compareBP10Logs(t, id, boundedLogs[id], unbounded[id])
	}
}

// TestSearchSlotWaitAbortsOnClose is BP-10's second assertion: a decision
// queued on a held slot unblocks with ctx.Err() when its table closes, and
// the play loop records a clean ABORT (never a crash, never a halt).
func TestSearchSlotWaitAbortsOnClose(t *testing.T) {
	h := newBP10Harness(1)
	registerBP10Spy(t, h)
	opts := testOptions(t)
	opts.SearchSlots = 1
	opts.Seats = h.seatBuilder(t)
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	addAndStartBP10(t, r, bp10Tables(bp10SpyPolicy))

	// Table A's first search holds the only slot, blocked in DecideEnv.
	first := awaitBP10(t, h.enteredC, "table A's search decision to enter DecideEnv")
	// Table B reaches its projection, is queued behind A's slot...
	other := ""
	deadline := time.After(10 * time.Second)
	for other == "" || bp10Tag(other) == bp10Tag(first) {
		select {
		case w := <-h.wantC:
			if bp10Tag(w) != bp10Tag(first) {
				other = w
			}
		case <-deadline:
			t.Fatalf("table B never reached WantsEnv (entries: %v)", h.enteredLabels())
		}
	}
	bp10WaitQueued(t, r, 1) // B is deterministically IN the gate's queue
	select {
	case l := <-h.enteredC:
		t.Fatalf("table B entered DecideEnv while the slot was held: %s", l)
	default:
	}

	// Close: B's queued wait must abort, A's blocked DecideEnv must see the
	// same cancellation, and both goroutines must unwind promptly.
	r.Close()
	done := make(chan struct{})
	go func() { r.Wait("s1"); r.Wait("s2"); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("tables did not finish after Close; a search-slot wait did not abort on cancellation")
	}

	// Both matches are cleanly aborted, not crashed, and both tables went
	// idle (run()'s ordinary abort path), never halted.
	for _, id := range []TableID{"s1", "s2"} {
		for _, ti := range r.Tables() {
			if ti.ID == string(id) && ti.State != protocol.TableIdle {
				t.Fatalf("table %s state %s after Close, want idle (not halted)", id, ti.State)
			}
		}
		m, _ := matchFor(t, r, id)
		m.mu.RLock()
		state, reason, intents := m.state, m.reason, m.intents
		m.mu.RUnlock()
		if state != protocol.MatchAborted {
			t.Fatalf("table %s match state %q (reason %q), want aborted", id, state, reason)
		}
		if reason != "" {
			t.Fatalf("table %s aborted with a crash reason %q", id, reason)
		}
		// Table A's match may have started before the abort; table B's must
		// not have answered anything while queued. Neither may show a crash.
		_ = intents
	}

	// B never entered DecideEnv at all: it was queued the whole time, and the
	// abort came from the WAIT, not from a deciding seat.
	for _, l := range h.enteredLabels() {
		if bp10Tag(l) == bp10Tag(other) {
			t.Fatalf("table B entered DecideEnv (%s) despite the held slot; the wait never happened", l)
		}
	}
}

// TestMaxSearchTablesRefusesAtCreate is BP-10's admission assertion: a live
// on-demand search table consumes the configured budget, a refusal names the
// non-search alternative, non-search and durable tables are out of scope, and
// a FINISHED table frees its capacity again.
func TestMaxSearchTablesRefusesAtCreate(t *testing.T) {
	h := newBP10Harness(1)
	registerBP10Spy(t, h)
	opts := testOptions(t)
	opts.MaxSearchTables = 1
	opts.Seats = h.seatBuilder(t)
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	g1 := TableConfig{ID: "g1", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260928,
		Pace: 0, Spectator: view.Public, OnDemand: true, BotPolicy: bp10SpyPolicy}
	if err := r.AddTable(g1); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("g1"); err != nil {
		t.Fatal(err)
	}
	// Precondition: the search table is actually live, holding its match.
	first := awaitBP10(t, h.enteredC, "g1's search decision to enter DecideEnv")
	_ = first
	bp10WaitTableState(t, r, "g1", protocol.TableLive)

	// At the limit: refused, with the refusal that names the alternative.
	err = r.AddTable(TableConfig{ID: "g2", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260929,
		Pace: 0, Spectator: view.Public, OnDemand: true, BotPolicy: bp10SpyPolicy})
	if err == nil || !strings.Contains(err.Error(), "search bots are at capacity; choose a non-search policy") {
		t.Fatalf("second on-demand search table error = %v, want the capacity refusal", err)
	}
	for _, ti := range r.Tables() {
		if ti.ID == "g2" {
			t.Fatal("the refused table g2 was retained")
		}
	}

	// Out of scope on both axes: a non-search on-demand table and a durable
	// search table are both admitted while g1 is live.
	if err := r.AddTable(TableConfig{ID: "g3", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260930,
		Pace: 0, Spectator: view.Public, OnDemand: true}); err != nil {
		t.Fatalf("non-search on-demand table refused by the search limit: %v", err)
	}
	if err := r.AddTable(TableConfig{ID: "g4", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260931,
		Pace: 0, Spectator: view.Public, BotPolicy: bp10SpyPolicy}); err != nil {
		t.Fatalf("durable search table refused by the on-demand limit: %v", err)
	}

	// A finished table frees its capacity: permit g1's match, let it reach
	// game over, and the next on-demand search table is admitted again.
	h.permitBlocked()
	h.setOpen()
	r.Wait("g1")
	bp10WaitTableState(t, r, "g1", protocol.TableIdle)
	if err := r.AddTable(TableConfig{ID: "g5", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260932,
		Pace: 0, Spectator: view.Public, OnDemand: true, BotPolicy: bp10SpyPolicy}); err != nil {
		t.Fatalf("on-demand search table refused after g1 finished: %v", err)
	}
}

func bp10WaitTableState(t *testing.T, r *Registry, id TableID, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, ti := range r.Tables() {
			if ti.ID == string(id) {
				if ti.State == want {
					return
				}
				if ti.State == protocol.TableHalted {
					t.Fatalf("table %s halted while waiting for %s", id, want)
				}
			}
		}
		select {
		case <-deadline:
			t.Fatalf("table %s never reached state %s", id, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestSearchSlotPassesBotSearchParallelismToBotsOptions pins the third
// Options wiring: the registry passes BotSearchParallelism into
// bots.Options.SearchParallelism for every seat it builds from a table's
// named policy.
func TestSearchSlotPassesBotSearchParallelismToBotsOptions(t *testing.T) {
	h := newBP10Harness(0)
	registerBP10Spy(t, h)
	opts := testOptions(t)
	opts.BotSearchParallelism = 5
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cfg := TableConfig{ID: "p1", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260933,
		Pace: 0, Spectator: view.Public, BotPolicy: bp10SpyPolicy}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("p1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("p1")
	if got := atomic.LoadInt64(&h.parallelism); got != 5 {
		t.Fatalf("bots.Options.SearchParallelism seen by the policy factory = %d, want 5", got)
	}
	// Precondition: the match really ran through the policy-built seats.
	if _, log := bp10Snapshot(t, r, "p1"); len(log.Intents) < 20 {
		t.Fatalf("match logged only %d intents; the pass-through was not exercised", len(log.Intents))
	}
}

// TestSearchSlotsNegativeOptionsRejected pins the option validation: a
// negative bound is a configuration error, not a silent unlimited registry.
func TestSearchSlotsNegativeOptionsRejected(t *testing.T) {
	for name, set := range map[string]func(*Options){
		"SearchSlots":          func(o *Options) { o.SearchSlots = -1 },
		"MaxSearchTables":      func(o *Options) { o.MaxSearchTables = -1 },
		"BotSearchParallelism": func(o *Options) { o.BotSearchParallelism = -1 },
	} {
		opts := Options{LoadDeck: sampleLoader(t), Sleep: func(time.Duration, <-chan struct{}) {}}
		set(&opts)
		if _, err := New(opts); err == nil || !strings.Contains(err.Error(), "host: "+name+" -1") {
			t.Fatalf("New with negative %s error = %v, want a named configuration error", name, err)
		}
	}
}
