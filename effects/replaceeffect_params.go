package effects

import (
	"slices"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:ReplaceEffect's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8). compileReplaceEffect is the ONLY reader of a ReplaceEffect body's own
// parameters: the resolution (replacement.go's effReplaceEffect) and rules'
// three readers of a ReplaceWith$ body -- the Scry proposal's Num rewrite
// (continueScryReplacements) and the gain-life/draw count operators
// (replaceCountOp) -- all read the compiled ReplaceEffectParams, so the
// in-flight rewrite and the pre-pricing of the same body can no longer
// disagree about VarName$/VarValue$. internal/codeshape's
// replaceEffectParamLeaks ratchet holds that: no parameter read in
// replacement.go, and no read of a ReplaceEffect-only key anywhere else in
// rules/ or effects/.

// ReplaceEffectParams is one ReplaceEffect body's parameters, compiled once.
type ReplaceEffectParams struct {
	paramBinding

	// VarName$ as written: the held event's field the body rewrites ("" =
	// nothing to rewrite).
	VarName string
	// VarValue$ as written (untrimmed; numText and the ReplaceCount$
	// readers trim it themselves).
	VarValue ParamText

	// Unread are the parameters present on the ability that no
	// ReplaceEffect reader consumes (replaceEffectKnownKeys):
	// effReplaceEffect Notes them.
	Unread []string
}

var zeroReplaceEffect ReplaceEffectParams

// ReplaceEffectOf returns sa's compiled ReplaceEffect parameters: the
// configured record's when bound to sa's Params map, else the front cache's
// entry for that map, else a fresh compile stored in the front cache
// (ChangeZoneOf's shape). A nil sa reads as an ability with no parameters.
func ReplaceEffectOf(sa *cards.SA) *ReplaceEffectParams {
	if sa == nil {
		return &zeroReplaceEffect
	}
	if f := LoadSAFacts(sa); f != nil {
		if p := f.ReplaceEffect; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &replaceEffectFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileReplaceEffect(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// replaceEffectFront is ReplaceEffectOf's direct-mapped front cache, keyed
// by the Params map's identity (a collision only costs a recompile).
var replaceEffectFront [1 << 10]atomic.Pointer[ReplaceEffectParams]

// compileReplaceEffect is the one reader of a ReplaceEffect body's own
// parameters.
func compileReplaceEffect(sa *cards.SA) *ReplaceEffectParams {
	p := &ReplaceEffectParams{paramBinding: bindParams(sa)}
	p.VarName = sa.ParamStr(cards.PKVarName)
	vv, vvOK := sa.Param(cards.PKVarValue)
	p.VarValue = ParamText{Text: vv, Present: vvOK}
	if sa.API == "ReplaceEffect" {
		p.Unread = unreadKeys(sa, replaceEffectKnownKeys[:])
	}
	return p
}

// replaceEffectKnownKeys is every parameter key a ReplaceEffect resolution
// consumes or deliberately ignores, sorted: compileReplaceEffect's own
// reads, the shared machinery's (Resolve's Condition* gate and the rest of
// the generic tier), the presentation/AI-only keys and the structural
// SubAbility$/Keyword$ tags. rules' TestReplaceEffectKnownKeysMatchTheCensus
// holds it equal to the parameter census's measured api:ReplaceEffect read
// set plus the ignored and structural keys.
var replaceEffectKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "Adapt", "AddKeywords", "AddStaticAbilities", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "Boast", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices",
	"ChooseFromList", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn", "ConditionPresent",
	"ConditionSVarCompare", "CopyCard", "Cost", "CostDesc", "CounterTypePerDefined",
	"Defined", "DefinedCards", "DefinedTarget", "Description", "EffectOwner", "Exclude",
	"Exhaust", "GameActivationLimit", "Image", "ImprintCards", "ImprintPlayed",
	"InstantSpeed", "IntoPlayTapped", "IsCurse", "IsPresent", "KW", "Keyword",
	"KeywordLine", "MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "ModeCost",
	"Monstrosity", "NewController", "NumDmg", "OpponentTurn",
	"Planeswalker", "PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc",
	"PresentCompare", "PresentDefined", "PresentZone", "ReduceAmount", "ReduceCost",
	"RememberCostMana", "RememberObjects", "ReplaceColor",
	"ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly",
	"ReplaceType", "RestrictValid", "SVarCompare", "SelectPrompt", "SetChosenMode",
	"SetColor", "ShowCards", "SorcerySpeed", "SpellDescription", "StackDescription",
	"StaticAbilities", "SubAbility", "TargetMax", "TargetMin", "TargetType",
	"TargetUnique", "TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls",
	"TargetsForEachPlayer", "TargetsWithControllerProperty",
	"TargetsWithDefinedController", "TargetsWithDifferentCMC",
	"TargetsWithDifferentControllers", "TargetsWithDifferentNames",
	"TargetsWithEqualToughness", "TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType", "TargetsWithSharedTypes",
	"TgtPrompt", "TgtZone", "TokenScript", "TriggerDescription", "TriggersWhenSpent",
	"Type", "Ultimate", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices",
	"ValidCounterType", "ValidDescription", "ValidTgts", "VarName", "VarValue",
	"VoteMessage", "WithoutManaCost", "XMax", "XMin",
}

// ReplaceEffectKnownKeys is a copy of replaceEffectKnownKeys, for the census
// check.
func ReplaceEffectKnownKeys() []string { return slices.Clone(replaceEffectKnownKeys[:]) }
