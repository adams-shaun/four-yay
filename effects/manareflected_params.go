package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:ManaReflected's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8). compileManaReflected is the ONLY reader of a reflected-mana ability's
// own parameters: the resolution (mana_reflected.go's effManaReflected), the
// candidate census rules' mana walk and activation share
// (ManaReflectedCandidates) and rules' reflected-mana activation gate
// (manaReflectedPresentHolds: ClassBand$, IsPresent$, PresentCompare$) all
// read the compiled ManaReflectedParams. internal/codeshape's
// manaReflectedParamLeaks ratchet holds that: no parameter read in
// mana_reflected.go, and no read of a ManaReflected-only key anywhere else in
// rules/ or effects/. A reflected ability's Produced$ as the mana walk sees it
// (the activation's one-letter rewrite) is read through ManaOf like any
// other mana ability's.

// ManaReflectedParams is one reflected-mana ability's parameters, compiled
// once.
type ManaReflectedParams struct {
	paramBinding

	// ReflectProperty$, trimmed (Produce / Is / Produced).
	ReflectProperty string
	// WidenType: ColorOrType$ Type admits colourless.
	WidenType bool
	// Valid$, trimmed: the objects reflected from.
	Valid string
	// Produced$, trimmed: the colour-ask re-entry's one-letter override.
	Produced string
	// Amount$ as written (numText, default 1).
	Amount ParamText
	// RestrictValid$, trimmed.
	RestrictValid string
	// Defined$, trimmed: the Produced shape's recipient role.
	Defined string
	// The activation gate: ClassBand$ as written, IsPresent$ trimmed
	// (blank = no gate) and PresentCompare$ trimmed.
	ClassBand      string
	IsPresent      string
	PresentCompare string

	// Unread are the parameters present on the ability that no
	// ManaReflected reader consumes (manaReflectedKnownKeys):
	// effManaReflected Notes them.
	Unread []string
}

var zeroManaReflected ManaReflectedParams

// ManaReflectedOf returns sa's compiled ManaReflected parameters: the
// configured record's when bound to sa's Params map, else the front cache's
// entry for that map, else a fresh compile stored in the front cache
// (ChangeZoneOf's shape). A nil sa reads as an ability with no parameters.
func ManaReflectedOf(sa *cards.SA) *ManaReflectedParams {
	if sa == nil {
		return &zeroManaReflected
	}
	if f := LoadSAFacts(sa); f != nil {
		if p := f.ManaReflected; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &manaReflectedFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileManaReflected(sa, DefinedOf(sa), ActivationOf(sa))
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// manaReflectedFront is ManaReflectedOf's direct-mapped front cache, keyed by
// the Params map's identity (a collision only costs a recompile).
var manaReflectedFront [1 << 10]atomic.Pointer[ManaReflectedParams]

// compileManaReflected is the one reader of a reflected-mana ability's own
// parameters.
func compileManaReflected(sa *cards.SA, dp *DefinedParams, ap *ActivationParams) *ManaReflectedParams {
	p := &ManaReflectedParams{paramBinding: bindParams(sa)}
	p.ReflectProperty = strings.TrimSpace(sa.ParamStr(cards.PKReflectProperty))
	p.WidenType = strings.TrimSpace(sa.ParamStr(cards.PKColorOrType)) == "Type"
	p.Valid = strings.TrimSpace(sa.ParamStr(cards.PKValid))
	p.Produced = strings.TrimSpace(sa.ParamStr(cards.PKProduced))
	amt, amtOK := sa.Param(cards.PKAmount)
	p.Amount = ParamText{Text: amt, Present: amtOK}
	p.RestrictValid = strings.TrimSpace(sa.ParamStr(cards.PKRestrictValid))
	p.Defined = dp.Defined.Text
	p.ClassBand = sa.ParamStr(cards.PKClassBand)
	p.IsPresent = ap.IsPresent.Text
	p.PresentCompare = ap.PresentCompare.Text
	if sa.API == "ManaReflected" {
		p.Unread = unreadKeys(sa, manaReflectedKnownKeys[:])
	}
	return p
}

// manaReflectedKnownKeys is every parameter key a ManaReflected resolution
// consumes or deliberately ignores, sorted: compileManaReflected's own reads,
// the shared machinery's (the cast/activation/targeting tier, Resolve's
// Condition* gate, the Defined$ resolver), the presentation/AI-only keys and
// the structural SubAbility$/Keyword$ tags. rules'
// TestManaReflectedKnownKeysMatchTheCensus holds it equal to the parameter
// census's measured api:ManaReflected read set plus the ignored and
// structural keys.
var manaReflectedKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat", "ActivationGameTypes",
	"ActivationLimit", "ActivationPhases", "ActivationZone", "Activator", "AddType",
	"AddTypes", "AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Amount", "Announce", "AnnounceTitle", "Boast", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices",
	"ChooseFromList", "ClassBand", "ClearImprinted", "ColorOrType", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn", "ConditionPresent",
	"ConditionSVarCompare", "CopyCard", "Cost", "CostDesc", "Defined", "DefinedCards",
	"DefinedTarget", "Description", "Exclude", "Exhaust", "ForgetOtherTargets", "GameActivationLimit", "Image",
	"ImprintCards", "ImprintPlayed", "InstantSpeed", "IsCurse", "IsPresent", "KW",
	"Keyword", "KeywordLine", "MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor",
	"ModeCost", "Monstrosity", "NewController", "NumDmg", "OpponentTurn", "Planeswalker",
	"PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc", "PresentCompare", "PresentDefined",
	"PresentZone", "Produced", "RandomNumTargets", "ReduceAmount", "ReduceCost",
	"ReflectProperty", "RememberAnimated", "RememberCostMana", "RememberObjects", "RememberTargets", "ReplaceColor",
	"ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly",
	"ReplaceType", "RestrictValid", "SVarCompare", "SelectPrompt", "SetChosenMode",
	"SetColor", "ShowCards", "SorcerySpeed", "SpellDescription", "StackDescription",
	"SubAbility", "TargetMax", "TargetMin", "TargetType", "TargetUnique",
	"TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls", "TargetsAtRandom",
	"TargetsForEachPlayer", "TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness", "TargetsWithSameCardType",
	"TargetsWithSameController", "TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenScript", "TriggerDescription",
	"Type", "Ultimate", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "Valid", "ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices",
	"ValidCounterType", "ValidDescription", "ValidTgts", "VoteMessage", "WithoutManaCost",
	"XMax", "XMin",
}

// ManaReflectedKnownKeys is a copy of manaReflectedKnownKeys, for the census
// check.
func ManaReflectedKnownKeys() []string { return slices.Clone(manaReflectedKnownKeys[:]) }
