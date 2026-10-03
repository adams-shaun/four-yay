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
	PKAddAbilities
	PKAddAbility
	PKAddAllCreatureTypes
	PKAddColor
	PKAddColors
	PKAddKeyword
	PKAddKeywords
	PKAddPower
	PKAddSVar
	PKAddSVars
	PKAddStaticAbilities
	PKAddStaticAbility
	PKAddToughness
	PKAddTrigger
	PKAddTriggers
	PKAddType
	PKAddTypes
	PKAdjustLandPlays
	PKAffected
	PKAffectedZone
	PKAmount
	PKAnnounce
	PKAnyNumber
	PKAttachedTo
	PKBoast
	PKCantHaveKeyword
	PKCaster
	PKChangeNum
	PKChangeType
	PKCharacteristicDefining
	PKCheckSVar
	PKCheckSecondSVar
	PKChoiceOptional
	PKChoiceTitle
	PKChoiceZone
	PKChoices
	PKChooseFromDefined
	PKChooser
	PKClassBand
	PKClearImprinted
	PKCondition
	PKConditionActivationLimit
	PKConditionDefined
	PKController
	PKCost
	PKCounterNum
	PKCounterType
	PKDefined
	PKDefinedCards
	PKDefinedPlayer
	PKDestAltSVar
	PKDestination
	PKDestinationAlternative
	PKDiscard
	PKDividedAsYouChoose
	PKDuration
	PKETB
	PKEffectOnly
	PKEffectZone
	PKEvolve
	PKExcludeZone
	PKExecute
	PKExhaust
	PKFirstForetell
	PKForgetOtherRemembered
	PKFoundSearchingLibrary
	PKGainControl
	PKGainsAbilitiesLimitPerTurn
	PKGainsAbilitiesOf
	PKGainsAbilitiesOfDefined
	PKGainsAbilitiesOfZones
	PKGainsTriggerAbsOf
	PKGainsValidAbilities
	PKGameActivationLimit
	PKGoad
	PKImprint
	PKImprintCards
	PKImprintLast
	PKIntoPlayTapped
	PKIsPresent
	PKIsPresent2
	PKKW
	PKKeyword
	PKLeaveBattlefield
	PKLibraryPosition
	PKMandatory
	PKMax
	PKMaxTotalTargetCMC
	PKMayLookAt
	PKMayPlay
	PKMayPlayAltManaCost
	PKMayPlayWithoutManaCost
	PKMentor
	PKMin
	PKMode
	PKMonstrosity
	PKNewController
	PKNoLooking
	PKNoShuffle
	PKNotThisAbility
	PKNumDmg
	PKObject
	PKOnlyFirstSpell
	PKOpponentTurn
	PKOptional
	PKOptionalDecider
	PKOptionalPrompt
	PKOrigin
	PKPhase
	PKPlayerTurn
	PKPowerUp
	PKPresentCompare
	PKPresentDefined
	PKPresentZone
	PKPrevent
	PKProduced
	PKRandom
	PKReduceCost
	PKRelative
	PKRememberChanged
	PKRememberLKI
	PKRememberObjects
	PKRememberPut
	PKRememberTargets
	PKRemoveAllAbilities
	PKRemoveCardTypes
	PKRemoveCreatureTypes
	PKRemoveKeyword
	PKRemoveType
	PKRestrictValid
	PKReveal
	PKRevolt
	PKSVarCompare
	PKSecretly
	PKSetColor
	PKSetName
	PKSetPower
	PKSetToughness
	PKShuffle
	PKSorcerySpeed
	PKSpellDescription
	PKStatic
	PKStaticAbilities
	PKSubAbility
	PKTapped
	PKTargetMax
	PKTargetMin
	PKTargetType
	PKTargetValidTargeting
	PKTargetingPlayer
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
	PKThisTurn
	PKTokenScript
	PKTriggerZones
	PKTriggers
	PKType
	PKUnattach
	PKUnlessCost
	PKUnlessSwitched
	PKUpTo
	PKValidCard
	PKValidCards
	PKValidCause
	PKValidChoices
	PKValidLKI
	PKValidPlayer
	PKValidSA
	PKValidSource
	PKValidSpell
	PKValidTarget
	PKValidTgts
	PKWithCountersAmount
	PKWithCountersType
	PKZone
	paramKeyCount
)

// paramMaskWords is a ParamMask's width in 64-bit words: 256 keys, the whole
// ParamKey (uint8) range, so the ordinal itself caps the vocabulary.
const paramMaskWords = 4

