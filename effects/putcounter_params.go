package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:PutCounter's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on ChangeZone's pattern (changezone_params.go). compilePutCounter is the
// ONLY reader of a PutCounter ability's own parameters: the resolution
// (counters_put.go), the rules-side entry-counter fold (entry_counters.go:
// ETB$, CounterType$, CounterNum$ and the asking-modifier presence gate), the
// Adapt$/Monstrosity$ offer gates, the opening-hand counter put and the
// per-recipient counter-kind answer binding all read the compiled
// PutCounterParams, so the offer, the entry fold and the resolution can no
// longer read a parameter differently. internal/codeshape's
// putCounterParamLeaks ratchet holds that: no parameter read in
// counters_put.go, and no read of a PutCounter-only key anywhere else in
// rules/ or effects/.
//
// The generic machinery a PutCounter runs -- the Defined$ resolver, the
// cast/activation/targeting tier (ValidTgts$, Cost$), the Monstrosity$ cost
// property (saFlagProperty) -- still reads its own keys; those are the next
// tier.

// PutCounterParams is one PutCounter ability's parameters, compiled once.
type PutCounterParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// The count: CounterNum$, then the Adapt$/Monstrosity$/Renown$ fallbacks
	// (each Set when its trimmed text is non-empty).
	CounterNum     ParamText
	CounterNumSet  bool
	Adapt          ParamText
	AdaptSet       bool
	Monstrosity    ParamText
	MonstrositySet bool
	Renown         ParamText
	RenownSet      bool

	// CounterType is CounterType$ as written; Kind is its canonical spelling
	// (canonicalCounterKind), "P1P1" when absent or empty.
	CounterType string
	Kind        string
	// Kinds is Kind split as a comma list (splitCounterKinds).
	Kinds []string

	// Placer$, trimmed.
	Placer string
	// Optional$ True.
	OptionalTrue bool
	// Bolster$ and Support$ (presence selects the shape).
	Bolster ParamText
	Support ParamText
	// Divided is a non-empty DividedAsYouChoose$.
	Divided bool
	// Choices$, trimmed; Chooser$ and ChoiceTitle$ as written.
	Choices     string
	Chooser     string
	ChoiceTitle string
	// MinChoiceAmount$ and ChoiceAmount$ (the pick's bounds).
	MinChoiceAmount ParamText
	ChoiceAmount    ParamText
	// ETB$ True: the counters land on the entering object.
	ETB bool
	// EachFromSource$, trimmed (the CounterType$ EachFromSource referent).
	EachFromSource string
	// CounterTypePerDefined$ True, RandomType$ True, a non-empty
	// ChooseDifferent$.
	CounterTypePerDefined bool
	RandomType            bool
	ChooseDifferent       bool
	// CounterNumPerDefined$, trimmed.
	CounterNumPerDefined string
	// RememberPut$ True, RememberCards$ True.
	RememberPut   bool
	RememberCards bool

	// EntryFoldBlocked reports that the ability carries an asking or
	// per-recipient modifier the entry-counter fold must leave to the
	// ordinary body path (entryBodyAbsorbable): Optional$, Choices$,
	// Divided$, DividedAsYouChoose$, RandomType$, Bolster$, Support$, Adapt$,
	// Monstrosity$, Renown$, CounterNumPerDefined$, CounterTypePerDefined$,
	// EachFromSource$ or PerDefined$, present with any value.
	EntryFoldBlocked bool

	// Unread are the parameters present on the ability that no PutCounter
	// reader consumes: the resolution Notes them once.
	Unread []string
}

