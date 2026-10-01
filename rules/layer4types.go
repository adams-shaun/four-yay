package rules

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// This file is the ONE bridge between rules' layer walk and the effects tier's
// ORDINARY type comparisons (a filter spec's bases and type predicates, as
// reached by a target offer, cost site, Count$Valid census or CantTarget spec).
//
// A layer-4 effect (CR 613.1d/613.1c: AddTypes$, RemoveCardTypes$,
// RemoveCreatureTypes$, AddAllCreatureTypes$, a face-down CR 708.5 set) changes
// an object's types, and only the layer walk can say which effect applies and
// which of two competing effects wins on timestamp. The layer walk already
// answers its OWN Affected$ match with a types-so-far list (SpecContext.
// ExtraTypes). But every filter OUTSIDE the walk -- the target offer a
// Goblin-killer's ValidTgts$ Goblin drives, a cost site's candidate census, a
// Count$Valid, a CantTarget -- read the printed face plus Changeling's CDA, so
// a creature a static made a Goblin was counted by a lord but not targetable
// by the Goblin-killer.
//
// The effects tier cannot import rules and must not re-derive any of that from
// a battlefield scan. So rules computes the derived type list itself and hands
// it down as DATA on the SpecContext the filter call already carries -- the
// same shape setname.go uses for layer-3 names and SpecContext.ExtraTypes
// uses for the walk's per-object types: a plain value slice, deliberately not
// a callable resolver field (which poisons escape analysis) and never a
// back-pointer from state.Game into a live engine (which would make an
// authoritative Game depend on something outside its event fold, and would
// survive a Game.Clone aimed at a different board).
//
// The table is a FIELD, refreshed after each emitted event, and specCtxSVars
// binds it with a plain field read. That is load-bearing, not a style choice:
// calling a function from inside specCtxSVars pushes it over the inline budget,
// its Resolve closure then escapes, and every SpecContext construction on the
// Derived/legal-actions hot path allocates
// (TestDerivedWithContinuousEffectsDoesNotAllocate,
// TestLegalActionsReusesActionStaticMembership). The refresh itself is gated on
// layer4InPool so a match whose cards carry no type-changing script -- most
// matches -- pays one predictable branch per event and nothing else, and it
// short-circuits on anyLayer4Active, whose precheck answers from a per-face
// derived probe and the registered-effect list without rebuilding active()
// at all, so a carrier still in a library costs one object walk. A run of
// priority bookkeeping events reuses the table outright (layercache.go).
//
// The entries are only the objects whose DERIVED list differs from the printed
// face (a handful at most, nil on the overwhelmingly common board), so the
// filter tier's linear scan is cheaper than building a map and an unchanged
// object keeps the compiled-predicate fast path (effects/filter.go's
// hasDerivedTypeEntry).

// poolHasLayer4Static reports whether any card this match can put on the
// battlefield prints a layer-4 type-changing effect. Computed once, at
// genesis, over the decks and the token table: a card outside the pool can
// never register one of these effects from its printed text. The per-card
// probe is cards.ChangesTypes -- card-data introspection, deliberately not a
// statics-parameter read on a rules path (rules/paramcensus_test.go attributes
// those to the primitive whose activeStatics root reaches them, and this probe
// has no such root because it runs before any board exists).
func poolHasLayer4Static(cfg Config) bool {
	for _, deck := range cfg.Decks {
		for _, c := range deck {
			if c.ChangesTypes() {
				return true
			}
		}
	}
	for _, c := range cfg.Tokens {
		if c.ChangesTypes() {
			return true
		}
	}
	return false
}

