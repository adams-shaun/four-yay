package cards

import "slices"

// Typed deep equality for the IR nodes, replacing reflect.DeepEqual in the
// engine's derived-memo verify checks and pay.SameManaAbility. Each function
// is reflect.DeepEqual over its node with these exact semantics:
//
//   - a nil slice or map equals only a nil one (DeepEqual's strictness);
//   - a pointer field equals when both are the same address, or both are
//     non-nil and their pointees are equal;
//
// with ONE deliberate tightening: SA.compiledCatalog compares by identity.
// It is the corpus's single bound catalog table (bindCompiledCatalog), so two
// SAs bound to distinct-but-identical catalogs do not arise; walking a whole
// catalog per comparison would be the cost DeepEqual used to hide.
//
// equal_test.go holds each field list to its struct's (a field added to a
// node fails the build's tests until the function here compares it).

const (
	saFieldCount       = 13
	triggerFieldCount  = 6
	staticFieldCount   = 5
	replFieldCount     = 8
	paramSetFieldCount = 6
	faceFieldCount     = 47
)

func sliceEq[T comparable](a, b []T) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return slices.Equal(a, b)
}

// StringMapEqual is reflect.DeepEqual over two map[string]string: nil equals
// only nil, otherwise the same key set with equal values. The range only
// decides a bool, so its order cannot reach anything.
func StringMapEqual(a, b map[string]string) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || va != vb {
			return false
		}
	}
	return true
}

func paramSetEqual(a, b *ParamSet) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return StringMapEqual(a.src, b.src) && a.n == b.n && a.has == b.has && a.rank == b.rank &&
		sliceEq(a.vals, b.vals) && sliceEq(a.codes, b.codes)
}

func extSlotEqual(a, b *ExtSlot) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Load() == b.Load()
}

func slotEqual(a, b *Slot) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.p.Load() == b.p.Load()
}

func stringPtrEqual(a, b *string) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// SAEqual is reflect.DeepEqual(*a, *b) for two abilities (a == b, or both
// nil, short-circuits), with the compiledCatalog identity noted above.
func SAEqual(a, b *SA) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Kind == b.Kind && a.API == b.API && a.Line == b.Line &&
		a.compiledCatalog == b.compiledCatalog && a.compiledID == b.compiledID &&
		a.api == b.api && a.apiBound == b.apiBound && a.exhaust == b.exhaust && a.exhaustBound == b.exhaustBound &&
		StringMapEqual(a.Params, b.Params) && paramSetEqual(a.ps, b.ps) && extSlotEqual(a.extSlot, b.extSlot) &&
		SAEqual(a.Sub, b.Sub)
}

// TriggerEqual is reflect.DeepEqual(*a, *b) for two triggers.
func TriggerEqual(a, b *Trigger) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Mode == b.Mode && a.mode == b.mode && a.modeBound == b.modeBound &&
		StringMapEqual(a.Params, b.Params) && paramSetEqual(a.ps, b.ps) && SAEqual(a.Effect, b.Effect)
}

// StaticEqual is reflect.DeepEqual(*a, *b) for two statics.
func StaticEqual(a, b *Static) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Mode == b.Mode && a.mode == b.mode && a.modeBound == b.modeBound &&
		StringMapEqual(a.Params, b.Params) && paramSetEqual(a.ps, b.ps)
}

// ReplEqual is reflect.DeepEqual(*a, *b) for two replacements.
func ReplEqual(a, b *Repl) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Event == b.Event && a.event == b.event && a.eventBound == b.eventBound &&
		a.optional == b.optional && a.optionalBound == b.optionalBound &&
		StringMapEqual(a.Params, b.Params) && paramSetEqual(a.ps, b.ps) && SAEqual(a.With, b.With)
}

func eqEach[T any](a, b []T, eq func(x, y *T) bool) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for i := range a {
		if !eq(&a[i], &b[i]) {
			return false
		}
	}
	return true
}

// FaceEqual is reflect.DeepEqual(*a, *b) for two faces.
func FaceEqual(a, b *Face) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.SpecializeColor != b.SpecializeColor || a.CopyFaceFrom != b.CopyFaceFrom || a.Name != b.Name ||
		a.ManaCost != b.ManaCost || a.PT != b.PT || a.Loyalty != b.Loyalty || a.Defense != b.Defense ||
		a.Colors != b.Colors || a.Oracle != b.Oracle ||
		a.power != b.power || a.toughness != b.toughness || a.characteristicDefining != b.characteristicDefining ||
		a.allCreatureTypesCDA != b.allCreatureTypesCDA || a.cmc != b.cmc || a.manaProduction != b.manaProduction ||
		a.colourIdentity != b.colourIdentity || a.compiledTriggerInterests != b.compiledTriggerInterests ||
		a.compiledTypeMask != b.compiledTypeMask || a.compiledID != b.compiledID ||
		a.typeStaticsLen != b.typeStaticsLen || a.typeStaticsBound != b.typeStaticsBound ||
		a.typeStaticsBF != b.typeStaticsBF || a.typeStaticsOffBF != b.typeStaticsOffBF ||
		a.contStaticsOffBF != b.contStaticsOffBF || a.anyStaticEZ != b.anyStaticEZ ||
		a.typeWords != b.typeWords || a.typeWordsLen != b.typeWordsLen || a.typeWordsBound != b.typeWordsBound ||
		a.kwHeads != b.kwHeads || a.kwHeadsLen != b.kwHeadsLen || a.kwHeadsBound != b.kwHeadsBound ||
		a.cmcSrc != b.cmcSrc || a.cmcBound != b.cmcBound {
		return false
	}
	if !sliceEq(a.Types, b.Types) || !sliceEq(a.Keywords, b.Keywords) || !sliceEq(a.Aliases, b.Aliases) ||
		!StringMapEqual(a.SVars, b.SVars) ||
		!stringPtrEqual(a.typeWordsFirst, b.typeWordsFirst) || !stringPtrEqual(a.kwHeadsFirst, b.kwHeadsFirst) ||
		!slotEqual(a.manaCostSlot, b.manaCostSlot) || !extSlotEqual(a.extSlot, b.extSlot) ||
		!StaticEqual(a.typeStaticsFirst, b.typeStaticsFirst) {
		return false
	}
	if (a.Abilities == nil) != (b.Abilities == nil) || len(a.Abilities) != len(b.Abilities) {
		return false
	}
	for i := range a.Abilities {
		if !SAEqual(a.Abilities[i], b.Abilities[i]) {
			return false
		}
	}
	return eqEach(a.Triggers, b.Triggers, TriggerEqual) && eqEach(a.Statics, b.Statics, StaticEqual) &&
		eqEach(a.Repls, b.Repls, ReplEqual)
}
