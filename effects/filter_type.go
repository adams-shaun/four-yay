package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// canReceiveCounter answers Forge's Card.canReceiveCounters <kind>: the object
// can have a counter of kind placed on it. A +1/+1 (or -1/-1) counter is
// hostable by a creature -- read through EffectiveIsCreature so a face-down
// permanent's folded set type decides -- and any other counter kind by any
// battlefield permanent with a face. Off the battlefield (or a face-less
// object) never matches.
func canReceiveCounter(kind string, o *state.Object) bool {
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		return false
	}
	switch canReceiveCounter32f1Codes.Code(string(strings.ToUpper(kind))) {
	case canReceiveCounter32f1P1P1:
		return o.EffectiveIsCreature()
	}
	return true
}

// hasType reads a printed type plus Changeling's type-defining ability. The
// rules package supplies SpecContext.Types when a layer-derived type list is
// available; this fallback remains deliberately useful to effects, which sits
// below rules and cannot import the layer engine.
func hasType(o *state.Object, t string) bool { return hasTypeID(o, t, 0) }

// hasTypeID is hasType with t's precompiled cards.InternTypeWord ordinal (0 =
// none): the printed type-line test is one bit test when the face is bound.
func hasTypeID(o *state.Object, t string, id cards.TypeWordID) bool {
	d, f := hasTypePrinted(o, t, id)
	if d != typeUndecided {
		return d == typeYes
	}
	// Intrinsic type-defining abilities, answered in every zone (CR 613.4a):
	// Changeling's keyword and the characteristic-defining
	// AddAllCreatureTypes$ True static (Mistform Ultimus). Both go through
	// the positive subtype vocabulary, so a non-creature word (Arcane,
	// Alara, Ajani) can never leak, and neither materialises subtypes into
	// the derived type list.
	return (f.HasKeywordID("Changeling", kwChangeling) || f.AllCreatureTypesCDA()) && changelingType(t)
}

// hasTypeSub is hasType with changelingType(t) supplied precomputed as sub
// (the compiled filter form classifies its type words once). Every function
// involved is pure, so reading sub first only skips the keyword probe for a
// word no Changeling can grant.
func hasTypeSub(o *state.Object, t string, id cards.TypeWordID, sub bool) bool {
	d, f := hasTypePrinted(o, t, id)
	if d != typeUndecided {
		return d == typeYes
	}
	return sub && (f.HasKeywordID("Changeling", kwChangeling) || f.AllCreatureTypesCDA())
}

type typeDecision uint8

const (
	typeUndecided typeDecision = iota
	typeYes
	typeNo
)

// hasTypePrinted is hasType up to (not including) its intrinsic-CDA tail:
// the bestow/reconfigure switches and the printed type line. Undecided means
// the answer is the CDA tail's, read off the returned face.
func hasTypePrinted(o *state.Object, t string, id cards.TypeWordID) (typeDecision, *cards.Face) {
	f := o.Face()
	if f == nil {
		return typeNo, nil
	}
	// CR 702.114e: a bestowed card attached to a creature is an Aura, not a
	// creature, in every filter read (Count$Valid, target offer, cost
	// candidates, statics' Affected$). Derived live state
	// (state.Object.BestowedAttached); the layer walk sees the same switch
	// through rules/chars/types.go's bestowedTypeSwitch, and hasTypeCtx inherits
	// this gate through the hasType call below.
	//
	// CR 702.114c: a card cast with its bestow ability is an Aura SPELL, not a
	// creature spell -- the same switch one zone earlier, derived from the
	// stack zone and the pay-time FlagBestowed provenance
	// (state.Object.BestowedAuraSpell). This is what keeps a bestowed cast
	// from firing "whenever you cast a creature spell" triggers.
	if o.BestowedAttached() || o.BestowedAuraSpell() {
		if strings.EqualFold(t, "Aura") {
			return typeYes, f
		}
		if strings.EqualFold(t, "Creature") {
			return typeNo, f
		}
	}
	// CR 702.150c: a Reconfigure card attached to a creature is not a
	// creature, in the same every-filter-read sense (the target ask's
	// ValidTgts$ Creature, a Count$Valid Creature census, the combat
	// eligibility scans). Equipment and Artifact stay true -- they are the
	// printed face's own types and the attached form keeps them.
	if o.ReconfiguredAttached() && !(o.FaceDown && o.Zone == state.ZBattlefield) {
		if strings.EqualFold(t, "Creature") {
			return typeNo, f
		}
	}
	if f.TypeLineHas(t, id) {
		return typeYes, f
	}
	return typeUndecided, f
}