// refreshDerivedTypes recomputes the derived-type table if the board or the
// continuous effects moved since the last build. The key is active()'s own --
// the log head plus the continuous-effect version -- because the derived type
// set is a pure function of exactly those two inputs.
//
// Between whole-board rebuilds the refresh is INCREMENTAL (the cardfuzz
// bigboard class, seed 6181111140895991800: with a layer-4 type effect live,
// every emitted event -- a Krenko token flood beside a crewed Clown Car --
// paid two whole-board passes, the staticsMayChangeTypes face probe and the
// walk's candidate scan). The incremental rebuild never scans e.G.Objs:
//
//   - the staticsMayChangeTypes precheck is served from a per-object probe
//     cache (staticsProbeCatchUp), maintained from the same event referents
//     the trigger-walk skip reads;
//   - the candidate set is the self-only source list plus typesMayDiffer, a
//     sorted slice of the battlefield objects whose layer-4 BASE can differ
//     from the printed face, maintained the same way (typesCatchUp);
//   - the build re-walks exactly that (small) candidate set and sorts the
//     few entries back into e.G.Objs order (a dense arena: Objs[i].ID ==
//     i+1), so the table is byte-identical to the whole-board walk's.
//
// The argument that the catch-up sees every relevant change is the
// trigger-walk skip's (trigger_zoneskip.go): every field the candidacy test
// and the probe read -- Zone, Card, FaceIdx, CopyFace, Unlocked, MergedCards,
// FaceDown, CopyNonLegendary, AttachedTo -- is written only inside
// events.Apply and keyed to that event's Obj/IDs/Pairs (the one exception,
// offerAsFace's scoped FaceIdx flip in faceprobe.go, brackets a pure read
// that emits no event and refreshes before flipping, so no refresh with a
// moved key ever runs inside it). Objects APPENDED while events advance
// the log are integrated unconditionally, even if a test helper appended
// one before the event. An eventless AddObject with no intervening event
// forces a full rebuild instead, and
// the source list is re-derived fresh from e.continuous every refresh, so a
// liveness flip (a source leaving the battlefield, a DurationSource moving,
// an UntilEndOfCombat/UntilTurn boundary) either changes the source list
// against its stamp -- full rebuild -- or changes nothing the table reads.
// Every uncertainty falls back to the whole-board rebuild: a
// continuousVersion bump, a negative or rewound epoch (onBoard's eventless
// staleness, a fresh engine), or the source stamp moving. The fallback is
// the pre-incremental behaviour, so the fast path can only ever get MORE
// conservative, never less.
//
// layer4PrecheckVerify re-derives the whole board on every incremental build
// and panics on any disagreement, so the whole rules suite doubles as the
// empirical check of the argument above.
func (e *Engine) refreshDerivedTypes() {
	if e.typesBuilding {
		// Re-entry: a Derived/typeCharacteristics call below reached something
		// that re-emitted. The half-built table must never be published; the
		// outer call finishes it.
		return
	}
	n := len(e.L.Events)
	// The key also carries len(e.G.Objs): a test helper (or any caller) may
	// place a permanent with a direct AddObject -- no event, so the log head
	// would not move -- and the table must not stay a stale cache hit. An
	// emitted move changes neither the object count nor the derived list of
	// any other object, so this adds no work on the ordinary path.
	if e.typesEpoch == n && e.typesVersion == e.continuousVersion && e.typesObjs == len(e.G.Objs) {
		return
	}
	// Layer-inert reuse (layercache.go): only priority bookkeeping moved the
	// log since the table was built, so the table is still exact. Those
	// events write no object field and append no object, so the incremental
	// state (mayDiffer, probe cache) is untouched too; the probe cache keeps
	// its own epoch and catches up on its next read.
	if e.typesVersion == e.continuousVersion && e.typesObjs == len(e.G.Objs) && e.layerInertSince(e.typesEpoch) {
		e.typesEpoch = n
		if layerInertVerify {
			e.verifyInertDerivedTypes()
		}
		return
	}
	// A direct test-helper AddObject with no logged event invalidates the
	// object-count guard. Rebuild the entire board rather than treating an
	// eventless arena change as an ordinary emitted change. When an event
	// DOES advance the key, typesCatchUp also integrates every appended
	// object, including one added directly before that event.
	if e.typesObjs != len(e.G.Objs) && n == e.typesEpoch {
		e.refreshDerivedTypesFull(n)
		return
	}
	// The incremental rebuild. Everything it cannot prove locally falls
	// through to the whole-board rebuild below.
	if e.typesIncrReady && e.typesVersion == e.continuousVersion && e.typesEpoch >= 0 && n >= e.typesEpoch &&
		e.refreshDerivedTypesIncremental(n) {
		return
	}
	e.refreshDerivedTypesFull(n)
}

// refreshDerivedTypesFull is the whole-board rebuild (the pre-incremental
// behaviour), plus the incremental state it repopulates: the mayDiffer
// candidate slice and the stamps the next incremental attempt needs.
func (e *Engine) refreshDerivedTypesFull(n int) {
	var arr [layer4MaxSelfSources]state.ObjID
	srcs, selfOnly := e.layer4SelfOnlySources(arr[:0])
	e.stampTypes(n, srcs, selfOnly)
	e.layer4Types = e.buildDerivedTypes(e.layer4Types[:0])
	e.typesVisited = len(e.G.Objs)
	e.typesMayDifferScan()
	e.typesIncrReady = true
}

