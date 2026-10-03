package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// This file is api:Pump's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8). compilePump is the ONLY reader of a Pump ability's own parameters: the
// resolution (pump.go), the shared per-object registration
// (registerPumpEffects), rules' pure-keyword-grant legality check
// (pureGrantKeywords) and the KWChoice$ answer mapping (modeAnswerNames) all
// read the compiled PumpParams, so the ask and the answer can no longer split
// KWChoice$ differently. internal/codeshape's pumpParamLeaks ratchet holds
// that: no parameter read in pump.go, and no read of a Pump-only key anywhere
// else in rules/ or effects/.
//
// PumpAll shares the registration's KW$/Duration$/LeaveBattlefield$ riders
// (PumpGrant), compiled on the stack per call through compilePumpGrant; its
// other parameters are its own and not compiled yet. The Defined$ selector
// resolver and the AtEOT$ rider are the shared tier and read their own keys.

// PumpGrant is the per-object registration's riders, shared with PumpAll.
type PumpGrant struct {
	// KWText is KW$ as written; KW its cards.SplitKeywordList split
	// (capacity-clipped and read-only: it is shared by every registration).
	KWText string
	KW     []string
	// Duration$ and LeaveBattlefield$ as written (durationTiming and the
	// leave-exile helpers trim them).
	Duration         string
	LeaveBattlefield string
}

// PumpParams is one Pump ability's parameters, compiled once.
type PumpParams struct {
	paramBinding

	// NoteNumber$: present-and-non-blank, and its value for numText.
	HasNoteNumber bool
	NoteNumber    ParamText
	// ClearNotedCardsFor$'s label list; NoteCardsFor$ and NoteCards$, trimmed.
	ClearNotedCardsFor []string
	NoteCardsFor       string
	NoteCards          string
	// Defined$, trimmed (the NoteCards$ Self fallback's "no selector" test;
	// the selector itself resolves through Defined).
	Defined string
	// Secondary$ True: Forge's presentation flag, recognised and ignored.
	Secondary bool
	// KWChoice$: present-and-non-blank, and its trimmed non-empty candidate
	// list -- the ask's options and the answer's vocabulary alike.
	HasKWChoice bool
	KWChoice    []string
	// PumpZone$, trimmed, with its ParseZones result.
	PumpZone    string
	PumpZones   []state.Zone
	PumpZoneAll bool
	PumpZoneOK  bool
	// RememberTargets$ True and RememberPumped$ True.
	RememberTargets bool
	RememberPumped  bool
	// NumAtt$ / NumDef$ (numForObjectText: "Double" reads the object's own
	// power/toughness).
	NumAtt ParamText
	NumDef ParamText
	// ForgetImprinted$, trimmed (a Defined$-grammar selector).
	ForgetImprinted string
	// Grant is the registration's riders.
	Grant PumpGrant

	// Unread are the parameters present on the ability that no Pump reader
	// consumes (pumpKnownKeys): effPump Notes them.
	Unread []string
}

var zeroPump PumpParams