// typePredicate handles the legacy predicate-map entries whose meaning is a
// type test. Keeping them in one context-aware path ensures layer-4 derived
// types and Changeling apply consistently to both positive and negated forms.
func typePredicate(p string, g *state.Game, o *state.Object, sc SpecContext) (bool, bool) {
	switch typePredicate32f2Codes.Code(string(p)) {
	case typePredicate32f2Legendary:
		return hasTypeCtx(o, p, sc), true
	case typePredicate32f2NonLand:
		return !hasTypeCtx(o, "Land", sc), true
	case typePredicate32f2NonCreature:
		return !hasTypeCtx(o, "Creature", sc), true
	case typePredicate32f2NonBasic:
		return !hasTypeCtx(o, "Basic", sc), true
	case typePredicate32f2ChosenType:
		s := g.Obj(sc.Source)
		return s != nil && s.ChosenType != "" && hasTypeCtx(o, s.ChosenType, sc), true
	case typePredicate32f2IsNotChosenType:
		s := g.Obj(sc.Source)
		return s != nil && s.ChosenType != "" && !hasTypeCtx(o, s.ChosenType, sc), true
	case typePredicate32f2ChosenCtrl:
		return chosenCtrlMatches(g, o, sc.Source), true
	}
	return false, false
}

// chosenCtrlMatches is the ONE implementation of the ChosenCtrl predicate
// (controlled by the source's secretly chosen player) the context-aware
// typePredicate path and the census-only predicates-map entry share, so the
// matcher and UnknownPredicates cannot drift. A source with no chosen player
// fails closed.
func chosenCtrlMatches(g *state.Game, o *state.Object, src state.ObjID) bool {
	s := g.Obj(src)
	if s == nil {
		return false
	}
	for _, t := range s.Chosen {
		if t.IsPlayer && o.Controller == t.Player {
			return true
		}
	}
	return false
}

// hasTypePredicateCtx matches a type word, or a space-separated multi-word
// subtype (Time Lord) as every one of its words. strings.Cut rather than
// strings.Fields: this is the filter hot path and must not allocate
// (TestSimpleFilterMatchingDoesNotAllocate).
func hasTypePredicateCtx(o *state.Object, t string, sc SpecContext) bool {
	return hasTypePredicateCtxPtr(o, t, &sc)
}

// hasTypePredicateCtxPtr is hasTypePredicateCtx through a pointer (see
// hasTypeCtxPtr).
func hasTypePredicateCtxPtr(o *state.Object, t string, sc *SpecContext) bool {
	if t == "" {
		return false
	}
	for {
		word, rest, more := strings.Cut(t, " ")
		if word != "" && !hasTypeCtxPtr(o, word, sc) {
			return false
		}
		if !more {
			return true
		}
		t = rest
	}
}

func hasTypeCtx(o *state.Object, t string, sc SpecContext) bool {
	return hasTypeCtxPtr(o, t, &sc)
}

// hasTypeCtxPtr is hasTypeCtx reading the context through a pointer, so the
// compiled predicate paths that already hold a *SpecContext do not copy the
// whole context per type test (see hasEffectiveNamePtr).
func hasTypeCtxPtr(o *state.Object, t string, sc *SpecContext) bool {
	return hasTypeCtxPtrID(o, t, 0, sc)
}

