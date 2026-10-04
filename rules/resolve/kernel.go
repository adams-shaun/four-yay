package resolve

import (
	"fmt"
	"sync/atomic"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
)

// Kernel is the per-engine kernel state, embedded by value in the engine.
// Its fields carry the engine's clone policy tags (rules' clone policy test
// walks them): a clone keeps the switch and shares a posed checkpoint by
// pointer, and starts every in-flight field zero (ForClone).
type Kernel struct {
	// posed is non-nil while a tape resolution is posed: the immutable
	// checkpoint, shared by pointer with every clone (a search world built
	// at the posed decision re-runs from the same S0).
	posed *Checkpoint `clone:"share"`
	// run is non-nil only inside a Submit executing a tape run.
	run *run `clone:"reset"`
	// noCkpt is set while a resolution the ask-free predicate exempted (or
	// one an Answerer serves) runs without a checkpoint; a legacy ask
	// reaching the engine meanwhile is a predicate miss.
	noCkpt bool `clone:"reset"`
	// intentEnd (> len(Intents)) is a re-execution's intent verify window:
	// the live intent array already holds the tape at those indices (the
	// tape was read from them), so LogIntent extends the slice instead of
	// rewriting recorded history a clone may share.
	intentEnd int `clone:"reset"`
	// answerer is the synchronous answerer of an all-policy engine. A clone
	// never inherits it: a search world's seats are the search's.
	answerer Answerer `clone:"reset"`
	// inline marks the Pose an inline answer makes.
	inline bool `clone:"reset"`
	// posedLen is the event log's length when the posed tape decision was
	// posed. Anything logged past it before the answering Submit was
	// appended from outside the engine's own flow while the decision was
	// posed (a hypothetical world's redeal, a test probe's emit); the
	// answer records it as an injection, re-applied at exactly that point
	// by every re-run. A clone at the posed decision has the same log, so it
	// keeps the length.
	posedLen int `clone:"deep"`
}

// Checkpoint is S0 and where the resolution's tape starts.
type Checkpoint struct {
	// S0 is the engine at the resolution-starting pass. Never mutated, never
	// recycled while a clone can share it.
	S0 Snapshot
	// World is set on a hypothetical world's copy of the checkpoint (Fork):
	// rules' RNG splice, which a restore installs so the re-executed prefix
	// draws S0's values up to the fork and the world's own after it (spec
	// §7.3).
	World any
	// k0 is the index in the intent log of the resolution-starting pass;
	// base is the event log's length at S0.
	k0, base int
	// shared is nonzero once a clone of the engine took this checkpoint
	// (ForClone, Fork): S0 is then never recycled, since another engine may
	// still restore from it. An unshared S0 is recycled (Board.Drop) when
	// its resolution completes. Atomic: a search may clone one posed root
	// from several goroutines at once.
	shared int32
	// probe, when set, is the function a test runs in place of an intent's
	// commit (Kernel.Probe): a re-run re-executes it instead of a Submit.
	probe func()
	// injects are the events appended from outside the engine's flow while
	// one of this resolution's tape decisions was posed (Kernel.posedLen):
	// a redealt world's Secret events, a test probe's emits. A re-run
	// re-applies each at exactly the log length it was appended at, so the
	// re-executed engine is the injected posed state (for a world, the
	// REDEALT one, never the true one). Copy-on-write: a checkpoint is shared
	// by pointer with every clone.
	injects []injection
}

// injection is one batch of events appended at log length at.
type injection struct {
	at  int
	evs []events.Event
}

// run is one execution of a tape resolution.
type run struct {
	cp     *Checkpoint
	serve  []decision.Intent // answers to serve, in order (the tape minus the pass)
	cursor int
	// inRes is true from the resolution-starting pass until the resolution
	// returns (ResolutionDone); only asks inside it are tape asks.
	inRes bool
	// converting marks the Pose Answer itself makes.
	converting bool
	// tapeAsked: a converted ask was served or posed during this run.
	tapeAsked bool
	// first: the live engine's first run (S0 was just taken), where a legacy
	// ask before any tape ask can switch to legacy in place.
	first bool
	// injected counts the checkpoint's injections already re-applied.
	injected int
	// rerun marks a re-execution from S0, whose observers are parked until
	// the run passes the recorded prefix.
	rerun bool
}

// unwind is the sentinel a stop-ask with an exhausted tape panics with.
type unwind struct{}

// Divergence is the failure a re-execution reports when its events do not
// reproduce the recorded prefix, or a served answer is refused. The kernel
// panics with it; in a hosted match the spec records a feedback snapshot
// and ends the match as an engine fault (not built yet), and in tests and
// the dual run it fails hard.
type Divergence struct{ Msg string }

