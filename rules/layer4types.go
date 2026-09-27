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
func (e *Engine) refreshDerivedTypes() {
	if e.typesBuilding {
		// Re-entry: a Derived/typeCharacteristics call below reached something
		// that re-emitted. The half-built table must never be published; the
		// outer call finishes it.
		return
	}
	// The key also carries len(e.G.Objs): a test helper (or any caller) may
	// place a permanent with a direct AddObject -- no event, so the log head
	// would not move -- and the table must not stay a stale cache hit. An
	// emitted move changes neither the object count nor the derived list of
	// any other object, so this adds no work on the ordinary path.
	if e.typesEpoch == len(e.L.Events) && e.typesVersion == e.continuousVersion && e.typesObjs == len(e.G.Objs) {
		return
	}
	// Layer-inert reuse (layercache.go): only priority bookkeeping moved the
	// log since the table was built, so the table is still exact.
	if e.typesVersion == e.continuousVersion && e.typesObjs == len(e.G.Objs) && e.layerInertSince(e.typesEpoch) {
		e.typesEpoch = len(e.L.Events)
		if layerInertVerify {
			e.verifyInertDerivedTypes()
		}
		return
	}
	e.typesEpoch, e.typesVersion, e.typesObjs = len(e.L.Events), e.continuousVersion, len(e.G.Objs)
	e.layer4Types = e.buildDerivedTypes(e.layer4Types[:0])
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
	for _, ce := range e.active() {
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
// AddAllCreatureTypes$ branch, reached from a face's own statics or from an
// AddStaticAbility$ grant on one, and only for a static whose zone gate
// admits the source's zone. The faces that scan walks are o.Face() for an
// object in a static-source zone of an alive seat, plus, on the battlefield,
// an unlocked Room's other face and a mutated pile's merged faces. This walk
// covers every object in e.G.Objs (a superset of every zone list: Game.Obj
// resolves ids into that slice), checks o.Face() under its zone class, and on
// the battlefield checks both faces of any unlocked two-faced card (a
// superset of the Room condition) and every merged face. It ignores
// face-down hiding and every IsPresent$/CheckSVar$ gate, each of which only
// removes emissions. o.Face() already routes a layer-1 copy (CopyFace), so a
// clone of a type-changer is seen. cards.Face.StaticsMayChangeTypes is itself
// conservative (a face whose probe is not bound to its current Statics
// answers true).
func (e *Engine) staticsMayChangeTypes() bool {
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZCeased {
			// Never walked: Game.Zone(ZCeased) lists nothing, and the zone is
			// not a static-source zone. Late in a game most of e.G.Objs is
			// resolved ability objects and ceased copies parked here.
			continue
		}
		onBF := o.Zone == state.ZBattlefield
		if o.Face().StaticsMayChangeTypes(onBF) {
			return true
		}
		if !onBF {
			continue
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
	}
	return false
}

// verifyLayer4Active is the verify-mode check behind layer4PrecheckVerify:
// it rebuilds active() and panics unless it agrees with the fast answer.
func (e *Engine) verifyLayer4Active(want bool) {
	got := false
	for _, ce := range e.active() {
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
