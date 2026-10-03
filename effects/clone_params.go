package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// This file is api:Clone's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on CopyPermanent's pattern (copypermanent_params.go). compileClone is
// the ONLY reader of a Clone ability's own parameters: the resolution
// (clone.go) and rules' as-enters copy election (cast_etbchoice.go) read the
// compiled CloneParams. Every value that is a pure function of the text --
// the ChoiceZone$ classification, the type/keyword/name lists, the SetColor$
// parse, the ETB whitelist's text half -- is decided here once.
// internal/codeshape's cloneParamLeaks ratchet holds that: no parameter read
// in clone.go, and no read of a Clone-only key anywhere else in rules/ or
// effects/.
//
// The lists below are SHARED by every resolution of the ability: a reader
// that hands one to game state (a continuous effect's AddTypes/AddKeywords/
// AddAbilities) clones it first, so no state object aliases the compiled
// record.

// CloneParams is one Clone ability's parameters, compiled once. Text fields
// are trimmed unless noted.
type CloneParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// CopyFromChosenName is CopyFromChosenName$ True.
	CopyFromChosenName bool
	// Defined is Defined$ (the Defined tier's Ref text), the copy source.
	Defined string
	// Choices is Choices$, the mid-resolution (and ETB) copy-template
	// selector; ChoicesRaw is the untrimmed value.
	Choices    string
	ChoicesRaw string
	// ChoiceZone is ChoiceZone$; ChoiceZoneKind/ChoiceZoneOK are its
	// cloneChoiceZone classification.
	ChoiceZone     string
	ChoiceZoneKind state.Zone
	ChoiceZoneOK   bool
	// ChoiceOptional is ChoiceOptional$ True; ChoiceTitle is ChoiceTitle$.
	ChoiceOptional bool
	ChoiceTitle    string
	// CloneTarget is CloneTarget$, the become operand.
	CloneTarget string
	// ExcludeChosen is ExcludeChosen$ True.
	ExcludeChosen bool
	// CloneZone is CloneZone$, the copy source's required zone.
	CloneZone string
	// Optional is Optional$ True.
	Optional bool

	// TypeAdds is the layer-4 add list: AddTypes$ members, then
	// SetCreatureTypes$ members; SetCreatureTypes reports a non-empty
	// SetCreatureTypes$ list.
	TypeAdds         []string
	SetCreatureTypes bool
	// The layer-4 removal flags: NonLegendary$, RemoveSubTypes$,
	// RemoveCardTypes$ and RemoveCreatureTypes$ True.
	NonLegendary        bool
	RemoveSubTypes      bool
	RemoveCardTypes     bool
	RemoveCreatureTypes bool
	// AddAbilities, AddSVars and AddTriggers are the named grants' SVar
	// names in printed order; StaticNames are AddStaticAbilities$' names.
	AddAbilities []string
	AddSVars     []string
	AddTriggers  []string
	StaticNames  []string
	// AddKeywords and PumpKeywords are the keyword lists; PumpDuration is
	// PumpDuration$.
	AddKeywords  []string
	PumpKeywords []string
	PumpDuration string
	// NewName is NewName$; GainThisAbility is GainThisAbility$ True.
	NewName         string
	GainThisAbility bool
	// SetPower and SetToughness are the raw P/T setters (the Num grammar).
	SetPower     ParamText
	SetToughness ParamText
	// SetColor is SetColor$; SetColors/SetColorOK are its colour parse.
	SetColor   string
	SetColors  []string
	SetColorOK bool
	// IntoPlayTapped is IntoPlayTapped$ as the script spells it (the
	// no-entry Note quotes it); IntoPlayTappedSet reports a non-blank value
	// and IntoPlayTappedTrue the exact True spelling.
	IntoPlayTapped     string
	IntoPlayTappedSet  bool
	IntoPlayTappedTrue bool
	// Duration is Duration$; AttachedTo is AttachedTo$.
	Duration   string
	AttachedTo string
	// FaceDown is FaceDown$ True; KeepFacedownFalse is KeepFacedown$ False.
	FaceDown          bool
	KeepFacedownFalse bool

	// ETBShapeOK is the text half of rules' ETB copy whitelist
	// (etbCloneWhitelist): every key inside the supported set, a Choices$
	// selector the no-resolver matcher can decide, single-word AddKeywords$
	// heads and an absent-or-True IntoPlayTapped$. The SVar-table half (each
	// StaticNames member readable) is the caller's.
	ETBShapeOK bool

	// Unread are the parameters present on the ability that no Clone reader
	// consumes: the resolution Notes them once.
	Unread []string
}