func (d Divergence) Error() string { return "resolve: tape kernel divergence: " + d.Msg }

// Posed reports whether a tape resolution is posed (S0 held).
func (k *Kernel) Posed() bool { return k.posed != nil }

// SetAnswerer installs (nil removes) the synchronous answerer.
func (k *Kernel) SetAnswerer(f Answerer) { k.answerer = f }

// ForClone is the kernel state a clone of the engine starts with: the posed checkpoint (shared) at the same posed log length, nothing in
// flight.
func (k *Kernel) ForClone() Kernel {
	if cp := k.posed; cp != nil && atomic.LoadInt32(&cp.shared) == 0 {
		atomic.StoreInt32(&cp.shared, 1)
	}
	return Kernel{posed: k.posed, posedLen: k.posedLen}
}

// Fork gives a hypothetical world forked at a posed tape resolution its own
// checkpoint copy carrying the world's RNG splice; forkAt is the world's log
// length, where whatever its builder appends before its first Submit (a
// redeal's Secret events) is injected (spec §7.3). Call it only when Posed.
func (k *Kernel) Fork(world any, forkAt int) {
	atomic.StoreInt32(&k.posed.shared, 1)
	cp := *k.posed
	cp.World = world
	k.posed = &cp
	k.posedLen = forkAt
}

// Watching reports whether the engine's ask choke point must call OnAsk.
func (k *Kernel) Watching() bool { return k.run != nil || k.noCkpt }

// ResolutionDone marks the end of the resolution: asks after it (trigger
// placement, the next priority) are ordinary.
func (k *Kernel) ResolutionDone() {
	k.noCkpt = false
	if r := k.run; r != nil {
		r.inRes = false
	}
}

// Boundary marks the end of the resolution at an engine flow's own decision
// (no resolution continuation behind it): like ResolutionDone, the decision
// is then posed as an ordinary one.
func (k *Kernel) Boundary() {
	if r := k.run; r != nil && !r.converting {
		r.inRes = false
	}
	k.noCkpt = false
}

// LogIntent appends in to the intent log, or, inside a re-execution's verify
// window, steps over the identical recorded intent.
func (k *Kernel) LogIntent(l *events.Log, in decision.Intent) {
	if n := len(l.Intents); n < k.intentEnd {
		l.Intents = l.Intents[:n+1]
		return
	}
	l.Intents = append(l.Intents, in)
}

// Submit runs a validated intent through the kernel and reports whether it
// handled it; false means the caller commits it as usual.
func (k *Kernel) Submit(b Board, d *decision.Decision, in decision.Intent) bool {
	if k.run != nil {
		return false
	}
	if k.posed != nil {
		k.resubmit(b, in)
		return true
	}
	if !b.StartsResolution(d, in) {
		return false
	}
	if k.answerer != nil || !b.MayAsk(d, in) {
		k.exempt(b, d, in)
		return true
	}
	k.firstRun(b, d, in)
	return true
}

// exempt runs a resolution-starting pass with no checkpoint: the predicate
// proved it ask-free, or an Answerer serves its converted asks inline. A
// legacy ask before the resolution ends is a miss (OnAsk), answered on the
// legacy path in place.
func (k *Kernel) exempt(b Board, d *decision.Decision, in decision.Intent) {
	stats.exempt.Add(1)
	k.noCkpt = true
	b.Commit(d, in)
	k.noCkpt = false
}

// firstRun checkpoints and runs the resolution-starting pass on the live
// engine.
func (k *Kernel) firstRun(b Board, d *decision.Decision, in decision.Intent) {
	l := b.Log()
	cp := &Checkpoint{S0: b.Checkpoint(), k0: len(l.Intents), base: len(l.Events)}
	stats.checkpoints.Add(1)
	k.run = &run{cp: cp, inRes: true, first: true}
	if k.runCommit(b, d, in) {
		k.run, k.posed = nil, cp
		k.posedLen = len(l.Events)
		stats.posed.Add(1)
		return
	}
	k.run = nil
	stats.noAsk.Add(1)
	b.Drop(cp.S0)
}

// runCommit runs b.Commit and reports whether it unwound at a posed stop-ask.
// Any other panic propagates.
func (k *Kernel) runCommit(b Board, d *decision.Decision, in decision.Intent) (posed bool) {
	defer func() {
		if p := recover(); p != nil {
			posed = sentinel(p)
		}
	}()
	b.Commit(d, in)
	return false
}

// runFn is runCommit for a test probe's function.
func (k *Kernel) runFn(f func()) (posed bool) {
	defer func() {
		if p := recover(); p != nil {
			posed = sentinel(p)
		}
	}()
	f()
	return false
}

