package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Livelock detection: a real bug -- I-1's stack object that re-resolved
// forever, and the rakdos-muscle-scam-exe run that pegged a core for 15+
// minutes with no output -- presents to a harness as a game that never
// returns from Advance/Submit. An outside `timeout` cannot tell "stuck"
// from "slow" and reports nothing about WHERE the loop is, so the engine
// itself watches its own event stream and aborts (panics with a
// *LivelockError) the moment the stream looks non-terminating.
//
// The watcher is pure observation: it reads only the events the engine
// already logged, holds no state events depend on, and never emits an
// event, so a game that does not trip it is byte-identical to an
// un-watched one (the chain-head goldens in rules/heads_test.go pin that).
// A game that DOES trip it is a bug by definition -- the harnesses
// (cmd/mtgsim, cmd/botbench) recover the panic, report the diagnostic
// against that game's seed/deck, and move on; anywhere else the panic is
// the bug surfacing, exactly as the determinism invariant intends.
//
// Two independent triggers, because two loop shapes evade each other:
//
//   - Repeating cycle: the trailing window of event signatures repeats a
//     short period exactly. This catches I-1's shape (the same 13-event
//     resolve cycle over and over) and stays precise enough to report the
//     cycle itself. Signatures deliberately EXCLUDE Seq, Amount and Text:
//     a stuck loop whose payload drifts a little each iteration (a life
//     total ticking down, a counter creeping) must still be caught, while
//     zone/object/step identity still separates genuinely different
//     events.
//
//   - Runaway resolution: this many events with no DecisionAsk, StepChange
//     or TurnChange among them. This is the catch-all for loops whose
//     cycle grows (a new object minted every iteration, a period past
//     MaxPeriod) that the exact-period detector cannot lock onto. Every
//     legitimate game relents and asks a decision or moves to the next
//     step far inside this bound (measured: max quiet run over the
//     acceptance games is three orders of magnitude smaller).

const (
	// defaultCycleEvents is how many consecutive events a repeating cycle
	// must run before the watcher aborts. I-1's loop ran 700+ events in a
	// 13-event cycle before anyone noticed; 400 trips that inside one
	// Submit call, in well under a second, and is far past anything a
	// legitimate resolution emits.
	defaultCycleEvents = 400
	// defaultMaxPeriod is the longest cycle the exact-period detector
	// tracks. A stuck loop longer than this falls to the runaway trigger.
	defaultMaxPeriod = 128
	// defaultRunawayEvents is the no-progress backstop. It exists to be
	// never-false-positive, not tight: the runaway diagnostic still names
	// the span and the last event, which is where a debugger looks.
	defaultRunawayEvents = 50000
)

// progressKinds are the events that prove the game is still moving: a
// decision was asked (control returned to a seat), or the turn/step
// structure advanced. Every other event can repeat inside one resolution.
var progressKinds = newKindSet(events.DecisionAsk, events.StepChange, events.TurnChange)

// kindSet is a set of event kinds as a 256-bit bitset (events.Kind is a
// uint8): the watcher tests two of them per emitted event, so a bit test
// replaces a map lookup on the emit path.
type kindSet [4]uint64

func newKindSet(ks ...events.Kind) kindSet {
	var s kindSet
	for _, k := range ks {
		s[k>>6] |= 1 << (k & 63)
	}
	return s
}

func (s *kindSet) has(k events.Kind) bool { return s[k>>6]&(1<<(k&63)) != 0 }

