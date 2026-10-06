package rules

import (
	"fmt"
	"sync"

	"github.com/adams-shaun/gorge/cards"
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
//
// A build that was also state-read-free (staticMemoStateRead false) admits
// more: the static-quiet kinds (staticQuietKinds), zone moves of objects that
// are static-cold on both sides (staticMoveCold), library reorders of
// libraries no effect can come from (staticLibraryOrderCold) and the
// TriggerPush / AbilityPush mints, whose new objects are face-less (the appended-object
// loop skips a face-less object; the scan skips it in every zone). A
// layer-inert run is admitted even on a gated build, exactly as
// layerInertSince admits it for active().
//
// gatesRechecked is the gated build's admission (static_gatememo.go): the
// caller re-evaluates the recorded gates before re-stamping, so a gated but
// otherwise state-read-free build is held to the quiet rules alone -- the
// gates are the only input the quiet kinds, a cold move or a static-cold
// token's arrival can reach, and the re-check covers them.
//
// The result also says whether the run was layer-inert throughout (the
// re-stamp a gated build admits without any re-check) and which kinds it
// held (the gate re-check's dependency filter).
func (e *Engine) staticSafeSince(epoch, oldObjs int, gatesRechecked bool) staticRun {
	n := len(e.L.Events)
	if epoch <= 0 || epoch > n || oldObjs < 0 || oldObjs > len(e.G.Objs) {
		return staticRun{}
	}
	// quiet: the build read nothing outside the static-quiet input (see
	// staticQuietKinds) -- or read it only through gates the caller
	// re-checks -- so the wider admissions below apply.
	quiet := e.staticMemoQuiet() || (gatesRechecked && !e.staticMemoStateRead)
	gateOpen := e.staticMemoGated && !gatesRechecked
	run := staticRun{inert: true}
	creates := 0
	for i := epoch; i < n; i++ {
		ev := &e.L.Events[i]
		k := ev.Kind
		switch k {
		case events.DecisionAsk, events.DecisionMade, events.Priority:
			// Layer-inert (layerInertSince): no gate reads them either.
			continue
		case events.TokenCreate:
			if gateOpen {
				return staticRun{}
			}
			creates++
		case events.TriggerPush, events.AbilityPush:
			// Each mints one face-less ability object onto the stack (checked
			// below); the scan skips a face-less object in every zone.
			if !quiet {
				return staticRun{}
			}
			creates++
		case events.MoveZone, events.Draw, events.PutOnStack:
			if !quiet || !e.staticMoveCold(ev.Obj) {
				return staticRun{}
			}
		case events.LibraryOrder, events.Shuffle:
			if !quiet || !e.staticLibraryOrderCold(ev) {
				return staticRun{}
			}
		case events.Imprint, events.Choose:
			// Object-local folds (static_memo_admit.go): admitted when the
			// object cannot contribute to the scan where it sits.
			if !quiet || !e.staticMoveCold(ev.Obj) {
				return staticRun{}
			}
		default:
			if !quiet || !staticQuietKinds.has(k) {
				return staticRun{}
			}
		}
		run.inert = false
		run.seen[k>>6] |= 1 << (k & 63)
	}
	if len(e.G.Objs)-oldObjs != creates {
		return staticRun{}
	}
	for i := oldObjs; i < len(e.G.Objs); i++ {
		o := &e.G.Objs[i]
		if o.Face() == nil {
			continue
		}
		if o.Zone != state.ZBattlefield || objectContinuousHot(o, state.ZBattlefield) {
			return staticRun{}
		}
	}
	run.ok = true
	return run
}

// staticRun is staticSafeSince's verdict on the event run since the memo's
// last stamp: ok when the admission holds, inert when every event in it was
// layer-inert, and seen the set of the other kinds it held.
type staticRun struct {
	ok, inert bool
	seen      kindSet
}

// staticMemoQuiet reports whether the memo's last full build made no read
// outside the static-quiet input (see staticQuietKinds): no continuous gate,
// no spec match, CDA count or imprint lookup.
func (e *Engine) staticMemoQuiet() bool { return !e.staticMemoGated && !e.staticMemoStateRead }

// invalidateScratchLayerLists drops active()'s two log-head-keyed lists
// around a read under an exclusion scratch (costCompositionEvent,
// stackGrantCast, an expired casualty grant): those scratches change only
// what a continuous gate (a CheckSVar$ cast count, an EQ0 grant) evaluates,
// and the static memo is the one active() input that evaluates gates at
// build. A quiet build evaluated none -- its list, and the active list built
// from it plus the gate-free registered effects, is the same under every
// scratch -- so it is kept; any other build (or one whose flags are not yet
// known) is dropped exactly as before. Callers that also need the
// cross-walk Derived memo retired still call retireCrossWalkMemo.
//
// A gated build whose every gate has a known read set (staticGatesScratchProof:
// the active player, a life total) is kept too: no scratch reaches those.
func (e *Engine) invalidateScratchLayerLists() {
	if e.staticEpoch <= 0 || !e.staticMemoQuiet() && !e.staticGatesScratchProof() {
		e.activeEpoch, e.staticEpoch = -1, -1
	}
}

// staticMoveCold reports whether a zone move of id cannot change a quiet
// (gate-free, state-read-free) static scan: the object contributed no effect
// to the memo where it was (no entry names it as Source) and can contribute
// none where it is now (objectContinuousHot for its current zone: on the
// battlefield no face, copied face or merged card it can resolve to carries
// a Continuous static; elsewhere none carries one that can function off the
// battlefield -- the scan's own off-battlefield gate, the stack included).
// A quiet
// scan reads nothing else a move writes: the move's other folds (the
// departing permanent's own battlefield state, soulbond and crew links,
// zone lists) feed only gates, spec matches and the static zone summaries,
// and the summaries are re-checked by the next real rescan.
func (e *Engine) staticMoveCold(id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || objectContinuousHot(o, o.Zone) {
		return false
	}
	for i := range e.staticContinuous {
		if e.staticContinuous[i].Source == id {
			return false
		}
	}
	return true
}

// staticLibraryOrderCold reports whether a library reorder (LibraryOrder or
// Shuffle: Apply writes only the player's library list) cannot change a
// quiet static scan. The scan reads the list only to visit its objects in
// order, so the list's order and membership matter only through an object
// that contributes, or could contribute, an effect from the library: none
// of the listed objects may be hot there (objectContinuousHot), and no
// memo entry may come from a library object -- a source that a reorder
// dropped from the list would otherwise vanish from the rescan. (An admitted
// later move never relocates a memo source: staticMoveCold refuses it, so a
// source in a library at the reorder is still there at this check.)
func (e *Engine) staticLibraryOrderCold(ev *events.Event) bool {
	for _, id := range ev.IDs {
		if o := e.G.Obj(id); o == nil || objectContinuousHot(o, state.ZLibrary) {
			return false
		}
	}
	for i := range e.staticContinuous {
		if o := e.G.Obj(e.staticContinuous[i].Source); o != nil && o.Zone == state.ZLibrary {
			return false
		}
	}
	return true
}

// staticQuietKinds are the event kinds whose Apply writes nothing a
// gate-free, state-read-free staticEffects scan reads. That scan's whole input
// is the seat list (AliveFrom), the static source zones' id lists, and per
// object its Card/FaceIdx/CopyFace (Face), FaceDown, PhasedOut, Unlocked,
// MergedCards, Zone, Controller, Timestamp, Chosen* fields and Imprinted.
// Every other read it can make -- a continuous gate, a GainsAbilitiesOf$ or
// AddStaticAbility$ spec match, a CDA count, an imprinted card's type line --
// sets staticMemoGated or staticMemoStateRead in the build, which refuses
// this admission. Each kind below folds only fields outside that input:
//
//   - Tap/Untap: o.Tapped.
//   - ManaAdd/ManaClear: a seat's mana pool and restriction batches.
//   - LifeChange: a seat's life. LandPlayed: a seat's land count.
//   - Damage: marked damage, damage-this-turn bookkeeping, loyalty/defense
//     counters, life, commander damage. DamageProvenance: a seat's or
//     object's damaged-by record.
//   - StepChange: g.Step, the combat-phase count, combat-persistent mana,
//     the YourLastCombat mode picks and combat stamps, LastUpkeepTurn.
//   - ClockTick: g.Clock (the scan reads an object's own Timestamp).
//   - DeclareAttackers/DeclareBlockers/EndCombatReset: attack and block
//     state. TargetsChosen: an object's Targets and copy-target bit.
//   - TurnChange: the turn number, active seat, per-turn object and seat
//     bookkeeping (summoning sickness, this-turn tallies, goads, this-turn
//     mode picks), the turn's Entered list and persistent-mana flags.
//   - Resolve: the per-ability ResolvedThisTurn tally.
//   - CounterChange/PlayerCounterChange: an object's or seat's counters
//     (read only by gates and CDA counts). StoreSVar: an object's runtime
//     SVars (read only by a CDA count). NoteNumber: an object's noted
//     number.
//   - Note, ModeChosen, ManaActivate, SearchedLibrary, Explore, Investigate,
//     Discover, Seek, Surveil, Scry, Proliferate, Evolved, GiveGift, Clash:
//     markers; Apply writes nothing.
//
// Zone moves (MoveZone/Draw/PutOnStack) and the two ability pushes are
// admitted separately in staticSafeSince: a move only of an object that is
// static-cold on both sides (staticMoveCold), a push because its new object
// is face-less.
//
// None of them adds an object, moves a zone list or changes a seat's Lost
// bit. layerInertVerify rescans on every re-stamp and panics on a
// difference, so the rules suite holds this list to the scan empirically.
var staticQuietKinds = newKindSet(events.Tap, events.Untap, events.ManaAdd, events.ManaClear,
	events.LifeChange, events.LandPlayed, events.Damage, events.DamageProvenance,
	events.StepChange, events.TurnChange, events.Resolve, events.ClockTick,
	events.DeclareAttackers, events.DeclareBlockers, events.EndCombatReset,
	events.TargetsChosen, events.Note,
	events.CounterChange, events.PlayerCounterChange, events.StoreSVar, events.NoteNumber,
	events.ModeChosen, events.ManaActivate, events.SearchedLibrary, events.Explore,
	events.Investigate, events.Discover, events.Seek, events.Surveil, events.Scry,
	events.Proliferate, events.Evolved, events.GiveGift, events.Clash, events.AbilityTriggered)

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
	//
	// A quiet build (staticMemoQuiet) reads nothing of the continuous
	// registry -- only a gate or a spec/count read reaches Derived and so
	// e.continuous -- so for it a registry move alone (an EndOfTurnCleanup
	// dropping a pump, an AddContinuous's ClockTick) does not stale the
	// memo either.
	//
	// A gated build whose only state reads are its gates (gatedQuiet,
	// static_gatememo.go) is held to the quiet rules instead: across a run
	// they admit it is current iff every recorded gate re-evaluates
	// unchanged. A layer-inert run still re-stamps it with no re-check, as
	// before. The gates read the registry only through Derived, so a
	// registry move is covered by the re-check as well.
	gatedQuiet := e.staticMemoGated && !e.staticMemoStateRead
	run := e.staticSafeSince(e.staticEpoch, e.staticObjs, gatedQuiet)
	if run.ok && (gatedQuiet && run.inert && e.staticVersion == e.continuousVersion ||
		!gatedQuiet && (e.staticVersion == e.continuousVersion || e.staticMemoQuiet())) {
		e.restampStatic(n)
		return
	}
	builds := e.activeBuildSeq
	if gatedQuiet && run.ok {
		// The epoch is stamped first, exactly as the rescan below stamps it
		// before its walk, so a gate's nested read sees the same memo
		// either way.
		e.staticEpoch = n
		if e.staticGatesUnchanged(&run.seen) {
			e.restampStatic(n)
			return
		}
	}
	e.staticEpoch = n
	e.staticVersion, e.staticObjs = e.continuousVersion, len(e.G.Objs)
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

// restampStatic re-keys the static memo at log head n without rescanning
// (the caller proved the rescan would reproduce it); layerInertVerify
// rescans and panics on a difference.
func (e *Engine) restampStatic(n int) {
	e.staticEpoch = n
	e.staticVersion, e.staticObjs = e.continuousVersion, len(e.G.Objs)
	if layerInertVerify {
		if fresh := e.staticEffects(nil); !continuousEffectsEqual(fresh, e.staticContinuous[:len(e.staticContinuous):len(e.staticContinuous)]) && !(len(fresh) == 0 && len(e.staticContinuous) == 0) {
			panic(fmt.Sprintf("rules: layer-inert static memo reuse at log %d disagrees with a rescan (%d vs %d effects)", n, len(e.staticContinuous), len(fresh)))
		}
	}
}

var verifyActivePool = sync.Pool{New: func() any { return new([]ContinuousEffect) }}

// verifyInertActive is active()'s layer-inert reuse check: it rebuilds the
// list from scratch under a forced miss and compares.
func (e *Engine) verifyInertActive() {
	// cached is a defensive copy of the served list (the forced rebuild is
	// given fresh storage, so it should never write it); its array comes
	// from a pool because this check runs on every reuse in the rules test
	// binary and the copies were gigabytes per suite run.
	cp := verifyActivePool.Get().(*[]ContinuousEffect)
	cached := append((*cp)[:0], e.activeBuf...)
	defer func() {
		clear(cached)
		*cp = cached[:0]
		verifyActivePool.Put(cp)
	}()
	savedEpoch, savedBuf := e.activeEpoch, e.activeBuf
	savedHeads, savedSeq := e.activeKWHeads, e.activeBuildSeq
	savedHeadSet, savedHeadSetOK := e.activeKWHeadSet, e.activeKWHeadSetOK
	savedStaticSeq := e.activeStaticSeq
	savedDerivedSeq, savedAlt := e.derivedSeq, e.activeBufAlt
	savedPrevEpoch, savedPrevVersion, savedPrevObjs := e.derivedPrevEpoch, e.derivedPrevVersion, e.derivedPrevObjs
	savedPrevEntered, savedBFSeq := e.derivedPrevEntered, e.derivedBFSeq
	savedList := e.activeList
	e.activeList = activeListKey{}
	// The forced rebuild must not write the served list's array (activeBuf)
	// nor the transparency baseline: give it fresh storage.
	e.activeBufAlt = nil
	e.activeEpoch, e.activeBuf, e.activeKWHeads = -1, nil, nil
	e.activeKWHeadSet, e.activeKWHeadSetOK = cards.KeywordHeadSet{}, false
	// A nested call would take the re-entrant private-buffer path; drop to
	// depth 0 so the forced rebuild is an ordinary outermost build.
	depth := e.activeDepth
	e.activeDepth = 0
	// The rebuilt list is the forced build's own fresh array, which nothing
	// else holds once the served state is restored below and nothing writes
	// before the comparison: compare it in place rather than copying it.
	fresh := e.active()
	e.activeDepth = depth
	// The forced rebuild is a check, not a rebuild of the served list: the
	// build count and head set stay the served list's (derivedmemo.go's
	// cross-walk reuse keys on the count).
	e.activeEpoch, e.activeBuf = savedEpoch, savedBuf
	e.activeKWHeads, e.activeBuildSeq = savedHeads, savedSeq
	e.activeKWHeadSet, e.activeKWHeadSetOK = savedHeadSet, savedHeadSetOK
	e.activeStaticSeq = savedStaticSeq
	e.derivedSeq, e.activeBufAlt = savedDerivedSeq, savedAlt
	e.derivedPrevEpoch, e.derivedPrevVersion, e.derivedPrevObjs = savedPrevEpoch, savedPrevVersion, savedPrevObjs
	e.derivedPrevEntered, e.derivedBFSeq = savedPrevEntered, savedBFSeq
	e.activeList = savedList
	if len(cached) != len(fresh) || (len(cached) > 0 && !continuousEffectsEqual(cached, fresh)) {
		panic(fmt.Sprintf("rules: layer-inert active() reuse at log %d disagrees with a rebuild (%d vs %d effects)", len(e.L.Events), len(cached), len(fresh)))
	}
}