// stampTypes records the key and the self-only source stamp under which the
// table (or the incremental state) was built. The stamp is what lets the
// next refresh decide whether the source list moved: layer4SelfOnlySources
// is re-derived fresh every refresh, so ANY liveness change among the live
// layer-4 effects -- a source zone move, a DurationSource move, an
// UntilEndOfCombat or UntilTurn boundary flipping continuousLive, the
// statics precheck's answer -- changes the re-derived (srcs, selfOnly) pair
// against this stamp and forces the whole-board rebuild.
func (e *Engine) stampTypes(n int, srcs []state.ObjID, selfOnly bool) {
	e.typesEpoch, e.typesVersion, e.typesObjs = n, e.continuousVersion, len(e.G.Objs)
	e.typesSelfOnly = selfOnly
	e.typesSrcs = append(e.typesSrcs[:0], srcs...)
}

// refreshDerivedTypesIncremental rebuilds the table from the incremental
// state without scanning e.G.Objs. It returns false when the incremental
// argument does not cover this refresh; the caller then does the whole-board
// rebuild (the conservative direction). The catch-up runs first: the
// mayDiffer slice must reflect the events logged since the last build
// before the build reads it.
func (e *Engine) refreshDerivedTypesIncremental(n int) bool {
	e.typesCatchUp(n)
	var arr [layer4MaxSelfSources]state.ObjID
	srcs, selfOnly := e.layer4SelfOnlySources(arr[:0])
	if !selfOnly || !slices.Equal(srcs, e.typesSrcs) {
		// A non-self-shaped live layer-4 effect, or the source list moved:
		// only the whole-board walk can say which objects the effects reach.
		return false
	}
	e.stampTypes(n, srcs, selfOnly)
	e.typesIncrBuilds++
	if len(srcs) == 0 {
		// No live registered layer-4 effect and the statics precheck found
		// none: anyLayer4Active is false and buildDerivedTypes answers an
		// empty table regardless of what the candidates' bases look like.
		e.layer4Types = e.layer4Types[:0]
		if layer4PrecheckVerify {
			if fresh := e.buildDerivedTypes(nil); len(fresh) != 0 {
				panic(fmt.Sprintf("rules: incremental layer-4 table (no live effect) at log %d is empty but a rebuild holds %d entries", n, len(fresh)))
			}
		}
		return true
	}
	e.layer4Types = e.buildDerivedTypesIncremental()
	if layer4PrecheckVerify {
		e.verifySelfOnlyDerivedTypes(e.layer4Types)
	}
	return true
}

// typesCatchUp folds the events logged since the table was last built into
// the incremental state: every object an event references (ev.Obj, ev.IDs,
// ev.Pairs -- exactly the referent set trigZonesCatchUp reads) plus every
// object appended since the last build, re-tested for layer4BaseMayDiffer
// membership. Returns the touched id list (the engine's reusable buffer).
func (e *Engine) typesCatchUp(n int) []state.ObjID {
	touch := e.typesTouch[:0]
	for i := e.typesEpoch; i < n; i++ {
		ev := &e.L.Events[i]
		if ev.Obj != 0 {
			touch = append(touch, ev.Obj)
		}
		for _, id := range ev.IDs {
			if id != 0 {
				touch = append(touch, id)
			}
		}
		for _, pr := range ev.Pairs {
			if pr[0] != 0 {
				touch = append(touch, pr[0])
			}
			if pr[1] != 0 {
				touch = append(touch, pr[1])
			}
		}
	}
	for i := e.typesObjs; i < len(e.G.Objs); i++ {
		touch = append(touch, e.G.Objs[i].ID)
	}
	e.typesTouch = touch
	for _, id := range touch {
		e.typesMayDifferSet(id, layer4MayDifferNow(e.G.Obj(id)))
	}
	return touch
}

// typesMayDifferSet inserts or removes id from the sorted candidate slice
// to match member. Idempotent, so a duplicated touch costs a binary search.
func (e *Engine) typesMayDifferSet(id state.ObjID, member bool) {
	i, found := slices.BinarySearch(e.typesMayDiffer, id)
	if found == member {
		return
	}
	if member {
		e.typesMayDiffer = slices.Insert(e.typesMayDiffer, i, id)
	} else {
		e.typesMayDiffer = slices.Delete(e.typesMayDiffer, i, i+1)
	}
}