// LoopGuard overrides the livelock watcher's thresholds for one game. Zero
// fields fall back to the defaults above; a nil *LoopGuard in Config is
// the defaults, so every existing Config is unchanged. A RunawayEvents of
// 0 explicitly means DEFAULT here (the zero-value-falls-back rule), not
// "off" -- the backstop is what catches a loop the period detector cannot
// see, so the zero value never disables it. The watcher CAN be disabled
// outright, but only explicitly: Disabled is the embedder's own opt-out
// for a deliberately supervised non-terminating game (the host's
// MaxDecisionsPerTurn = 0 propagates it -- the host stall-guard opt-out
// and the engine watcher are the same protection at two levels, and opting
// out of one opts out of both). It is never set by any default path.
//
// MaxObjs arms the watcher with a mid-resolution OBJECT-COUNT cap (the
// board-size budget cmd/cardfuzz derives from -max-objects). Zero means
// off -- unlike the three thresholds above there is no default, so a nil
// or zero Config arms nothing and every existing game is unchanged. When
// set, the watcher aborts the moment the game's object arena exceeds the
// cap: a harness cannot run a check between the decisions of one
// resolution (a token-doubling storm mints thousands of objects inside a
// single Submit with no decision to yield on), so the cap is checked on
// the watcher's own per-event path, where it is one O(1) comparison. The
// arena length bounds the live count from above (it also counts ceased
// objects), so the abort may fire a little before the live count itself
// crosses the cap; the caller re-classifies and renders the record.
type LoopGuard struct {
	CycleEvents   int
	MaxPeriod     int
	RunawayEvents int
	MaxObjs       int
	Disabled      bool
}

func (g *LoopGuard) filled() LoopGuard {
	out := LoopGuard{CycleEvents: defaultCycleEvents, MaxPeriod: defaultMaxPeriod, RunawayEvents: defaultRunawayEvents}
	if g != nil {
		out.Disabled = g.Disabled
		out.MaxObjs = g.MaxObjs
		if g.CycleEvents > 0 {
			out.CycleEvents = g.CycleEvents
		}
		if g.MaxPeriod > 0 {
			out.MaxPeriod = g.MaxPeriod
		}
		if g.RunawayEvents > 0 {
			out.RunawayEvents = g.RunawayEvents
		}
	}
	if out.MaxPeriod < 1 {
		out.MaxPeriod = 1
	}
	return out
}

// LivelockError is the panic value the watcher aborts a stuck game with,
// and the diagnostic the harnesses print. It is deliberately a struct (not
// a string) so a harness can record the pieces (object, kind, repeat
// count, cycle length) structurally as well as render the text.
type LivelockError struct {
	// Reason is "repeating cycle" or "runaway resolution" -- which of the
	// two triggers fired.
	Reason string
	// Kind and Object identify the repeating event shape: for a cycle, the
	// first event of one period; for a runaway, the last event emitted.
	Kind   events.Kind
	Object state.ObjID
	// Repeats is how many full cycles the detector counted (0 for a
	// runaway); CycleLen is the period length in events (0 for a runaway).
	Repeats  int
	CycleLen int
	// FirstSeq and LastSeq bound the stuck span in the event log.
	FirstSeq uint64
	LastSeq  uint64
	// QuietEvents is the no-progress run length (0 for a period trip).
	QuietEvents int
	// Cycle renders one period of the repeating events (nil for a runaway),
	// earliest first.
	Cycle []string
	// Count and Cap are the object-arena population and the cap that tripped
	// the "object cap" abort (zeros for the other reasons).
	Count int
	Cap   int
}

func (e *LivelockError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "livelock detected (%s): object %d, kind %s, ", e.Reason, e.Object, e.Kind)
	switch e.Reason {
	case "repeating cycle":
		fmt.Fprintf(&b, "cycle of %d event(s) repeated %d time(s), events %d-%d", e.CycleLen, e.Repeats, e.FirstSeq, e.LastSeq)
	case "object cap":
		fmt.Fprintf(&b, "the object arena holds %d object(s), cap %d", e.Count, e.Cap)
	default:
		fmt.Fprintf(&b, "%d event(s) with no decision, step or turn change, events %d-%d", e.QuietEvents, e.FirstSeq, e.LastSeq)
	}
	if len(e.Cycle) > 0 {
		fmt.Fprintf(&b, "; cycle: [%s]", strings.Join(e.Cycle, "; "))
	}
	return b.String()
}

