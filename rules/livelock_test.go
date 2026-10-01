package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The livelock watcher (rules/livelock.go) aborts a game whose event stream
// provably never terminates by panicking with a *LivelockError from inside
// the emit path -- the harnesses (cmd/mtgsim, cmd/botbench) recover it and
// report it. These tests drive the engine's own emit with synthetic
// non-terminating patterns: the same (object, event-kind) shape repeating
// forever is the I-1 failure family (a stack object re-resolving 700+ times
// in a 13-event cycle), and the tests pin the diagnostic the brief asks
// for: object id, event kind, repeat count and cycle length.

// drivePattern emits n events through the real engine emit path, recovering
// the watcher's abort so the test can assert on the diagnostic. A nil
// result means the watcher never fired.
func drivePattern(t *testing.T, e *Engine, make func(i int) events.Event, n int) (lle *LivelockError) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if l, ok := r.(*LivelockError); ok {
				lle = l
				return
			}
			panic(r)
		}
	}()
	for i := 0; i < n; i++ {
		e.emit(make(i))
	}
	return nil
}

func livelockTestEngine(t *testing.T, g *LoopGuard) *Engine {
	t.Helper()
	return New(Config{Seed: 1, Names: []string{"A", "B"}, LoopGuard: g})
}

// TestLivelockWatcherAbortsSameObjectSameKindRepeats is the brief's minimum
// shape: the same (object, event-kind) pair emitting forever is detected
// within a bounded number of repeats and aborts with the object id, event
// kind, repeat count and cycle length -- not a silent hang.
func TestLivelockWatcherAbortsSameObjectSameKindRepeats(t *testing.T) {
	e := livelockTestEngine(t, &LoopGuard{CycleEvents: 30, MaxPeriod: 8})
	lle := drivePattern(t, e, func(int) events.Event {
		return events.Event{Kind: events.Note, Obj: 42, Text: "loop"}
	}, 1000)
	if lle == nil {
		t.Fatal("watcher never fired on a same-(object,kind) repeat loop")
	}
	if lle.Reason != "repeating cycle" || lle.Kind != events.Note || lle.Object != 42 {
		t.Errorf("diagnostic = reason %q kind %v object %d, want repeating cycle / Note / 42", lle.Reason, lle.Kind, lle.Object)
	}
	if lle.CycleLen != 1 {
		t.Errorf("CycleLen = %d, want 1 (the tightest period is the single repeated event)", lle.CycleLen)
	}
	if lle.Repeats != 30 {
		t.Errorf("Repeats = %d, want 30 (CycleEvents 30 at period 1)", lle.Repeats)
	}
	msg := lle.Error()
	for _, want := range []string{"livelock detected", "object 42", "note", "repeated 30 time(s)", "cycle of 1 event(s)"} {
		if !strings.Contains(msg, want) {
			t.Errorf("diagnostic %q missing %q", msg, want)
		}
	}
	if len(lle.Cycle) != 1 || !strings.Contains(lle.Cycle[0], "obj=42") {
		t.Errorf("rendered cycle = %v, want one entry naming obj=42", lle.Cycle)
	}
}

// TestLivelockWatcherAbortsMultiEventCycle pins the I-1 shape directly: a
// 13-event resolve cycle (here synthesised as a 3-event rotation) repeating
// forever is caught, with the cycle length reported.
func TestLivelockWatcherAbortsMultiEventCycle(t *testing.T) {
	e := livelockTestEngine(t, &LoopGuard{CycleEvents: 12, MaxPeriod: 8})
	lle := drivePattern(t, e, func(i int) events.Event {
		return events.Event{Kind: events.Note, Obj: state.ObjID(1 + i%3), Text: "resolve step"}
	}, 1000)
	if lle == nil {
		t.Fatal("watcher never fired on a 3-event repeating cycle")
	}
	if lle.CycleLen != 3 {
		t.Errorf("CycleLen = %d, want 3", lle.CycleLen)
	}
	if lle.Repeats != 4 {
		t.Errorf("Repeats = %d, want 4 (CycleEvents 12 at period 3)", lle.Repeats)
	}
	if len(lle.Cycle) != 3 {
		t.Errorf("rendered cycle = %v, want all 3 events of one period", lle.Cycle)
	}
	if lle.FirstSeq == 0 || lle.LastSeq <= lle.FirstSeq {
		t.Errorf("span %d-%d does not name a real event range", lle.FirstSeq, lle.LastSeq)
	}
}

// TestLivelockWatcherCatchesDriftingPayload is why the signature excludes
// Amount and Text: a stuck loop whose payload drifts a little each
// iteration (a life total ticking, a counter creeping) must still abort.
func TestLivelockWatcherCatchesDriftingPayload(t *testing.T) {
	e := livelockTestEngine(t, &LoopGuard{CycleEvents: 20, MaxPeriod: 8})
	lle := drivePattern(t, e, func(i int) events.Event {
		return events.Event{Kind: events.Note, Obj: 7, Text: strings.Repeat("x", i+1)}
	}, 1000)
	if lle == nil {
		t.Fatal("watcher never fired on a payload-drifting repeat loop")
	}
	if lle.CycleLen != 1 || lle.Object != 7 {
		t.Errorf("diagnostic = cycle %d object %d, want cycle 1 object 7", lle.CycleLen, lle.Object)
	}
}

