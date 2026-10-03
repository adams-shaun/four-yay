package events

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Log is an append-only event log plus the intents that produced it, with a
// rolling hash chain over the events. Two logs with the same Head describe the
// same match; a replay that diverges says so in one comparison.
type Log struct {
	Seed    uint64            `json:"seed"`
	Events  []Event           `json:"events"`
	Intents []decision.Intent `json:"intents"`

	// NoHash disables chaining. Benchmarks use it to price the audit trail;
	// production never sets it.
	// Must be set before the first Append and never changed after.
	NoHash bool `json:"-"`

	chain    [sha256.Size]byte
	buf      []byte
	headHash hash.Hash
	started  bool
	// unhashed counts the trailing Events that Append stored but has not yet
	// folded into chain: the fold is deferred to the first chain reader
	// (Head, Clone's copy of the chain state via catchUp), so a search clone
	// that plays a simulation and is discarded without its head ever being
	// read never pays a SHA-256 per event. Stored events are append-only
	// history (Append copies IDs/Pairs; nothing writes a logged event in
	// place), so folding Events[len-unhashed:] later reads exactly the bytes
	// an eager fold would have read at Append time, in the same order, and
	// chain ends identical. A log built any other way (a JSON decode, a
	// struct literal) has unhashed == 0 and its chain is untouched, exactly
	// as before.
	unhashed int
	// noHashSet records NoHash's value on the first Append to pin it from
	// changing (see the check in Append).
	noHashSet bool
	// forked marks a log made by Clone. Its Events arrive with cap == len
	// (the shared prefix), so its first append must copy the whole prefix;
	// growEvents then grows it by a modest slack instead of doubling the
	// copied prefix (see forkGrowth). Capacity only: no reader of Events can
	// observe it.
	forked bool
	// prefixFrom/prefixLen are Events' provenance (Clone, CloneInto): the
	// first prefixLen events are the prefixLen events of the array starting
	// at prefixFrom -- the parent's history, shared or copied. Stored events
	// are append-only history and growEvents copies them on regrowth, so the
	// claim holds for the log's whole life; a recycled array that still
	// carries it skips re-copying that history from the same parent
	// (CloneIntoFrom). The pointer keeps the parent's array alive, so its
	// address cannot be reused by another array while the claim is held.
	prefixFrom *Event
	prefixLen  int

	// verifyEnd (> len(Events)) is an open verify window: rules' resolution
	// kernel (rules/resolve) re-executing a recorded resolution prefix in
	// place, after RewindTo. An append at an index below verifyEnd stores
	// nothing -- the backing array already holds the recorded event there --
	// it checks the new event against it byte for byte and extends Events by
	// one. No recorded history is ever rewritten, so a clone sharing the
	// prefix sees nothing change, and a divergence panics (LogDivergence)
	// before anything is written. Zero (closed) at every intent boundary.
	verifyEnd int
	// vbufA/vbufB are the verify window's encoding scratch.
	vbufA, vbufB []byte
}

// LogDivergence is the panic a verify-window append raises when the
// re-executed event differs from the recorded one.
type LogDivergence struct {
	Index              int
	Recorded, Replayed Event
}

func (d LogDivergence) Error() string {
	return fmt.Sprintf("events: re-executed event %d differs from the recorded one: recorded %v obj=%d text=%q, re-executed %v obj=%d text=%q",
		d.Index, d.Recorded.Kind, d.Recorded.Obj, d.Recorded.Text, d.Replayed.Kind, d.Replayed.Obj, d.Replayed.Text)
}

// RewindTo rewinds l to s0, a clone of l taken earlier (rules' resolution
// kernel restoring its checkpoint): l keeps its own arrays, whose first
// len(s0.Events) events are s0's history, takes s0's chain state, and opens a
// verify window over its first end events (the recorded history a
// re-execution must reproduce; end is l's length when the rewind began, so a
// second rewind within one re-execution keeps the same window). Intents are
// the caller's. The backing array must hold the recorded events up to end,
// which a log that has appended since s0 was cloned always does.
func (l *Log) RewindTo(s0 *Log, end int) {
	l.Seed, l.NoHash, l.chain = s0.Seed, s0.NoHash, s0.chain
	l.started, l.noHashSet, l.unhashed = s0.started, s0.noHashSet, s0.unhashed
	l.verifyEnd = end
	l.Events = l.Events[:len(s0.Events)]
}

// VerifyClose closes the verify window and reports whether every recorded
// event in it was reproduced (Events reached the window's end).
func (l *Log) VerifyClose() bool {
	ok := len(l.Events) >= l.verifyEnd
	l.verifyEnd = 0
	return ok
}