// cloneKnownKeys is every parameter key a Clone resolution consumes or
// deliberately ignores, sorted (changeZoneKnownKeys' contract): rules'
// TestCloneKnownKeysMatchTheCensus holds it equal to the parameter census's
// measured api:Clone read set plus its ignored and structural keys.
var cloneKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment",
	"AITgts", "Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "AddAbilities", "AddKeywords", "AddSVars", "AddStaticAbilities",
	"AddTriggers", "AddType", "AddTypes", "AdditionalDesc", "AdditionalDescription",
	"Affected", "AlternateCost", "AlternativeCost", "Announce", "AnnounceTitle",
	"AttachedTo", "Boast", "ChangeTypeDesc", "CharacteristicDefining", "CheckSVar",
	"ChoiceOptional", "ChoiceTitle", "ChoiceZone", "Choices", "ChooseFromList",
	"ClassBand", "ClearImprinted", "CloneTarget", "CloneZone", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn",
	"ConditionPresent", "ConditionSVarCompare", "CopyCard", "CopyFromChosenName",
	"Cost", "CostDesc", "Defined", "DefinedCards", "DefinedTarget", "Description",
	"Duration", "Exclude", "ExcludeChosen", "Execute", "Exhaust", "FaceDown",
	"GainThisAbility", "GameActivationLimit", "Image", "ImprintCards",
	"ImprintPlayed", "InstantSpeed", "IntoPlayTapped", "IsCurse", "IsPresent", "KW",
	"KeepFacedown", "Keyword", "KeywordLine", "MaxTotalTargetCMC",
	"MaxTotalTargetPower", "Mentor", "ModeCost", "Monstrosity", "NewController",
	"NewName", "NonLegendary", "NumDmg", "OpponentTurn", "Optional", "Planeswalker",
	"PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc", "PresentCompare",
	"PresentDefined", "PresentZone", "PumpDuration", "PumpKeywords", "ReduceAmount",
	"ReduceCost", "RememberCostMana", "RememberObjects", "RemoveCardTypes",
	"RemoveCreatureTypes", "RemoveSubTypes", "ReplaceColor", "ReplaceGraveyard",
	"ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly", "ReplaceType",
	"SVarCompare", "SelectPrompt", "SetChosenMode", "SetColor", "SetCreatureTypes",
	"SetPower", "SetToughness", "ShowCards", "SorcerySpeed", "SpellDescription",
	"StackDescription", "SubAbility", "TargetMax", "TargetMin", "TargetType",
	"TargetUnique", "TargetValidTargeting", "TargetingPlayer",
	"TargetingPlayerControls", "TargetsForEachPlayer",
	"TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness",
	"TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenScript",
	"TriggerDescription", "Type", "Ultimate", "UnlessAI", "UnlessCost",
	"UnlessPayer", "UnlessResolveSubs", "UnlessSwitched", "ValidCard", "ValidCards",
	"ValidCardsDesc", "ValidChoices", "ValidCounterType", "ValidDescription",
	"ValidTgts", "VoteMessage", "WithoutManaCost", "XMax", "XMin",
}

// isCloneSA reports whether sa resolves as api:Clone.
func isCloneSA(sa *cards.SA) bool {
	return sa != nil && sa.API == "Clone"
}

// CloneOf returns sa's compiled Clone parameters: the configured record's
// when it is bound to sa's Params map, else the front cache's entry for that
// map, else a fresh compile stored in the front cache (ChangeZoneOf's
// contract).
func CloneOf(sa *cards.SA) *CloneParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Clone; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &cloneFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileClone(sa, DefinedOf(sa))
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// cloneFront is CloneOf's direct-mapped front cache (czFront's shape).
var cloneFront [1 << 10]atomic.Pointer[CloneParams]