// typesMayDifferScan rebuilds the candidate slice from the whole board (the
// whole-board rebuild's own repopulation, and what every fallback resets it
// to). Objs order is id order, so the append is already sorted.
func (e *Engine) typesMayDifferScan() {
	list := e.typesMayDiffer[:0]
	for i := range e.G.Objs {
		if layer4MayDifferNow(&e.G.Objs[i]) {
			list = append(list, e.G.Objs[i].ID)
		}
	}
	e.typesMayDiffer = list
}

// buildDerivedTypesIncremental is the incremental build: the candidate set
// (the maintained mayDiffer slice plus the fresh self-only source list)
// re-walked and sorted back into e.G.Objs order. It walks only the few
// candidates -- never the board -- and produces exactly the whole-board
// self-only walk's table: same candidates, same per-candidate
// typeCharacteristics, and a dense arena's id order equals its Objs order.
func (e *Engine) buildDerivedTypesIncremental() []effects.ObjectTypes {
	buf := e.layer4Types[:0]
	e.typesBuilding = true
	defer func() { e.typesBuilding = false }()
	// Apply only the live registered LType effects: layer4SelfOnlySources has
	// already proved staticsMayChangeTypes() false, so no static-derived
	// effect can change a type and the memoized static scan cannot contribute
	// an LType effect. Passing the filtered list to typeCharacteristicsActive
	// skips active()'s whole-board static rescan (the cardfuzz bigboard's
	// residual linear term) while producing the identical table; the verify
	// path compares every such build against the typeCharacteristics full
	// walk.
	act := e.liveLTypeEffects(e.typesAct[:0])
	e.typesAct = act
	e.typesVisited = len(e.typesMayDiffer) + len(e.typesSrcs)
	for _, id := range e.typesMayDiffer {
		buf = e.appendDerivedEntry(buf, act, id)
	}
	for _, id := range e.typesSrcs {
		buf = e.appendDerivedEntry(buf, act, id)
	}
	slices.SortFunc(buf, func(a, b effects.ObjectTypes) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	// A source that is also a mayDiffer member was walked twice; the sorted
	// run of equal ids collapses to one entry.
	out := buf[:0]
	for i := range buf {
		if i > 0 && buf[i].ID == buf[i-1].ID {
			continue
		}
		out = append(out, buf[i])
	}
	return out
}

// appendDerivedEntry walks one candidate and appends its table entry, if
// its derived types differ from its printed face (the same test the whole
// walk applies, in the same order: zone and face first). act is the live
// LType effect list typeCharacteristicsActive applies.
func (e *Engine) appendDerivedEntry(buf []effects.ObjectTypes, act []ContinuousEffect, id state.ObjID) []effects.ObjectTypes {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		return buf
	}
	ty := e.typeCharacteristicsActive(act, id, 0)
	if sameTypeWordSet(ty, o.Face().Types) {
		return buf
	}
	// The list may alias a scratch buffer or the face's own slice, so copy
	// it into the table (only for the few changed objects).
	return append(buf, effects.ObjectTypes{ID: id, Types: append([]string(nil), ty...)})
}

// liveLTypeEffects returns the live registered layer-4 effects, in the same
// relative order active() gives them: active() stable-sorts the live
// registered effects plus the static memo by (Layer, Sub, Timestamp) with a
// layer-6 removal-before-grant tie-break, so within one layer the order is
// (Sub, Timestamp) stable. The layer-6 tie-break cannot apply at LType. A
// caller that has proved no static can change a type may apply this list
// instead of active() and skip the static memo rescan.
func (e *Engine) liveLTypeEffects(dst []ContinuousEffect) []ContinuousEffect {
	dst = dst[:0]
	for i := range e.continuous {
		ce := &e.continuous[i]
		if ce.Layer == LType && e.continuousLive(ce) {
			dst = append(dst, *ce)
		}
	}
	slices.SortStableFunc(dst, func(a, b ContinuousEffect) int {
		if a.Sub != b.Sub {
			if a.Sub < b.Sub {
				return -1
			}
			return 1
		}
		if a.Timestamp != b.Timestamp {
			if a.Timestamp < b.Timestamp {
				return -1
			}
			return 1
		}
		return 0
	})
	return dst
}