// TestLivelockWatcherRunawayBackstop is the catch-all for loops the exact
// period cannot lock onto (a new object minted every iteration, a period
// past MaxPeriod): a bounded no-decision/no-step/no-turn run aborts.
func TestLivelockWatcherRunawayBackstop(t *testing.T) {
	e := livelockTestEngine(t, &LoopGuard{RunawayEvents: 50})
	lle := drivePattern(t, e, func(i int) events.Event {
		// A fresh object every event: no period for the cycle detector.
		return events.Event{Kind: events.Note, Obj: state.ObjID(1000 + i)}
	}, 1000)
	if lle == nil {
		t.Fatal("watcher never fired on a runaway resolution")
	}
	if lle.Reason != "runaway resolution" {
		t.Errorf("Reason = %q, want runaway resolution", lle.Reason)
	}
	if lle.QuietEvents != 51 {
		t.Errorf("QuietEvents = %d, want 51 (threshold crossed at the 51st consecutive non-progress event)", lle.QuietEvents)
	}
	if lle.FirstSeq == 0 || lle.LastSeq <= lle.FirstSeq {
		t.Errorf("span %d-%d does not name a real event range", lle.FirstSeq, lle.LastSeq)
	}
	if len(lle.Cycle) != 0 {
		t.Errorf("a runaway has no cycle to render, got %v", lle.Cycle)
	}
}

// TestLivelockWatcherProgressResetsQuiet: a game that keeps asking
// decisions and changing steps never trips the runaway backstop, however
// many events it produces in between.
func TestLivelockWatcherProgressResetsQuiet(t *testing.T) {
	e := livelockTestEngine(t, &LoopGuard{RunawayEvents: 50})
	lle := drivePattern(t, e, func(i int) events.Event {
		if i%10 == 9 {
			return events.Event{Kind: events.StepChange, Step: 1}
		}
		return events.Event{Kind: events.Note, Obj: state.ObjID(1000 + i)}
	}, 500)
	if lle != nil {
		t.Fatalf("watcher fired on a stream with progress every 10 events: %v", lle)
	}
}

// TestLivelockWatcherQuietBelowThresholdDoesNotAbort: under the threshold
// the watcher is pure observation -- no panic, and the game's log is
// untouched by it.
func TestLivelockWatcherQuietBelowThresholdDoesNotAbort(t *testing.T) {
	e := livelockTestEngine(t, &LoopGuard{CycleEvents: 30, MaxPeriod: 8, RunawayEvents: 50})
	lle := drivePattern(t, e, func(int) events.Event {
		return events.Event{Kind: events.Note, Obj: 42, Text: "loop"}
	}, 29)
	if lle != nil {
		t.Fatalf("watcher fired %d events under the 30-event threshold: %v", 29, lle)
	}
}

// TestLivelockWatcherCloneCarriesGuard: a cloned engine watches its own
// stream under the same Config-given thresholds, so host paths (clone at
// every turn start) keep the protection.
func TestLivelockWatcherCloneCarriesGuard(t *testing.T) {
	e := livelockTestEngine(t, &LoopGuard{CycleEvents: 10, MaxPeriod: 4})
	c := e.Clone()
	if c.loop.guard != e.loop.guard {
		t.Fatalf("clone guard = %+v, want %+v", c.loop.guard, e.loop.guard)
	}
	lle := drivePattern(t, c, func(int) events.Event {
		return events.Event{Kind: events.Note, Obj: 42}
	}, 100)
	if lle == nil {
		t.Fatal("cloned engine's watcher never fired")
	}
	// The ORIGINAL keeps working: the clone's abort must not have shared
	// run state with it.
	lle2 := drivePattern(t, e, func(int) events.Event {
		return events.Event{Kind: events.Note, Obj: 42}
	}, 9)
	if lle2 != nil {
		t.Fatalf("original engine's watcher fired under its own threshold: %v", lle2)
	}
}

func BenchmarkLivelockWatcherSteadyAperiodic(b *testing.B) {
	maxInt := int(^uint(0) >> 1)
	w := newLivelockWatcher(&LoopGuard{
		CycleEvents: maxInt, RunawayEvents: maxInt, MaxPeriod: 128,
	})
	for i := range 256 {
		w.observe(events.Event{Seq: uint64(i + 1), Kind: events.Note, Obj: state.ObjID(i + 1)})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		w.observe(events.Event{Seq: uint64(257 + i), Kind: events.Note, Obj: state.ObjID(257 + i)})
	}
	b.StopTimer()
	if w.quiet != b.N+256 || len(w.sigs) != 256 || len(w.recent) != 128 {
		b.Fatalf("watcher digest = quiet %d, sigs %d, recent %d", w.quiet, len(w.sigs), len(w.recent))
	}
}

