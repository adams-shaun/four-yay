package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:Draw's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8). compileDraw is the ONLY reader of a Draw ability's own parameters: the
// resolution (draw.go) and rules' scry-replacement draw (a ReplaceWith$ Draw
// body's NumCards$) both read the compiled DrawParams. internal/codeshape's
// drawParamLeaks ratchet holds that: no parameter read in draw.go, and no read
// of a Draw-only key anywhere else in rules/ or effects/. The Defined$/
// ValidTgts$ selectors (actingPlayers) are the shared tier.

// DrawParams is one Draw ability's parameters, compiled once.
type DrawParams struct {
	paramBinding

	// NumCards$ (default 1, numText).
	NumCards ParamText
	// RememberDrawn$ present-and-non-blank (True and AllReplaced alike).
	RememberDrawn bool
	// OptionalDecider$, trimmed ("" = a mandatory draw).
	OptionalDecider string
	// Upto$ True.
	Upto bool

	// Unread are the parameters present on the ability that no Draw reader
	// consumes (drawKnownKeys): effDraw Notes them.
	Unread []string
}

var zeroDraw DrawParams

// DrawOf returns sa's compiled Draw parameters: the configured record's when
// bound to sa's Params map, else the front cache's entry for that map, else a
// fresh compile stored in the front cache (ChangeZoneOf's shape). A nil sa
// reads as an ability with no parameters.
func DrawOf(sa *cards.SA) *DrawParams {
	if sa == nil {
		return &zeroDraw
	}
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Draw; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &drawFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileDraw(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// drawFront is DrawOf's direct-mapped front cache, keyed by the Params map's
// identity (a collision only costs a recompile).
var drawFront [1 << 10]atomic.Pointer[DrawParams]

// compileDraw is the one reader of a Draw ability's own parameters.
func compileDraw(sa *cards.SA) *DrawParams {
	p := &DrawParams{paramBinding: bindParams(sa)}
	nc, ncOK := sa.Params["NumCards"]
	p.NumCards = ParamText{Text: nc, Present: ncOK}
	p.RememberDrawn = strings.TrimSpace(sa.Params["RememberDrawn"]) != ""
	p.OptionalDecider = strings.TrimSpace(sa.ParamStr(cards.PKOptionalDecider))
	p.Upto = isTrue(sa.Params["Upto"])
	if sa.API == "Draw" {
		p.Unread = unreadKeys(sa, drawKnownKeys[:])
	}
	return p
}

// drawKnownKeys is every parameter key a Draw resolution consumes or
// deliberately ignores, sorted: compileDraw's own reads, the shared
// machinery's (the cast/activation/targeting tier, Resolve's Condition* gate,
// the selector resolvers), the presentation/AI-only keys and the structural
// SubAbility$/Keyword$ tags. rules' TestDrawKnownKeysMatchTheCensus
// holds it equal to the parameter census's measured api:Draw read set plus
// the ignored and structural keys.
var drawKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "AddKeywords", "AddStaticAbilities", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "Boast", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices",
	"ChooseFromList", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn", "ConditionPresent",
	"ConditionSVarCompare", "CopyCard", "Cost", "CostDesc",
	"Defined", "DefinedCards", "DefinedTarget", "Description", "Exclude",
	"Exhaust", "GameActivationLimit", "Image", "ImprintCards", "ImprintPlayed",
	"InstantSpeed", "IntoPlayTapped", "IsCurse", "IsPresent", "KW", "Keyword",
	"KeywordLine", "MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "ModeCost",
	"Monstrosity", "NewController", "NumCards", "NumDmg", "OpponentTurn",
	"OptionalDecider", "Planeswalker", "PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc",
	"PresentCompare", "PresentDefined", "PresentZone", "RandomNumTargets", "ReduceAmount", "ReduceCost",
	"RememberCostMana", "RememberDrawn", "RememberObjects", "ReplaceColor",
	"ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly",
	"ReplaceType", "SVarCompare", "SelectPrompt", "SetChosenMode",
	"SetColor", "ShowCards", "SorcerySpeed", "SpellDescription", "StackDescription",
	"SubAbility", "TargetMax", "TargetMin", "TargetType",
	"TargetUnique", "TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls", "TargetsAtRandom",
	"TargetsForEachPlayer", "TargetsWithControllerProperty",
	"TargetsWithDefinedController", "TargetsWithDifferentCMC",
	"TargetsWithDifferentControllers", "TargetsWithDifferentNames",
	"TargetsWithEqualToughness", "TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType", "TargetsWithSharedTypes",
	"TgtPrompt", "TgtZone", "TokenScript", "TriggerDescription", "Type", "Ultimate", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "Upto", "ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices",
	"ValidCounterType", "ValidDescription", "ValidTgts", "VoteMessage", "WithoutManaCost", "XMax", "XMin",
}

// DrawKnownKeys is a copy of drawKnownKeys, for the census check.
func DrawKnownKeys() []string { return slices.Clone(drawKnownKeys[:]) }
