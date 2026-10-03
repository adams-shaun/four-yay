package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:DealDamage's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on ChangeZone's pattern (changezone_params.go). compileDealDamage is the
// ONLY reader of a DealDamage ability's own parameters: the resolution
// (damage_deal.go), the rules-side target-ask damage preview
// (targetDamageAmount's NumDmg$), the payment planner's self-damage rider
// probe (paymentPlanDamageBody) all read the compiled DealDamageParams, so the
// offer, the planner and the resolution can no longer read a parameter
// differently. internal/codeshape's dealDamageParamLeaks ratchet holds that:
// no parameter read in damage_deal.go, and no read of a DealDamage-only key
// anywhere else in rules/ or effects/.
//
// The two riders DealDamage shares with DamageAll, EachDamage, Fight and
// DamageResolve -- DamageSource$ (newDamageRider) and ReplaceDyingDefined$
// (registerReplaceDying) -- are compiled here too, and the sibling APIs read
// each through its single-key reader at the bottom of this file, so each of
// those keys still has exactly one reader. The generic machinery a DealDamage
// runs -- the Defined$ resolver, the cast/activation/targeting tier
// (ValidTgts$, TgtPrompt$, TargetMin$/TargetMax$, Cost$) -- still reads its
// own keys; those are the next tier.

// DealDamageParams is one DealDamage ability's parameters, compiled once.
type DealDamageParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// NumDmg is NumDmg$, the per-recipient amount (default 0, Ruling T14-f).
	NumDmg ParamText
	// Divided is a non-empty DividedAsYouChoose$; DividedTotal is its value
	// text (the named total divided among the chosen targets).
	Divided      bool
	DividedTotal ParamText
	// RememberDamaged is a non-empty RememberDamaged$.
	RememberDamaged bool
	// ExcessSVar$ and ExcessSVarCondition$ (CR 120.10), trimmed.
	ExcessSVar          string
	ExcessSVarCondition string
	// DamageSource is DamageSource$, trimmed (newDamageRider's spec).
	DamageSource string
	// DamageMap is DamageMap$ True: mark the damage for a DamageResolve.
	DamageMap bool
	// RelativeTarget is a non-empty RelativeTarget$ (the multi-source arm
	// re-resolves the recipients per source).
	RelativeTarget bool
	// ReplaceDyingDefined is ReplaceDyingDefined$, trimmed.
	ReplaceDyingDefined string

	// Unread are the parameters present on the ability that no DealDamage
	// reader consumes: the resolution Notes them once.
	Unread []string
}

// dealDamageKnownKeys is every parameter key a DealDamage resolution consumes
// or deliberately ignores, sorted (changeZoneKnownKeys' contract): rules'
// TestDealDamageKnownKeysMatchTheCensus holds it equal to the parameter
// census's measured api:DealDamage read set plus its ignored and structural
// keys.
var dealDamageKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment",
	"AITgts", "Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "Boast", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices",
	"ChooseFromList", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn",
	"ConditionPresent", "ConditionSVarCompare", "CopyCard", "Cost", "CostDesc",
	"DamageMap", "DamageSource", "Defined", "DefinedCards", "DefinedTarget",
	"Description", "DividedAsYouChoose", "ExcessSVar",
	"ExcessSVarCondition", "Exclude", "Exhaust", "GameActivationLimit", "Image",
	"ImprintCards", "ImprintPlayed", "InstantSpeed", "IsCurse",
	"IsPresent", "KW", "Keyword", "KeywordLine", "MaxTotalTargetCMC",
	"MaxTotalTargetPower", "Mentor", "ModeCost", "Monstrosity", "NewController",
	"NumDmg", "OpponentTurn", "Planeswalker",
	"PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc", "PresentCompare",
	"PresentDefined", "PresentZone", "RandomNumTargets", "ReduceAmount", "ReduceCost", "RelativeTarget",
	"RememberCostMana", "RememberDamaged", "RememberObjects", "ReplaceColor",
	"ReplaceDyingDefined", "ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana",
	"ReplaceOnly", "ReplaceType", "SVarCompare", "SelectPrompt",
	"SetChosenMode", "SetColor", "ShowCards", "SorcerySpeed", "SpellDescription",
	"StackDescription", "SubAbility", "TargetMax", "TargetMin",
	"TargetType", "TargetUnique", "TargetValidTargeting", "TargetingPlayer",
	"TargetingPlayerControls", "TargetsAtRandom", "TargetsForEachPlayer",
	"TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness",
	"TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenScript",
	"TriggerDescription", "Type", "Ultimate", "UnlessAI",
	"UnlessCost", "UnlessPayer", "UnlessResolveSubs", "UnlessSwitched", "ValidCard",
	"ValidCards", "ValidCardsDesc", "ValidChoices", "ValidCounterType",
	"ValidDescription", "ValidTgts", "VoteMessage",
	"WithoutManaCost", "XMax", "XMin",
}