// Idle reports whether no tape run, posed resolution or exempted resolution
// is in flight: the state in which a test may start a Probe.
func (k *Kernel) Idle() bool { return k.run == nil && k.posed == nil && !k.noCkpt }

// Probe runs f as a resolution-starting step under the kernel, for tests that
// drive an engine's internals (resolveTop, an entry) instead of Submitting an
// intent: f checkpoints the engine, runs, and a tape ask inside it poses its
// decision and unwinds exactly as in a Submit. The answering Submit restores
// the checkpoint and re-executes f with the recorded answers served, so f
// must be repeatable from the checkpoint (it may only read what the engine
// and the test captured before the probe). A probe nested in a run, or while
// a decision is posed, just runs f.
func (k *Kernel) Probe(b Board, f func()) {
	if k.run != nil || k.posed != nil {
		f()
		return
	}
	l := b.Log()
	cp := &Checkpoint{S0: b.Checkpoint(), k0: len(l.Intents), base: len(l.Events), probe: f}
	stats.checkpoints.Add(1)
	k.run = &run{cp: cp, inRes: true, first: true}
	if k.runFn(f) {
		k.run, k.posed = nil, cp
		k.posedLen = len(l.Events)
		stats.posed.Add(1)
		return
	}
	k.run = nil
	stats.noAsk.Add(1)
	b.Drop(cp.S0)
}

// runSubmit is runCommit for a whole Submit.
func (k *Kernel) runSubmit(b Board, in decision.Intent) (posed bool, err error) {
	defer func() {
		if p := recover(); p != nil {
			posed = sentinel(p)
		}
	}()
	err = b.Submit(in)
	return false, err
}

// sentinel classifies a recovered panic: the kernel's own unwind, or
// anything else re-raised.
func sentinel(p any) (posed bool) {
	if _, ok := p.(unwind); ok {
		return true
	}
	panic(p)
}

// resubmit answers a posed tape resolution: restore S0 in place, re-execute
// the pass with the whole tape, verify the recorded prefix.
func (k *Kernel) resubmit(b Board, in decision.Intent) {
	cp := k.posed
	l := b.Log()
	if n := len(l.Events); n > k.posedLen && k.posedLen >= cp.base {
		// Logged while the decision was posed, from outside the engine's
		// flow (nothing of its own runs while a decision is posed): record it
		// on this engine's own checkpoint copy as an injection.
		ncp := *cp
		ncp.injects = append(append([]injection(nil), cp.injects...),
			injection{at: k.posedLen, evs: append([]events.Event(nil), l.Events[k.posedLen:]...)})
		cp = &ncp
		k.posed = cp
	}
	tape := make([]decision.Intent, 0, len(l.Intents)-cp.k0+1)
	tape = append(append(tape, l.Intents[cp.k0:]...), in)
	liveLen, liveIntents := len(l.Events), len(l.Intents)
	stats.reruns.Add(1)

	k.restore(b, cp, liveLen, liveIntents)
	var r *run
	var posed bool
	if cp.probe != nil {
		r = &run{cp: cp, serve: tape, inRes: true, rerun: true}
		k.run = r
		posed = k.runFn(cp.probe)
		k.run = nil
	} else {
		r = &run{cp: cp, serve: tape[1:], inRes: true, rerun: true}
		k.run = r
		var err error
		posed, err = k.runSubmit(b, tape[0])
		k.run = nil
		if err != nil {
			panic(Divergence{fmt.Sprintf("re-executed pass refused: %v", err)})
		}
	}
	if r.cursor != len(r.serve) {
		panic(Divergence{fmt.Sprintf("re-execution served %d of %d recorded answers", r.cursor, len(r.serve))})
	}
	stats.noteK(int64(r.cursor))
	k.closeWindows(l)
	stats.prefixEvents.Add(int64(liveLen - cp.base))
	b.Observe()
	if posed {
		k.posed = cp
		k.posedLen = len(l.Events)
	} else {
		k.posed = nil
		if atomic.LoadInt32(&cp.shared) == 0 {
			// No clone took the checkpoint: S0 is this engine's alone, so
			// its storage recycles into the next checkpoint.
			b.Drop(cp.S0)
		}
	}
}

// restore makes the engine S0 again with verify windows over the recorded
// events and intents.
func (k *Kernel) restore(b Board, cp *Checkpoint, evEnd, intentEnd int) {
	b.Restore(cp, evEnd)
	l := b.Log()
	l.Intents = l.Intents[:cp.k0]
	k.intentEnd = intentEnd
}