// compileClone is the one reader of a Clone ability's own parameters; the
// Defined$ source selector comes from the Defined-reference tier's compiled
// DefinedParams.
func compileClone(sa *cards.SA, dr *DefinedParams) *CloneParams {
	p := &CloneParams{paramBinding: bindParams(sa)}
	p.CopyFromChosenName = strings.EqualFold(rawParamText(sa, "CopyFromChosenName").Text, "True")
	p.Defined = dr.Defined.Text
	p.ChoicesRaw = sa.ParamStr(cards.PKChoices)
	p.Choices = strings.TrimSpace(p.ChoicesRaw)
	p.ChoiceZone = strings.TrimSpace(sa.ParamStr(cards.PKChoiceZone))
	p.ChoiceZoneKind, p.ChoiceZoneOK = cloneChoiceZone(p.ChoiceZone)
	p.ChoiceOptional = isTrue(sa.ParamStr(cards.PKChoiceOptional))
	p.ChoiceTitle = strings.TrimSpace(sa.ParamStr(cards.PKChoiceTitle))
	p.CloneTarget = strings.TrimSpace(rawParamText(sa, "CloneTarget").Text)
	p.ExcludeChosen = isTrue(rawParamText(sa, "ExcludeChosen").Text)
	p.CloneZone = strings.TrimSpace(rawParamText(sa, "CloneZone").Text)
	p.Optional = isTrue(sa.ParamStr(cards.PKOptional))

	p.TypeAdds = splitAmp(strings.TrimSpace(sa.ParamStr(cards.PKAddTypes)))
	if setCreatureTypes := splitAmp(strings.TrimSpace(rawParamText(sa, "SetCreatureTypes").Text)); len(setCreatureTypes) > 0 {
		p.TypeAdds = append(p.TypeAdds, setCreatureTypes...)
		p.SetCreatureTypes = true
	}
	p.NonLegendary = isTrue(sa.ParamStr(cards.PKNonLegendary))
	p.RemoveSubTypes = isTrue(rawParamText(sa, "RemoveSubTypes").Text)
	p.RemoveCardTypes = isTrue(sa.ParamStr(cards.PKRemoveCardTypes))
	p.RemoveCreatureTypes = isTrue(sa.ParamStr(cards.PKRemoveCreatureTypes))
	p.AddAbilities = cloneNames(sa.ParamStr(cards.PKAddAbilities))
	p.AddSVars = cloneNames(sa.ParamStr(cards.PKAddSVars))
	p.AddTriggers = cloneNames(sa.ParamStr(cards.PKAddTriggers))
	p.StaticNames = strings.FieldsFunc(sa.ParamStr(cards.PKAddStaticAbilities), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	p.AddKeywords = cards.SplitKeywordList(sa.ParamStr(cards.PKAddKeywords))
	p.PumpKeywords = cards.SplitKeywordList(sa.ParamStr(cards.PKPumpKeywords))
	p.PumpDuration = strings.TrimSpace(sa.ParamStr(cards.PKPumpDuration))
	p.NewName = strings.TrimSpace(rawParamText(sa, "NewName").Text)
	p.GainThisAbility = isTrue(rawParamText(sa, "GainThisAbility").Text)
	setPower, ok := sa.Param(cards.PKSetPower)
	p.SetPower = ParamText{Text: setPower, Present: ok}
	setToughness, ok := sa.Param(cards.PKSetToughness)
	p.SetToughness = ParamText{Text: setToughness, Present: ok}
	p.SetColor = strings.TrimSpace(sa.ParamStr(cards.PKSetColor))
	if p.SetColor != "" {
		p.SetColors, p.SetColorOK = colorLetters(p.SetColor)
	}
	p.IntoPlayTapped = sa.ParamStr(cards.PKIntoPlayTapped)
	p.IntoPlayTappedSet = strings.TrimSpace(p.IntoPlayTapped) != ""
	p.IntoPlayTappedTrue = strings.EqualFold(p.IntoPlayTapped, "True")
	p.Duration = strings.TrimSpace(sa.ParamStr(cards.PKDuration))
	p.AttachedTo = strings.TrimSpace(sa.ParamStr(cards.PKAttachedTo))
	p.FaceDown = strings.EqualFold(sa.ParamStr(cards.PKFaceDown), "True")
	p.KeepFacedownFalse = strings.EqualFold(rawParamText(sa, "KeepFacedown").Text, "False")

	p.ETBShapeOK = cloneETBShapeOK(sa, p)
	p.Unread = unreadKeys(sa, cloneKnownKeys[:])
	return p
}

// cloneETBShapeOK is ETBShapeOK's computation: rules' etbCloneWhitelist's
// POSITIVE key whitelist (Choices$, AddKeywords$, AddTypes$,
// SpellDescription$, AddStaticAbilities$, IntoPlayTapped$) plus its
// text-only value checks. The key scan only answers a boolean, so the map's
// iteration order cannot reach an event.
func cloneETBShapeOK(sa *cards.SA, p *CloneParams) bool {
	for k := range sa.Params {
		switch k {
		case "Choices", "AddKeywords", "AddTypes", "SpellDescription", "AddStaticAbilities", "IntoPlayTapped":
		default:
			return false
		}
	}
	if SpecNeedsResolver(p.Choices) {
		return false
	}
	for _, kw := range p.AddKeywords {
		if strings.ContainsAny(cards.KeywordHead(kw), " \t") {
			return false
		}
	}
	if _, ok := sa.Param(cards.PKIntoPlayTapped); ok && !p.IntoPlayTappedTrue {
		return false
	}
	return true
}

// CloneKnownKeys is a copy of cloneKnownKeys, for the census check.
func CloneKnownKeys() []string { return slices.Clone(cloneKnownKeys[:]) }
