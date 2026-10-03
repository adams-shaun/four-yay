package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:CopyPermanent's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on ChangeZone's pattern (changezone_params.go). compileCopyPermanent is
// the ONLY reader of a CopyPermanent ability's own parameters: the resolution
// (copypermanent.go) reads the compiled CopyPermanentParams, including the
// one-per-call "does not implement" Note and the mint-blocking verdict, which
// are pure functions of the text and so are decided here once.
// internal/codeshape's copyPermanentParamLeaks ratchet holds that: no
// parameter read in copypermanent.go, and no read of a CopyPermanent-only key
// anywhere else in rules/ or effects/.
//
// The machinery CopyPermanent shares with api:Token -- TokenRemembered$
// (tokenRememberedTargets) and the TokenAttacking$ rider's defender pick --
// still reads its own keys in token.go; the Defined$ resolver and the
// targeting tier are the next tier.

// CopyPermanentParams is one CopyPermanent ability's parameters, compiled
// once. Text fields are trimmed unless noted; ParamText fields keep the raw
// value the Num grammar or a Note reads.
type CopyPermanentParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// SkippedNote is the one-per-call Note naming every unimplemented source
	// or modification family ("" when none), and Blocked reports that one of
	// them is a source-selection family, so the call mints nothing.
	SkippedNote string
	Blocked     bool
	// SupportsChoice is the measured Zndrsplt Choices$/Chooser$/Controller$
	// shape the chooser ask implements.
	SupportsChoice bool

	// AtEOT$ and AtEOTTrig$.
	AtEOT     string
	AtEOTTrig string

	// The characteristic modifications.
	AddTypes            ParamText
	SetColor            ParamText
	SetPower            ParamText
	SetToughness        ParamText
	SetCreatureTypes    ParamText
	RemoveCardTypes     ParamText
	RemoveCreatureTypes ParamText
	RemoveSubTypes      ParamText
	NonLegendary        ParamText
	// AddKeywords, PumpKeywords and RemoveKeywords are the raw keyword lists
	// (cards.SplitKeywordList's input); AddKeywordsPresent is AddKeywords$
	// presence.
	AddKeywords        string
	AddKeywordsPresent bool
	PumpKeywords       string
	RemoveKeywords     string
	PumpDuration       string

	// WithCountersType$ (trimmed) and WithCountersAmount$.
	WithCountersType   string
	WithCountersAmount ParamText

	// Entry-state riders: TokenTapped$ and TokenAttacking$, trimmed.
	TokenTapped    string
	TokenAttacking string
	// NumCopies is NumCopies$.
	NumCopies ParamText

	// Source selection: Defined$, ValidTgts$ presence and Populate$ True.
	Defined  string
	HasTgts  bool
	Populate bool
	// Controller is Controller$.
	Controller string
	// RememberTokens is RememberTokens$ True; ImprintTokens ImprintTokens$
	// True.
	RememberTokens bool
	ImprintTokens  bool
	// AttachedTo is AttachedTo$.
	AttachedTo string
	// The named grants, raw comma lists over the source's SVar table.
	AddTriggers        string
	AddSVars           string
	AddAbilities       string
	AddStaticAbilities string

	// Unread are the parameters present on the ability that no CopyPermanent
	// reader consumes: the resolution Notes them once.
	Unread []string
}

