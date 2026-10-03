package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:Effect's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on ChangeZone's pattern (changezone_params.go). compileEffect is the
// ONLY reader of an Effect ability's own parameters: the resolution
// (effect.go), the rules-side target ask's granted-static scan
// (staticModesFromSVars' StaticAbilities$) and the opening-hand Effect
// (Triggers$, EffectOwner$) all read the compiled EffectParams.
// internal/codeshape's effectParamLeaks ratchet holds that: no parameter read
// in effect.go, and no read of an Effect-only key anywhere else in rules/ or
// effects/.
//
// The conversion is a read move, not a semantic one: every field is the
// value the resolution read in place before, at the spelling (raw or
// trimmed) it read. The Triggers$ bodies an Effect arms are parsed per
// resolution from SVar text; their own parameters are read once per body by
// readEffectTriggerLine below. The shared readers the resolution calls with
// the ability -- RememberObjects$ (effectRemembered,
// effectRememberedPlayers), ForgetOtherRemembered$ (forgetOtherRemembered)
// and the restriction/static-line registration gates -- still read their own
// keys; those are the next tier.

// EffectParams is one Effect ability's parameters, compiled once.
type EffectParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// Duration is Duration$ as written ("" when absent: the absent-duration
	// rules key on that).
	Duration string
	// StaticAbilities$, Triggers$ and ReplacementEffects$, as written (SVar
	// name lists).
	StaticAbilities    string
	Triggers           string
	ReplacementEffects string
	// Name$, trimmed.
	Name string
	// NotStackable is Stackable$ False.
	NotStackable bool
	// ForgetOnMoved$, ExileOnMoved$ and ForgetCounter$, trimmed.
	ForgetOnMoved string
	ExileOnMoved  string
	ForgetCounter string
	// ForgetOnCast is ForgetOnCast$, trimmed, with Forge's explicit False
	// folded to "".
	ForgetOnCast string
	// ImprintOnHost$, trimmed, and whether it is True.
	ImprintOnHost     string
	ImprintOnHostTrue bool
	// ForgetOnPhasedIn$ True.
	ForgetOnPhasedIn bool
	// RememberLKI$, SetChosenNumber$ and EffectOwner$, trimmed.
	RememberLKI     string
	SetChosenNumber string
	EffectOwner     string

	// Unread are the parameters present on the ability that no Effect
	// reader consumes: the resolution Notes them once.
	Unread []string
}

// effectKnownKeys is every parameter key an Effect resolution consumes or
// deliberately ignores, sorted (changeZoneKnownKeys' contract): rules'
// TestEffectKnownKeysMatchTheCensus holds it equal to the parameter census's
// measured api:Effect read set plus its ignored and structural keys.
var effectKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment",
	"AITgts", "Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "AddKeywords", "AddStaticAbilities", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "BecomeStartingPlayer", "Boast",
	"ChangeTypeDesc", "CharacteristicDefining", "CheckSVar", "ChoiceTitle",
	"ChoiceZone", "Choices", "ChooseFromList", "ClassBand", "ClearImprinted",
	"Condition", "ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn",
	"ConditionPresent", "ConditionSVarCompare", "CopyCard", "Cost", "CostDesc",
	"Defined", "DefinedCards", "DefinedTarget", "Description", "Duration",
	"EffectOwner", "EffectZone", "Exclude", "Execute", "Exhaust", "ExileOnMoved",
	"ForgetCounter", "ForgetOnCast", "ForgetOnMoved", "ForgetOnPhasedIn",
	"ForgetOtherRemembered", "GameActivationLimit", "Image", "ImprintCards",
	"ImprintOnHost", "ImprintPlayed", "InstantSpeed", "IntoPlayTapped", "IsCurse",
	"IsPresent", "KW", "Keyword", "KeywordLine", "MaxTotalTargetCMC",
	"MaxTotalTargetPower", "Mentor", "ModeCost", "Monstrosity", "Name",
	"NewController", "NumDmg", "OneOff", "OpponentTurn", "OptionalDecider", "Phase",
	"Planeswalker", "PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc",
	"PresentCompare", "PresentDefined", "PresentZone", "ReduceAmount", "ReduceCost",
	"RememberCostMana", "RememberLKI", "RememberObjects", "ReplaceColor",
	"ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly",
	"ReplaceType", "ReplacementEffects", "SVarCompare",
	"SelectPrompt", "SetChosenMode", "SetChosenNumber", "SetColor", "ShowCards",
	"SorcerySpeed", "SpellDescription", "StackDescription", "Stackable", "Static",
	"StaticAbilities", "SubAbility", "TargetMax", "TargetMin", "TargetType",
	"TargetUnique", "TargetValidTargeting", "TargetingPlayer",
	"TargetingPlayerControls", "TargetsForEachPlayer",
	"TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness",
	"TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "ThisTurn", "TokenScript",
	"TriggerDescription", "Triggers", "Type", "Ultimate",
	"UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs", "UnlessSwitched",
	"ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices", "ValidCounterType",
	"ValidDescription", "ValidPlayer", "ValidTgts", "VoteMessage", "WithoutManaCost", "XMax", "XMin",
}