// hasTypeCtxPtrID is hasTypeCtxPtr with t's precompiled type-word ordinal
// (0 = none), which only the printed-face fallback reads.
func hasTypeCtxPtrID(o *state.Object, t string, id cards.TypeWordID, sc *SpecContext) bool {
	// ExtraTypes is the layer walk's accumulating type list for the ONE
	// object being matched: a plain value slice, deliberately not a callable
	// resolver. Any call made through a SpecContext field makes escape
	// analysis leak the whole context to the heap on every hot-path
	// construction (the statics/action hotspot pins measure exactly that),
	// while a slice field is read-only and allocation-free. It is checked
	// first because it is the layer walk's OWN types-so-far list, which can
	// differ from the published table mid-walk.
	for _, x := range sc.ExtraTypes {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	// When the layer walk bound a types-so-far list, it is authoritative and
	// the pre-existing printed-face/Changeling fallback below is kept verbatim.
	// The published table is not consulted in that case: it may carry a type a
	// LATER effect grants, which would break the walk's ordering (rules/layers.go
	// clears sc.DerivedTypes for the same reason, but this guard keeps the
	// contract even for a caller that sets ExtraTypes without clearing it).
	if sc.ExtraTypes != nil {
		return hasTypeID(o, t, id)
	}
	// Outside the walk a published layer-4 entry makes the object's DERIVED
	// type list authoritative for type words: it already carries the printed
	// types the effect kept (rules' typeCharacteristics folds them in), so a
	// RemoveCardTypes$ cannot be resurrected by a fallback to the printed face,
	// while a granted word (a static's AddTypes$, AddAllCreatureTypes$) is
	// found exactly as the layer walk finds it. Only the intrinsic CDAs the
	// list deliberately does not materialise (Changeling's keyword, Mistform
	// Ultimus's AddAllCreatureTypes$ CDA) are added back on this path.
	if types, ok := derivedTypesForPtr(o, sc); ok {
		for _, x := range types {
			if strings.EqualFold(x, t) {
				return true
			}
		}
		return intrinsicCDAType(o, t)
	}
	// No derived entry: the printed face plus intrinsic CDAs, as before.
	return hasTypeID(o, t, id)
}

// hasTypeCtxSub is hasTypeCtx with changelingType(t) precomputed as sub: the
// same reads in the same order, the intrinsic-CDA tails taking sub.
func hasTypeCtxSub(o *state.Object, t string, id cards.TypeWordID, sub bool, sc *SpecContext) bool {
	for _, x := range sc.ExtraTypes {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	if sc.ExtraTypes != nil {
		return hasTypeSub(o, t, id, sub)
	}
	for _, d := range sc.Layers.DerivedTypes {
		if d.ID != o.ID {
			continue
		}
		// derivedTypesFor's first entry for o is authoritative.
		for _, x := range d.Types {
			if strings.EqualFold(x, t) {
				return true
			}
		}
		if !sub {
			return false
		}
		f := o.Face()
		return f != nil && (f.HasKeywordID("Changeling", kwChangeling) || f.AllCreatureTypesCDA())
	}
	return hasTypeSub(o, t, id, sub)
}

// changelingType reports whether t is an actual creature subtype. This uses
// a positive authoritative vocabulary rather than treating every type word
// outside an exclusion list as a creature type: Arcane, Alara, and Ajani are
// respectively spell, plane, and planeswalker subtypes, not types Changeling
// grants.
func changelingType(t string) bool { return CreatureTypeWords(t) }

// intrinsicCDAType is hasType's intrinsic type-defining-ability branch on its
// own (Changeling's keyword, and the characteristic-defining
// AddAllCreatureTypes$ True static -- Mistform Ultimus). A layer-4 derived type
// list deliberately never materialises these subtypes, so hasTypeCtx must still
// answer them when the published table is authoritative for the object; the
// positive vocabulary keeps a non-creature word out.
func intrinsicCDAType(o *state.Object, t string) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	return (f.HasKeywordID("Changeling", kwChangeling) || f.AllCreatureTypesCDA()) && changelingType(t)
}

// kwChangeling is Changeling's interned keyword head (cards.InternKeywordHead).
var kwChangeling = cards.InternKeywordHead("Changeling")

const (
	canReceiveCounter32f1P1P1 uint16 = 1 // "P1P1", "M1M1"
)

var canReceiveCounter32f1Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "P1P1", Val: canReceiveCounter32f1P1P1},
	state.StrEntry[uint16]{Key: "M1M1", Val: canReceiveCounter32f1P1P1},
)

const (
	typePredicate32f2Legendary       uint16 = 1 // "Legendary", "Basic", "Snow"
	typePredicate32f2NonLand         uint16 = 2 // "nonLand"
	typePredicate32f2NonCreature     uint16 = 3 // "nonCreature"
	typePredicate32f2NonBasic        uint16 = 4 // "nonBasic"
	typePredicate32f2ChosenType      uint16 = 5 // "ChosenType"
	typePredicate32f2IsNotChosenType uint16 = 6 // "IsNotChosenType"
	typePredicate32f2ChosenCtrl      uint16 = 7 // "ChosenCtrl"
)

var typePredicate32f2Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Legendary", Val: typePredicate32f2Legendary},
	state.StrEntry[uint16]{Key: "Basic", Val: typePredicate32f2Legendary},
	state.StrEntry[uint16]{Key: "Snow", Val: typePredicate32f2Legendary},
	state.StrEntry[uint16]{Key: "nonLand", Val: typePredicate32f2NonLand},
	state.StrEntry[uint16]{Key: "nonCreature", Val: typePredicate32f2NonCreature},
	state.StrEntry[uint16]{Key: "nonBasic", Val: typePredicate32f2NonBasic},
	state.StrEntry[uint16]{Key: "ChosenType", Val: typePredicate32f2ChosenType},
	state.StrEntry[uint16]{Key: "IsNotChosenType", Val: typePredicate32f2IsNotChosenType},
	state.StrEntry[uint16]{Key: "ChosenCtrl", Val: typePredicate32f2ChosenCtrl},
)