// putCounterKnownKeys is every parameter key a PutCounter resolution consumes
// or deliberately ignores, sorted (changeZoneKnownKeys' contract): rules'
// TestPutCounterKnownKeysMatchTheCensus holds it equal to the parameter
// census's measured api:PutCounter read set plus its ignored and structural
// keys.
var putCounterKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment",
	"AITgts", "Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "Adapt", "AddKeywords", "AddStaticAbilities", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "BecomeStartingPlayer", "Boast",
	"Bolster", "ChangeTypeDesc", "CharacteristicDefining", "CheckSVar",
	"ChoiceAmount", "ChoiceTitle", "ChoiceZone", "Choices", "ChooseDifferent",
	"ChooseFromList", "Chooser", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn",
	"ConditionPresent", "ConditionSVarCompare", "CopyCard", "Cost", "CostDesc",
	"CounterNum", "CounterNumPerDefined", "CounterType", "CounterTypePerDefined",
	"Defined", "DefinedCards", "DefinedTarget", "Description", "Divided",
	"DividedAsYouChoose", "ETB", "EachFromSource", "Exclude",
	"Exhaust", "GameActivationLimit", "Image", "ImprintCards", "ImprintPlayed",
	"InstantSpeed", "IntoPlayTapped", "IsCurse", "IsPresent", "KW",
	"Keyword", "KeywordLine", "MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor",
	"MinChoiceAmount", "ModeCost", "Monstrosity", "NewController",
	"NumDmg", "OpponentTurn", "Optional", "PerDefined",
	"Placer", "Planeswalker", "PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc",
	"PresentCompare", "PresentDefined", "PresentZone", "RandomType", "ReduceAmount",
	"ReduceCost", "RememberCards", "RememberCostMana", "RememberObjects",
	"RememberPut", "Renown", "ReplaceColor", "ReplaceGraveyard",
	"ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly", "ReplaceType",
	"RestrictValid", "SVarCompare", "SelectPrompt", "SetChosenMode", "SetColor",
	"ShowCards", "SorcerySpeed", "SpellDescription", "StackDescription",
	"SubAbility", "Support", "TargetMax", "TargetMin",
	"TargetType", "TargetUnique", "TargetValidTargeting", "TargetingPlayer",
	"TargetingPlayerControls", "TargetsForEachPlayer",
	"TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness",
	"TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenScript",
	"TriggerDescription", "TriggersWhenSpent", "Type", "Ultimate",
	"UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs", "UnlessSwitched",
	"ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices", "ValidCounterType",
	"ValidDescription", "ValidTgts", "VarName", "VarValue", "VoteMessage",
	"WithoutManaCost", "XMax", "XMin",
}

// isPutCounterSA reports whether sa resolves as api:PutCounter.
func isPutCounterSA(sa *cards.SA) bool {
	return sa != nil && (sa.CompiledAPI() == cards.APIPutCounter || sa.API == "PutCounter")
}

// IsPutCounter reports whether sa resolves as api:PutCounter (the rules-side
// readers' gate before PutCounterOf).
func IsPutCounter(sa *cards.SA) bool { return isPutCounterSA(sa) }