// buildDerivedTypes builds the derived-type table for the current board into
// buf (truncated) and returns it.
//
// The table is rebuilt after every emitted event while a layer-4 effect is
// live, so its cost is paid per event. The full walk runs the layer-4 match
// of every live type effect against every battlefield object; a board of
// thousands of tokens beside one crewed Vehicle (cardfuzz seed
// 6181111140895991800: Clown Car's crew effects plus a Krenko doubling) made
// each token creation and each attacker's tap O(battlefield) in that match,
// O(battlefield^2) per resolution. So when every live layer-4 effect is a
// registered `Affected$ Card.Self` effect (layer4SelfOnlySources), only the
// objects whose derived types CAN differ from their face are run through the
// walk: those effects' sources, plus the objects whose type base is already
// not the printed face (layer4BaseMayDiffer). Every other object has no
// layer-4 effect applying to it and its printed base, so the full walk would
// skip it by sameTypeWordSet anyway. The fast path's table is therefore the
// full walk's exactly -- the rules test binary rebuilds it with the full walk
// on every fast-path build (layer4PrecheckVerify) and panics on a difference.
func (e *Engine) buildDerivedTypes(buf []effects.ObjectTypes) []effects.ObjectTypes {
	if !e.anyLayer4Active() {
		return buf[:0]
	}
	var arr [layer4MaxSelfSources]state.ObjID
	srcs, selfOnly := e.layer4SelfOnlySources(arr[:0])
	if !selfOnly {
		return e.buildDerivedTypesWalk(buf, nil, false)
	}
	buf = e.buildDerivedTypesWalk(buf, srcs, true)
	if layer4PrecheckVerify {
		e.verifySelfOnlyDerivedTypes(buf)
	}
	return buf
}

// buildDerivedTypesFull is buildDerivedTypes without the self-only fast path
// (every battlefield object through the layer-4 match): the reference the
// fast path's tests compare against.
func (e *Engine) buildDerivedTypesFull(buf []effects.ObjectTypes) []effects.ObjectTypes {
	if !e.anyLayer4Active() {
		return buf[:0]
	}
	return e.buildDerivedTypesWalk(buf, nil, false)
}

// layer4MaxSelfSources bounds the self-only fast path's source list: its
// membership test is a linear scan, so past a handful of sources the full
// walk runs instead.
const layer4MaxSelfSources = 8

// layer4SelfOnlySources reports whether every layer-4 effect active() can
// hold is a registered, live `Affected$ Card.Self` effect, and if so returns
// their distinct sources (appended to buf) in first-registration order.
// active()'s layer-4 effects are the live registered ones plus whatever
// staticEffects emits; staticsMayChangeTypes false proves the latter is none
// (the conservative precheck anyLayer4Active already relies on). A Card.Self
// spec matches exactly the object whose ID is the effect's Source (effects'
// "Self" predicate), so no other object can be reached by these effects.
func (e *Engine) layer4SelfOnlySources(buf []state.ObjID) ([]state.ObjID, bool) {
	for i := range e.continuous {
		ce := &e.continuous[i]
		if ce.Layer != LType || !e.continuousLive(ce) {
			continue
		}
		if ce.Source == 0 || ce.Affects != "Card.Self" {
			return buf, false
		}
		if slices.Contains(buf, ce.Source) {
			continue
		}
		if len(buf) == layer4MaxSelfSources {
			return buf, false
		}
		buf = append(buf, ce.Source)
	}
	if e.staticsMayChangeTypes() {
		return buf, false
	}
	return buf, true
}

// layer4BaseMayDiffer reports whether a battlefield object's layer-4 BASE
// (typeCharacteristics before any effect applies) can differ from its
// printed face types: a face-down permanent's CR 708.5 set, a
// CopyNonLegendary copy's stripped list, and an attached Bestow or
// Reconfigure card's creature-type switch (both key on AttachedTo). A
// deliberately cheap superset -- field reads only -- so the self-only path
// sends every such object through the real walk.
func layer4BaseMayDiffer(o *state.Object) bool {
	return o.FaceDown || o.CopyNonLegendary || o.AttachedTo != 0
}

// layer4MayDifferNow is layer4BaseMayDiffer under the battlefield/face gates
// the walk itself applies to every candidate. It is the maintained
// membership test of the incremental build's candidate slice.
func layer4MayDifferNow(o *state.Object) bool {
	return o != nil && o.Zone == state.ZBattlefield && o.Face() != nil && layer4BaseMayDiffer(o)
}