// VerifyEnd reports the open verify window's end (0 when closed).
func (l *Log) VerifyEnd() int { return l.verifyEnd }

const expectedEventsPerGame = 4096

func NewLog(seed uint64) *Log { return NewLogInto(seed, nil) }

// NewLogInto is NewLog backed by a spent event array a batch runner recycled
// from a finished game (rules.Engine.Release). The array is reused only when
// it can hold the ordinary preallocation, and it is re-capped to exactly that
// preallocation, so the log's growth points -- and so every capacity-visible
// behaviour -- are identical to a fresh NewLog's; only the allocation is
// saved. spare's contents are ignored (each slot is overwritten on Append)
// and its caller must hold no other reference into it.
func NewLogInto(seed uint64, spare []Event) *Log {
	events := spare[:0]
	if cap(events) >= expectedEventsPerGame {
		events = events[:0:expectedEventsPerGame]
	} else {
		events = make([]Event, 0, expectedEventsPerGame)
	}
	l := &Log{Seed: seed, Events: events}
	// Seed the chain with the seed value
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], seed)
	l.chain = sha256.Sum256(b[:])
	return l
}

// Append assigns the next sequence number, folds the event into the chain and
// stores it. It returns the stored event so callers see the assigned Seq.
func (l *Log) Append(e Event) Event {
	l.AppendPtr(&e)
	return e
}

// AppendPtr is Append in place: it assigns e.Seq, detaches e.IDs/e.Pairs and
// stores a copy, so on return *e equals the stored event. e must not point
// into l.Events (the store may reallocate it).
func (l *Log) AppendPtr(e *Event) {
	// Check NoHash immutability: must not change after the log is started
	if l.started {
		if l.noHashSet != l.NoHash {
			panic("events: NoHash changed after the log was started")
		}
	} else {
		l.started = true
		l.noHashSet = l.NoHash
	}

	e.Seq = uint64(len(l.Events))

	if n := len(l.Events); n < l.verifyEnd {
		// An open verify window (RewindTo): the recorded event is already
		// stored at n. Check, never write.
		rec := l.Events[: n+1 : n+1][n]
		l.vbufA = rec.Append(l.vbufA[:0])
		l.vbufB = e.Append(l.vbufB[:0])
		if string(l.vbufA) != string(l.vbufB) {
			panic(LogDivergence{Index: n, Recorded: rec, Replayed: *e})
		}
		*e = rec
		l.Events = l.Events[:n+1]
		if !l.NoHash {
			l.unhashed++
		}
		return
	}

	// Copy IDs and Pairs so a caller mutating its own slice afterwards cannot
	// retroactively rewrite a logged event and desync Head from HeadAt.
	// This also normalises a non-nil empty slice to nil. That is deliberate:
	// the encoding writes a length prefix only, so nil and empty are already
	// indistinguishable on the wire, and collapsing them keeps the in-memory
	// log canonical with what the chain actually hashed.
	e.IDs = append([]state.ObjID(nil), e.IDs...)
	e.Pairs = append([][2]state.ObjID(nil), e.Pairs...)

	l.Events = growEvents(l.Events, len(l.Events)+1, l.forked)
	l.Events[len(l.Events)-1] = *e
	if l.NoHash {
		return
	}
	l.unhashed++
}

// catchUp folds every stored-but-unfolded event (see unhashed) into chain,
// oldest first: sha256(chain || encode(e)) per event, the fold Append used to
// run eagerly. A log whose Events were cut below its unfolded tail by a
// direct field write (no engine path does that to a live log) folds what is
// left of the tail.
func (l *Log) catchUp() {
	if l.unhashed == 0 {
		return
	}
	n := len(l.Events)
	from := n - l.unhashed
	if from < 0 {
		from = 0
	}
	l.unhashed = 0
	if l.headHash == nil {
		l.headHash = sha256.New()
	}
	for i := from; i < n; i++ {
		l.buf = l.Events[i].Append(l.buf[:0])
		// Reuse one digest instead of sha256.New() per event — a fresh hasher
		// per append was a measurable per-event allocation on a long log.
		// Reset restores the initial (empty) state, so the fold into the
		// chain is byte-identical to a fresh hasher's.
		l.headHash.Reset()
		l.headHash.Write(l.chain[:])
		l.headHash.Write(l.buf)
		l.headHash.Sum(l.chain[:0])
	}
}

// Head is the chain head over every event so far.
func (l *Log) Head() string {
	if l.NoHash {
		return ""
	}
	l.catchUp()
	return hex.EncodeToString(l.chain[:8])
}