// livelockWatcher is the per-engine observer state. Plain values and
// capped slices only, so Clone can carry or reset it cheaply; it is
// written by observe during an emission burst and read by nothing else.
type livelockWatcher struct {
	guard LoopGuard
	// sigs is the trailing signature window, newest last, capped at
	// 2*MaxPeriod (the two halves the period detector compares). sigHead is
	// the oldest logical entry once the bounded slice is full.
	sigs    []uint64
	sigHead int
	// recent is the trailing event window, newest last, capped at
	// MaxPeriod, so an abort can render one real period. recentHead is the
	// oldest logical entry once the bounded slice is full.
	recent     []events.Event
	recentHead int
	// runPeriod/runEvents track the active periodic run: runEvents counts
	// the consecutive events (>= 2*runPeriod at detection) matching the
	// runPeriod period.
	runPeriod int
	runEvents int
	// quiet/quietSince track the no-progress run for the runaway backstop.
	quiet      int
	quietSince uint64
	// mints counts the object-minting events observed (mintingKinds). Each
	// one's signature folds in its ordinal, so a batch of identical mints is
	// never read as a stuck period (see mintingKinds).
	mints uint64
	// The candidate index: every signature in the window is chained to the
	// previous one sharing its low bits (livelockCandSlots), so detect walks
	// exactly the earlier positions that can equal the newest signature,
	// newest first, instead of every position of the window. total counts
	// the signatures ever appended (position t is the t-th, from 0, and sits
	// at physical slot t % len(sigs) once the ring wraps, t before);
	// prevPos[slot of t] is 1 + the position of the previous same-slot
	// signature (0: none), and slotHead[low bits] is 1 + the newest such
	// position. A stale link can only name a position older than the
	// window, which the walk's bound stops at before reading its slot.
	// Positions are uint32: no engine logs 2^32 events.
	total    uint32
	prevPos  []uint32
	slotHead []uint32
}

// livelockCandSlots is the size of the watcher's candidate index.
const livelockCandSlots = 1024

// livelockCandVerify (the rules test binary) re-runs the full window scan
// after every indexed detect and panics if the verdicts differ.
var livelockCandVerify = derivedMemoVerifyFlag != ""

// mintingKinds are the events that create a NEW object whose id their own
// payload does not name (TokenCreate carries only the script name, and
// CardToken/StackCopy name the SOURCE being copied). Two such events with
// equal payloads are still different game transitions -- each adds a
// fresh object -- so the exact-period detector must never match them
// against each other: Krenko, Mob Boss's doubling activation legitimately
// logs X identical TokenCreate events in a row, and at X >= CycleEvents
// (495 Goblins by a long game's turn 28) that single resolution read as a
// period-1 "cycle". A genuinely unbounded minting loop still grows the
// object arena every iteration, which is exactly the loop shape the
// runaway backstop exists for, so nothing escapes the watcher.
var mintingKinds = newKindSet(events.TokenCreate, events.CardToken, events.StackCopy)

func newLivelockWatcher(g *LoopGuard) livelockWatcher {
	return newLivelockWatcherInto(g, nil, nil, nil, nil)
}

// newLivelockWatcherInto is newLivelockWatcher over recycled window arrays
// (Config.Spare); see newLivelockWatcherFromGuard.
func newLivelockWatcherInto(g *LoopGuard, sigs []uint64, recent []events.Event, prev, heads []uint32) livelockWatcher {
	return newLivelockWatcherFromGuard(g.filled(), sigs, recent, prev, heads)
}

// newLivelockWatcherFromGuard is Clone's constructor: a fresh watcher over
// thresholds the source engine was already running (Clone's doc above).
// sigs and recent are recycled window arrays (Spare) or nil; the watcher
// reads only their length, which starts at zero, so a recycled array's
// capacity and old contents are invisible -- it only saves the regrowth
// from nil every clone otherwise pays as its windows fill.
func newLivelockWatcherFromGuard(g LoopGuard, sigs []uint64, recent []events.Event, prev, heads []uint32) livelockWatcher {
	// prev is read only below its length (which starts at zero) and heads
	// is zeroed here, so a recycled pair is invisible like sigs/recent.
	if cap(heads) >= livelockCandSlots {
		heads = heads[:livelockCandSlots]
		clear(heads)
	} else {
		heads = nil
	}
	return livelockWatcher{guard: g, sigs: sigs[:0], recent: recent[:0], prevPos: prev[:0], slotHead: heads}
}

