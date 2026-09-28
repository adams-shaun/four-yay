package rules

import (
	"fmt"
	"reflect"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
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
// The staticEffects memo has a second, narrower re-stamp admission
// (staticSafeSince) that also spans TokenCreate events. It is sound only when
// the memo's build encountered no continuous static carrying a gate param --
// a static-cold token's ARRIVAL can flip an existing gate's IsPresent$ count
// -- so it further requires !e.staticMemoGated; see staticSafeSince.
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

// CacheVerificationEnabled reports whether this binary was linked with the
// layer/Derived memo verify flags on (see layerInertVerify and
// derivedMemoVerify). It exists so a test or bench driver built with
// -ldflags can assert the invariant is actually being checked instead of
// passing vacuously without it.
func CacheVerificationEnabled() bool { return layerInertVerify || derivedMemoVerify }

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

// staticSafeSince admits the ordinary layer-inert run plus TokenCreate events
// that each appended exactly one battlefield object with no printed or copied
// static, but only when the memo's last build encountered no gate-carrying
// Continuous static (Engine.staticMemoGated). TokenCreate's event has no Obj
// referent, so object growth is matched by the suffix count and every appended
// object is classified directly.
//
// A static-cold token adds no static of its own (this loop's check), but its
// ARRIVAL changes the battlefield composition an EXISTING static's continuous
// gate reads: IsPresent$/IsPresent2$ count battlefield objects against a type
// filter, so a Goblin token entering can turn Bolg's Company's
// `IsPresent$ Goblin.Other+YouCtrl` haste grant ON, and a
// "you control exactly one creature" gate OFF (this is the bug
// agent-20260928T171650Z-006e2611 fixes: the re-stamp served a static list a
// fresh rescan would no longer produce). staticSafeSince's own loop cannot
// see that, and re-parsing the gate values here would re-implement the gate
// grammar -- the fragile direction. Instead the build records whether the
// scan saw any gate-carrying static at all (staticMemoGated, set at the one
// gate site in staticEffectsWalk): only a gate-free build's output is
// invariant under a static-cold token entry, so only then is the re-stamp
// admitted. A static-carrying token, a gate-carrying board, an unaccounted
// object append, or any other event forces the full rebuild. The gate-free
// board is the runtime common case, so the perf win of e748c8f1b survives.
func (e *Engine) staticSafeSince(epoch, oldObjs int) bool {
	n := len(e.L.Events)
	if e.staticMemoGated {
		return false
	}
	if epoch <= 0 || epoch > n || oldObjs < 0 || oldObjs > len(e.G.Objs) {
		return false
	}
	creates := 0
	for i := epoch; i < n; i++ {
		switch e.L.Events[i].Kind {
		case events.DecisionAsk, events.DecisionMade, events.Priority:
		case events.TokenCreate:
			creates++
		default:
			return false
		}
	}
	if len(e.G.Objs)-oldObjs != creates {
		return false
	}
	for i := oldObjs; i < len(e.G.Objs); i++ {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield || objectStaticHotOn(o) {
			return false
		}
	}
	return true
}

// refreshStaticContinuous brings the staticEffects memo up to the current log
// head: a no-op on an exact hit, a re-stamp across a layer-safe run, a full
// rescan otherwise. active() and staticControlWants share it.
func (e *Engine) refreshStaticContinuous() {
	n := len(e.L.Events)
	if e.staticEpoch == n {
		return
	}
	// The re-stamp is admitted only when the memo's last build was gate-free
	// (staticSafeSince's staticMemoGated check): a gate-carrying build's
	// output can be changed by a token's arrival, so it always rescans. See
	// staticSafeSince.
	if e.staticVersion == e.continuousVersion && e.staticSafeSince(e.staticEpoch, e.staticObjs) {
		e.staticEpoch = n
		e.staticObjs = len(e.G.Objs)
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
	// A content change: any activeBuf built from the previous list is stale,
	// even at an unmoved log head and continuousVersion (staticControlWants
	// refreshes this memo outside active()).
	e.staticBuildSeq++
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
	savedStaticSeq := e.activeStaticSeq
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
	e.activeStaticSeq = savedStaticSeq
	if len(cached) != len(fresh) || (len(cached) > 0 && !reflect.DeepEqual(cached, fresh)) {
		panic(fmt.Sprintf("rules: layer-inert active() reuse at log %d disagrees with a rebuild (%d vs %d effects)", len(e.L.Events), len(cached), len(fresh)))
	}
}
