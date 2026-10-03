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
	PKActivation
	PKActivationAfterBlockers
	PKActivationFirstCombat
	PKActivationGameTypes
	PKActivationLimit
	PKActivationPhases
	PKActivationZone
	PKActivator
	PKActiveZones
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
	PKAmount
	PKAnnounce
	PKBoast
	PKCantHaveKeyword
	PKCaster
	PKCharacteristicDefining
	PKCheckSVar
	PKCheckSecondSVar
	PKChoices
	PKClassBand
	PKCondition
	PKConditionActivationLimit
	PKConditionDefined
	PKCost
	PKCounterType
	PKDefined
	PKDefinedPlayer
	PKDestination
	PKDiscard
	PKDuration
	PKEffectOnly
	PKEffectZone
	PKEvolve
	PKExcludeZone
	PKExecute
	PKExhaust
	PKFirstForetell
	PKFoundSearchingLibrary
	PKGainControl
	PKGainsAbilitiesOf
	PKGainsAbilitiesOfDefined
	PKGainsTriggerAbsOf
	PKGameActivationLimit
	PKGoad
	PKImprintCards
	PKIsPresent
	PKIsPresent2
	PKKeyword
	PKMaxTotalTargetCMC
	PKMayLookAt
	PKMayPlay
	PKMayPlayAltManaCost
	PKMayPlayWithoutManaCost
	PKMentor
	PKMonstrosity
	PKNotThisAbility
	PKOnlyFirstSpell
	PKOpponentTurn
	PKOptional
	PKOptionalDecider
	PKOrigin
	PKPhase
	PKPlayerTurn
	PKPowerUp
	PKPresentZone
	PKProduced
	PKReduceCost
	PKRelative
	PKRememberChanged
	PKRemoveAllAbilities
	PKRemoveCardTypes
	PKRemoveCreatureTypes
	PKRemoveKeyword
	PKRevolt
	PKSetColor
	PKSetName
	PKSetPower
	PKSetToughness
	PKSorcerySpeed
	PKSpellDescription
	PKSubAbility
	PKTargetMax
	PKTargetMin
	PKTargetType
	PKTargetValidTargeting
	PKTargetingPlayerControls
	PKTargetsForEachPlayer
	PKTargetsWithControllerProperty
	PKTargetsWithDefinedController
	PKTargetsWithDifferentCMC
	PKTargetsWithDifferentControllers
	PKTargetsWithEqualToughness
	PKTargetsWithSameCardType
	PKTargetsWithSameController
	PKTargetsWithSameCreatureType
	PKTargetsWithSharedCardType
	PKTgtZone
	PKTriggerZones
	PKType
	PKUnattach
	PKUnlessCost
	PKValidCard
	PKValidCards
	PKValidCause
	PKValidLKI
	PKValidPlayer
	PKValidSource
	PKValidSpell
	PKValidTarget
	PKValidTgts
	paramKeyCount
)

// A ParamMask holds 128 keys.
const _ = uint(128 - int(paramKeyCount))