// HeadAt recomputes the chain over the first n events, which is what makes
// "playback to N" verifiable against a full log.
func (l *Log) HeadAt(n int) string {
	if l.NoHash {
		return ""
	}
	// Start with the seeded chain state
	var cur [sha256.Size]byte
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], l.Seed)
	h := sha256.New()
	h.Write(b[:])
	h.Sum(cur[:0])

	var buf []byte
	for i := 0; i < n && i < len(l.Events); i++ {
		buf = l.Events[i].Append(buf[:0])
		h := sha256.New()
		h.Write(cur[:])
		h.Write(buf)
		h.Sum(cur[:0])
	}
	return hex.EncodeToString(cur[:8])
}

// Clone returns a log that continues the same chain from exactly where l's
// was: the same Seed, NoHash and chain state. Events and Intents are shared
// by backing array, not copied — a stored Event or Intent is append-only,
// hash-chained history: once Append has folded it into the chain, mutating
// it (or its IDs/Pairs/Choices) in place would desync Head from HeadAt, so
// the engine never does. Because nothing writes to the shared region, both
// logs can read it freely. The full-slice expressions below are the point:
// each fixes cap == len, so the first append on EITHER side allocates a
// fresh array and the two logs diverge cleanly, never writing into the
// other's storage (pinned by TestLogCloneAppendsDiverge). buf is a scratch
// buffer reused only by Append, so a clone starts from nil and gets its own
// backing on first use — sharing it could race two append paths. Sharing
// here is what keeps memory-profile hot: ViewAt clones per time-travel query
// and Engine.Clone per turn-start snapshot, and a deep copy of an ever-
// growing log was ~17 GB of allocated space in the host test.
func (l *Log) Clone() *Log {
	c := *l
	c.Events = l.Events[:len(l.Events):len(l.Events)]
	c.Intents = l.Intents[:len(l.Intents):len(l.Intents)]
	c.buf = nil
	c.forked = true
	// headHash is mutable scratch catchUp reuses; two logs must not share it,
	// exactly as they must not share buf. A clone allocates its own on its
	// first fold. The unfolded tail (unhashed) travels with the struct copy:
	// it names the same shared, immutable events in both logs, so each folds
	// it to the same chain whenever it is first read.
	c.headHash = nil
	c.verifyEnd, c.vbufA, c.vbufB = 0, nil, nil
	c.prefixFrom, c.prefixLen = nil, len(l.Events)
	if len(l.Events) > 0 {
		c.prefixFrom = &l.Events[0]
	}
	return &c
}

// Provenance reports what l's history was cloned from (Clone, CloneInto):
// its first n events are the n events of the array starting at from. A log
// not made by a clone reports nil.
func (l *Log) Provenance() (from *Event, n int) { return l.prefixFrom, l.prefixLen }

// CloneInto is Clone with the copy's Events and Intents copied into the
// caller's recycled arrays (a spent clone's, handed back through the rules
// engine's Release) instead of shared with l, so a search that clones one
// root per simulation stops allocating a fresh log per simulation. An array
// too small to hold l's history plus forkMinSlack appends is ignored and the
// copy shares l's prefix exactly as Clone does. The recycled arrays' contents
// are never read (every slot up to len is overwritten here, and every slot
// past it is overwritten by Append before it is read); the caller must hold
// no other reference into them. Everything a reader can observe -- Events,
// Intents, Seed, the chain -- is identical to Clone's.
func (l *Log) CloneInto(events []Event, intents []decision.Intent) *Log {
	return l.CloneIntoFrom(events, nil, 0, 0, intents)
}

// CloneIntoFrom is CloneInto for a recycled array that may still hold a
// spent clone's history: events[:dirty] may hold stale events (every slot
// past dirty is zero), and events[:n] are the n events of the array starting
// at from (the spent clone's Provenance). When from is l's own array and
// n <= len(l.Events), those n events are still exactly l's -- stored events
// are append-only history, never rewritten in place -- so only l.Events[n:]
// are copied. Every stale slot past the copied history is zeroed, so the
// copy's array is zero past its length, as a fresh one is. Everything a
// reader can observe is identical to Clone's.
func (l *Log) CloneIntoFrom(events []Event, from *Event, n, dirty int, intents []decision.Intent) *Log {
	c := l.Clone()
	if L := len(l.Events); cap(events) >= L+forkMinSlack {
		k := 0
		if L > 0 && from == &l.Events[0] && n <= L && n <= dirty {
			k = n
		}
		c.Events = append(events[:k], l.Events[k:]...)
		if dirty > L {
			clear(events[L:dirty])
		}
	}
	if len(l.Intents) > 0 && cap(intents) > len(l.Intents) {
		c.Intents = append(intents[:0], l.Intents...)
	}
	return c
}

