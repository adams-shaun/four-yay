package rules

import (
	"fmt"
	"reflect"

	"github.com/adams-shaun/gorge/events"
)

// Layer-inert cache reuse.
//
// active()'s sorted effect list, the staticEffects memo under it and the
// layer-4 derived-type table (refreshDerivedTypes) were each keyed on the log
// head, so every emitted event rebuilt them -- the whole staticEffects scan
// over every object's statics, once per event. But ~70% of a game's events
// are priority bookkeeping that cannot move any of their inputs:
//
//   - DecisionAsk and DecisionMade are pure markers: events.Apply writes
//     nothing for them.
//   - Priority writes only g.Priority and g.Passes, which nothing on the layer
//     path reads (only the priority-flow code in turn.go/legal.go does).
//
// None of the three is read back out of the log by a layer-path census
// either (the log scans on that path look for PutOnStack, TurnChange,
// CounterChange, Damage and the like). So when every event appended since a
// cache was built is one of them, AND continuousVersion and len(e.G.Objs) are
// unchanged since the build (the two non-log inputs the caches can see move
// without an event: a registry write and a test helper's direct AddObject),
// the cached value is exactly what a rebuild would produce, and the cache is
// re-stamped with the new log head instead. A rebuild still happens on every
// other event, exactly as before, so the reuse only ever removes rebuilds
// whose result would have been identical -- the chain, replay and every
// output are unchanged.
//
// An epoch <= 0 (a fresh engine, a clone, or cascade.go's explicit -1
// invalidation around its scratch-driven walk) never reuses.
//
// layerInertVerify (set by the rules test binary, or at link time through
// layerInertVerifyFlag) recomputes on every reuse and panics on a difference,
// so the whole rules suite checks that argument empirically.
// go build -ldflags "-X github.com/adams-shaun/gorge/rules.layerInertVerifyFlag=1".
var layerInertVerifyFlag string

var layerInertVerify = layerInertVerifyFlag != ""

// layerInertSince reports whether every event appended to the log at or after
// index epoch is layer-inert (see above). It scans only the new suffix and
// stops at the first other kind, so its cost is bounded by the run of
// priority bookkeeping since the last build.
func (e *Engine) layerInertSince(epoch int) bool {
	n := len(e.L.Events)
	if epoch <= 0 || epoch > n {
		return false
	}
	for i := epoch; i < n; i++ {
		switch e.L.Events[i].Kind {
		case events.DecisionAsk, events.DecisionMade, events.Priority:
		default:
			return false
		}
	}
	return true
}

// refreshStaticContinuous brings the staticEffects memo up to the current log
// head: a no-op on an exact hit, a re-stamp across a layer-inert run, a full
// rescan otherwise. active() and staticControlWants share it.
func (e *Engine) refreshStaticContinuous() {
	n := len(e.L.Events)
	if e.staticEpoch == n {
		return
	}
	if e.staticVersion == e.continuousVersion && e.staticObjs == len(e.G.Objs) && e.layerInertSince(e.staticEpoch) {
		e.staticEpoch = n
		if layerInertVerify {
			if fresh := e.staticEffects(nil); !reflect.DeepEqual(fresh, e.staticContinuous[:len(e.staticContinuous):len(e.staticContinuous)]) && !(len(fresh) == 0 && len(e.staticContinuous) == 0) {
				panic(fmt.Sprintf("rules: layer-inert static memo reuse at log %d disagrees with a rescan (%d vs %d effects)", n, len(e.staticContinuous), len(fresh)))
			}
		}
		return
	}
	e.staticEpoch = n
	e.staticVersion, e.staticObjs = e.continuousVersion, len(e.G.Objs)
	builds := e.activeBuildSeq
	e.staticContinuous = e.staticEffects(e.staticContinuous)
	// A gate the scan evaluates can read Derived (a CheckSVar$ count
	// matching a spell's type), and Derived builds active() -- which, with
	// staticEpoch already stamped n above, took the PREVIOUS static list.
	// When this refresh was not itself entered from active() (the
	// staticControlWants caller), that nested build is an outermost one and
	// stamped activeBuf at n: drop it so the next active() rebuilds with the
	// list just scanned. Measured with layerInertVerify: a Leapfrog-shaped
	// CheckSVar$ flying grant built while Gust of Wind's cast was in flight
	// (inFlightCast) was re-adopted across the priority bookkeeping after
	// the cast completed.
	if e.activeBuildSeq != builds && e.activeDepth == 0 && e.activeEpoch == n {
		e.activeEpoch = -1
	}
}

// verifyInertActive is active()'s layer-inert reuse check: it rebuilds the
// list from scratch under a forced miss and compares.
func (e *Engine) verifyInertActive() {
	cached := append([]ContinuousEffect(nil), e.activeBuf...)
	savedEpoch, savedBuf := e.activeEpoch, e.activeBuf
	savedHeads, savedSeq := e.activeKWHeads, e.activeBuildSeq
	e.activeEpoch, e.activeBuf, e.activeKWHeads = -1, nil, nil
	// A nested call would take the re-entrant private-buffer path; drop to
	// depth 0 so the forced rebuild is an ordinary outermost build.
	depth := e.activeDepth
	e.activeDepth = 0
	fresh := append([]ContinuousEffect(nil), e.active()...)
	e.activeDepth = depth
	// The forced rebuild is a check, not a rebuild of the served list: the
	// build count and head set stay the served list's (derivedmemo.go's
	// cross-walk reuse keys on the count).
	e.activeEpoch, e.activeBuf = savedEpoch, savedBuf
	e.activeKWHeads, e.activeBuildSeq = savedHeads, savedSeq
	if len(cached) != len(fresh) || (len(cached) > 0 && !reflect.DeepEqual(cached, fresh)) {
		panic(fmt.Sprintf("rules: layer-inert active() reuse at log %d disagrees with a rebuild (%d vs %d effects)", len(e.L.Events), len(cached), len(fresh)))
	}
}
