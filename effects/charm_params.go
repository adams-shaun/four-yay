package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is the modal family's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8). compileCharm is the ONLY reader of a modal ability's own parameters --
// api:Charm and api:GenericChoice (both resolve through effCharm) and the
// Choices$ list of the other modal carriers (VillainousChoice, and a Vote or
// GenericChoice trigger's placement ask) -- so the resolution (charm.go),
// the cast announcement, the trigger placement ask, the target census and
// the answer mapping in rules all read one compiled CharmParams instead of
// each splitting Choices$ on its own. internal/codeshape's charmParamLeaks
// ratchet holds that: no parameter read in charm.go, and no read of a
// Charm-only key anywhere else in rules/ or effects/.
//
// Not compiled here: the MODE BODIES' parameters (a mode's ValidTgts$,
// TargetUnique$, TargetMin$/Max$, SpellDescription$, UnlessCost$), which
// belong to the mode's own API and are read through charm_modes.go; and the
// generic "does this ability carry Choices$ at all" presence tests rules
// makes over every trigger and cost (a ParamKey mask test on an ability of
// any API, not a Charm interpretation).

// CharmParams is one modal ability's parameters, compiled once.
type CharmParams struct {
	paramBinding

	// Choices$: the text, whether it is non-blank, and Modes --
	// cards.SplitModeNames of it, the one split the resolution kernel's
	// ask-free predicate (cards.SAChainMayAsk) walks too: exactly the list
	// every reader built by hand before (an absent Choices$ is [""]).
	// Modes is shared and read-only: its capacity is clipped, so an append
	// copies, and no caller writes through it.
	ChoicesText string
	HasChoices  bool
	Modes       []string

	// CharmNum$ (default 1) and MinCharmNum$ (default CharmNum$), evaluated
	// through numText at the ask; Optional$ True lowers the minimum to 0.
	CharmNum     ParamText
	MinCharmNum  ParamText
	OptionalTrue bool
	// CanRepeatModes$ True.
	CanRepeatModes bool
	// ChoiceRestriction$ (ThisTurn / ThisGame / YourLastCombat).
	ChoiceRestriction string
	// Random$ (True / Compare) with RandomCompareSVar$ / RandomCompare$ as
	// written (CheckSVarHolds trims them itself).
	Random            string
	RandomCompareSVar string
	RandomCompare     string

	// GenericChoice's per-player chooser: Defined$ (trimmed), TempRemember$
	// present, FallbackAbility$.
	Defined         string
	TempRemember    bool
	FallbackAbility string

	// AtRandom$ (trimmed; GenericChoice): True or Urza makes the pick the
	// engine's rng draw (effects/atrandom.go GenericChoiceAtRandom).
	AtRandom string
	// AILogic$ Random: the script's AI-picks-at-random marker, carried on
	// the ask as decision.Decision.AIRandom for unattended bots.
	AILogicRandom bool

	// Unread are the parameters present on a Charm or GenericChoice ability
	// that no reader of its resolution consumes (charmKnownKeys): effCharm
	// Notes them (the loud-degrade contract). nil for the other modal
	// carriers, whose own APIs own their unread checks.
	Unread []string
}

var zeroCharm = CharmParams{Modes: []string{""}[:1:1]}

// isCharmAPI reports whether sa resolves through effCharm.
func isCharmAPI(sa *cards.SA) bool {
	return sa != nil && (sa.API == "Charm" || sa.API == "GenericChoice")
}