// observe feeds one just-logged event to the watcher. It panics with a
// *LivelockError when a trigger fires; every other return leaves the
// game byte-identical to an un-watched one. A Disabled guard observes
// nothing at all -- an explicitly opted-out game is supervised by whoever
// set the flag, exactly as the host stall-guard opt-out intends.
func (w *livelockWatcher) observe(ev events.Event) { w.observeFrom(ev, 0, 0) }

// observeFrom is observe with the engine's current damage source (the
// e.damaging scratch the emit ran under) and the engine's current object
// arena population. A Damage event's payload names only its RECIPIENT -- a
// player hit carries no object at all -- so the combat damage step of a wide
// board (1790 Goblin tokens from Krenko, Mob Boss, each dealing 1 to the
// same player: cardfuzz batch8 lines 2-3) logs hundreds of byte-identical
// Damage events that are each a DIFFERENT creature's damage. Folding the
// source into a Damage event's signature keeps those distinct, while a real
// loop -- one source damaging the same recipient again and again -- still
// repeats its signature exactly.
func (w *livelockWatcher) observeFrom(ev events.Event, damageSource state.ObjID, nObjs int) {
	if w.guard.Disabled {
		return
	}
	// The armed object cap (LoopGuard.MaxObjs) is a harness-side board-size
	// budget checked on the engine's own per-event path: between two
	// decisions a single resolution can mint thousands of objects (one
	// Krenko, Mob Boss activation), and no decision-boundary check the
	// harness runs can see inside it. The arena length is one O(1) read at
	// the emit site and bounds the live count from above, so the cap fires
	// within one event of the crossing -- well before any wall-clock budget
	// the caller runs under -- instead of at the next decision, which may be
	// hundreds of thousands of expensive events later.
	if w.guard.MaxObjs > 0 && nObjs > w.guard.MaxObjs {
		panic(&LivelockError{
			Reason: "object cap",
			Kind:   ev.Kind,
			Object: ev.Obj,
			Count:  nObjs,
			Cap:    w.guard.MaxObjs,
		})
	}
	// A CR 800.4a departure sweep is a finite arena walk: each matching
	// MoveZone consumes one owned object, so its events are progress even
	// though they do not yield to a decision or advance the turn structure.
	// Keep them visible to the period detector, but do not charge them to
	// the runaway-resolution budget.
	eliminationSweep := ev.Kind == events.MoveZone && ev.To == state.ZCeased && ev.Text == "player left the game"
	// Runaway backstop: count the events since the last progress event.
	if progressKinds.has(ev.Kind) || eliminationSweep {
		w.quiet = 0
	} else {
		if w.quiet == 0 {
			w.quietSince = ev.Seq
		}
		w.quiet++
	}
	if w.quiet > w.guard.RunawayEvents {
		panic(&LivelockError{
			Reason:      "runaway resolution",
			Kind:        ev.Kind,
			Object:      ev.Obj,
			FirstSeq:    w.quietSince,
			LastSeq:     ev.Seq,
			QuietEvents: w.quiet,
		})
	}

	// A ClockTick is pure bookkeeping -- AddContinuous stamps one per
	// registered effect (Ruling T19-a) -- and carries no object, so a single
	// resolution that legitimately registers one effect per affected
	// permanent (a PumpAll over a 400-creature board: Moogles' Valor at the
	// end of a token-doubling game, cardfuzz batch5 line 4) is a run of
	// identical signatures that is not a loop. It is invisible to the
	// exact-period detector (a real loop that also ticks the clock still
	// repeats its other events, which the detector sees with the ticks
	// elided), and it still counts toward the runaway backstop above, so a
	// loop that does nothing BUT register effects is still caught.
	if ev.Kind == events.ClockTick {
		return
	}
	sig := eventSignature(&ev)
	if ev.Kind == events.Damage && damageSource != 0 {
		sig = fnvU32(sig, uint32(damageSource))
	}
	if mintingKinds.has(ev.Kind) {
		w.mints++
		sig = fnvU64(sig, w.mints)
	}
	w.pushSig(sig)
	if len(w.recent) < w.guard.MaxPeriod {
		w.recent = append(w.recent, ev)
	} else {
		w.recent[w.recentHead] = ev
		w.recentHead = (w.recentHead + 1) % w.guard.MaxPeriod
	}

	// Exact-period detector. While a run is active, each event that
	// continues the period extends it; any event that breaks the pattern
	// ends the run and the trailing window is re-scanned for a fresh one.
	if w.runPeriod > 0 {
		n := len(w.sigs)
		if n > w.runPeriod && w.sigAt(n-1) == w.sigAt(n-1-w.runPeriod) {
			w.runEvents++
			if w.runEvents >= w.guard.CycleEvents {
				w.abort()
			}
			return
		}
		w.runPeriod, w.runEvents = 0, 0
	}
	w.detect()
}

