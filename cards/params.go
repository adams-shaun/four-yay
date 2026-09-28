package cards

import (
	"math/bits"
	"unsafe"
)

// ParamKey is a compiled parameter key: a dense ordinal over the fixed
// vocabulary below, the keys the rules hot paths read. A node's ParamSet
// answers a ParamKey read with a mask test and a popcount rank into a value
// slice instead of a string-keyed map probe. Keys outside the vocabulary are
// read from Params as before.
//
// The rules parameter census (rules/paramcensus_test.go) counts
// `x.Param(cards.PK<Key>)`, `x.ParamStr(cards.PK<Key>)` and
// `x.HasParam(cards.PK<Key>)` as reads of <Key>, exactly like
// `x.Params["<Key>"]`, so a constant's name MUST be "PK" + the key text.
type ParamKey uint8

const (
	pkNone ParamKey = iota
	PKAddAbility
	PKAddAllCreatureTypes
	PKAddColor
	PKAddColors
	PKAddKeyword
	PKAddPower
	PKAddSVar
	PKAddStaticAbility
	PKAddToughness
	PKAddTrigger
	PKAddType
	PKAddTypes
	PKAdjustLandPlays
	PKAffected
	PKAffectedZone
	PKCantHaveKeyword
	PKCharacteristicDefining
	PKCheckSVar
	PKClassBand
	PKCondition
	PKEffectZone
	PKExcludeZone
	PKGainControl
	PKGainsAbilitiesOf
	PKGainsAbilitiesOfDefined
	PKGainsTriggerAbsOf
	PKIsPresent
	PKIsPresent2
	PKMayLookAt
	PKMayPlay
	PKOrigin
	PKPhase
	PKPresentZone
	PKRemoveAllAbilities
	PKRemoveCardTypes
	PKRemoveCreatureTypes
	PKRemoveKeyword
	PKSetColor
	PKSetName
	PKSetPower
	PKSetToughness
	paramKeyCount
)

// paramKeyNames maps each ParamKey to its Forge key text.
var paramKeyNames = [paramKeyCount]string{
	PKAddAbility:              "AddAbility",
	PKAddAllCreatureTypes:     "AddAllCreatureTypes",
	PKAddColor:                "AddColor",
	PKAddColors:               "AddColors",
	PKAddKeyword:              "AddKeyword",
	PKAddPower:                "AddPower",
	PKAddSVar:                 "AddSVar",
	PKAddStaticAbility:        "AddStaticAbility",
	PKAddToughness:            "AddToughness",
	PKAddTrigger:              "AddTrigger",
	PKAddType:                 "AddType",
	PKAddTypes:                "AddTypes",
	PKAdjustLandPlays:         "AdjustLandPlays",
	PKAffected:                "Affected",
	PKAffectedZone:            "AffectedZone",
	PKCantHaveKeyword:         "CantHaveKeyword",
	PKCharacteristicDefining:  "CharacteristicDefining",
	PKCheckSVar:               "CheckSVar",
	PKClassBand:               "ClassBand",
	PKCondition:               "Condition",
	PKEffectZone:              "EffectZone",
	PKExcludeZone:             "ExcludeZone",
	PKGainControl:             "GainControl",
	PKGainsAbilitiesOf:        "GainsAbilitiesOf",
	PKGainsAbilitiesOfDefined: "GainsAbilitiesOfDefined",
	PKGainsTriggerAbsOf:       "GainsTriggerAbsOf",
	PKIsPresent:               "IsPresent",
	PKIsPresent2:              "IsPresent2",
	PKMayLookAt:               "MayLookAt",
	PKMayPlay:                 "MayPlay",
	PKOrigin:                  "Origin",
	PKPhase:                   "Phase",
	PKPresentZone:             "PresentZone",
	PKRemoveAllAbilities:      "RemoveAllAbilities",
	PKRemoveCardTypes:         "RemoveCardTypes",
	PKRemoveCreatureTypes:     "RemoveCreatureTypes",
	PKRemoveKeyword:           "RemoveKeyword",
	PKSetColor:                "SetColor",
	PKSetName:                 "SetName",
	PKSetPower:                "SetPower",
	PKSetToughness:            "SetToughness",
}

// String is the key's Forge text.
func (k ParamKey) String() string { return paramKeyNames[k] }

// paramKeyByName is the load-time inverse of paramKeyNames (never read on a
// hot path: newParamSet runs once per node at load).
var paramKeyByName = func() map[string]ParamKey {
	m := make(map[string]ParamKey, paramKeyCount)
	for k := ParamKey(1); k < paramKeyCount; k++ {
		m[paramKeyNames[k]] = k
	}
	return m
}()

// ParamMask is a set of ParamKeys.
type ParamMask [2]uint64

