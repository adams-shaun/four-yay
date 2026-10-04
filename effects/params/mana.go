package params

import (
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:Mana's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8). compileMana is the ONLY reader of a mana ability's production
// parameters (Produced$, Amount$, RestrictValid$, the AddsNoCounter$/
// AddsCounters$/PersistentMana$/PersistentUntilEndOfCombat$/
// TriggersWhenSpent$ riders, the Defined$ recipient presence): the
// resolution (mana_effect.go's effMana), and every rules reader of a mana
// ability's production -- the activation and colour ask, the wheel labels,
// the AvailableMana/PotentialMana projections, the payment windows, the
// payment planner and its census facts (rules' manaSAFacts derive from this
// struct instead of re-reading the Params) -- read the compiled ManaParams.
// internal/codeshape's manaParamLeaks ratchet holds that: no parameter read
// in mana_effect.go, and no read of a Mana-only rider key anywhere else in
// rules/ or effects/.
//
// Not compiled here: the activation tier every AB$ ability shares (Cost$,
// the Activation*/ActivationLimit$ gates, Activator$, IsPresent$, CheckSVar$),
// which rules' activation gates read for every API alike.
//
// Every field is compiled for an ability of ANY API (rules' mana walk reads
// a ManaReflected or granted ability's production through the same struct);
// only the unread report is api:Mana's own.

// ManaParams is one mana ability's production parameters, compiled once.
type ManaParams struct {
	paramBinding

	// Produced$: present, as written, and trimmed.
	HasProduced bool
	ProducedRaw string
	Produced    string
	// Counts/CountsAny are cards.ProducedCounts(Produced$).
	Counts    [6]int32
	CountsAny bool

	// Amount$ as written (numText), trimmed, and its strconv literal value
	// (AmountIsLit false for a blank or non-literal amount).
	Amount      ParamText
	AmountTrim  string
	AmountLit   int
	AmountIsLit bool

	// RestrictValid$, trimmed.
	RestrictValid string
	// The production riders, trimmed ("" = absent).
	AddsNoCounter              string
	AddsCounters               string
	PersistentMana             string
	PersistentUntilEndOfCombat string
	TriggersWhenSpent          string
	// HasDefined: a non-blank Defined$ names the recipients (ManaRecipients).
	HasDefined bool

	// PlainSym/PlainAmt: the parameter set is a plain production -- one
	// W/U/B/R/G/C Produced$ symbol, an absent or literal Amount$ in
	// [1, 1<<20], and nothing beyond Cost$ and the presentation/AI keys
	// (rules' plainManaShape adds the API and SubAbility$ tests). PlainSym is
	// 0 otherwise.
	PlainSym byte
	PlainAmt int32

	// Unread are the parameters present on an api:Mana ability that no Mana
	// reader consumes (manaKnownKeys): effMana Notes them.
	Unread []string
}

var zeroMana ManaParams

func init() { zeroMana.Counts, zeroMana.CountsAny = cards.ProducedCounts("") }

// ManaOf returns sa's compiled mana parameters: the configured record's when
// bound to sa's Params map, else the front cache's entry for that map, else a
// fresh compile stored in the front cache (ChangeZoneOf's shape). A nil sa
// reads as an ability with no parameters.
func ManaOf(sa *cards.SA) *ManaParams {
	if sa == nil {
		return &zeroMana
	}
	if f := LoadFacts(sa); f != nil {
		if p := f.Mana; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &manaFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileMana(sa, DefinedOf(sa))
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// manaFront is ManaOf's direct-mapped front cache, keyed by the Params map's
// identity (a collision only costs a recompile).
var manaFront [1 << 10]atomic.Pointer[ManaParams]

// compileMana is the one reader of a mana ability's production parameters.
func compileMana(sa *cards.SA, dp *DefinedParams) *ManaParams {
	p := &ManaParams{paramBinding: bindParams(sa)}
	p.ProducedRaw, p.HasProduced = sa.Param(cards.PKProduced)
	p.Produced = strings.TrimSpace(p.ProducedRaw)
	p.Counts, p.CountsAny = cards.ProducedCounts(p.ProducedRaw)

	amt, amtOK := sa.Param(cards.PKAmount)
	p.Amount = ParamText{Text: amt, Present: amtOK}
	p.AmountTrim = strings.TrimSpace(amt)
	if v, err := strconv.Atoi(p.AmountTrim); err == nil {
		p.AmountLit, p.AmountIsLit = v, true
	}

	p.RestrictValid = strings.TrimSpace(sa.ParamStr(cards.PKRestrictValid))
	p.AddsNoCounter = strings.TrimSpace(sa.ParamStr(cards.PKAddsNoCounter))
	p.AddsCounters = strings.TrimSpace(sa.ParamStr(cards.PKAddsCounters))
	p.PersistentMana = strings.TrimSpace(sa.ParamStr(cards.PKPersistentMana))
	p.PersistentUntilEndOfCombat = strings.TrimSpace(sa.ParamStr(cards.PKPersistentUntilEndOfCombat))
	p.TriggersWhenSpent = strings.TrimSpace(sa.ParamStr(cards.PKTriggersWhenSpent))
	p.HasDefined = dp.Defined.Set()

	p.PlainSym, p.PlainAmt = plainManaParams(sa, p)
	if sa.API == "Mana" {
		p.Unread = unreadKeys(sa, manaKnownKeys[:])
	}
	return p
}

// plainManaKeys are the keys a plain production may carry: Produced$,
// Amount$, Cost$ and the presentation/AI keys, sorted.
var plainManaKeys = [...]string{"AILogic", "AINoRecursiveCheck", "Amount", "Cost", "CostDesc",
	"PrecostDesc", "Produced", "SpellDescription", "StackDescription"}

// plainManaParams is ManaParams.PlainSym/PlainAmt (see there).
func plainManaParams(sa *cards.SA, p *ManaParams) (byte, int32) {
	if !p.HasProduced {
		return 0, 0
	}
	// A subset test (key order cannot change the answer): beyond Produced$
	// and Amount$, only Cost$ and the presentation/AI keys.
	for k := range sa.Params {
		if _, ok := slices.BinarySearch(plainManaKeys[:], k); !ok {
			return 0, 0
		}
	}
	amt := int32(1)
	if p.Amount.Present {
		v := p.AmountLit
		if !p.AmountIsLit || v < 1 || v > 1<<20 {
			return 0, 0
		}
		amt = int32(v)
	}
	if len(p.Produced) != 1 || !strings.Contains("WUBRGC", p.Produced) {
		return 0, 0
	}
	return p.Produced[0], amt
}

// manaKnownKeys is every parameter key a Mana resolution consumes or
// deliberately ignores, sorted: compileMana's own reads, the shared
// machinery's (the cast/activation/targeting tier, Resolve's Condition* gate,
// the Defined$ resolver), the presentation/AI-only keys and the structural
// SubAbility$/Keyword$ tags. rules' TestManaKnownKeysMatchTheCensus holds it
// equal to the parameter census's measured api:Mana read set plus the
// ignored and structural keys.
var manaKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat", "ActivationGameTypes",
	"ActivationLimit", "ActivationPhases", "ActivationZone", "Activator", "AddType",
	"AddTypes", "AdditionalDesc", "AdditionalDescription", "AddsCounters", "AddsNoCounter",
	"Affected", "AlternateCost", "AlternativeCost", "Amount", "Announce", "AnnounceTitle",
	"Boast", "ChangeTypeDesc", "CharacteristicDefining", "CheckSVar", "ChoiceTitle",
	"ChoiceZone", "Choices", "ChooseFromList", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn", "ConditionPresent",
	"ConditionSVarCompare", "CopyCard", "Cost", "CostDesc", "Defined", "DefinedCards",
	"DefinedTarget", "Description", "Exclude", "Exhaust", "GameActivationLimit", "Image",
	"ImprintCards", "ImprintPlayed", "InstantSpeed", "IsCurse", "IsPresent", "KW",
	"Keyword", "KeywordLine", "MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor",
	"ModeCost", "Monstrosity", "NewController", "NumDmg", "OpponentTurn", "PersistentMana",
	"PersistentUntilEndOfCombat", "Planeswalker", "PlayCost", "PlayerTurn", "PowerUp",
	"PrecostDesc", "PresentCompare", "PresentDefined", "PresentZone", "Produced",
	"RandomNumTargets", "ReduceAmount", "ReduceCost", "RememberCostMana", "RememberObjects",
	"ReplaceColor", "ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana",
	"ReplaceOnly", "ReplaceType", "RestrictValid", "SVarCompare", "SelectPrompt",
	"SetChosenMode", "SetColor", "ShowCards", "SorcerySpeed", "SpellDescription",
	"StackDescription", "SubAbility", "TargetMax", "TargetMin", "TargetType",
	"TargetUnique", "TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls",
	"TargetsAtRandom", "TargetsForEachPlayer", "TargetsWithControllerProperty",
	"TargetsWithDefinedController", "TargetsWithDifferentCMC",
	"TargetsWithDifferentControllers", "TargetsWithDifferentNames",
	"TargetsWithEqualToughness", "TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType", "TargetsWithSharedTypes",
	"TgtPrompt", "TgtZone", "TokenScript", "TriggerDescription", "TriggersWhenSpent",
	"Type", "Ultimate", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices",
	"ValidCounterType", "ValidDescription", "ValidTgts", "VoteMessage", "WithoutManaCost",
	"XMax", "XMin",
}

// ManaKnownKeys is a copy of manaKnownKeys, for the census check.
func ManaKnownKeys() []string { return slices.Clone(manaKnownKeys[:]) }
