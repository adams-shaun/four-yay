package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:Token's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on CopyPermanent's pattern (copypermanent_params.go). compileToken is
// the ONLY reader of a Token ability's own parameters: the resolution
// (token.go) reads the compiled TokenParams, including the split
// TokenScript$ stem list and the PumpKeywords$ list, which are pure
// functions of the text and so are decided here once.
// internal/codeshape's tokenParamLeaks ratchet holds that: no parameter read
// in token.go, and no read of a Token-only key anywhere else in rules/ or
// effects/.
//
// The machinery Token shares with other primitives -- the AtEOT$ scheduler,
// ForgetOtherRemembered$, the Defined$ resolver -- still reads its own keys
// in its own files.

// TokenParams is one Token ability's parameters, compiled once. Text fields
// are trimmed unless noted; ParamText fields keep the raw value the Num
// grammar or a Note reads.
type TokenParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// Amount is TokenAmount$ (the Num grammar, default 1).
	Amount ParamText
	// Owner is TokenOwner$, raw (the owner switch matches it verbatim).
	Owner string
	// Scripts are TokenScript$'s comma-separated stems, trimmed, blanks
	// dropped, in written order.
	Scripts []string
	// Remember is RememberTokens$ or RememberOriginalTokens$ spelled exactly
	// "True".
	Remember bool
	// AttachedTo is AttachedTo$: the Defined$-grammar bearer selector.
	AttachedTo string
	// Power and Toughness are TokenPower$ and TokenToughness$ (raw, the Num
	// grammar; Present names a dynamic side).
	Power, Toughness ParamText
	// WithCountersType is WithCountersType$; WithCountersAmount is
	// WithCountersAmount$ (raw, the Num grammar, default 1).
	WithCountersType   string
	WithCountersAmount ParamText
	// PumpKeywords is PumpKeywords$ split by cards.SplitKeywordList (shared,
	// never mutated); PumpDuration is PumpDuration$.
	PumpKeywords []string
	PumpDuration string
	// Tapped is TokenTapped$ True.
	Tapped bool
	// Remembered is TokenRemembered$: the Defined$ group bound to each
	// minted token's memory.
	Remembered string
	// Attacking is TokenAttacking$ ("" when the tokens do not enter
	// attacking).
	Attacking string
	// ImprintTokens is ImprintTokens$ True.
	ImprintTokens bool

	// Unread are the parameters present on the ability that no Token reader
	// consumes: the resolution Notes them once.
	Unread []string
}

// tokenKnownKeys is every parameter key a Token resolution consumes or
// deliberately ignores, sorted (changeZoneKnownKeys' contract): rules'
// TestTokenKnownKeysMatchTheCensus holds it equal to the parameter census's
// measured api:Token read set plus its ignored and structural keys.
var tokenKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat", "ActivationGameTypes",
	"ActivationLimit", "ActivationPhases", "ActivationZone", "Activator", "AddType",
	"AddTypes", "AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "AtEOT", "AttachedTo", "Boast",
	"ChangeTypeDesc", "CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone",
	"Choices", "ChooseFromList", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn", "ConditionPresent",
	"ConditionSVarCompare", "CopyCard", "Cost", "CostDesc", "Defined", "DefinedCards",
	"DefinedTarget", "Description", "Exclude", "Exhaust", "ForgetOtherRemembered",
	"GameActivationLimit", "Image", "ImprintCards", "ImprintPlayed", "ImprintTokens",
	"InstantSpeed", "IsCurse", "IsPresent", "KW", "Keyword", "KeywordLine",
	"MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "ModeCost", "Monstrosity",
	"NewController", "NumDmg", "OpponentTurn", "Planeswalker", "PlayCost", "PlayerTurn",
	"PowerUp", "PrecostDesc", "PresentCompare", "PresentDefined", "PresentZone",
	"PumpDuration", "PumpKeywords", "RandomNumTargets", "ReduceAmount", "ReduceCost",
	"RememberCostMana", "RememberObjects", "RememberOriginalTokens", "RememberTokens",
	"ReplaceColor", "ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana",
	"ReplaceOnly", "ReplaceType", "SVarCompare", "SelectPrompt", "SetChosenMode",
	"SetColor", "ShowCards", "SorcerySpeed", "SpellDescription", "StackDescription",
	"SubAbility", "TargetMax", "TargetMin", "TargetType", "TargetUnique",
	"TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls", "TargetsAtRandom",
	"TargetsForEachPlayer", "TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness", "TargetsWithSameCardType",
	"TargetsWithSameController", "TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenAmount", "TokenAttacking",
	"TokenOwner", "TokenPower", "TokenRemembered", "TokenScript", "TokenTapped",
	"TokenToughness", "TriggerDescription", "Type", "Ultimate", "UnlessAI", "UnlessCost",
	"UnlessPayer", "UnlessResolveSubs", "UnlessSwitched", "ValidCard", "ValidCards",
	"ValidCardsDesc", "ValidChoices", "ValidCounterType", "ValidDescription", "ValidTgts",
	"VoteMessage", "WithCountersAmount", "WithCountersType", "WithoutManaCost", "XMax",
	"XMin",
}

