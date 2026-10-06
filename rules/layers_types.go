package rules

import (
	"fmt"
	"sync/atomic"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// faceDownPrintedHides is the one CR 708.8 gate every printed-face scan
// shares: while a battlefield object is face down, its printed abilities,
// triggers and statics do not exist. The ability-offer loop, the
// mana-ability collector, the trigger scan and both static scans all
// consult it, so no printed face of a manifested card can leak into any
// offer or queue while it is face down.
func (e *Engine) faceDownPrintedHides(o *state.Object) bool {
	return o != nil && o.FaceDown && o.Zone == state.ZBattlefield
}

// typeCharacteristics applies layer 4 before anything that tests a type. The
// accumulated types-so-far list passed into the shared effects filter is what
// lets a later effect select a creature made a Goblin by an earlier layer-4
// effect rather than looking back at its printed face. atStack is
// derivedWith's zone override for AffectedZone$ Stack grants; the zero value
// reads the object's live zone.
func (e *Engine) typeCharacteristics(id state.ObjID, atStack state.Zone) []string {
	return e.typeCharacteristicsActive(e.active(), id, atStack)
}

// typeCharacteristicsActive is typeCharacteristics with the layer walk's own
// active()-list supplied by the caller: chars.Types, the layer-4 walk, over
// the engine's Board. The list IS what active() returns -- the live
// registered effects plus the memoized static scan, layer/sub/timestamp
// sorted -- so a caller that has already proven no static can change a type
// (layer4types.go's staticsMayChangeTypes precheck) may pass just the live
// registered LType effects and skip rebuilding the static memo; the result
// is identical by that proof, and layer4PrecheckVerify compares every such
// fast build against the typeCharacteristics full walk.
func (e *Engine) typeCharacteristicsActive(act []ContinuousEffect, id state.ObjID, atStack state.Zone) []string {
	return chars.Types(asChars(e), act, id, atStack)
}

func (e *Engine) matchesWithTypes(ce *ContinuousEffect, id state.ObjID, types []string, atStack state.Zone) bool {
	return e.matchesWithChars(ce, id, types, nil, atStack)
}

// matchesWithChars is matchesWithTypes with the walk's KEYWORDS-so-far list
// bound as well. It is the one seam a `with<Keyword>`/`without<Keyword>`
// predicate in an `Affected$` spec is answered through, for the same reason
// ExtraTypes exists: the effects filter's keyword predicates read the object
// alone (printed face plus marker counters) and cannot see a layer-6
// AddKeyword$ grant, so a lord that selects on a granted keyword would never
// match. Cavalry Master's `Creature.Other+withFlanking+YouCtrl` over a
// Sidewinder Sliver whose own static granted the flanking is the measured
// case (CR 702.25b: each instance triggers separately).
//
// The list is keywords-SO-FAR in the walk's own layer/timestamp order, which
// is the same reading ExtraTypes gives: a grant whose effect is applied
// earlier is visible, a later one is not. Within layer 6 the sequence itself
// is CR 613.6 dependency order (rules/chars abilityDependencyOrder), so a lord
// whose gate reads a keyword another layer-6 effect grants is applied after
// that grant regardless of timestamps -- the Cavalry Master-over-a-Sidewinder
// Sliver case. The layer-7 P/T walk binds the SAME finished list (CR 613
// orders layer 6 strictly before layer 7, so the walk's final keyword list
// is what a layer-7 applicability gate reads): Windstorm Drake's
// `Creature.withFlying+Other+YouCtrl` +1/+0 over a creature an earlier
// layer-6 effect granted flying is the measured case.
func (e *Engine) matchesWithChars(ce *ContinuousEffect, id state.ObjID, types, keywords []string, atStack state.Zone) bool {
	return e.matchesWithCharsPT(ce, id, types, keywords, atStack, 0, 0, 0, 0, false)
}

// matchesWithCharsPT binds the layer-7 walk's in-progress P/T values when an
// Affected$ predicate is evaluated during that walk. Calling Derived here
// would recurse through the same active layer scan.
func (e *Engine) matchesWithCharsPT(ce *ContinuousEffect, id state.ObjID, types, keywords []string, atStack state.Zone, power, toughness, basePower, baseToughness int32, hasPT bool) bool {
	// CR 702.25b: a phased-out permanent is treated as though it does not
	// exist, so NO continuous effect applies to it -- a lord's pump, a
	// keyword grant, a type change. This is the one applicability gate every
	// layer walk goes through, so the exclusion cannot be missed by a layer
	// the way a per-layer check could. PhasedOut is only ever true on a
	// battlefield permanent.
	if o := e.G.Obj(id); o != nil && o.PhasedOut {
		return false
	}
	// A spec whose Affects is exactly Card.Self names only the effect's own
	// source (effects' "Self" predicate is o.ID == src), so a different id can
	// never match it -- no later gate can rescue the match. Rejecting here,
	// before the cast-provenance gate and specCtx/MatchesSpecCtx, keeps a
	// board with several self-only type effects from paying the full match
	// per candidate: Clown Car's four crewed self type effects against tens
	// of thousands of goblin tokens was the measured case (seed
	// 6181111140895991800). This is a pure early-out -- identical result -- and
	// it lives on the one seam every layer's match shares, so a layer-6/7
	// self-only effect is covered too.
	if ce.Affects == "Card.Self" && id != ce.Source {
		if layer4PrecheckVerify {
			selfRejectVerify.Add(1)
			// Recompute through the full match and confirm the early-out
			// agreed: a non-source Card.Self must never match. This is the
			// empirical proof the shortcut is result-preserving.
			if e.matchesWithCharsPTSlow(ce, id, types, keywords, atStack, power, toughness, basePower, baseToughness, hasPT) {
				panic(fmt.Sprintf("rules: Card.Self early reject for id %d != source %d but the full match admitted it", id, ce.Source))
			}
		}
		return false
	}
	return e.matchesWithCharsPTSlow(ce, id, types, keywords, atStack, power, toughness, basePower, baseToughness, hasPT)
}

// selfRejectVerify counts the Card.Self early rejections the shortcut made
// under verify mode; a test asserts it advances so the shortcut cannot be
// silently removed. It is written only under the layer4PrecheckVerify branch,
// so production pays one predictable branch and no store. It is atomic
// because the test binary turns verify mode on and runs engines on parallel
// test goroutines (TestInvariantsUnderSeedFuzz's seed subtests), which all
// bump this one counter.
var selfRejectVerify atomic.Int64

// matchesWithCharsPTSlow is matchesWithCharsPT with the Card.Self early
// rejection removed. Verify mode calls it to prove the shortcut agrees with
// the full match; production never does.
func (e *Engine) matchesWithCharsPTSlow(ce *ContinuousEffect, id state.ObjID, types, keywords []string, atStack state.Zone, power, toughness, basePower, baseToughness int32, hasPT bool) bool {
	// The cast-provenance qualifiers (castprov1/2/3 — the_twelfth_doctor's
	// `Affected$ Card.YouCtrl+!wasCastFromYourHand`, quandrix_the_proof's
	// `Instant.wasCastByYou+wasCastFromYourHand`) are split out before the
	// filter match, through the combined entry point; its one-probe
	// specProvenanceGate is the early-out, so every Affected$ spec
	// without the tokens costs one cached lookup on this shared hot path.
	affects, ok := e.castProvenanceAdmitsWindow(ce.Affects, id, ce.Controller, atStack != 0)
	if !ok {
		return false
	}
	// ExtraTypes is the walk's types-so-far list for THIS object: a later
	// layer-4 effect selects a creature an earlier one made a Goblin, and a
	// layer-7 lord's Affected$ sees the derived type. A value slice, not a
	// callable, keeps the context stack-allocated on this hot path.
	sc := e.specCtx(ce.Source, ce.Controller)
	sc.AsStack = atStack != 0
	sc.ExtraTypes, sc.ExtraTypesOwner = types, id
	sc.ExtraKeywords, sc.ExtraKeywordsOwner = keywords, id
	if hasPT {
		sc.DerivedPower, sc.DerivedToughness, sc.HasDerivedPT = power, toughness, true
		sc.BasePower, sc.BaseToughness, sc.HasBasePT = basePower, baseToughness, true
	}
	// The walk's types-so-far list above is authoritative for this match, so
	// the published layer-4 table (layer4types.go's DerivedTypes, bound by
	// specCtx) must not be consulted as a fallback: it may carry a type a
	// LATER effect grants, which would break the walk's own layer/timestamp
	// ordering. This is the same deferral the PredicatePrograms clear below
	// practises, and it leaves the walk's printed-face/Changeling fallback
	// (hasTypeCtx -> hasType) exactly as it was.
	sc.Layers.DerivedTypes = nil
	// An Effect-delivered grant's Affected$ spec may name the objects the
	// Effect remembered (`Affected$ Permanent.IsRemembered`, energybending's
	// "lands you control gain all basic land types"). The restriction walk
	// (restrictionApplies) already binds the registered set; the layer walk
	// must too, or such an Affected$ would match nobody. Printed statics carry
	// no Remembered, so this is a no-op for them.
	for _, r := range ce.Remembered {
		sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
	}
	// The compiled predicate sidecar is ExtraTypes-aware: its type predicates
	// are answered through hasTypeCtx (effects/compiled_predicate.go), the same
	// helper the textual oracle uses, so it sees this bind's types-so-far list
	// exactly as the text path does. It therefore stays installed -- clearing it
	// would discard the immutable optimization across the whole derived walk.
	// A face-down candidate's colour-bearing programs still take the textual
	// fallback inside evaluate (CR 708.5), so no new printed-colour claim is
	// introduced for a context whose characteristics do not exist.
	return effects.MatchesSpecCtxPtr(e.G, affects, id, &sc)
}
