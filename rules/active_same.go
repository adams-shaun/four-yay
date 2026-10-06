package rules

import (
	"fmt"
	"slices"
)

// Unchanged-list rebuilds of active().
//
// active() rebuilds on every event that is not layer-inert, but most of
// those rebuilds reassemble exactly the list it already holds: the static
// memo was re-stamped rather than rescanned (layercache.go,
// static_gatememo.go), the continuous registry did not move, and every
// registry entry is live or not exactly as before. The rebuild still copied
// every ~1 KB effect into the other buffer and compared the two lists effect
// by effect for derived_transparent.go.
//
// The assembled list is a pure function of three things: the registry's
// entries (unchanged while continuousVersion is -- every e.continuous
// mutation goes through continuousChanged), which of them pass
// continuousLive (recorded per build as a bitmask), and the static memo's
// entries (unchanged while staticBuildSeq is: a re-stamp never writes the
// memo). The stable sort of the same pointer sequence is the same
// permutation. So when all three match the build that produced activeBuf,
// the rebuild's list IS activeBuf: activeSameBuild keeps it (and its keyword
// heads) and runs only derived_transparent.go's event half, with the
// per-effect equality known and locality cached from the list's build.
//
// layerInertVerify assembles the full list anyway on every such build and
// panics if it differs.

// activeListKey records what the list in activeBuf was assembled from (set
// only by a depth-1 build, the one that owns activeBuf).
type activeListKey struct {
	ok        bool
	version   int
	staticSeq uint64
	// liveN is len(e.continuous) at the build (-1: wider than the mask) and
	// live the continuousLive bit of each entry.
	liveN int
	live  uint64
	// allLocal is derivedEffectLocal over every effect of the list.
	allLocal bool
}

// activeListUnchanged reports whether a depth-1 rebuild whose registry walk
// produced (liveN, live) would assemble activeBuf again.
func (e *Engine) activeListUnchanged(liveN int, live uint64) bool {
	k := &e.activeList
	return k.ok && liveN >= 0 && k.liveN == liveN && k.live == live &&
		k.version == e.continuousVersion && k.staticSeq == e.staticBuildSeq
}

// activeSameBuild finishes a depth-1 rebuild whose list is activeBuf itself.
func (e *Engine) activeSameBuild() []ContinuousEffect {
	if layerInertVerify {
		e.verifyActiveSame()
	}
	if !e.derivedRebuildTransparent(e.activeBuf, e.activeBuf, true, e.activeList.allLocal) {
		e.derivedSeq++
	}
	e.derivedNoteBuild()
	return e.activeBuf
}

// verifyActiveSame assembles the list from scratch, exactly as the full
// build does, and panics if it is not activeBuf.
func (e *Engine) verifyActiveSame() {
	var src []*ContinuousEffect
	for i := range e.continuous {
		if e.continuousLive(&e.continuous[i]) {
			src = append(src, &e.continuous[i])
		}
	}
	for i := range e.staticContinuous {
		src = append(src, &e.staticContinuous[i])
	}
	if !continuousPtrsSorted(src) {
		slices.SortStableFunc(src, compareContinuousPtr)
	}
	fresh := make([]ContinuousEffect, 0, len(src))
	for _, p := range src {
		fresh = append(fresh, *p)
	}
	if len(fresh) != len(e.activeBuf) || (len(fresh) > 0 && !continuousEffectsEqual(fresh, e.activeBuf)) {
		panic(fmt.Sprintf("rules: unchanged-list active() rebuild at log %d disagrees with a full build (%d vs %d effects)", len(e.L.Events), len(e.activeBuf), len(fresh)))
	}
}

// effectsAllLocal is derivedEffectLocal over a whole list.
func effectsAllLocal(list []ContinuousEffect) bool {
	for i := range list {
		if !derivedEffectLocal(&list[i]) {
			return false
		}
	}
	return true
}