func TestLivelockWatcherSteadyWindowDoesNotAllocate(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	w := newLivelockWatcher(&LoopGuard{
		CycleEvents: maxInt, RunawayEvents: maxInt, MaxPeriod: 8,
	})
	seq := 0
	for range 16 {
		seq++
		w.observe(events.Event{Seq: uint64(seq), Kind: events.Note, Obj: state.ObjID(seq)})
	}
	allocs := testing.AllocsPerRun(1000, func() {
		seq++
		w.observe(events.Event{Seq: uint64(seq), Kind: events.Note, Obj: state.ObjID(seq)})
	})
	if allocs != 0 {
		t.Fatalf("steady bounded observation allocated %.2f objects/event, want zero", allocs)
	}
}

// TestLivelockWatcherShortWindowDoesNotReadBeforeTheWindow pins the detect()
// bound: a period-p match compares the trailing 2p signatures as two halves
// (j from n-1 down to n-p against j-p). The old bound (j >= n-2p+1) walked
// p-1 pairs further and, on a window not yet wrapped (a fresh or freshly
// Cloned watcher), read sigAt(-1): four events alternating A,B,A,B panicked
// with an index out of range instead of recognising period 2 (found by the
// search-teacher spike's rollouts, ~1% of cloned cast rollouts).
func TestLivelockWatcherShortWindowDoesNotReadBeforeTheWindow(t *testing.T) {
	w := newLivelockWatcher(nil)
	a := events.Event{Kind: events.Note, Obj: 11}
	b := events.Event{Kind: events.Tap, Obj: 12}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("A,B,A,B on a fresh watcher panicked: %v", r)
		}
	}()
	for _, ev := range []events.Event{a, b, a, b} {
		w.observe(ev)
	}
	if w.runPeriod != 2 {
		t.Fatalf("runPeriod = %d after A,B,A,B, want 2", w.runPeriod)
	}
}

// TestLivelockWatcherCombatDamageFromManySourcesIsNotACycle pins cardfuzz
// batch8 lines 2-3: the combat damage step of a board of ~1800 Goblin
// tokens logs one Damage event per attacker, each to the same player and
// byte-identical in payload (a player hit names no object). Folding the
// engine's damage source into the signature keeps 500 different creatures'
// hits from reading as a period-1 cycle, while one source damaging the same
// player over and over is still caught.
func TestLivelockWatcherCombatDamageFromManySourcesIsNotACycle(t *testing.T) {
	w := newLivelockWatcher(nil)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("500 hits from 500 distinct sources tripped the watcher: %v", r)
			}
		}()
		for i := 0; i < 500; i++ {
			w.observeFrom(&events.Event{Seq: uint64(i + 1), Kind: events.Damage, Player: 1, Amount: 1},
				state.ObjID(1000+i), 0)
		}
	}()
	w = newLivelockWatcher(nil)
	defer func() {
		r := recover()
		if _, ok := r.(*LivelockError); !ok {
			t.Fatalf("500 hits from ONE source did not trip the watcher (recovered %v)", r)
		}
	}()
	for i := 0; i < 500; i++ {
		w.observeFrom(&events.Event{Seq: uint64(i + 1), Kind: events.Damage, Player: 1, Amount: 1}, 77, 0)
	}
}

// TestLivelockDetectMatchesReference pins detect()'s candidate scan against
// the plain every-period reference over wrapped and unwrapped rings of small
// alphabets (so periods of every length actually occur).
func TestLivelockDetectMatchesReference(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, maxPeriod := range []int{1, 2, 3, 5, 8} {
		for alpha := 1; alpha <= 3; alpha++ {
			w := newLivelockWatcher(&LoopGuard{CycleEvents: maxInt, RunawayEvents: maxInt, MaxPeriod: maxPeriod})
			x := uint32(maxPeriod*7 + alpha)
			for i := 0; i < 400; i++ {
				x = x*1664525 + 1013904223
				w.observe(events.Event{Seq: uint64(i + 1), Kind: events.Note, Obj: state.ObjID(1 + (x>>16)%uint32(alpha))})
				// Reference: the shortest p whose trailing halves agree.
				n := len(w.sigs)
				want := 0
				lim := maxPeriod
				if n/2 < lim {
					lim = n / 2
				}
				for p := 1; p <= lim && want == 0; p++ {
					ok := true
					for j := n - 1; j >= n-p; j-- {
						if w.sigAt(j) != w.sigAt(j-p) {
							ok = false
							break
						}
					}
					if ok {
						want = p
					}
				}
				saved := w.runPeriod
				w.runPeriod, w.runEvents = 0, 0
				w.detect()
				if w.runPeriod != want {
					t.Fatalf("maxPeriod %d alpha %d event %d: detect period %d, reference %d", maxPeriod, alpha, i, w.runPeriod, want)
				}
				w.runPeriod = saved
				if saved == 0 {
					w.runEvents = 0
				} else {
					w.runEvents = 2 * saved
				}
			}
		}
	}
}