// paramKeyNames maps each ParamKey to its Forge key text.
var paramKeyNames = [paramKeyCount]string{
	PKActivation:                      "Activation",
	PKActivationAfterBlockers:         "ActivationAfterBlockers",
	PKActivationFirstCombat:           "ActivationFirstCombat",
	PKActivationGameTypes:             "ActivationGameTypes",
	PKActivationLimit:                 "ActivationLimit",
	PKActivationPhases:                "ActivationPhases",
	PKActivationZone:                  "ActivationZone",
	PKActivator:                       "Activator",
	PKActiveZones:                     "ActiveZones",
	PKAddAbility:                      "AddAbility",
	PKAddAllCreatureTypes:             "AddAllCreatureTypes",
	PKAddColor:                        "AddColor",
	PKAddColors:                       "AddColors",
	PKAddKeyword:                      "AddKeyword",
	PKAddPower:                        "AddPower",
	PKAddSVar:                         "AddSVar",
	PKAddStaticAbility:                "AddStaticAbility",
	PKAddToughness:                    "AddToughness",
	PKAddTrigger:                      "AddTrigger",
	PKAddType:                         "AddType",
	PKAddTypes:                        "AddTypes",
	PKAdjustLandPlays:                 "AdjustLandPlays",
	PKAffected:                        "Affected",
	PKAffectedZone:                    "AffectedZone",
	PKAmount:                          "Amount",
	PKAnnounce:                        "Announce",
	PKBoast:                           "Boast",
	PKCantHaveKeyword:                 "CantHaveKeyword",
	PKCaster:                          "Caster",
	PKCharacteristicDefining:          "CharacteristicDefining",
	PKCheckSVar:                       "CheckSVar",
	PKCheckSecondSVar:                 "CheckSecondSVar",
	PKChoices:                         "Choices",
	PKClassBand:                       "ClassBand",
	PKCondition:                       "Condition",
	PKConditionActivationLimit:        "ConditionActivationLimit",
	PKConditionDefined:                "ConditionDefined",
	PKCost:                            "Cost",
	PKCounterType:                     "CounterType",
	PKDefined:                         "Defined",
	PKDefinedPlayer:                   "DefinedPlayer",
	PKDestination:                     "Destination",
	PKDiscard:                         "Discard",
	PKDuration:                        "Duration",
	PKEffectOnly:                      "EffectOnly",
	PKEffectZone:                      "EffectZone",
	PKEvolve:                          "Evolve",
	PKExcludeZone:                     "ExcludeZone",
	PKExecute:                         "Execute",
	PKExhaust:                         "Exhaust",
	PKFirstForetell:                   "FirstForetell",
	PKFoundSearchingLibrary:           "FoundSearchingLibrary",
	PKGainControl:                     "GainControl",
	PKGainsAbilitiesOf:                "GainsAbilitiesOf",
	PKGainsAbilitiesOfDefined:         "GainsAbilitiesOfDefined",
	PKGainsTriggerAbsOf:               "GainsTriggerAbsOf",
	PKGameActivationLimit:             "GameActivationLimit",
	PKGoad:                            "Goad",
	PKImprintCards:                    "ImprintCards",
	PKIsPresent:                       "IsPresent",
	PKIsPresent2:                      "IsPresent2",
	PKKeyword:                         "Keyword",
	PKMaxTotalTargetCMC:               "MaxTotalTargetCMC",
	PKMayLookAt:                       "MayLookAt",
	PKMayPlay:                         "MayPlay",
	PKMayPlayAltManaCost:              "MayPlayAltManaCost",
	PKMayPlayWithoutManaCost:          "MayPlayWithoutManaCost",
	PKMentor:                          "Mentor",
	PKMonstrosity:                     "Monstrosity",
	PKNotThisAbility:                  "NotThisAbility",
	PKOnlyFirstSpell:                  "OnlyFirstSpell",
	PKOpponentTurn:                    "OpponentTurn",
	PKOptional:                        "Optional",
	PKOptionalDecider:                 "OptionalDecider",
	PKOrigin:                          "Origin",
	PKPhase:                           "Phase",
	PKPlayerTurn:                      "PlayerTurn",
	PKPowerUp:                         "PowerUp",
	PKPresentZone:                     "PresentZone",
	PKProduced:                        "Produced",
	PKReduceCost:                      "ReduceCost",
	PKRelative:                        "Relative",
	PKRememberChanged:                 "RememberChanged",
	PKRemoveAllAbilities:              "RemoveAllAbilities",
	PKRemoveCardTypes:                 "RemoveCardTypes",
	PKRemoveCreatureTypes:             "RemoveCreatureTypes",
	PKRemoveKeyword:                   "RemoveKeyword",
	PKRevolt:                          "Revolt",
	PKSetColor:                        "SetColor",
	PKSetName:                         "SetName",
	PKSetPower:                        "SetPower",
	PKSetToughness:                    "SetToughness",
	PKSorcerySpeed:                    "SorcerySpeed",
	PKSpellDescription:                "SpellDescription",
	PKSubAbility:                      "SubAbility",
	PKTargetMax:                       "TargetMax",
	PKTargetMin:                       "TargetMin",
	PKTargetType:                      "TargetType",
	PKTargetValidTargeting:            "TargetValidTargeting",
	PKTargetingPlayerControls:         "TargetingPlayerControls",
	PKTargetsForEachPlayer:            "TargetsForEachPlayer",
	PKTargetsWithControllerProperty:   "TargetsWithControllerProperty",
	PKTargetsWithDefinedController:    "TargetsWithDefinedController",
	PKTargetsWithDifferentCMC:         "TargetsWithDifferentCMC",
	PKTargetsWithDifferentControllers: "TargetsWithDifferentControllers",
	PKTargetsWithEqualToughness:       "TargetsWithEqualToughness",
	PKTargetsWithSameCardType:         "TargetsWithSameCardType",
	PKTargetsWithSameController:       "TargetsWithSameController",
	PKTargetsWithSameCreatureType:     "TargetsWithSameCreatureType",
	PKTargetsWithSharedCardType:       "TargetsWithSharedCardType",
	PKTgtZone:                         "TgtZone",
	PKTriggerZones:                    "TriggerZones",
	PKType:                            "Type",
	PKUnattach:                        "Unattach",
	PKUnlessCost:                      "UnlessCost",
	PKValidCard:                       "ValidCard",
	PKValidCards:                      "ValidCards",
	PKValidCause:                      "ValidCause",
	PKValidLKI:                        "ValidLKI",
	PKValidPlayer:                     "ValidPlayer",
	PKValidSource:                     "ValidSource",
	PKValidSpell:                      "ValidSpell",
	PKValidTarget:                     "ValidTarget",
	PKValidTgts:                       "ValidTgts",
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
	// src is the map the set was built from. Held as the map itself (not
	// its address) so two identically parsed nodes stay reflect.DeepEqual;
	// bound compares identities.
	src  map[string]string
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
	ps := &ParamSet{src: m, n: len(m)}
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
	return ps != nil && ps.n == len(m) && mapIdentity(ps.src) == mapIdentity(m)
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

// Param is Params[k] with presence, through the compiled set when bound.
func (r Repl) Param(k ParamKey) (string, bool) { return paramGet(r.ps, r.Params, k) }

// ParamStr is Params[k] ("" when absent).
func (r Repl) ParamStr(k ParamKey) string { v, _ := paramGet(r.ps, r.Params, k); return v }

// HasParam reports whether key k is present.
func (r Repl) HasParam(k ParamKey) bool { _, ok := paramGet(r.ps, r.Params, k); return ok }

// ParamSetParam reads key k of m through ps (a view carrying a node's Params
// map and its ParamSet side by side).
func ParamSetParam(ps *ParamSet, m map[string]string, k ParamKey) (string, bool) {
	return paramGet(ps, m, k)
}

// ParamSetMayHaveAny is MayHaveAnyParam for a view's (ps, map) pair.
func ParamSetMayHaveAny(ps *ParamSet, m map[string]string, mask ParamMask) bool {
	return paramMayHaveAny(ps, m, mask)
}

// Param is Params[k] with presence, through the compiled set when bound.
func (sa *SA) Param(k ParamKey) (string, bool) { return paramGet(sa.ps, sa.Params, k) }

// ParamStr is Params[k] ("" when absent).
func (sa *SA) ParamStr(k ParamKey) string { v, _ := paramGet(sa.ps, sa.Params, k); return v }

// HasParam reports whether key k is present.
func (sa *SA) HasParam(k ParamKey) bool { _, ok := paramGet(sa.ps, sa.Params, k); return ok }

// MayHaveAnyParam is false only when the ability provably holds no key of
// mask.
func (sa *SA) MayHaveAnyParam(mask ParamMask) bool { return paramMayHaveAny(sa.ps, sa.Params, mask) }

// deriveParamSets binds each printed static's, trigger's and ability's
// ParamSet (every ability reachable from the face: its Abilities, their
// SubAbility$ chains, trigger Execute$ bodies and replacement bodies). It
// runs at load (derive, and again at the end of link, which re-resolves
// bodies); a node built later stays unbound and reads its map.
func (f *Face) deriveParamSets() {
	for i := range f.Statics {
		f.Statics[i].ps = newParamSet(f.Statics[i].Params)
	}
	for i := range f.Triggers {
		f.Triggers[i].ps = newParamSet(f.Triggers[i].Params)
	}
	for i := range f.Repls {
		f.Repls[i].ps = newParamSet(f.Repls[i].Params)
	}
	bindSA := func(sa *SA) {
		for d := 0; sa != nil && d <= maxSVarDepth+1; d++ {
			sa.ps = newParamSet(sa.Params)
			if sa.extSlot == nil {
				sa.extSlot = &ExtSlot{}
			}
			sa = sa.Sub
		}
	}
	for _, a := range f.Abilities {
		bindSA(a)
	}
	for i := range f.Triggers {
		bindSA(f.Triggers[i].Effect)
	}
	for i := range f.Repls {
		bindSA(f.Repls[i].With)
	}
}
