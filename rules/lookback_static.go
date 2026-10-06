package rules

import (
	"fmt"
)

// Look-back observer static memo seeding.
//
// checkTriggers builds a fresh observer Engine over the window's snapshot
// board for every leaves-the-battlefield event, and the observer's first
// Derived read rescans every static on that board (staticEffects). When the
// window opened, though, the live engine's own memo was often current for
// exactly that board: same log head, same object count, and a quiet build
// (staticMemoQuiet: no gate, no state read -- its list is a function of the
// Game alone, which the snapshot cloned). noteLookBackStatic records that,
// with the memo's build count; seedLookBackStatic hands the observer a copy
// of the live memo when the live engine has not rebuilt it since (a build
// is the only write to its content, and the only bump of staticBuildSeq),
// stamped at the current log head, exactly as Clone carries a memo across.
// The observer's rescan would reproduce it: same board, quiet inputs.
// layerInertVerify rescans the seeded observer and panics on a difference.

// noteLookBackStatic marks snap as taken while e's static memo was current
// and quiet for e's board.
func (e *Engine) noteLookBackStatic(snap *triggerSnapshot) {
	if e.staticEpoch > 0 && e.staticEpoch == len(e.L.Events) && e.staticObjs == len(e.G.Objs) &&
		e.staticMemoQuiet() && e.staticGatesKnown && len(e.staticGates) == 0 {
		snap.staticOwner, snap.staticSeq = e, e.staticBuildSeq
	}
}

// seedLookBackStatic installs e's memo into the look-back observer o built
// over the current window's board when its snapshot recorded the memo and e
// has not rebuilt it since.
func (e *Engine) seedLookBackStatic(o *Engine) {
	owner, seq := e.lookBackStaticSnap()
	if owner != e || seq != e.staticBuildSeq {
		return
	}
	o.staticContinuous = append(o.staticContinuous[:0], e.staticContinuous...)
	o.staticEpoch, o.staticObjs = len(o.L.Events), len(o.G.Objs)
	o.staticVersion = o.continuousVersion
	o.staticMemoGated, o.staticMemoStateRead = false, false
	o.staticGates, o.staticGatesKnown = o.staticGates[:0], true
	if layerInertVerify {
		seeded := append([]ContinuousEffect(nil), o.staticContinuous...)
		fresh := o.staticEffects(nil)
		if len(fresh) != len(seeded) || (len(fresh) > 0 && !continuousEffectsEqual(fresh, seeded)) {
			panic(fmt.Sprintf("rules: look-back observer seeded %d static effects, a rescan of the snapshot finds %d", len(seeded), len(fresh)))
		}
		if o.staticMemoGated || o.staticMemoStateRead {
			panic("rules: look-back observer seeded from a quiet memo, but the snapshot's rescan is not quiet")
		}
	}
}
