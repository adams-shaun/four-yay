package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// This file is api:RemoveCounter's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on CopyPermanent's pattern (copypermanent_params.go).
// compileRemoveCounter is the ONLY reader of a RemoveCounter ability's own
// parameters: the resolution (removecounter.go) reads the compiled
// RemoveCounterParams, including both loud exotic-shape Notes (the defined
// path's and the Choices$ arm's), which are pure functions of the text and
// so are decided here once. internal/codeshape's removeCounterParamLeaks
// ratchet holds that: no parameter read in removecounter.go, and no read of
// a RemoveCounter-only key anywhere else in rules/ or effects/.

// RemoveCounterParams is one RemoveCounter ability's parameters, compiled
// once.
type RemoveCounterParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// Defined is the Defined tier's compiled Defined$ reference: the objects
	// and players the defined path acts on.
	Defined Ref
	// Choices is Choices$, trimmed: non-empty selects the card-election arm.
	Choices string
	// ExoticNote is the defined path's one loud Note for the shapes it does
	// not model ("" when none): CounterType$ Any, ChoiceOptional$, UpTo$,
	// CounterNum$ Any, CounterNumShared$, a non-battlefield TgtZone$,
	// RememberAmount$ and Optional$.
	ExoticNote string
	// ChoiceNote is the Choices$ arm's one loud Note for the shapes it does
	// not model ("" when none): CounterType$ Any/All, CounterNum$ Any, UpTo$,
	// CounterNumShared$ and a ChoiceZone$ naming neither the battlefield nor
	// exile; ChoiceZone is the arm's zone.
	ChoiceNote string
	ChoiceZone state.Zone
	// Kind is CounterType$ (default P1P1); AllKinds is its All spelling.
	Kind     string
	AllKinds bool
	// CounterNum is CounterNum$ (the Num grammar, default 1); NumAll is its
	// All spelling and NumSet a non-blank value.
	CounterNum ParamText
	NumAll     bool
	NumSet     bool
	// ChoiceNum is ChoiceNum$ (the Num grammar, default 1); ChoiceNumSet is
	// a non-blank value.
	ChoiceNum    ParamText
	ChoiceNumSet bool
	// ChoiceOptional is ChoiceOptional$ True.
	ChoiceOptional bool
	// RememberAmount and RememberRemoved are RememberAmount$ and
	// RememberRemoved$ True.
	RememberAmount  bool
	RememberRemoved bool

	// Unread are the parameters present on the ability that no
	// RemoveCounter reader consumes: the resolution Notes them once.
	Unread []string
}

// removeCounterKnownKeys is every parameter key a RemoveCounter resolution
// consumes or deliberately ignores, sorted (changeZoneKnownKeys' contract):
// rules' TestRemoveCounterKnownKeysMatchTheCensus holds it equal to the
// parameter census's measured api:RemoveCounter read set plus its ignored
// and structural keys.
var removeCounterKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat", "ActivationGameTypes",
	"ActivationLimit", "ActivationPhases", "ActivationZone", "Activator", "AddType",
	"AddTypes", "AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "Boast", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceNum", "ChoiceOptional", "ChoiceTitle",
	"ChoiceZone", "Choices", "ChooseFromList", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare", "ConditionCompare2",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn", "ConditionPresent", "ConditionPresent2",
	"ConditionSVarCompare", "ConditionZone", "CopyCard", "Cost", "CostDesc", "CounterNum",
	"CounterNumShared", "CounterType", "Defined", "DefinedCards", "DefinedTarget",
	"Description", "Exclude", "Exhaust", "ForgetOtherTargets", "GameActivationLimit", "Image", "ImprintCards",
	"ImprintPlayed", "InstantSpeed", "IsCurse", "IsPresent", "KW", "Keyword", "KeywordLine",
	"MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "ModeCost", "Monstrosity",
	"NewController", "NumDmg", "OpponentTurn", "Optional", "Planeswalker", "PlayCost",
	"PlayerTurn", "PowerUp", "PrecostDesc", "PresentCompare", "PresentDefined",
	"PresentZone", "RandomNumTargets", "ReduceAmount", "ReduceCost", "RememberAmount",
	"RememberAnimated", "RememberCostMana", "RememberObjects", "RememberRemoved", "RememberTargets", "ReplaceColor",
	"ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly",
	"ReplaceType", "SVarCompare", "SelectPrompt", "SetChosenMode", "SetColor", "ShowCards",
	"SorcerySpeed", "SpellDescription", "StackDescription", "SubAbility", "TargetMax",
	"TargetMin", "TargetType", "TargetUnique", "TargetValidTargeting", "TargetingPlayer",
	"TargetingPlayerControls", "TargetsAtRandom", "TargetsForEachPlayer",
	"TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness", "TargetsWithSameCardType",
	"TargetsWithSameController", "TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenScript", "TriggerDescription",
	"Type", "Ultimate", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "UpTo", "ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices",
	"ValidCounterType", "ValidDescription", "ValidTgts", "VoteMessage", "WithoutManaCost",
	"XMax", "XMin",
}

// isRemoveCounterSA reports whether sa resolves as api:RemoveCounter.
func isRemoveCounterSA(sa *cards.SA) bool {
	return sa != nil && sa.API == "RemoveCounter"
}