// pushSig appends one signature to the trailing window (capped at
// 2*MaxPeriod, the oldest overwritten once full), indexing it first (see
// total/prevPos/slotHead): its link is the previous same-slot position.
func (w *livelockWatcher) pushSig(sig uint64) {
	sigCap := 2 * w.guard.MaxPeriod
	if w.slotHead == nil {
		w.slotHead = make([]uint32, livelockCandSlots)
	}
	slot := sig % livelockCandSlots
	link := w.slotHead[slot]
	w.slotHead[slot] = w.total + 1
	w.total++
	if len(w.sigs) < sigCap {
		w.sigs = append(w.sigs, sig)
		w.prevPos = append(w.prevPos, link)
	} else {
		w.sigs[w.sigHead] = sig
		w.prevPos[w.sigHead] = link
		w.sigHead = (w.sigHead + 1) % sigCap
	}
}

func (w *livelockWatcher) sigAt(i int) uint64 {
	i += w.sigHead
	if i >= len(w.sigs) {
		i -= len(w.sigs)
	}
	return w.sigs[i]
}

func (w *livelockWatcher) recentAt(i int) events.Event {
	i += w.recentHead
	if i >= len(w.recent) {
		i -= len(w.recent)
	}
	return w.recent[i]
}

// detect scans for the shortest period p whose trailing 2p signatures are
// two identical halves, and if one is found, opens a run on it.
//
// A period p needs sig(n-1) == sig(n-1-p) before anything else, so only the
// earlier positions holding the newest signature are candidates: the
// same-slot chain (prevPos) yields every position that can, newest first --
// increasing p -- and a candidate pays the full two-halves comparison only
// after its second signature matches (sig(n-2) == sig(n-2-p), inline; p ==
// 1 needs none). Same p order, same verdict as testing every p in turn
// (detectScan, which verify mode re-runs on every call).
func (w *livelockWatcher) detect() {
	got := w.detectIndexed()
	if livelockCandVerify {
		if want := w.detectScan(); want != got {
			panic(fmt.Sprintf("rules: livelock candidate index found period %d, the window scan %d", got, want))
		}
	}
	if got > 0 {
		w.runPeriod, w.runEvents = got, 2*got
		if w.runEvents >= w.guard.CycleEvents {
			w.abort()
		}
	}
}

// detectMaxP is the longest period the n-signature window can show.
func (w *livelockWatcher) detectMaxP() int {
	maxP := w.guard.MaxPeriod
	if lim := len(w.sigs) / 2; lim < maxP {
		maxP = lim
	}
	return maxP
}

// detectIndexed is detect's verdict through the candidate index: the
// shortest holding period, or 0.
func (w *livelockWatcher) detectIndexed() int {
	n := len(w.sigs)
	maxP := w.detectMaxP()
	if maxP < 1 {
		return 0
	}
	t := w.total - 1 // the newest position
	phys := func(pos uint32) int { return int(pos % uint32(n)) }
	last := w.sigs[phys(t)]
	prev := w.sigs[phys(t-1)]
	for link := w.prevPos[phys(t)]; link != 0; {
		pos := link - 1
		p := int(t - pos)
		if p > maxP {
			break
		}
		if w.sigs[phys(pos)] == last && (p == 1 || w.sigs[phys(pos-1)] == prev && w.periodHolds(n, p)) {
			return p
		}
		link = w.prevPos[phys(pos)]
	}
	return 0
}

// detectScan is the reference window scan: every position, newest first.
func (w *livelockWatcher) detectScan() int {
	n := len(w.sigs)
	maxP := w.detectMaxP()
	if maxP < 1 {
		return 0
	}
	last := w.sigAt(n - 1)
	for p := 1; p <= maxP; p++ {
		if w.sigAt(n-1-p) == last && w.periodHolds(n, p) {
			return p
		}
	}
	return 0
}

