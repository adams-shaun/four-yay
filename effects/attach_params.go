package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// This file is api:Attach's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on ChangeZone's pattern (changezone_params.go). compileAttach is the
// ONLY reader of an Attach ability's own parameters: the resolution
// (attach.go), the rules-side target-zone inference (targetZones,
// originImpliedTargetZone: ValidTgts$' inZone<X> words and Origin$) and the
// Reconfigure offer gate (Unattach$) all read the compiled AttachParams, so
// the offer and the resolution can no longer read a parameter differently.
// internal/codeshape's attachParamLeaks ratchet holds that: no parameter read
// in attach.go, and no read of an Attach-only key anywhere else in rules/ or
// effects/.
//
// The generic machinery an Attach runs -- the Defined$ resolver, the
// cast/activation/targeting tier (ValidTgts$ legality, TgtZone$, TargetType$,
// Cost$, Boast$) -- still reads its own keys; those are the next tier.

// attachObjectKind is Object$ compiled: which object(s) the Attach fastens.
type attachObjectKind uint8

const (
	attachObjectSelf       attachObjectKind = iota // absent, or Self
	attachObjectRemembered                         // Remembered
	attachObjectAttachedTo                         // the plural "AttachedTo <referent>" family
	attachObjectDefined                            // any other definedSpec selector
)

// AttachParams is one Attach ability's parameters, compiled once.
type AttachParams struct {
	// src/n are the Params map the struct was compiled from and its size
	// then (the identity rule cards.SameParamMap applies).
	src map[string]string
	n   int

	// Unattach$ True: kw:Reconfigure's unattach half.
	Unattach bool
	// Object$: its text, presence and compiled kind.
	Object        string
	ObjectPresent bool
	objectKind    attachObjectKind
	// EnchantSelf is Keyword$ Enchant with Object$ Self: the Aura's own
	// attach spell, whose Enchant:Player/Opponent spec names a seat.
	EnchantSelf bool
	// PlayerChoices$ (the destination player pool) and Choices$ (the object
	// or destination pool), trimmed.
	PlayerChoices string
	Choices       string
	// Optional$ True, RememberAttached$ True.
	OptionalTrue     bool
	RememberAttached bool
	// ChoicePrompt is a Choices$ ask's prompt: ChoiceTitle$, else the
	// generic default.
	ChoicePrompt string

	// ValidTgtsZones are the zones ValidTgts$' comma-split alternatives name
	// with inZone<X> words (targetZones' Attach inference), nil when none.
	ValidTgtsZones []state.Zone
	// OriginSingle reports that Origin$ parses as exactly one concrete zone
	// (not Any/All, no unknown token), OriginZone (originImpliedTargetZone).
	OriginSingle bool
	OriginZone   state.Zone

	// Unread are the parameters present on the ability that no Attach reader
	// consumes: the resolution Notes them once.
	Unread []string
}

// attachKnownKeys is every parameter key an Attach resolution consumes or
// deliberately ignores, sorted (changeZoneKnownKeys' contract): rules'
// TestAttachKnownKeysMatchTheCensus holds it equal to the parameter census's
// measured api:Attach read set plus its ignored and structural keys.
var attachKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment",
	"AITgts", "Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "AddKeywords", "AddStaticAbilities", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "Boast", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices",
	"ChooseFromList", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn",
	"ConditionPresent", "ConditionSVarCompare", "CopyCard", "Cost", "CostDesc",
	"Defined", "DefinedCards", "DefinedTarget",
	"Description", "EffectOwner", "Exclude", "Exhaust", "GameActivationLimit",
	"Image", "ImprintCards", "ImprintPlayed", "InstantSpeed", "IntoPlayTapped",
	"IsCurse", "IsPresent", "KW", "KWChoice", "Keyword", "KeywordLine",
	"MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "ModeCost", "Monstrosity",
	"NewController", "NumAtt", "NumCards", "NumDef", "NumDmg", "Object",
	"OpponentTurn", "Optional", "Origin", "Planeswalker", "PlayCost", "PlayerChoices",
	"PlayerTurn", "PowerUp", "PrecostDesc", "PresentCompare", "PresentDefined",
	"PresentZone", "ReduceAmount", "ReduceCost", "RememberAttached",
	"RememberCostMana", "RememberObjects", "ReplaceColor", "ReplaceGraveyard",
	"ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly", "ReplaceType",
	"RestrictValid", "SVarCompare", "SelectPrompt", "SetChosenMode", "SetColor",
	"ShowCards", "SorcerySpeed", "SpellDescription", "StackDescription",
	"StaticAbilities", "SubAbility", "TargetMax", "TargetMin", "TargetType",
	"TargetUnique", "TargetValidTargeting", "TargetingPlayer",
	"TargetingPlayerControls", "TargetsForEachPlayer",
	"TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness",
	"TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenScript",
	"TriggerDescription", "TriggersWhenSpent", "Type", "Ultimate", "Unattach",
	"UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs", "UnlessSwitched",
	"ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices", "ValidCounterType",
	"ValidDescription", "ValidTgts", "VarName", "VarValue", "VoteMessage",
	"WithoutManaCost", "XMax", "XMin",
}