// copyPermanentKnownKeys is every parameter key a CopyPermanent resolution
// consumes or deliberately ignores, sorted (changeZoneKnownKeys' contract):
// rules' TestCopyPermanentKnownKeysMatchTheCensus holds it equal to the
// parameter census's measured api:CopyPermanent read set plus its ignored
// and structural keys.
var copyPermanentKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment",
	"AITgts", "Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "AddAbilities", "AddKeywords", "AddSVars", "AddStaticAbilities",
	"AddTriggers", "AddType", "AddTypes", "AdditionalDesc", "AdditionalDescription",
	"Affected", "AlternateCost", "AlternativeCost", "Announce", "AnnounceTitle",
	"AtEOT", "AtEOTTrig", "AttachedTo", "Boast", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices",
	"ChooseFromList", "Chooser", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn",
	"ConditionPresent", "ConditionSVarCompare", "Controller", "CopyCard", "Cost",
	"CostDesc", "Defined", "DefinedCards", "DefinedName", "DefinedTarget",
	"Description", "Exclude", "Execute", "Exhaust", "GameActivationLimit", "Image",
	"ImprintCards", "ImprintPlayed", "ImprintTokens", "InstantSpeed",
	"IsCurse", "IsPresent", "KW", "Keyword", "KeywordLine",
	"MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "ModeCost", "Monstrosity",
	"NewController", "NonLegendary", "NumCopies", "NumDmg", "OpponentTurn",
	"Pawprint", "Planeswalker", "PlayCost", "PlayerTurn", "Populate", "PowerUp",
	"PrecostDesc", "PresentCompare", "PresentDefined", "PresentZone", "PumpDuration",
	"PumpKeywords", "RandomCopied", "RandomNum", "RandomNumTargets", "ReduceAmount", "ReduceCost",
	"RememberCostMana", "RememberObjects", "RememberTokens", "RemoveCardTypes",
	"RemoveCreatureTypes", "RemoveKeywords", "RemoveSubTypes", "ReplaceColor",
	"ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly",
	"ReplaceType", "SVarCompare", "SelectPrompt", "SetChosenMode", "SetColor",
	"SetCreatureTypes", "SetPower", "SetToughness", "ShowCards", "SorcerySpeed",
	"SpellDescription", "StackDescription", "SubAbility", "TargetMax", "TargetMin",
	"TargetType", "TargetUnique", "TargetValidTargeting", "TargetingPlayer",
	"TargetingPlayerControls", "TargetsAtRandom", "TargetsForEachPlayer",
	"TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness",
	"TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenAttacking",
	"TokenRemembered", "TokenScript", "TokenTapped", "TriggerDescription", "Type",
	"Ultimate", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices",
	"ValidCounterType", "ValidDescription", "ValidSupportedCopy", "ValidTgts",
	"VoteMessage", "WithCountersAmount", "WithCountersType", "WithDifferentNames",
	"WithoutManaCost", "XMax", "XMin",
}

// isCopyPermanentSA reports whether sa resolves as api:CopyPermanent.
func isCopyPermanentSA(sa *cards.SA) bool {
	return sa != nil && sa.API == "CopyPermanent"
}

// CopyPermanentOf returns sa's compiled CopyPermanent parameters: the
// configured record's when it is bound to sa's Params map, else the front
// cache's entry for that map, else a fresh compile stored in the front cache
// (ChangeZoneOf's contract).
func CopyPermanentOf(sa *cards.SA) *CopyPermanentParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.CopyPermanent; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &copyPermanentFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileCopyPermanent(sa, DefinedOf(sa))
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// copyPermanentFront is CopyPermanentOf's direct-mapped front cache
// (czFront's shape).
var copyPermanentFront [1 << 10]atomic.Pointer[CopyPermanentParams]