// isEffectSA reports whether sa resolves as api:Effect.
func isEffectSA(sa *cards.SA) bool {
	return sa != nil && (sa.CompiledAPI() == cards.APIEffect || sa.API == "Effect")
}

// EffectOf returns sa's compiled Effect parameters: the configured record's
// when it is bound to sa's Params map, else the front cache's entry for that
// map, else a fresh compile stored in the front cache (ChangeZoneOf's
// contract).
func EffectOf(sa *cards.SA) *EffectParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Effect; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &effectFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileEffect(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// effectFront is EffectOf's direct-mapped front cache (czFront's shape).
var effectFront [1 << 10]atomic.Pointer[EffectParams]

// compileEffect is the one reader of an Effect ability's parameters.
func compileEffect(sa *cards.SA) *EffectParams {
	p := &EffectParams{paramBinding: bindParams(sa)}
	p.Duration = sa.ParamStr(cards.PKDuration)
	p.StaticAbilities = sa.ParamStr(cards.PKStaticAbilities)
	p.Triggers = sa.ParamStr(cards.PKTriggers)
	p.ReplacementEffects = sa.Params["ReplacementEffects"]
	p.Name = strings.TrimSpace(sa.Params["Name"])
	if stackable, present := sa.Params["Stackable"]; present && strings.EqualFold(strings.TrimSpace(stackable), "False") {
		p.NotStackable = true
	}
	p.ForgetOnMoved = strings.TrimSpace(sa.Params["ForgetOnMoved"])
	p.ExileOnMoved = strings.TrimSpace(sa.Params["ExileOnMoved"])
	p.ForgetCounter = strings.TrimSpace(sa.Params["ForgetCounter"])
	p.ForgetOnCast = strings.TrimSpace(sa.Params["ForgetOnCast"])
	if strings.EqualFold(p.ForgetOnCast, "False") {
		p.ForgetOnCast = ""
	}
	p.ImprintOnHost = strings.TrimSpace(sa.Params["ImprintOnHost"])
	p.ImprintOnHostTrue = strings.EqualFold(p.ImprintOnHost, "True")
	p.ForgetOnPhasedIn = isTrue(sa.Params["ForgetOnPhasedIn"])
	p.RememberLKI = strings.TrimSpace(sa.ParamStr(cards.PKRememberLKI))
	p.SetChosenNumber = strings.TrimSpace(sa.Params["SetChosenNumber"])
	p.EffectOwner = strings.TrimSpace(sa.Params["EffectOwner"])
	p.Unread = unreadKeys(sa, effectKnownKeys[:])
	return p
}

// effectTriggerLine is one Triggers$ body's parameters as the Effect
// resolution arms it (read once per parsed body, on the stack).
type effectTriggerLine struct {
	// Execute$, OptionalDecider$ and ThisTurn$, trimmed.
	Execute         string
	OptionalDecider string
	ThisTurn        string
	// OneOff is OneOff$ True; Static is a non-empty Static$.
	OneOff bool
	Static bool
	// Phase is Phase$ as written; ValidPlayer$ trimmed.
	Phase       string
	ValidPlayer string
}

// readEffectTriggerLine reads one parsed Triggers$ body's parameters.
func readEffectTriggerLine(tr *cards.Trigger) effectTriggerLine {
	return effectTriggerLine{
		Execute:         strings.TrimSpace(tr.ParamStr(cards.PKExecute)),
		OptionalDecider: strings.TrimSpace(tr.ParamStr(cards.PKOptionalDecider)),
		ThisTurn:        strings.TrimSpace(tr.ParamStr(cards.PKThisTurn)),
		OneOff:          isTrue(tr.Params["OneOff"]),
		Static:          strings.TrimSpace(tr.ParamStr(cards.PKStatic)) != "",
		Phase:           tr.ParamStr(cards.PKPhase),
		ValidPlayer:     strings.TrimSpace(tr.ParamStr(cards.PKValidPlayer)),
	}
}

// EffectKnownKeys is a copy of effectKnownKeys, for the census check.
func EffectKnownKeys() []string { return slices.Clone(effectKnownKeys[:]) }