// isTokenSA reports whether sa resolves as api:Token.
func isTokenSA(sa *cards.SA) bool {
	return sa != nil && sa.API == "Token"
}

// TokenOf returns sa's compiled Token parameters: the configured record's
// when it is bound to sa's Params map, else the front cache's entry for that
// map, else a fresh compile stored in the front cache (ChangeZoneOf's
// contract).
func TokenOf(sa *cards.SA) *TokenParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Token; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &tokenFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileToken(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// tokenFront is TokenOf's direct-mapped front cache (czFront's shape).
var tokenFront [1 << 10]atomic.Pointer[TokenParams]

// compileToken is the one reader of a Token ability's parameters.
func compileToken(sa *cards.SA) *TokenParams {
	p := &TokenParams{paramBinding: bindParams(sa)}
	p.Amount = rawParamText(sa, "TokenAmount")
	p.Owner = sa.ParamStr(cards.PKTokenOwner)
	for key := range strings.SplitSeq(sa.ParamStr(cards.PKTokenScript), ",") {
		if key = strings.TrimSpace(key); key != "" {
			p.Scripts = append(p.Scripts, key)
		}
	}
	p.Remember = sa.ParamStr(cards.PKRememberTokens) == "True" || sa.ParamStr(cards.PKRememberOriginalTokens) == "True"
	p.AttachedTo = strings.TrimSpace(sa.ParamStr(cards.PKAttachedTo))
	p.Power = rawParamText(sa, "TokenPower")
	p.Toughness = rawParamText(sa, "TokenToughness")
	p.WithCountersType = strings.TrimSpace(sa.ParamStr(cards.PKWithCountersType))
	wa, ok := sa.Param(cards.PKWithCountersAmount)
	p.WithCountersAmount = ParamText{Text: wa, Present: ok}
	p.PumpKeywords = cards.SplitKeywordList(sa.ParamStr(cards.PKPumpKeywords))
	p.PumpDuration = strings.TrimSpace(sa.ParamStr(cards.PKPumpDuration))
	p.Tapped = isTrue(sa.ParamStr(cards.PKTokenTapped))
	p.Remembered = strings.TrimSpace(sa.ParamStr(cards.PKTokenRemembered))
	p.Attacking = strings.TrimSpace(sa.ParamStr(cards.PKTokenAttacking))
	p.ImprintTokens = isTrue(sa.ParamStr(cards.PKImprintTokens))
	p.Unread = unreadKeys(sa, tokenKnownKeys[:])
	return p
}

// TokenKnownKeys is a copy of tokenKnownKeys, for the census check.
func TokenKnownKeys() []string { return slices.Clone(tokenKnownKeys[:]) }