// ParamMaskOf builds a mask of keys (package-init constant tables).
func ParamMaskOf(keys ...ParamKey) ParamMask {
	var m ParamMask
	for _, k := range keys {
		m[k>>6] |= 1 << (k & 63)
	}
	return m
}

// ParamSet is one Params map compiled against the ParamKey vocabulary: the
// presence mask of the vocabulary keys it holds and their values in key
// order. It is immutable once built and answers only for the exact map it
// was built from (src, n): a node copy whose Params was replaced or resized
// falls back to the map read.
type ParamSet struct {
	src  unsafe.Pointer
	n    int
	has  ParamMask
	vals []string
}

func mapIdentity(m map[string]string) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&m))
}

func newParamSet(m map[string]string) *ParamSet {
	if m == nil {
		return nil
	}
	ps := &ParamSet{src: mapIdentity(m), n: len(m)}
	for key := range m {
		if k, ok := paramKeyByName[key]; ok {
			ps.has[k>>6] |= 1 << (k & 63)
		}
	}
	ps.vals = make([]string, 0, bits.OnesCount64(ps.has[0])+bits.OnesCount64(ps.has[1]))
	for k := ParamKey(1); k < paramKeyCount; k++ {
		if ps.has[k>>6]&(1<<(k&63)) != 0 {
			ps.vals = append(ps.vals, m[paramKeyNames[k]])
		}
	}
	return ps
}

func (ps *ParamSet) bound(m map[string]string) bool {
	return ps != nil && ps.src == mapIdentity(m) && ps.n == len(m)
}

func (ps *ParamSet) get(k ParamKey) (string, bool) {
	w, b := k>>6, uint64(1)<<(k&63)
	if ps.has[w]&b == 0 {
		return "", false
	}
	r := bits.OnesCount64(ps.has[w] & (b - 1))
	if w == 1 {
		r += bits.OnesCount64(ps.has[0])
	}
	return ps.vals[r], true
}

func paramGet(ps *ParamSet, m map[string]string, k ParamKey) (string, bool) {
	if ps.bound(m) {
		return ps.get(k)
	}
	v, ok := m[paramKeyNames[k]]
	return v, ok
}

// paramMayHaveAny reports whether m may hold any key of mask: exact when ps
// is bound to m, else conservatively true.
func paramMayHaveAny(ps *ParamSet, m map[string]string, mask ParamMask) bool {
	if ps.bound(m) {
		return ps.has[0]&mask[0] != 0 || ps.has[1]&mask[1] != 0
	}
	return true
}

// Param is Params[k] with presence, through the compiled set when bound.
func (s Static) Param(k ParamKey) (string, bool) { return paramGet(s.ps, s.Params, k) }

// ParamStr is Params[k] ("" when absent).
func (s Static) ParamStr(k ParamKey) string { v, _ := paramGet(s.ps, s.Params, k); return v }

// HasParam reports whether key k is present.
func (s Static) HasParam(k ParamKey) bool { _, ok := paramGet(s.ps, s.Params, k); return ok }

// MayHaveAnyParam is false only when the static provably holds no key of mask.
func (s Static) MayHaveAnyParam(mask ParamMask) bool { return paramMayHaveAny(s.ps, s.Params, mask) }

// ParamSetOf is the static's compiled parameter set, for a view that carries
// the Params map onward (nil when unbound).
func (s Static) ParamSetOf() *ParamSet { return s.ps }

// Param is Params[k] with presence, through the compiled set when bound.
func (t Trigger) Param(k ParamKey) (string, bool) { return paramGet(t.ps, t.Params, k) }

// ParamStr is Params[k] ("" when absent).
func (t Trigger) ParamStr(k ParamKey) string { v, _ := paramGet(t.ps, t.Params, k); return v }

// HasParam reports whether key k is present.
func (t Trigger) HasParam(k ParamKey) bool { _, ok := paramGet(t.ps, t.Params, k); return ok }

// ParamSetParam reads key k of m through ps (a view carrying a node's Params
// map and its ParamSet side by side).
func ParamSetParam(ps *ParamSet, m map[string]string, k ParamKey) (string, bool) {
	return paramGet(ps, m, k)
}

// ParamSetMayHaveAny is MayHaveAnyParam for a view's (ps, map) pair.
func ParamSetMayHaveAny(ps *ParamSet, m map[string]string, mask ParamMask) bool {
	return paramMayHaveAny(ps, m, mask)
}

// deriveParamSets binds each printed static's and trigger's ParamSet.
func (f *Face) deriveParamSets() {
	for i := range f.Statics {
		f.Statics[i].ps = newParamSet(f.Statics[i].Params)
	}
	for i := range f.Triggers {
		f.Triggers[i].ps = newParamSet(f.Triggers[i].Params)
	}
}