// buildDerivedTypesWalk is the table walk. With selfOnly set it visits only
// srcs and the layer4BaseMayDiffer objects (see buildDerivedTypes).
func (e *Engine) buildDerivedTypesWalk(buf []effects.ObjectTypes, srcs []state.ObjID, selfOnly bool) []effects.ObjectTypes {
	buf = buf[:0]
	e.typesBuilding = true
	defer func() { e.typesBuilding = false }()
	// e.G.Objs is append-ordered, so this walk is deterministic; only the
	// battlefield is scanned because a layer effect applies nowhere else.
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield || o.Face() == nil {
			continue
		}
		if selfOnly && !layer4BaseMayDiffer(o) && !slices.Contains(srcs, o.ID) {
			continue
		}
		ty := e.typeCharacteristics(o.ID, 0)
		if sameTypeWordSet(ty, o.Face().Types) {
			continue
		}
		// The list may alias a scratch buffer or the face's own slice, so copy
		// it into the table (only for the few changed objects).
		buf = append(buf, effects.ObjectTypes{ID: o.ID, Types: append([]string(nil), ty...)})
	}
	return buf
}

// anyLayer4Active reports whether any active continuous effect changes a type.
// Without this gate every refresh would pay for a full battlefield type walk on
// a board whose type-changing carrier is still in a library.
func (e *Engine) anyLayer4Active() bool {
	// A live registered layer-4 effect is in active() by construction (the
	// same continuousLive filter admits it there), so the answer is yes
	// without rebuilding the list.
	for i := range e.continuous {
		if e.continuous[i].Layer == LType && e.continuousLive(&e.continuous[i]) {
			if layer4PrecheckVerify {
				e.verifyLayer4Active(true)
			}
			return true
		}
	}
	if !e.staticsMayChangeTypes() {
		if layer4PrecheckVerify {
			e.verifyLayer4Active(false)
		}
		return false
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Layer == LType {
			return true
		}
	}
	return false
}

// EffectiveTypes publishes the current layer-4 derived type table to the
// effects tier, which reads it once at the top of every effects.Resolve walk
// (effects' typeTableHost) and binds it on the resolving Ctx. That is what
// makes a resolving effect's own filter calls -- a target offer, a Count$Valid
// census, a CantTarget spec -- agree with the layer walk instead of reading
// the printed face. It is a plain value-slice read, never a live engine
// pointer: effects answer type filters during a resolution without a
// back-pointer on state.Game, and a cloned game cannot read another game's
// board. The table is refreshed by emit and continuousChanged, so it is
// current at Resolve entry.
func (e *Engine) EffectiveTypes() []effects.ObjectTypes { return e.layer4Types }