// RemoveCounterOf returns sa's compiled RemoveCounter parameters: the
// configured record's when it is bound to sa's Params map, else the front
// cache's entry for that map, else a fresh compile stored in the front cache
// (ChangeZoneOf's contract).
func RemoveCounterOf(sa *cards.SA) *RemoveCounterParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.RemoveCounter; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &removeCounterFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileRemoveCounter(sa, DefinedOf(sa))
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// removeCounterFront is RemoveCounterOf's direct-mapped front cache
// (czFront's shape).
var removeCounterFront [1 << 10]atomic.Pointer[RemoveCounterParams]

// compileRemoveCounter is the one reader of a RemoveCounter ability's own
// parameters; the Defined$ selector comes from the Defined-reference tier's
// compiled DefinedParams.
func compileRemoveCounter(sa *cards.SA, dr *DefinedParams) *RemoveCounterParams {
	p := &RemoveCounterParams{paramBinding: bindParams(sa), Defined: dr.Defined}
	p.Choices = strings.TrimSpace(sa.ParamStr(cards.PKChoices))
	rawKind := strings.TrimSpace(sa.ParamStr(cards.PKCounterType))
	p.Kind = rawKind
	if p.Kind == "" {
		p.Kind = "P1P1"
	}
	p.AllKinds = strings.EqualFold(p.Kind, "All")
	counterNum, ok := sa.Param(cards.PKCounterNum)
	p.CounterNum = ParamText{Text: counterNum, Present: ok}
	numRaw := strings.TrimSpace(counterNum)
	p.NumSet = numRaw != ""
	p.NumAll = strings.EqualFold(numRaw, "All")
	numAny := strings.EqualFold(numRaw, "Any")
	p.ChoiceNum = rawParamText(sa, "ChoiceNum")
	p.ChoiceNumSet = strings.TrimSpace(p.ChoiceNum.Text) != ""
	choiceOptional := strings.TrimSpace(sa.ParamStr(cards.PKChoiceOptional))
	p.ChoiceOptional = strings.EqualFold(choiceOptional, "True")
	upTo := strings.TrimSpace(sa.ParamStr(cards.PKUpTo)) != ""
	numShared := strings.TrimSpace(rawParamText(sa, "CounterNumShared").Text) != ""
	p.RememberAmount = isTrue(sa.ParamStr(cards.PKRememberAmount))
	p.RememberRemoved = isTrue(rawParamText(sa, "RememberRemoved").Text)

	// The defined path's exotic shapes, in the historical Note order.
	var exotic []string
	if strings.EqualFold(rawKind, "Any") {
		exotic = append(exotic, "CounterType$ Any")
	}
	if choiceOptional != "" {
		exotic = append(exotic, "ChoiceOptional$")
	}
	if upTo {
		exotic = append(exotic, "UpTo$")
	}
	if numAny {
		exotic = append(exotic, "CounterNum$ Any")
	}
	if numShared {
		exotic = append(exotic, "CounterNumShared$")
	}
	if zone := TargetsOf(sa).ZoneText; zone != "" && !strings.EqualFold(zone, "Battlefield") {
		exotic = append(exotic, "TgtZone$ "+zone)
	}
	if p.RememberAmount {
		exotic = append(exotic, "RememberAmount$")
	}
	if isTrue(sa.ParamStr(cards.PKOptional)) {
		exotic = append(exotic, "Optional$")
	}
	if len(exotic) > 0 {
		p.ExoticNote = "unimplemented RemoveCounter shape: " + strings.Join(exotic, ", ")
	}

	// The Choices$ arm's loud shapes, in the historical Note order.
	var loud []string
	if strings.EqualFold(p.Kind, "Any") || p.AllKinds {
		loud = append(loud, "CounterType$ "+p.Kind)
	}
	if numAny {
		loud = append(loud, "CounterNum$ Any")
	}
	if upTo {
		loud = append(loud, "UpTo$")
	}
	if numShared {
		loud = append(loud, "CounterNumShared$")
	}
	p.ChoiceZone = state.ZBattlefield
	switch zone := strings.TrimSpace(sa.ParamStr(cards.PKChoiceZone)); removeCounterChoiceZoneCodes.Code(zone) {
	case removeCounterChoiceZoneBattlefield:
	case removeCounterChoiceZoneExile:
		p.ChoiceZone = state.ZExile
	default:
		loud = append(loud, "ChoiceZone$ "+zone)
	}
	if len(loud) > 0 {
		p.ChoiceNote = "unimplemented RemoveCounter choice shape: " + strings.Join(loud, ", ")
	}
	p.Unread = unreadKeys(sa, removeCounterKnownKeys[:])
	return p
}

// RemoveCounterKnownKeys is a copy of removeCounterKnownKeys, for the census
// check.
func RemoveCounterKnownKeys() []string { return slices.Clone(removeCounterKnownKeys[:]) }

type removeCounterChoiceZoneCode uint16

const (
	removeCounterChoiceZoneBattlefield removeCounterChoiceZoneCode = iota + 1
	removeCounterChoiceZoneExile
)

var removeCounterChoiceZoneCodes = state.NewStrCodes(
	state.StrEntry[removeCounterChoiceZoneCode]{Key: "", Val: removeCounterChoiceZoneBattlefield},
	state.StrEntry[removeCounterChoiceZoneCode]{Key: "Battlefield", Val: removeCounterChoiceZoneBattlefield},
	state.StrEntry[removeCounterChoiceZoneCode]{Key: "Exile", Val: removeCounterChoiceZoneExile},
)