// PumpOf returns sa's compiled Pump parameters: the configured record's when
// bound to sa's Params map, else the front cache's entry for that map, else a
// fresh compile stored in the front cache (ChangeZoneOf's shape). A nil sa
// reads as an ability with no parameters.
func PumpOf(sa *cards.SA) *PumpParams {
	if sa == nil {
		return &zeroPump
	}
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Pump; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &pumpFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compilePump(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// pumpFront is PumpOf's direct-mapped front cache, keyed by the Params map's
// identity (a collision only costs a recompile).
var pumpFront [1 << 10]atomic.Pointer[PumpParams]

// compilePump is the one reader of a Pump ability's own parameters.
func compilePump(sa *cards.SA) *PumpParams {
	p := &PumpParams{paramBinding: bindParams(sa)}
	nn, nnOK := sa.Params["NoteNumber"]
	p.NoteNumber = ParamText{Text: nn, Present: nnOK}
	p.HasNoteNumber = strings.TrimSpace(nn) != ""
	p.ClearNotedCardsFor = splitTrimList(sa.Params["ClearNotedCardsFor"])
	p.NoteCardsFor = strings.TrimSpace(sa.Params["NoteCardsFor"])
	p.NoteCards = strings.TrimSpace(sa.Params["NoteCards"])
	p.Defined = strings.TrimSpace(sa.ParamStr(cards.PKDefined))
	p.Secondary = isTrue(sa.ParamStr(cards.PKSecondary))
	kw := strings.TrimSpace(sa.Params["KWChoice"])
	p.HasKWChoice = kw != ""
	p.KWChoice = slices.Clip(splitTrimList(kw))
	p.PumpZone = strings.TrimSpace(sa.Params["PumpZone"])
	if p.PumpZone != "" {
		p.PumpZones, p.PumpZoneAll, p.PumpZoneOK = ParseZones(p.PumpZone)
	}
	p.RememberTargets = isTrue(sa.ParamStr(cards.PKRememberTargets))
	p.RememberPumped = isTrue(sa.ParamStr(cards.PKRememberPumped))
	na, naOK := sa.Params["NumAtt"]
	p.NumAtt = ParamText{Text: na, Present: naOK}
	nd, ndOK := sa.Params["NumDef"]
	p.NumDef = ParamText{Text: nd, Present: ndOK}
	p.ForgetImprinted = strings.TrimSpace(sa.Params["ForgetImprinted"])
	p.Grant = compilePumpGrant(sa)
	if sa.API == "Pump" {
		p.Unread = unreadParams(sa, pumpKnownKeys[:])
	}
	return p
}

// compilePumpGrant is the one reader of the registration riders. It returns
// a value: PumpAll compiles it on the stack per call, Pump once per ability.
func compilePumpGrant(sa *cards.SA) PumpGrant {
	var g PumpGrant
	g.KWText = sa.ParamStr(cards.PKKW)
	g.KW = slices.Clip(cards.SplitKeywordList(g.KWText))
	g.Duration = sa.ParamStr(cards.PKDuration)
	g.LeaveBattlefield = sa.ParamStr(cards.PKLeaveBattlefield)
	return g
}

// numForObjectText is NumForObject over a compiled parameter: "Double" reads
// obj's own power (power) or toughness, else numText.
func numForObjectText(h Host, c *Ctx, p ParamText, power bool, def int32, obj state.ObjID) int32 {
	if obj != 0 && strings.TrimSpace(p.Text) == "Double" {
		if power {
			return h.Power(obj)
		}
		return h.Toughness(obj)
	}
	return numText(h, c, p, def)
}

// pumpKnownKeys is every parameter key a Pump resolution consumes or
// deliberately ignores, sorted: compilePump's own reads, the shared
// machinery's (the cast/activation/targeting tier, Resolve's Condition* gate,
// the Defined$ resolver, the AtEOT$ rider), the presentation/AI-only keys and
// the structural SubAbility$/Keyword$ tags. rules'
// TestTypedParamKnownKeysMatchTheCensus holds it equal to the parameter
// census's measured api:Pump read set plus the ignored and structural keys.
var pumpKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "Adapt", "AddKeywords", "AddStaticAbilities", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "AtEOT", "Boast", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices",
	"ChooseFromList", "ClassBand", "ClearImprinted", "ClearNotedCardsFor", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn", "ConditionPresent",
	"ConditionSVarCompare", "CopyCard", "Cost", "CostDesc", "CounterTypePerDefined",
	"Defined", "DefinedCards", "DefinedTarget", "Description", "Destination", "Duration",
	"EffectOwner", "Exclude", "Exhaust", "ForgetImprinted", "GameActivationLimit",
	"Image", "ImprintCards", "ImprintPlayed", "InstantSpeed", "IntoPlayTapped", "IsCurse",
	"IsPresent", "KW", "KWChoice", "Keyword", "KeywordLine", "LeaveBattlefield",
	"MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "ModeCost", "Monstrosity",
	"NewController", "NoteCards", "NoteCardsFor", "NoteNumber", "NumAtt", "NumDef",
	"NumDmg", "OpponentTurn", "Origin", "Planeswalker", "PlayCost", "PlayerTurn",
	"PowerUp", "PrecostDesc", "PresentCompare", "PresentDefined", "PresentZone",
	"PumpZone", "ReduceAmount", "ReduceCost", "RememberCostMana", "RememberObjects",
	"RememberPumped", "RememberTargets", "ReplaceColor", "ReplaceGraveyard",
	"ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly", "ReplaceType", "RestrictValid",
	"SVarCompare", "Secondary", "SelectPrompt", "SetChosenMode", "SetColor", "ShowCards",
	"SorcerySpeed", "SpellDescription", "StackDescription", "StaticAbilities",
	"SubAbility", "TargetMax", "TargetMin", "TargetType", "TargetUnique",
	"TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls",
	"TargetsForEachPlayer", "TargetsWithControllerProperty",
	"TargetsWithDefinedController", "TargetsWithDifferentCMC",
	"TargetsWithDifferentControllers", "TargetsWithDifferentNames",
	"TargetsWithEqualToughness", "TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType", "TargetsWithSharedTypes",
	"TgtPrompt", "TgtZone", "TokenScript", "TriggerDescription", "TriggersWhenSpent",
	"Type", "Ultimate", "Unattach", "UnlessAI", "UnlessCost", "UnlessPayer",
	"UnlessResolveSubs", "UnlessSwitched", "ValidCard", "ValidCards", "ValidCardsDesc",
	"ValidChoices", "ValidCounterType", "ValidDescription", "ValidTgts", "VarName",
	"VarValue", "VoteMessage", "WithoutManaCost", "XMax", "XMin",
}

// PumpKnownKeys is a copy of pumpKnownKeys, for the census check.
func PumpKnownKeys() []string { return slices.Clone(pumpKnownKeys[:]) }