// sameTypeWordSet reports whether two type lists carry the same words,
// case-insensitively and ignoring order. It decides whether an object's derived
// types differ from its printed face and therefore need a table entry. The
// lists are tiny (a handful of words), so the quadratic scan is cheaper than
// building a set on this per-object refresh path.
func sameTypeWordSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		found := false
		for _, y := range b {
			if strings.EqualFold(x, y) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// layer4PrecheckVerify (set by the rules test binary, or at link time through
// layer4PrecheckVerifyFlag for a botbench run) re-derives active() whenever
// the fast path answers "no" and panics if it holds a layer-4 effect, so the
// whole rules suite checks the precheck's conservativeness empirically.
// go build -ldflags "-X github.com/adams-shaun/gorge/rules.layer4PrecheckVerifyFlag=1".
var layer4PrecheckVerifyFlag string

var layer4PrecheckVerify = layer4PrecheckVerifyFlag != ""

// staticsMayChangeTypes is anyLayer4Active's conservative precheck over the
// static half of active(): it returns false only when staticEffects' scan
// provably emits no LType effect, WITHOUT running that scan (which after
// every event re-walks every object's statics -- the single largest cost on
// the emit path when, as almost always, the answer is "no").
//
// staticEffects' ONLY LType emission is the AddType$/AddTypes$/
// AddAllCreatureTypes$/RemoveType$ branch (RemoveType$ stands alone on the
// devotion gods), reached from a face's own statics or from an
// AddStaticAbility$ grant on one, and only for a static whose zone gate
// admits the source's zone. The faces that scan walks are o.Face() for an
// object in a static-source zone of an alive seat, plus, on the battlefield,
// an unlocked Room's other face and a mutated pile's merged faces. The
// reference walk (staticsMayChangeTypesWalk) visits the same zone lists for
// every seat (a superset of the alive ones):
// the battlefield, the stack and the command zone in full, and the
// static-hot subsequence of each library, hand, graveyard and exile
// (static_zoneskip.go) -- a static-cold object answers false to
// StaticsMayChangeTypes(false) on every face it could resolve to, so leaving
// it out loses no "yes". It checks o.Face() under its zone class, and on the
// battlefield both faces of any unlocked two-faced card (a superset of the
// Room condition) and every merged face. It ignores face-down hiding and
// every IsPresent$/CheckSVar$ gate, each of which only removes emissions.
// o.Face() already routes a layer-1 copy (CopyFace), so a clone of a
// type-changer is seen. cards.Face.StaticsMayChangeTypes is itself
// conservative (a face whose probe is not bound to its current Statics
// answers true).
//
// The answer is served from a per-object probe cache (staticsProbeCatchUp)
// over every non-ceased object in e.G.Objs -- a superset of the walk's zone
// lists, so the cached answer is never "no" where the walk says "yes". Under
// layer4PrecheckVerify every read is held to the walk.
func (e *Engine) staticsMayChangeTypes() bool {
	n := len(e.L.Events)
	if e.typesProbeEpoch != n || e.typesProbeVersion != e.continuousVersion || e.typesProbeObjs != len(e.G.Objs) {
		e.staticsProbeCatchUp(n)
	}
	got := e.typesProbeTrue > 0
	if layer4PrecheckVerify && !got && e.staticsMayChangeTypesWalk() {
		panic(fmt.Sprintf("rules: cached staticsMayChangeTypes at log %d answered false but the zone walk says true", n))
	}
	return got
}

// staticsProbeCatchUp brings the per-object probe cache up to the current
// log head. A nil cache (fresh engine, clone), a negative or rewound epoch,
// or a continuousVersion bump re-probes the whole board; otherwise it
// re-probes exactly the objects the events since the last probe reference
// (every field the probe reads -- Zone, Card, FaceIdx, CopyFace, Unlocked,
// MergedCards -- is written only inside events.Apply and keyed to the
// event's Obj/IDs/Pairs, the same argument as the trigger-walk skip; the one
// in-place face mutation, the AddStaticAbility$ grant, swaps in a FRESH face
// under the same event key, and a face's own StaticsMayChangeTypes probe
// self-invalidates when its Statics list grows) plus every object appended
// since, and re-stamps.
func (e *Engine) staticsProbeCatchUp(n int) {
	if !e.typesProbeReady || e.typesProbeEpoch < 0 || n < e.typesProbeEpoch || e.typesProbeVersion != e.continuousVersion {
		e.staticsProbeFull()
		return
	}
	for i := e.typesProbeEpoch; i < n; i++ {
		ev := &e.L.Events[i]
		e.staticsProbeTouch(ev.Obj)
		for _, id := range ev.IDs {
			e.staticsProbeTouch(id)
		}
		for _, pr := range ev.Pairs {
			e.staticsProbeTouch(pr[0])
			e.staticsProbeTouch(pr[1])
		}
	}
	for i := e.typesProbeObjs; i < len(e.G.Objs); i++ {
		e.staticsProbeStore(e.G.Objs[i].ID)
	}
	e.typesProbeEpoch, e.typesProbeVersion, e.typesProbeObjs = n, e.continuousVersion, len(e.G.Objs)
}

// staticsProbeTouch re-probes one object; ids that resolve to nothing (zero,
// a PlayerRef sentinel, out of range) have no probe and cannot flip.
func (e *Engine) staticsProbeTouch(id state.ObjID) {
	if id == 0 || e.G.Obj(id) == nil {
		// A zero or PlayerRef/out-of-range id has no probe and cannot flip.
		return
	}
	e.staticsProbeStore(id)
}

// staticsProbeStore recomputes one object's probe answer and keeps the count
// of true answers in step. Objects absent from the map (never probed) count
// from zero.
func (e *Engine) staticsProbeStore(id state.ObjID) {
	now := e.objectStaticsMayChangeTypes(e.G.Obj(id))
	i := int(id) - 1
	for len(e.typesProbe) <= i {
		e.typesProbe = append(e.typesProbe, probeUnset)
	}
	ans := probeNo
	if now {
		ans = probeYes
	}
	switch old := e.typesProbe[i]; {
	case old == ans:
		return
	case old == probeYes:
		e.typesProbeTrue--
	case now:
		e.typesProbeTrue++
	}
	e.typesProbe[i] = ans
}

// The typesProbe cell values.
const (
	probeUnset uint8 = iota
	probeNo
	probeYes
)

// staticsMayChangeTypesWalk is the uncached zone-list walk the probe cache
// replaces: layer4PrecheckVerify holds the cached answer to it (a "yes" here
// the cache misses panics; the cache answering "yes" where the walk says
// "no" is only ever conservative -- it probes every non-ceased object, a
// superset of these zone lists).
func (e *Engine) staticsMayChangeTypesWalk() bool {
	for p := range e.G.Players {
		pid := state.PlayerID(p)
		for _, z := range staticSourceZones {
			if z == state.ZStack && p > 0 {
				continue
			}
			for _, id := range e.staticSourceIDs(pid, z) {
				if e.objectStaticsMayChangeTypes(e.G.Obj(id)) {
					return true
				}
			}
		}
	}
	return false
}

// objectStaticsMayChangeTypes is staticsMayChangeTypes' per-object test, the
// one the probe cache stores per object.
func (e *Engine) objectStaticsMayChangeTypes(o *state.Object) bool {
	if o == nil || o.Zone == state.ZCeased {
		// Never walked: Game.Zone(ZCeased) lists nothing, and the zone is
		// not a static-source zone. Late in a game most of e.G.Objs is
		// resolved ability objects and ceased copies parked here.
		return false
	}
	onBF := o.Zone == state.ZBattlefield
	if o.Face().StaticsMayChangeTypes(onBF) {
		return true
	}
	if !onBF {
		return false
	}
	if o.Unlocked && o.Card != nil && len(o.Card.Faces) == 2 {
		if o.Card.Faces[0].StaticsMayChangeTypes(true) || o.Card.Faces[1].StaticsMayChangeTypes(true) {
			return true
		}
	}
	for j := range o.MergedCards {
		if o.MergedFaceAt(j).StaticsMayChangeTypes(true) {
			return true
		}
	}
	return false
}

// cloneObjectHeadroomProbe is the spare probe capacity a full re-probe
// allocates, so the objects a game mints next extend it in place.
const cloneObjectHeadroomProbe = 64

// staticsProbeFull re-probes the whole board (the pre-incremental walk,
// once, populating the cache).
func (e *Engine) staticsProbeFull() {
	m := e.typesProbe[:0]
	if cap(m) < len(e.G.Objs) {
		m = make([]uint8, 0, len(e.G.Objs)+cloneObjectHeadroomProbe)
	}
	m = m[:len(e.G.Objs)]
	count := 0
	for i := range e.G.Objs {
		// Objs is the dense arena: Objs[i].ID == i+1, the cell's index.
		if e.objectStaticsMayChangeTypes(&e.G.Objs[i]) {
			m[i] = probeYes
			count++
		} else {
			m[i] = probeNo
		}
	}
	e.typesProbe, e.typesProbeReady = m, true
	e.typesProbeTrue = count
	e.typesProbeEpoch = len(e.L.Events)
	e.typesProbeVersion = e.continuousVersion
	e.typesProbeObjs = len(e.G.Objs)
}

// verifyLayer4Active is the verify-mode check behind layer4PrecheckVerify:
// it rebuilds active() and panics unless it agrees with the fast answer.
func (e *Engine) verifyLayer4Active(want bool) {
	got := false
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Layer == LType {
			got = true
			break
		}
	}
	if got != want {
		panic(fmt.Sprintf("rules: anyLayer4Active fast path answered %v but active() says %v", want, got))
	}
}

// verifySelfOnlyDerivedTypes is buildDerivedTypes' self-only check under
// layer4PrecheckVerify: it rebuilds the table with the full walk into fresh
// storage and panics unless the fast path's table is identical.
func (e *Engine) verifySelfOnlyDerivedTypes(got []effects.ObjectTypes) {
	full := e.buildDerivedTypesWalk(nil, nil, false)
	if len(got) != len(full) || (len(got) > 0 && !reflect.DeepEqual(got, full)) {
		panic(fmt.Sprintf("rules: self-only layer-4 table at log %d disagrees with the full walk (%d vs %d entries)", len(e.L.Events), len(got), len(full)))
	}
}

// verifyInertDerivedTypes is refreshDerivedTypes' layer-inert reuse check: it
// rebuilds the table into fresh storage and compares.
func (e *Engine) verifyInertDerivedTypes() {
	cached := e.layer4Types
	fresh := e.buildDerivedTypes(nil)
	if len(cached) != len(fresh) || (len(cached) > 0 && !reflect.DeepEqual(cached, fresh)) {
		panic(fmt.Sprintf("rules: layer-inert derived-type table reuse at log %d disagrees with a rebuild (%d vs %d entries)", len(e.L.Events), len(cached), len(fresh)))
	}
}