// closeWindows ends a re-execution's verify windows; every recorded event
// and intent must have been reproduced.
func (k *Kernel) closeWindows(l *events.Log) {
	end := l.VerifyEnd()
	if !l.VerifyClose() {
		panic(Divergence{fmt.Sprintf("re-execution logged %d events, recorded prefix ends at %d", len(l.Events), end)})
	}
	if len(l.Intents) < k.intentEnd {
		panic(Divergence{fmt.Sprintf("re-execution logged %d intents, recorded tape ends at %d", len(l.Intents), k.intentEnd)})
	}
	k.intentEnd = 0
}

// Answer is the converted ask boundary (effects.AskTape through the engine's
// Host seam). ok false means the caller asks through the legacy path: there
// is no tape run, the run is past its resolution, or a legacy decision is
// outstanding.
func (k *Kernel) Answer(b Board, d *decision.Decision) (decision.Intent, bool) {
	r := k.run
	if r == nil {
		if k.noCkpt && k.answerer != nil && b.Pending() == nil && !b.Busy() {
			return k.inlineAnswer(b, d), true
		}
		return decision.Intent{}, false
	}
	if !r.inRes || b.Pending() != nil || b.Busy() {
		return decision.Intent{}, false
	}
	r.converting = true
	b.Pose(d)
	r.converting = false
	if b.Pending() != d {
		// Deferred behind a commander-zone choice: not a shape the kernel
		// serves yet.
		panic(Divergence{"tape ask deferred behind another decision"})
	}
	r.tapeAsked = true
	k.inject(b, r)
	if r.cursor == len(r.serve) {
		panic(unwind{})
	}
	in := r.serve[r.cursor]
	r.cursor++
	if err := b.Validate(d, in); err != nil {
		panic(Divergence{fmt.Sprintf("served answer %d refused by a %s ask: %v", r.cursor-1, d.Kind, err)})
	}
	stats.served.Add(1)
	acted := b.Record(d, in)
	if r.cursor == len(r.serve) && r.rerun {
		// Past the recorded prefix: new territory, so the live observers
		// see what the run does from here (spec §7.5).
		b.Observe()
	}
	return acted, true
}

// inlineAnswer poses d and answers it from the synchronous answerer,
// recording exactly what a posed decision answered by Submit records.
func (k *Kernel) inlineAnswer(b Board, d *decision.Decision) decision.Intent {
	stats.inline.Add(1)
	k.inline = true
	b.Pose(d)
	k.inline = false
	if b.Pending() != d {
		panic(Divergence{"inline ask deferred behind another decision"})
	}
	var reject error
	for {
		in, ok := k.answerer(d, reject)
		if !ok {
			panic(Divergence{fmt.Sprintf("synchronous answerer gave up on a %s ask: %v", d.Kind, reject)})
		}
		if reject = b.Validate(d, in); reject == nil {
			return b.Record(d, in)
		}
	}
}

// inject re-applies the checkpoint's injected events when a re-run reaches
// the log length they were appended at (right after the posed ask's
// DecisionAsk).
func (k *Kernel) inject(b Board, r *run) {
	cp := r.cp
	for r.injected < len(cp.injects) && len(b.Log().Events) == cp.injects[r.injected].at {
		for _, ev := range cp.injects[r.injected].evs {
			b.Emit(ev)
		}
		r.injected++
	}
}

// InRun reports whether a tape run is executing its resolution.
func (k *Kernel) InRun() bool { return k.run != nil && k.run.inRes }

// Unservable reports a step of a served answer the kernel cannot serve: an
// engine-posed continuation nothing answers from the tape. It always fails
// hard (a Divergence); it is an assertion that the conversion is closed.
func (k *Kernel) Unservable() {
	stats.unservable.Add(1)
	panic(Divergence{"unservable tape step"})
}

// Unserved reports whether an engine decision reaching the ask choke point
// now is an ask nothing serves inside a tape run's resolution (observation
// only; OnAsk fails the run on it).
func (k *Kernel) Unserved() bool {
	r := k.run
	return r != nil && r.inRes && !r.converting
}

// OnAsk runs at the engine's ask choke point while Watching. An engine
// decision no tape path serves inside a tape run's resolution is a closed
// conversion that is not: it fails hard (a Divergence). One reaching an
// exempted resolution is a predicate miss: the resolution has no checkpoint
// to re-run from, so it fails hard too (the census sees it first through
// the caller's observer; the predicate is held to this by the cardfuzz
// miss census).
func (k *Kernel) OnAsk() (miss bool) {
	if k.Unserved() {
		panic(Divergence{"an engine ask no tape path serves inside a resolution"})
	}
	if k.noCkpt && !k.inline {
		k.noCkpt = false
		stats.misses.Add(1)
		return true
	}
	return false
}

// MissFailure is the Divergence an engine ask that reaches an exempted
// resolution fails with.
func MissFailure(what string) Divergence {
	return Divergence{"the ask-free predicate missed an ask: " + what}
}