// compileCopyPermanent is the one reader of a CopyPermanent ability's
// parameters.
func compileCopyPermanent(sa *cards.SA, dp *DefinedParams) *CopyPermanentParams {
	p := &CopyPermanentParams{paramBinding: bindParams(sa)}

	// The one-per-call skipped-family Note (see effCopyPermanent). Literally
	// keyed reads only: the param census rejects a dynamic Params key it
	// cannot attribute.
	var skipped []string
	note := func(label string) { skipped = append(skipped, label) }
	if _, ok := sa.Params["DefinedName"]; ok {
		note("DefinedName$")
		p.Blocked = true
	}
	if _, ok := sa.Params["Pawprint"]; ok {
		note("Pawprint$")
		p.Blocked = true
	}
	if _, ok := sa.Params["RandomCopied"]; ok {
		note("RandomCopied$")
		p.Blocked = true
	}
	if _, ok := sa.Params["RandomNum"]; ok {
		note("RandomNum$")
		p.Blocked = true
	}
	if _, ok := sa.Params["ValidSupportedCopy"]; ok {
		note("ValidSupportedCopy$")
		p.Blocked = true
	}
	choices, choicesOK := sa.Param(cards.PKChoices)
	chooser, chooserOK := sa.Param(cards.PKChooser)
	p.Controller = strings.TrimSpace(sa.ParamStr(cards.PKController))
	p.SupportsChoice = strings.TrimSpace(choices) == "Creature.RememberedPlayerCtrl" &&
		strings.TrimSpace(chooser) == "Remembered" && p.Controller == "Remembered"
	if choicesOK && !p.SupportsChoice {
		note("Choices$")
		p.Blocked = true
	}
	if _, ok := sa.Params["WithDifferentNames"]; ok {
		note("WithDifferentNames$")
	}
	if chooserOK && !p.SupportsChoice {
		note("Chooser$")
		p.Blocked = true
	}
	if len(skipped) > 0 {
		p.SkippedNote = "CopyPermanent does not implement " + strings.Join(skipped, ", ") +
			"; the copy keeps the original's printed characteristics"
	}

	p.AtEOT = strings.TrimSpace(sa.Params["AtEOT"])
	p.AtEOTTrig = strings.TrimSpace(sa.Params["AtEOTTrig"])

	addTypes, ok := sa.Param(cards.PKAddTypes)
	p.AddTypes = ParamText{Text: addTypes, Present: ok}
	setColor, ok := sa.Param(cards.PKSetColor)
	p.SetColor = ParamText{Text: setColor, Present: ok}
	setPower, ok := sa.Param(cards.PKSetPower)
	p.SetPower = ParamText{Text: setPower, Present: ok}
	setToughness, ok := sa.Param(cards.PKSetToughness)
	p.SetToughness = ParamText{Text: setToughness, Present: ok}
	p.SetCreatureTypes = rawParamText(sa, "SetCreatureTypes")
	removeCardTypes, ok := sa.Param(cards.PKRemoveCardTypes)
	p.RemoveCardTypes = ParamText{Text: removeCardTypes, Present: ok}
	removeCreatureTypes, ok := sa.Param(cards.PKRemoveCreatureTypes)
	p.RemoveCreatureTypes = ParamText{Text: removeCreatureTypes, Present: ok}
	p.RemoveSubTypes = rawParamText(sa, "RemoveSubTypes")
	nonLegendary, ok := sa.Param(cards.PKNonLegendary)
	p.NonLegendary = ParamText{Text: nonLegendary, Present: ok}
	p.AddKeywords, p.AddKeywordsPresent = sa.Param(cards.PKAddKeywords)
	p.PumpKeywords = sa.ParamStr(cards.PKPumpKeywords)
	p.RemoveKeywords = sa.ParamStr(cards.PKRemoveKeywords)
	p.PumpDuration = strings.TrimSpace(sa.ParamStr(cards.PKPumpDuration))

	p.WithCountersType = strings.TrimSpace(sa.ParamStr(cards.PKWithCountersType))
	withAmt, ok := sa.Param(cards.PKWithCountersAmount)
	p.WithCountersAmount = ParamText{Text: withAmt, Present: ok}

	p.TokenTapped = strings.TrimSpace(sa.Params["TokenTapped"])
	p.TokenAttacking = strings.TrimSpace(sa.Params["TokenAttacking"])
	p.NumCopies = rawParamText(sa, "NumCopies")

	p.Defined = dp.Defined.Text
	p.HasTgts = TargetsOf(sa).Has(TgtValidPresent)
	p.Populate = isTrue(sa.Params["Populate"])
	p.RememberTokens = isTrue(sa.Params["RememberTokens"])
	p.ImprintTokens = isTrue(sa.Params["ImprintTokens"])
	p.AttachedTo = strings.TrimSpace(sa.ParamStr(cards.PKAttachedTo))
	p.AddTriggers = sa.ParamStr(cards.PKAddTriggers)
	p.AddSVars = sa.ParamStr(cards.PKAddSVars)
	p.AddAbilities = sa.ParamStr(cards.PKAddAbilities)
	p.AddStaticAbilities = sa.ParamStr(cards.PKAddStaticAbilities)

	p.Unread = unreadKeys(sa, copyPermanentKnownKeys[:])
	return p
}

// CopyPermanentKnownKeys is a copy of copyPermanentKnownKeys, for the census
// check.
func CopyPermanentKnownKeys() []string { return slices.Clone(copyPermanentKnownKeys[:]) }