// isAttachSA reports whether sa resolves as api:Attach.
func isAttachSA(sa *cards.SA) bool {
	return sa != nil && (sa.CompiledAPI() == cards.APIAttach || sa.API == "Attach")
}

// AttachOf returns sa's compiled Attach parameters: the configured record's
// when it is bound to sa's Params map, else the front cache's entry for that
// map, else a fresh compile stored in the front cache (ChangeZoneOf's
// contract).
func AttachOf(sa *cards.SA) *AttachParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Attach; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &attachFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileAttach(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

func (p *AttachParams) boundTo(m map[string]string) bool {
	return p.n == len(m) && cards.SameParamMap(p.src, m)
}

// attachFront is AttachOf's direct-mapped front cache (czFront's shape).
var attachFront [1 << 10]atomic.Pointer[AttachParams]

// compileAttach is the one reader of an Attach ability's parameters.
func compileAttach(sa *cards.SA) *AttachParams {
	p := &AttachParams{src: sa.Params, n: len(sa.Params)}
	p.Unattach = isTrue(sa.ParamStr(cards.PKUnattach))
	p.Object, p.ObjectPresent = sa.Param(cards.PKObject)
	switch {
	case p.Object == "" || p.Object == "Self":
		p.objectKind = attachObjectSelf
	case p.Object == "Remembered":
		p.objectKind = attachObjectRemembered
	case strings.HasPrefix(p.Object, "AttachedTo "):
		p.objectKind = attachObjectAttachedTo
	default:
		p.objectKind = attachObjectDefined
	}
	p.EnchantSelf = sa.ParamStr(cards.PKKeyword) == "Enchant" && p.Object == "Self"
	p.PlayerChoices = strings.TrimSpace(sa.Params["PlayerChoices"])
	p.Choices = strings.TrimSpace(sa.ParamStr(cards.PKChoices))
	p.OptionalTrue = isTrue(sa.ParamStr(cards.PKOptional))
	p.RememberAttached = isTrue(sa.Params["RememberAttached"])
	p.ChoicePrompt = strings.TrimSpace(sa.ParamStr(cards.PKChoiceTitle))
	if p.ChoicePrompt == "" {
		p.ChoicePrompt = "Choose card"
	}

	if zones := attachValidTgtsZones(sa.ParamStr(cards.PKValidTgts)); len(zones) > 0 {
		p.ValidTgtsZones = zones[:len(zones):len(zones)]
	}
	if zones, all, ok := ParseZones(sa.ParamStr(cards.PKOrigin)); ok && !all && len(zones) == 1 {
		p.OriginSingle, p.OriginZone = true, zones[0]
	}
	p.Unread = unreadKeys(sa, attachKnownKeys[:])
	return p
}

// attachValidTgtsZones is the zones the comma-split ValidTgts$ alternatives'
// inZone<X> words name (the same word classifier the filter tier's wordInZone
// uses), in first-seen order.
func attachValidTgtsZones(spec string) []state.Zone {
	var zones []state.Zone
	for alt := range strings.SplitSeq(spec, ",") {
		for word := range strings.SplitSeq(alt, ".") {
			z, has := strings.CutPrefix(strings.TrimSpace(word), "inZone")
			if !has {
				continue
			}
			if zn, ok := ParseZoneWord(z); ok && !slices.Contains(zones, zn) {
				zones = append(zones, zn)
			}
		}
	}
	return zones
}

// AttachKnownKeys is a copy of attachKnownKeys, for the census check.
func AttachKnownKeys() []string { return slices.Clone(attachKnownKeys[:]) }