// Forked reports whether l was made by Clone or CloneInto. A forked log's
// Events and Intents may still be its parent's backing arrays: they are the
// fork's own only once an append has regrown them, and a shared prefix always
// has cap == len (Clone's full-slice expressions), which is how the rules
// engine's Release tells the two apart without ever recycling a parent's
// history.
func (l *Log) Forked() bool { return l.forked }

// Reserve grows the Events backing array's CAPACITY to at least n events,
// leaving length and every stored event untouched. It is an expected-size
// hint: a caller who knows a real match runs to roughly n events (host caps
// intents and can name a bound just above a real match's length) preallocates
// once so the log's common growth path never reallocates -- growEvents then
// charges a single make at Reserve time instead of a whole doubling series as
// the log climbs. Appends beyond n fall back to growEvents's doubling exactly
// as before, so a bad (too-small) hint only costs one extra growth, never a
// wrong result. It is a pure capacity hint and touches no chain state, so it
// is safe to call at any point (including after appends, e.g. host right after
// rules.New has written genesis): it reallocates the backing array once and
// copies existing events, and because stored Events are append-only history no
// one holds a reference to the old backing array to be invalidated. Clone
// safety is unchanged: Clone still fixes c.Events = l.Events[:len:len], so a
// clone never inherits reserved spare capacity and the first append on either
// side reallocates and the two logs diverge (pinned by
// TestLogReserveDoesNotLeakSpareCapacityToClone).
func (l *Log) Reserve(n int) {
	if n <= cap(l.Events) {
		return
	}
	old := l.Events
	l.Events = make([]Event, len(old), n)
	copy(l.Events, old)
}

// growEvents grows the backing array geometrically, then returns s with len ==
// need. It replaces the built-in append's growth in one way: it doubles
// (factor 2) while the target stays small, and tapers to 1.25x past
// growTaperAt. Why: a fresh log climbs from cap 16 to its final length and
// doubling is the lowest-total-allocation policy there (a geometric series to
// a final capacity C sums to ~2C for factor 2, ~4C for factor 1.25), so small
// logs double. But most of growEvents' allocation is a log that a Clone made
// cap == len at a fully-grown length L and then appended to — a time-travel
// view replays into a clone of the live log; the first append must copy L
// elements and the doubling rule would allocate 2L (a 73823-event log jumps to
// 147456) for a replay that lands a few events past head. Tapering past
// growTaperAt makes that first grow cost ~1.25L instead, which is the measured
// win in ./host (growEvents was the top allocator; the large-start grows that
// doubling saddled with 2x dominate it 9:1). It returns s with len == need,
// whether in place or in a freshly-allocated array, so the ordinary no-realloc
// Append path costs nothing. Growth never overshoots need by more than the
// taper factor at any single step and always bounds the total, and the policy
// change does not touch the hash chain. Clone safety is untouched: Clone has
// already fixed a clone's cap == len, so the first append on either side
// arrives here with no spare capacity, allocates a fresh array, and the two
// logs diverge cleanly (pinned by TestLogCloneAppendsDiverge).
//
// A forked log (one Clone made) grows differently: its first append arrives
// with cap == len == L, the shared prefix, and the ordinary rule would copy L
// events into a 2L array (L < growTaperAt) -- half of it never written, since
// the clone is typically a search world or a time-travel replay that lands a
// few hundred events past L. So a forked log grows by need + forkSlack(need)
// instead: an eighth of the length, at least forkMinSlack events. That is the
// measured AlphaZero-search case -- every simulation clones a mid-game root
// and plays ~40 intents (~200-300 events) -- where the doubled copy was 28% of
// all bytes allocated. Later growths of the same forked log use the same rule,
// which stays geometric (1.125x), so a long replay still costs amortised O(1)
// per append. Capacity only: contents, Seq and the chain are untouched.
const (
	growMinCap   = 16
	growTaperAt  = 4096 // double below this many elements, taper above
	growTaperDiv = 4    // past growTaperAt, grow by (1 + 1/growTaperDiv) = 1.25x
	forkMinSlack = 256  // a forked log's growth adds at least this many slots
	forkSlackDiv = 8    // ... or need/forkSlackDiv, whichever is larger
)

func growEvents(s []Event, need int, forked bool) []Event {
	if need <= cap(s) {
		return s[:need]
	}
	if forked {
		out := make([]Event, need, need+max(forkMinSlack, need/forkSlackDiv))
		copy(out, s)
		return out
	}
	newCap := cap(s)
	if newCap < growMinCap {
		newCap = growMinCap
	}
	for newCap < need {
		if newCap < growTaperAt {
			newCap *= 2
		} else {
			newCap += newCap / growTaperDiv
		}
	}
	out := make([]Event, need, newCap)
	copy(out, s)
	return out
}