// CharmOf returns sa's compiled modal parameters: the configured record's
// when bound to sa's Params map, else the front cache's entry for that map,
// else a fresh compile stored in the front cache (ChangeZoneOf's shape). A
// nil sa reads as an ability with no parameters.
func CharmOf(sa *cards.SA) *CharmParams {
	if sa == nil {
		return &zeroCharm
	}
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Charm; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &charmFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileCharm(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// charmFront is CharmOf's direct-mapped front cache, keyed by the Params
// map's identity (a collision only costs a recompile).
var charmFront [1 << 10]atomic.Pointer[CharmParams]

// compileCharm is the one reader of a modal ability's own parameters.
func compileCharm(sa *cards.SA) *CharmParams {
	p := &CharmParams{paramBinding: bindParams(sa)}
	p.ChoicesText = sa.ParamStr(cards.PKChoices)
	p.HasChoices = strings.TrimSpace(p.ChoicesText) != ""
	p.Modes = slices.Clip(cards.SplitModeNames(p.ChoicesText))

	cn, cnOK := sa.Params["CharmNum"]
	p.CharmNum = ParamText{Text: cn, Present: cnOK}
	mn, mnOK := sa.Params["MinCharmNum"]
	p.MinCharmNum = ParamText{Text: mn, Present: mnOK}
	p.OptionalTrue = strings.EqualFold(sa.ParamStr(cards.PKOptional), "True")
	p.CanRepeatModes = isTrue(sa.Params["CanRepeatModes"])
	p.ChoiceRestriction = strings.TrimSpace(sa.Params["ChoiceRestriction"])
	p.Random = strings.TrimSpace(sa.ParamStr(cards.PKRandom))
	p.RandomCompareSVar = sa.Params["RandomCompareSVar"]
	p.RandomCompare = sa.Params["RandomCompare"]

	p.Defined = strings.TrimSpace(sa.ParamStr(cards.PKDefined))
	p.TempRemember = strings.TrimSpace(sa.Params["TempRemember"]) != ""
	p.FallbackAbility = strings.TrimSpace(sa.Params["FallbackAbility"])
	p.AtRandom = strings.TrimSpace(sa.ParamStr(cards.PKAtRandom))
	p.AILogicRandom = strings.TrimSpace(sa.ParamStr(cards.PKAILogic)) == "Random"
	if isCharmAPI(sa) {
		p.Unread = unreadKeys(sa, charmKnownKeys[:])
	}
	return p
}

// charmKnownKeys is every parameter key a Charm or GenericChoice resolution
// consumes or deliberately ignores, sorted: compileCharm's own reads, the
// keys the shared machinery its resolution runs reads (the cast/activation/
// targeting tier, Resolve's Condition* gate, the Defined$ resolver), the
// presentation/AI-only keys and the structural SubAbility$/Keyword$ tags.
// rules' TestCharmKnownKeysMatchTheCensus holds it equal to the parameter
// census's measured api:Charm and api:GenericChoice read sets plus the
// ignored and structural keys.
var charmKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "Adapt", "AddKeywords", "AddStaticAbilities", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "AtRandom", "Boast", "CanRepeatModes",
	"ChangeTypeDesc", "CharacteristicDefining", "CharmNum", "CheckSVar",
	"ChoiceRestriction", "ChoiceTitle", "ChoiceZone", "Choices", "ChooseFromList",
	"ClassBand", "ClearImprinted", "Condition", "ConditionActivationLimit",
	"ConditionCheckSVar", "ConditionCompare", "ConditionDefined", "ConditionDescription",
	"ConditionFirstCombat", "ConditionNotPresent", "ConditionPhases",
	"ConditionPlayerTurn", "ConditionPresent", "ConditionSVarCompare", "CopyCard", "Cost",
	"CostDesc", "CounterTypePerDefined", "Defined", "DefinedCards", "DefinedTarget",
	"Description", "EffectOwner", "Exclude", "Exhaust", "FallbackAbility",
	"GameActivationLimit", "Image", "ImprintCards", "ImprintPlayed", "InstantSpeed",
	"IntoPlayTapped", "IsCurse", "IsPresent", "KW", "Keyword", "KeywordLine",
	"MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "MinCharmNum", "ModeCost",
	"Monstrosity", "NewController", "NumDmg", "OpponentTurn", "Optional",
	"Planeswalker", "PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc", "PresentCompare",
	"PresentDefined", "PresentZone", "Random", "RandomCompare", "RandomCompareSVar",
	"ReduceAmount", "ReduceCost", "RememberCostMana", "RememberObjects", "ReplaceColor",
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
	"TempRemember", "TgtPrompt", "TgtZone", "TokenScript", "TriggerDescription",
	"TriggersWhenSpent", "Type", "Ultimate", "UnlessAI", "UnlessCost",
	"UnlessPayer", "UnlessResolveSubs", "UnlessSwitched", "ValidCard", "ValidCards",
	"ValidCardsDesc", "ValidChoices", "ValidCounterType", "ValidDescription", "ValidTgts",
	"VarName", "VarValue", "VoteMessage", "WithoutManaCost", "XMax", "XMin",
}

// CharmKnownKeys is a copy of charmKnownKeys, for the census check.
func CharmKnownKeys() []string { return slices.Clone(charmKnownKeys[:]) }

// isModalSA reports whether sa is a modal carrier whose Choices$ names
// ability bodies (the APIs rules' trigger placement ask serves): the
// configured binding compiles CharmParams for exactly these.
func isModalSA(sa *cards.SA) bool {
	return isCharmAPI(sa) || sa != nil && (sa.API == "VillainousChoice" || sa.API == "Vote")
}