// PutCounterOf returns sa's compiled PutCounter parameters: the configured
// record's when it is bound to sa's Params map, else the front cache's entry
// for that map, else a fresh compile stored in the front cache (ChangeZoneOf's
// contract).
func PutCounterOf(sa *cards.SA) *PutCounterParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.PutCounter; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &putCounterFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compilePutCounter(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// putCounterFront is PutCounterOf's direct-mapped front cache (czFront's
// shape).
var putCounterFront [1 << 10]atomic.Pointer[PutCounterParams]

// CounterNumResolved is CounterNum$ through NumResolved (default 1): the
// entry-counter fold's count, the same read the resolution makes.
func (p *PutCounterParams) CounterNumResolved(h Host, c *Ctx) (int32, bool) {
	return numResolvedText(h, c, p.CounterNum, 1)
}

// CounterNumValue is CounterNum$ through Num (default 1).
func (p *PutCounterParams) CounterNumValue(h Host, c *Ctx) int32 {
	return numText(h, c, p.CounterNum, 1)
}

// compilePutCounter is the one reader of a PutCounter ability's parameters.
func compilePutCounter(sa *cards.SA) *PutCounterParams {
	p := &PutCounterParams{paramBinding: bindParams(sa)}
	// Each key is read by its literal name (the parameter census attributes
	// a read by the key the call spells).
	num, numOK := sa.Param(cards.PKCounterNum)
	p.CounterNum, p.CounterNumSet = paramTextSet(num, numOK)
	adapt, adaptOK := sa.Param(cards.PKAdapt)
	p.Adapt, p.AdaptSet = paramTextSet(adapt, adaptOK)
	mono, monoOK := sa.Param(cards.PKMonstrosity)
	p.Monstrosity, p.MonstrositySet = paramTextSet(mono, monoOK)
	renown, renownOK := sa.Params["Renown"]
	p.Renown, p.RenownSet = paramTextSet(renown, renownOK)

	p.CounterType = sa.ParamStr(cards.PKCounterType)
	p.Kind = canonicalCounterKind(p.CounterType)
	if p.Kind == "" {
		p.Kind = "P1P1"
	}
	kinds := splitCounterKinds(p.Kind)
	p.Kinds = kinds[:len(kinds):len(kinds)]

	p.Placer = strings.TrimSpace(sa.ParamStr(cards.PKPlacer))
	optional, optionalOK := sa.Param(cards.PKOptional)
	p.OptionalTrue = isTrue(optional)
	bolster, bolsterOK := sa.Params["Bolster"]
	p.Bolster = ParamText{Text: bolster, Present: bolsterOK}
	support, supportOK := sa.Params["Support"]
	p.Support = ParamText{Text: support, Present: supportOK}
	divided, dividedOK := sa.Param(cards.PKDividedAsYouChoose)
	p.Divided = strings.TrimSpace(divided) != ""
	choices, choicesOK := sa.Param(cards.PKChoices)
	p.Choices = strings.TrimSpace(choices)
	p.Chooser = sa.ParamStr(cards.PKChooser)
	p.ChoiceTitle = sa.ParamStr(cards.PKChoiceTitle)
	p.MinChoiceAmount = rawParamText(sa, "MinChoiceAmount")
	p.ChoiceAmount = rawParamText(sa, "ChoiceAmount")
	p.ETB = isTrue(sa.ParamStr(cards.PKETB))
	eachFrom, eachFromOK := sa.Params["EachFromSource"]
	p.EachFromSource = strings.TrimSpace(eachFrom)
	perType, perTypeOK := sa.Params["CounterTypePerDefined"]
	p.CounterTypePerDefined = isTrue(perType)
	random, randomOK := sa.Params["RandomType"]
	p.RandomType = isTrue(random)
	p.ChooseDifferent = strings.TrimSpace(sa.Params["ChooseDifferent"]) != ""
	perNum, perNumOK := sa.Params["CounterNumPerDefined"]
	p.CounterNumPerDefined = strings.TrimSpace(perNum)
	p.RememberPut = isTrue(sa.ParamStr(cards.PKRememberPut))
	p.RememberCards = isTrue(sa.Params["RememberCards"])

	p.EntryFoldBlocked = optionalOK || choicesOK || rawParamText(sa, "Divided").Present || dividedOK ||
		randomOK || bolsterOK || supportOK || p.Adapt.Present || p.Monstrosity.Present || renownOK ||
		perNumOK || perTypeOK || eachFromOK || rawParamText(sa, "PerDefined").Present

	p.Unread = unreadKeys(sa, putCounterKnownKeys[:])
	return p
}

// rawParamText is one key outside the ParamKey vocabulary read as ParamText
// (Num's raw read shape); every call site spells its key as a literal, so the
// parameter census attributes each read to the caller.
func rawParamText(sa *cards.SA, key string) ParamText {
	v, ok := sa.Params[key]
	return ParamText{Text: v, Present: ok}
}

// paramTextSet is a raw parameter read as ParamText, and whether its trimmed
// text is non-empty.
func paramTextSet(v string, ok bool) (ParamText, bool) {
	return ParamText{Text: v, Present: ok}, strings.TrimSpace(v) != ""
}

// PutCounterKnownKeys is a copy of putCounterKnownKeys, for the census check.
func PutCounterKnownKeys() []string { return slices.Clone(putCounterKnownKeys[:]) }