// isDealDamageSA reports whether sa resolves as api:DealDamage.
func isDealDamageSA(sa *cards.SA) bool {
	return sa != nil && (sa.CompiledAPI() == cards.APIDealDamage || sa.API == "DealDamage")
}

// DealDamageOf returns sa's compiled DealDamage parameters: the configured
// record's when it is bound to sa's Params map, else the front cache's entry
// for that map, else a fresh compile stored in the front cache (ChangeZoneOf's
// contract).
func DealDamageOf(sa *cards.SA) *DealDamageParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.DealDamage; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &dealDamageFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileDealDamage(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// dealDamageFront is DealDamageOf's direct-mapped front cache (czFront's
// shape).
var dealDamageFront [1 << 10]atomic.Pointer[DealDamageParams]

// compileDealDamage is the one reader of a DealDamage ability's parameters.
func compileDealDamage(sa *cards.SA) *DealDamageParams {
	p := &DealDamageParams{paramBinding: bindParams(sa)}
	p.NumDmg = damageAmountParam(sa)
	if div, ok := dividedParam(sa); ok {
		p.Divided = true
		p.DividedTotal = div
	}
	p.RememberDamaged = strings.TrimSpace(sa.ParamStr(cards.PKRememberDamaged)) != ""
	p.ExcessSVar = strings.TrimSpace(sa.Params["ExcessSVar"])
	p.ExcessSVarCondition = strings.TrimSpace(sa.Params["ExcessSVarCondition"])
	p.DamageSource = damageSourceParam(sa)
	p.DamageMap = isTrue(sa.Params["DamageMap"])
	p.RelativeTarget = strings.TrimSpace(sa.Params["RelativeTarget"]) != ""
	p.ReplaceDyingDefined = replaceDyingParam(sa)
	p.Unread = unreadKeys(sa, dealDamageKnownKeys[:])
	return p
}

// DamageAmount is a damage-dealing ability's NumDmg$ (DealDamage's compiled
// one, else the raw key for the sibling damage APIs): the rules-side target
// ask's damage preview reads it here, so it reads exactly what the
// resolution deals.
func DamageAmount(sa *cards.SA) ParamText {
	if isDealDamageSA(sa) {
		return DealDamageOf(sa).NumDmg
	}
	return damageAmountParam(sa)
}

// damageAmountParam is NumDmg$'s one reader.
func damageAmountParam(sa *cards.SA) ParamText {
	v, ok := sa.Param(cards.PKNumDmg)
	return ParamText{Text: v, Present: ok}
}

// damageSourceParam is DamageSource$'s one reader (trimmed): DealDamage
// compiles it, and DamageAll and EachDamage read it here for newDamageRider.
func damageSourceParam(sa *cards.SA) string {
	return strings.TrimSpace(sa.Params["DamageSource"])
}

// replaceDyingParam is ReplaceDyingDefined$'s one reader (trimmed):
// DealDamage compiles it, and Fight, DamageAll, EachDamage and DamageResolve
// read it here for registerReplaceDying.
func replaceDyingParam(sa *cards.SA) string {
	return strings.TrimSpace(sa.Params["ReplaceDyingDefined"])
}

// DealDamageKnownKeys is a copy of dealDamageKnownKeys, for the census check.
func DealDamageKnownKeys() []string { return slices.Clone(dealDamageKnownKeys[:]) }