// periodHolds reports whether the trailing 2p signatures of an n-entry
// window are two identical halves.
func (w *livelockWatcher) periodHolds(n, p int) bool {
	for j := n - 1; j >= n-p; j-- {
		if w.sigAt(j) != w.sigAt(j-p) {
			return false
		}
	}
	return true
}

// abort panics with the diagnostic for the active run. The rendered cycle
// is the last runPeriod real events -- exactly one period, however many
// times it has repeated.
func (w *livelockWatcher) abort() {
	p := w.runPeriod
	first := w.recentAt(len(w.recent) - p)
	cycle := make([]string, 0, p)
	for i := len(w.recent) - p; i < len(w.recent); i++ {
		ev := w.recentAt(i)
		cycle = append(cycle, describeEvent(ev))
	}
	panic(&LivelockError{
		Reason:   "repeating cycle",
		Kind:     first.Kind,
		Object:   first.Obj,
		Repeats:  w.runEvents / p,
		CycleLen: p,
		FirstSeq: first.Seq,
		LastSeq:  w.recentAt(len(w.recent) - 1).Seq,
		Cycle:    cycle,
	})
}

// eventSignature is the watcher's per-event fingerprint: everything that
// identifies the event's shape (kind, seat, object, zone/step movement,
// counter name, secret bit, id and pair payloads), and deliberately NOT
// Seq (every event has a fresh one), Amount and Text (a stuck loop whose
// payload drifts must still be caught). Length-prefixing keeps adjacent
// fields unambiguous.
func eventSignature(ev *events.Event) uint64 {
	h := uint64(fnvOffset)
	h = fnvByte(h, byte(ev.Kind))
	h = fnvByte(h, byte(ev.Player))
	h = fnvByte(h, byte(ev.From))
	h = fnvByte(h, byte(ev.To))
	h = fnvByte(h, byte(ev.Step))
	if ev.Secret {
		h = fnvByte(h, 1)
	} else {
		h = fnvByte(h, 0)
	}
	h = fnvU32(h, uint32(ev.Obj))
	h = fnvU32(h, uint32(len(ev.Counter)))
	for i := 0; i < len(ev.Counter); i++ {
		h = fnvByte(h, ev.Counter[i])
	}
	h = fnvU32(h, uint32(len(ev.IDs)))
	for _, id := range ev.IDs {
		h = fnvU32(h, uint32(id))
	}
	h = fnvU32(h, uint32(len(ev.Pairs)))
	for _, pr := range ev.Pairs {
		h = fnvU32(h, uint32(pr[0]))
		h = fnvU32(h, uint32(pr[1]))
	}
	return h
}

// The signature's FNV-1a parameters. The fold is byte-wise, little-endian
// for integers (fnvU32 is four fnvByte steps in that order), so the value
// is the one the byte-slice formulation produced.
const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

func fnvByte(h uint64, b byte) uint64 { return (h ^ uint64(b)) * fnvPrime }

func fnvU32(h uint64, v uint32) uint64 {
	h = fnvByte(h, byte(v))
	h = fnvByte(h, byte(v>>8))
	h = fnvByte(h, byte(v>>16))
	return fnvByte(h, byte(v>>24))
}

func fnvU64(h uint64, v uint64) uint64 {
	return fnvU32(fnvU32(h, uint32(v)), uint32(v>>32))
}

// describeEvent renders one event for the diagnostic: compact, stable, and
// enough to name the object and what was being done to it.
func describeEvent(ev events.Event) string {
	s := fmt.Sprintf("seq %d %s", ev.Seq, ev.Kind)
	if ev.Obj != 0 {
		s += fmt.Sprintf(" obj=%d", ev.Obj)
	}
	if ev.Player != 0 || ev.Obj == 0 {
		s += fmt.Sprintf(" player=%d", ev.Player)
	}
	if ev.Text != "" {
		t := ev.Text
		if len(t) > 80 {
			t = t[:80] + "..."
		}
		s += fmt.Sprintf(" %q", t)
	}
	return s
}
