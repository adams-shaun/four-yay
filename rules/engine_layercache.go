package rules

// engineLayerCaches groups the Engine's layer memoisation and arena-scan
// caches that a clone deliberately leaves zero. It is embedded by value in
// Engine (rules/engine_struct.go), so every field keeps its documented
// contract comment and every existing e.<field> access keeps compiling
// unchanged through Go's field promotion. Clone's per-field copy classes
// (rules/clone.go) are unchanged by the move.
type engineLayerCaches struct {
	// ascend is checkBlessingGrants' incremental "could anything carry
	// Ascend" arena scan (rules/ascend.go); a pure cache, zero = rescan.
	ascend ascendScan

	// storied is checkEnduringStoryGrants' incremental "could anything carry
	// Storied" arena scan (rules/storied.go); a pure cache, zero = rescan.
	storied storiedScan

	// staticContinuous memoizes the S:Mode$ Continuous statics on battlefield
	// permanents (layers.go's staticEffects), keyed on staticEpoch. staticEpoch
	// is the log length at the last build; the memo refreshes once per emitted
	// event rather than once per Derived() call, because most events
	// (Priority/Damage/Mana) leave the battlefield permanent set untouched
	// while Derived is the hottest path in a turn (every legal action, combat
	// step and trigger predicate reads it). Clone() leaves both fields zero, so
	// a cloned engine rebuilds the memo on its first Derived -- staticEffects
	// is a pure function of the current board, so the rebuilt result is
	// identical and deterministic. Rebuilds reuse the outer slice's capacity,
	// clearing obsolete slots when it shrinks, but never reuse the nested
	// keyword/type slices. activeBuf copies the effect values into distinct
	// storage before sorting; neither buffer may alias a clone's scratch.
	staticContinuous []ContinuousEffect
	staticEpoch      int
	// staticVersion/staticObjs are continuousVersion and len(e.G.Objs) at the
	// last staticEffects build: layerInertSince's reuse across a run of
	// layer-inert events (layercache.go) additionally requires both unchanged.
	staticVersion int
	staticObjs    int
	// staticMemoGated records whether the last full staticEffects build
	// encountered any Continuous static carrying a continuousGateKeys param
	// (IsPresent$/IsPresent2$/Condition$/CheckSVar$/ClassBand$), whether or
	// not the gate currently passes. A gate that passes now can be flipped
	// off by a later battlefield-composition change (a token entering), so
	// the re-stamp admission in layercache.go's staticSafeSince must be
	// refused whenever this is true: only a gate-free build's output is
	// invariant under a static-cold token entry. Reset at the top of each
	// full staticEffectsWalk and set at the one gate site.
	staticMemoGated bool
	// staticBuildSeq counts staticEffects REBUILDS (never a layer-inert
	// re-stamp or an exact hit). The memo is refreshable OUTSIDE active() --
	// staticControlWants (control_static.go) calls refreshStaticContinuous
	// directly -- so the sorted activeBuf can be left describing an older
	// static list at the same log head and continuousVersion. activeStaticSeq
	// records the value active() built its buffer with, and both of active()'s
	// hit paths require the pair to match, exactly as the Derived memo keys on
	// activeBuildSeq. Never cloned: a clone's zero value rebuilds both.
	staticBuildSeq  uint64
	activeStaticSeq uint64

	// sbaQuiet is the state-based-action quiet key (rules/sbaquiet.go): the
	// board at which the last checkStateBased pass loop applied nothing.
	// sbaUnquiet is that loop's scratch flag for a no-op that depended on a
	// non-event input. Clone() leaves both zero, so a clone never skips its
	// first pass loop.
	sbaQuiet   sbaQuietKey
	sbaUnquiet bool

	// staticQueueBuf is staticEffects' AddStaticAbility$ work queue's reused
	// backing array: truncated to zero at every scan, grown only when a
	// static-grant fires (the warm-rescan allocation budget,
	// static_effects_buffer_test, is why it is reused rather than re-made).
	// Per-scan scratch, never cloned: a clone starts nil and grows its own.
	staticQueueBuf []staticWork

	// activeBuf is the cached, fully CR-613-sorted result of layers.go's
	// active(), the effect list every Derived() call ranges over for every
	// object of every board build and projection. Rebuilding that sorted list
	// once per emitted event instead of once per Derived() call is the whole
	// saving here -- active() used to allocate a fresh slice per call, and
	// Derived is the hottest path in a turn. The cache key is the pair
	// (activeEpoch, activeVersion): activeEpoch is the log length at the last
	// build and activeVersion the continuousVersion (bumped by AddContinuous
	// and EndOfTurnCleanup), because the effect list is a pure function of the
	// current board plus e.continuous, and those are exactly the two inputs
	// the key captures -- every board change moves the log head (emit), and
	// e.continuous changes through exactly the two mutators above. activeDepth
	// is a re-entry guard (Task A2's forEachObject pattern): it lets a nested
	// Derived (HasKeyword inside a MatchesSpecFrom) atomically share the
	// cached list and, on the never-happens-in-practice rebuild-mid-range
	// path, build a private list instead of clobbering the outer call's.
	// Clone() copies none of these fields (see clone.go); a cloned engine
	// starts with a zero key and rebuilds identically on its first Derived.
	activeBuf []ContinuousEffect
	// activeKWHeads is the deduplicated KeywordHead of every AddKeywords
	// entry across activeBuf, rebuilt with it (layers.go's active()) and read
	// by keywordmay.go's exact Derived-keyword precheck. Never cloned, like
	// activeBuf: a clone's zero key rebuilds both together.
	activeKWHeads []string
	// activeBuildSeq counts active()'s REBUILDS (never its exact or
	// layer-inert hits). derivedmemo.go's cross-walk reuse keys on it: an
	// unchanged count means no non-inert event, continuous-registry write,
	// object-count change or explicit invalidation has reached active() since.
	// Never cloned: a clone starts at zero with an empty memo.
	activeBuildSeq uint64
	// derivedSeq is the Derived memo's cross-walk key (derived_transparent.go):
	// it moves with activeBuildSeq except across a rebuild that provably left
	// every derivation unchanged. activeBufAlt is the other half of activeBuf's
	// double buffer (the previous build's list, kept intact so the next
	// rebuild can be compared with it), and derivedPrev* the key the previous
	// build (or layer-inert re-stamp) was taken at. Never cloned: a clone's
	// zero values make its first build move derivedSeq off zero.
	derivedSeq         uint64
	activeBufAlt       []ContinuousEffect
	derivedPrevEpoch   int
	derivedPrevVersion int
	derivedPrevObjs    int
	activeEpoch        int
	activeVersion      int
	activeDepth        int
	// activeObjs is len(e.G.Objs) at the last active() build, read only by
	// the layer-inert reuse (layercache.go).
	activeObjs int
	// goadProbe is the static-goad derivation's re-entry guard (staticgoad1):
	// staticallyGoaded matches each candidate's Affected$ spec through
	// matchesSpec, and a spec that itself consults the IsGoaded predicate
	// would derive the set again — an infinite walk. While the counter is
	// above zero the IsGoaded binding in matchesSpec stands down and the
	// predicate answers the event-backed half alone, so a (hypothetical)
	// IsGoaded-conditioned goad static degrades instead of looping. Never
	// cloned (clone.go copies none of the derivation caches).
	goadProbe int
	// renames is the layer-3 rename table (setname.go) the effects tier's
}