// A ParamMask holds every ParamKey.
const _ = uint(paramMaskWords*64 - int(paramKeyCount))

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
	PKAddAbilities:                    "AddAbilities",
	PKAddAbility:                      "AddAbility",
	PKAddAllCreatureTypes:             "AddAllCreatureTypes",
	PKAddColor:                        "AddColor",
	PKAddColors:                       "AddColors",
	PKAddKeyword:                      "AddKeyword",
	PKAddKeywords:                     "AddKeywords",
	PKAddPower:                        "AddPower",
	PKAddSVar:                         "AddSVar",
	PKAddSVars:                        "AddSVars",
	PKAddStaticAbilities:              "AddStaticAbilities",
	PKAddStaticAbility:                "AddStaticAbility",
	PKAddToughness:                    "AddToughness",
	PKAddTrigger:                      "AddTrigger",
	PKAddTriggers:                     "AddTriggers",
	PKAddType:                         "AddType",
	PKAddTypes:                        "AddTypes",
	PKAdjustLandPlays:                 "AdjustLandPlays",
	PKAffected:                        "Affected",
	PKAffectedZone:                    "AffectedZone",
	PKAmount:                          "Amount",
	PKAnnounce:                        "Announce",
	PKAnyNumber:                       "AnyNumber",
	PKAttachedTo:                      "AttachedTo",
	PKBoast:                           "Boast",
	PKCantHaveKeyword:                 "CantHaveKeyword",
	PKCaster:                          "Caster",
	PKChangeNum:                       "ChangeNum",
	PKChangeType:                      "ChangeType",
	PKCharacteristicDefining:          "CharacteristicDefining",
	PKCheckSVar:                       "CheckSVar",
	PKCheckSecondSVar:                 "CheckSecondSVar",
	PKChoiceOptional:                  "ChoiceOptional",
	PKChoiceTitle:                     "ChoiceTitle",
	PKChoiceZone:                      "ChoiceZone",
	PKChoices:                         "Choices",
	PKChooseFromDefined:               "ChooseFromDefined",
	PKChooser:                         "Chooser",
	PKClassBand:                       "ClassBand",
	PKClearImprinted:                  "ClearImprinted",
	PKCondition:                       "Condition",
	PKConditionActivationLimit:        "ConditionActivationLimit",
	PKConditionDefined:                "ConditionDefined",
	PKController:                      "Controller",
	PKCost:                            "Cost",
	PKCounterNum:                      "CounterNum",
	PKCounterType:                     "CounterType",
	PKDefined:                         "Defined",
	PKDefinedCards:                    "DefinedCards",
	PKDefinedPlayer:                   "DefinedPlayer",
	PKDestAltSVar:                     "DestAltSVar",
	PKDestination:                     "Destination",
	PKDestinationAlternative:          "DestinationAlternative",
	PKDiscard:                         "Discard",
	PKDividedAsYouChoose:              "DividedAsYouChoose",
	PKDuration:                        "Duration",
	PKETB:                             "ETB",
	PKEffectOnly:                      "EffectOnly",
	PKEffectZone:                      "EffectZone",
	PKEvolve:                          "Evolve",
	PKExcludeZone:                     "ExcludeZone",
	PKExecute:                         "Execute",
	PKExhaust:                         "Exhaust",
	PKFirstForetell:                   "FirstForetell",
	PKForgetOtherRemembered:           "ForgetOtherRemembered",
	PKFoundSearchingLibrary:           "FoundSearchingLibrary",
	PKGainControl:                     "GainControl",
	PKGainsAbilitiesLimitPerTurn:      "GainsAbilitiesLimitPerTurn",
	PKGainsAbilitiesOf:                "GainsAbilitiesOf",
	PKGainsAbilitiesOfDefined:         "GainsAbilitiesOfDefined",
	PKGainsAbilitiesOfZones:           "GainsAbilitiesOfZones",
	PKGainsTriggerAbsOf:               "GainsTriggerAbsOf",
	PKGainsValidAbilities:             "GainsValidAbilities",
	PKGameActivationLimit:             "GameActivationLimit",
	PKGoad:                            "Goad",
	PKImprint:                         "Imprint",
	PKImprintCards:                    "ImprintCards",
	PKImprintLast:                     "ImprintLast",
	PKIntoPlayTapped:                  "IntoPlayTapped",
	PKIsPresent:                       "IsPresent",
	PKIsPresent2:                      "IsPresent2",
	PKKW:                              "KW",
	PKKeyword:                         "Keyword",
	PKLeaveBattlefield:                "LeaveBattlefield",
	PKLibraryPosition:                 "LibraryPosition",
	PKMandatory:                       "Mandatory",
	PKMax:                             "Max",
	PKMaxTotalTargetCMC:               "MaxTotalTargetCMC",
	PKMayLookAt:                       "MayLookAt",
	PKMayPlay:                         "MayPlay",
	PKMayPlayAltManaCost:              "MayPlayAltManaCost",
	PKMayPlayWithoutManaCost:          "MayPlayWithoutManaCost",
	PKMentor:                          "Mentor",
	PKMin:                             "Min",
	PKMode:                            "Mode",
	PKMonstrosity:                     "Monstrosity",
	PKNewController:                   "NewController",
	PKNoLooking:                       "NoLooking",
	PKNoShuffle:                       "NoShuffle",
	PKNotThisAbility:                  "NotThisAbility",
	PKNumDmg:                          "NumDmg",
	PKObject:                          "Object",
	PKOnlyFirstSpell:                  "OnlyFirstSpell",
	PKOpponentTurn:                    "OpponentTurn",
	PKOptional:                        "Optional",
	PKOptionalDecider:                 "OptionalDecider",
	PKOptionalPrompt:                  "OptionalPrompt",
	PKOrigin:                          "Origin",
	PKPhase:                           "Phase",
	PKPlayerTurn:                      "PlayerTurn",
	PKPowerUp:                         "PowerUp",
	PKPresentCompare:                  "PresentCompare",
	PKPresentDefined:                  "PresentDefined",
	PKPresentZone:                     "PresentZone",
	PKPrevent:                         "Prevent",
	PKProduced:                        "Produced",
	PKRandom:                          "Random",
	PKReduceCost:                      "ReduceCost",
	PKRelative:                        "Relative",
	PKRememberChanged:                 "RememberChanged",
	PKRememberLKI:                     "RememberLKI",
	PKRememberObjects:                 "RememberObjects",
	PKRememberPut:                     "RememberPut",
	PKRememberTargets:                 "RememberTargets",
	PKRemoveAllAbilities:              "RemoveAllAbilities",
	PKRemoveCardTypes:                 "RemoveCardTypes",
	PKRemoveCreatureTypes:             "RemoveCreatureTypes",
	PKRemoveKeyword:                   "RemoveKeyword",
	PKRemoveType:                      "RemoveType",
	PKRestrictValid:                   "RestrictValid",
	PKReveal:                          "Reveal",
	PKRevolt:                          "Revolt",
	PKSVarCompare:                     "SVarCompare",
	PKSecretly:                        "Secretly",
	PKSetColor:                        "SetColor",
	PKSetName:                         "SetName",
	PKSetPower:                        "SetPower",
	PKSetToughness:                    "SetToughness",
	PKShuffle:                         "Shuffle",
	PKSorcerySpeed:                    "SorcerySpeed",
	PKSpellDescription:                "SpellDescription",
	PKStatic:                          "Static",
	PKStaticAbilities:                 "StaticAbilities",
	PKSubAbility:                      "SubAbility",
	PKTapped:                          "Tapped",
	PKTargetMax:                       "TargetMax",
	PKTargetMin:                       "TargetMin",
	PKTargetType:                      "TargetType",
	PKTargetValidTargeting:            "TargetValidTargeting",
	PKTargetingPlayer:                 "TargetingPlayer",
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
	PKThisTurn:                        "ThisTurn",
	PKTokenScript:                     "TokenScript",
	PKTriggerZones:                    "TriggerZones",
	PKTriggers:                        "Triggers",
	PKType:                            "Type",
	PKUnattach:                        "Unattach",
	PKUnlessCost:                      "UnlessCost",
	PKUnlessSwitched:                  "UnlessSwitched",
	PKUpTo:                            "UpTo",
	PKValidCard:                       "ValidCard",
	PKValidCards:                      "ValidCards",
	PKValidCause:                      "ValidCause",
	PKValidChoices:                    "ValidChoices",
	PKValidLKI:                        "ValidLKI",
	PKValidPlayer:                     "ValidPlayer",
	PKValidSA:                         "ValidSA",
	PKValidSource:                     "ValidSource",
	PKValidSpell:                      "ValidSpell",
	PKValidTarget:                     "ValidTarget",
	PKValidTgts:                       "ValidTgts",
	PKWithCountersAmount:              "WithCountersAmount",
	PKWithCountersType:                "WithCountersType",
	PKZone:                            "Zone",
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
type ParamMask [paramMaskWords]uint64

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
//
// A read is a mask test and a rank: rank[w] is the number of keys present in
// the words below w (a per-word popcount prefix computed once at build), so
// a key's value index is rank[w] plus the popcount of its own word below its
// bit, whichever word it lives in. No loop, no map, one branch.
type ParamSet struct {
	// src is the map the set was built from. Held as the map itself (not
	// its address) so two identically parsed nodes stay reflect.DeepEqual;
	// bound compares identities.
	src  map[string]string
	n    int
	has  ParamMask
	rank [paramMaskWords]uint8
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
	n := 0
	for w, h := range ps.has {
		ps.rank[w] = uint8(n)
		n += bits.OnesCount64(h)
	}
	ps.vals = make([]string, 0, n)
	for w, h := range ps.has {
		for ; h != 0; h &= h - 1 {
			k := ParamKey(w<<6 | bits.TrailingZeros64(h))
			ps.vals = append(ps.vals, m[paramKeyNames[k]])
		}
	}
	return ps
}

func (ps *ParamSet) bound(m map[string]string) bool {
	return ps != nil && ps.n == len(m) && mapIdentity(ps.src) == mapIdentity(m)
}

func (ps *ParamSet) get(k ParamKey) (string, bool) {
	w := k >> 6
	h, b := ps.has[w], uint64(1)<<(k&63)
	if h&b == 0 {
		return "", false
	}
	return ps.vals[int(ps.rank[w])+bits.OnesCount64(h&(b-1))], true
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
		var x uint64
		for w := range mask {
			x |= ps.has[w] & mask[w]
		}
		return x != 0
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
