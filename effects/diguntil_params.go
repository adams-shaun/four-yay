package effects

import (
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// This file is api:DigUntil's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on CopyPermanent's pattern (copypermanent_params.go). compileDigUntil
// is the ONLY reader of a DigUntil ability's own parameters: the resolution
// (diguntil.go) reads the compiled DigUntilParams, including the parsed
// destinations and the withheld-rider list, which are pure functions of the
// text and so are decided here once. Only a non-literal Amount$ is resolved
// at resolution (its SVar body is a live count).
// internal/codeshape's digUntilParamLeaks ratchet holds that: no parameter
// read in diguntil.go, and no read of a DigUntil-only key anywhere else in
// rules/ or effects/.
//
// The machinery DigUntil shares with Dig and the ChangeZone movers -- the
// attacking-entry rider, StaticEffect$, ForgetOtherRemembered$ and the
// Defined$ resolver -- still reads its own keys; that is the next tier.

// DigUntilParams is one DigUntil ability's parameters, compiled once.
type DigUntilParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// Spec is Valid$ (default "Card") through permanentCardSpec.
	Spec string
	// RevDest is RevealedDestination$ (default Library), parsed; RevPos is
	// RevealedLibraryPosition$.
	RevDest state.Zone
	RevPos  string
	// FoundWithRevealed reports an absent FoundDestination$ (the found card
	// rides the revealed pile); FoundDest is the found card's destination
	// (RevDest when FoundWithRevealed).
	FoundWithRevealed bool
	FoundDest         state.Zone
	// FoundPos is FoundLibraryPosition$ when it is "0" or "-1", else "".
	FoundPos string
	// DeclineDest is OptionalNoDestination$, parsed (default RevDest).
	DeclineDest state.Zone
	// The boolean riders: OptionalFoundMove$, NoMoveRevealed$,
	// RevealRandomOrder$, Tapped$, GainControl$, RememberFound$ and
	// RememberRevealed$ True; NoMoveFound$, Shuffle$, ImprintFound$ and
	// ImprintRevealed$ True (any other non-False value is withheld);
	// ShuffleNoneFound is ShuffleCondition$ NoneFound.
	OptionalMove      bool
	NoMoveRevealed    bool
	RevealRandomOrder bool
	Tapped            bool
	GainControl       bool
	RememberFound     bool
	RememberRevealed  bool
	NoMoveFound       bool
	Shuffle           bool
	ShuffleNoneFound  bool
	ImprintFound      bool
	ImprintRevealed   bool
	// AmountRaw is Amount$, trimmed; AmountLit is its literal value when it
	// is 2..5 (zero otherwise -- "" and "1" are the default amount 1, and any
	// other token is an SVar name resolved at resolution).
	AmountRaw string
	AmountLit int32
	// MinTotalCMC is MinTotalCMC$ (Dream Harvest, Improvisation Capstone,
	// Tasha's Hideous Laughter): reveal until the matching cards' cumulative
	// mana value reaches the threshold instead of stopping at Amount$ matches.
	MinTotalCMC ParamText
	// NoneFoundSet reports a NoneFoundDestination$ or
	// NoneFoundLibraryPosition$; NoneFoundDest (default Library) and
	// NoneFoundPos ("0", "-1" or "") are the nothing-found branch's
	// placement.
	NoneFoundSet  bool
	NoneFoundDest state.Zone
	NoneFoundPos  string
	// Defined is the Defined tier's compiled Defined$ reference.
	Defined Ref
	// Withheld are the rider values this build does not model, in the
	// resolution's Note order (Amount$ excluded: it is decided at
	// resolution and noted first).
	Withheld []string

	// Unread are the parameters present on the ability that no DigUntil
	// reader consumes: the resolution Notes them once.
	Unread []string
}

// digUntilKnownKeys is every parameter key a DigUntil resolution consumes or
// deliberately ignores, sorted (changeZoneKnownKeys' contract): rules'
// TestDigUntilKnownKeysMatchTheCensus holds it equal to the parameter
// census's measured api:DigUntil read set plus its ignored and structural
// keys.
var digUntilKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat", "ActivationGameTypes",
	"ActivationLimit", "ActivationPhases", "ActivationZone", "Activator", "AddType",
	"AddTypes", "AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Amount", "Announce", "AnnounceTitle", "Attacking", "Boast",
	"ChangeTypeDesc", "CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone",
	"Choices", "ChooseFromList", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare", "ConditionCompare2",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn", "ConditionPresent", "ConditionPresent2",
	"ConditionSVarCompare", "ConditionZone", "CopyCard", "Cost", "CostDesc", "Defined", "DefinedCards",
	"DefinedTarget", "Description", "DigZone", "Exclude", "Exhaust",
	"ForgetOtherRemembered", "ForgetOtherTargets", "FoundDestination", "FoundLibraryPosition", "GainControl",
	"GameActivationLimit", "Image", "ImprintCards", "ImprintFound", "ImprintPlayed",
	"ImprintRevealed", "InstantSpeed", "IsCurse", "IsPresent", "KW", "Keyword",
	"KeywordLine", "MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "MinTotalCMC", "ModeCost",
	"Monstrosity", "NewController", "NoMoveFound", "NoMoveRevealed", "NoneFoundDestination",
	"NoneFoundLibraryPosition", "NumDmg", "OpponentTurn", "OptionalFoundMove",
	"OptionalNoDestination", "Planeswalker", "PlayCost", "PlayerTurn", "PowerUp",
	"PrecostDesc", "PresentCompare", "PresentDefined", "PresentZone", "RandomNumTargets",
	"ReduceAmount", "ReduceCost", "RememberAnimated", "RememberCostMana", "RememberFound", "RememberObjects",
	"RememberRevealed", "RememberTargets", "ReplaceColor", "ReplaceGraveyard", "ReplaceGraveyardValid",
	"ReplaceMana", "ReplaceOnly", "ReplaceType", "RevealRandomOrder", "RevealedDestination",
	"RevealedLibraryPosition", "SVarCompare", "SelectPrompt", "SetChosenMode", "SetColor",
	"ShowCards", "Shuffle", "ShuffleCondition", "SorcerySpeed", "SpellDescription",
	"StackDescription", "StaticEffect", "StaticEffectCheckSVar", "StaticEffectSVarCompare",
	"SubAbility", "Tapped", "TargetMax", "TargetMin", "TargetType", "TargetUnique",
	"TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls", "TargetsAtRandom",
	"TargetsForEachPlayer", "TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness", "TargetsWithSameCardType",
	"TargetsWithSameController", "TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenScript", "TriggerDescription",
	"Type", "Ultimate", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "Valid", "ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices",
	"ValidCounterType", "ValidDescription", "ValidTgts", "VoteMessage", "WithoutManaCost",
	"XMax", "XMin",
}

// isDigUntilSA reports whether sa resolves as api:DigUntil.
func isDigUntilSA(sa *cards.SA) bool {
	return sa != nil && sa.API == "DigUntil"
}

// DigUntilOf returns sa's compiled DigUntil parameters: the configured
// record's when it is bound to sa's Params map, else the front cache's entry
// for that map, else a fresh compile stored in the front cache
// (ChangeZoneOf's contract).
func DigUntilOf(sa *cards.SA) *DigUntilParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.DigUntil; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &digUntilFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileDigUntil(sa, DefinedOf(sa))
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// digUntilFront is DigUntilOf's direct-mapped front cache (czFront's shape).
var digUntilFront [1 << 10]atomic.Pointer[DigUntilParams]

// compileDigUntil is the one reader of a DigUntil ability's own
// parameters; the Defined$ selector comes from the Defined-reference tier's
// compiled DefinedParams.
func compileDigUntil(sa *cards.SA, dr *DefinedParams) *DigUntilParams {
	p := &DigUntilParams{paramBinding: bindParams(sa)}
	spec := sa.ParamStr(cards.PKValid)
	if spec == "" {
		spec = "Card"
	}
	p.Spec = permanentCardSpec(spec)
	p.RevDest = state.ZLibrary
	if raw := strings.TrimSpace(rawParamText(sa, "RevealedDestination").Text); raw != "" {
		p.RevDest = ParseZone(raw)
	}
	foundDestName := strings.TrimSpace(rawParamText(sa, "FoundDestination").Text)
	p.FoundWithRevealed = foundDestName == ""
	p.FoundDest = p.RevDest
	if !p.FoundWithRevealed {
		p.FoundDest = ParseZone(foundDestName)
	}
	p.RevPos = strings.TrimSpace(rawParamText(sa, "RevealedLibraryPosition").Text)
	p.OptionalMove = isTrue(rawParamText(sa, "OptionalFoundMove").Text)
	p.NoMoveRevealed = isTrue(rawParamText(sa, "NoMoveRevealed").Text)
	p.RevealRandomOrder = isTrue(rawParamText(sa, "RevealRandomOrder").Text)
	p.Tapped = isTrue(sa.ParamStr(cards.PKTapped))
	p.GainControl = isTrue(sa.ParamStr(cards.PKGainControl))
	p.RememberFound = isTrue(rawParamText(sa, "RememberFound").Text)
	p.RememberRevealed = isTrue(rawParamText(sa, "RememberRevealed").Text)
	p.AmountRaw = strings.TrimSpace(sa.ParamStr(cards.PKAmount))
	if p.AmountRaw != "" && p.AmountRaw != "1" {
		if n, err := strconv.Atoi(p.AmountRaw); err == nil && n > 1 && n <= 5 {
			p.AmountLit = int32(n)
		}
	}
	p.MinTotalCMC = rawParamText(sa, "MinTotalCMC")
	// The withheld riders, in the resolution's historical Note order.
	// DigZone$ stays withheld: every corpus value is PlanarDeck, and this
	// build has no planar deck or planar zone.
	if v := digUntilParamValue(rawParamText(sa, "DigZone").Text); v != "" {
		p.Withheld = append(p.Withheld, "DigZone$ "+v)
	}
	p.NoMoveFound = digUntilTrueFlag("NoMoveFound", rawParamText(sa, "NoMoveFound").Text, &p.Withheld)
	p.Shuffle = digUntilTrueFlag("Shuffle", sa.ParamStr(cards.PKShuffle), &p.Withheld)
	// ShuffleCondition$ models exactly NoneFound (Tunnel Vision's
	// FindThePrecious: "otherwise, that player shuffles"); any other value is
	// named loudly rather than guessed at.
	if v := digUntilParamValue(rawParamText(sa, "ShuffleCondition").Text); v != "" {
		if strings.EqualFold(v, "NoneFound") {
			p.ShuffleNoneFound = true
		} else {
			p.Withheld = append(p.Withheld, "ShuffleCondition$ "+v)
		}
	}
	p.ImprintFound = digUntilTrueFlag("ImprintFound", rawParamText(sa, "ImprintFound").Text, &p.Withheld)
	p.ImprintRevealed = digUntilTrueFlag("ImprintRevealed", digUntilImprintRevealedText(sa), &p.Withheld)
	// FoundLibraryPosition$ places a found card whose destination IS the
	// library: "-1" the bottom, "0"/absent the stay-in-place top default. Any
	// other value is named loudly and the card stays on top.
	p.FoundPos = strings.TrimSpace(rawParamText(sa, "FoundLibraryPosition").Text)
	if p.FoundPos != "" && p.FoundPos != "0" && p.FoundPos != "-1" {
		p.Withheld = append(p.Withheld, "FoundLibraryPosition$ "+p.FoundPos)
		p.FoundPos = ""
	}
	// NoneFound* is the nothing-found branch (Tunnel Vision carries both).
	noneFoundDest := strings.TrimSpace(rawParamText(sa, "NoneFoundDestination").Text)
	noneFoundPos := strings.TrimSpace(rawParamText(sa, "NoneFoundLibraryPosition").Text)
	p.NoneFoundSet = noneFoundDest != "" || noneFoundPos != ""
	p.NoneFoundDest = state.ZLibrary
	if noneFoundDest != "" {
		p.NoneFoundDest = ParseZone(noneFoundDest)
	}
	if noneFoundPos != "" {
		if noneFoundPos != "0" && noneFoundPos != "-1" {
			p.Withheld = append(p.Withheld, "NoneFoundLibraryPosition$ "+noneFoundPos)
		} else {
			p.NoneFoundPos = noneFoundPos
		}
	}
	p.Defined = dr.Defined
	p.DeclineDest = p.RevDest
	if raw := strings.TrimSpace(rawParamText(sa, "OptionalNoDestination").Text); raw != "" {
		p.DeclineDest = ParseZone(raw)
	}
	p.Unread = unreadKeys(sa, digUntilKnownKeys[:])
	return p
}

// digUntilParamValue is a withheld rider's trimmed value. Empty means
// absent-or-False.
func digUntilParamValue(raw string) string {
	v := strings.TrimSpace(raw)
	if strings.EqualFold(v, "False") {
		return ""
	}
	return v
}

// digUntilTrueFlag reads a boolean DigUntil rider. Absent or False means the
// rider is off. Any other value is not part of the modelled grammar: it is
// named loudly (appended to withheld) and treated as off rather than guessed
// at.
func digUntilTrueFlag(key, raw string, withheld *[]string) bool {
	v := digUntilParamValue(raw)
	if v == "" {
		return false
	}
	if strings.EqualFold(v, "True") {
		return true
	}
	*withheld = append(*withheld, key+"$ "+v)
	return false
}

// digUntilImprintRevealedText is the one raw read of this parameter shared
// by DigUntil and PeekAndReveal. Both compilers use this source.
func digUntilImprintRevealedText(sa *cards.SA) string {
	return rawParamText(sa, "ImprintRevealed").Text
}

func digUntilSharedImprintRevealed(sa *cards.SA) bool {
	return isTrue(digUntilImprintRevealedText(sa))
}

// DigUntilKnownKeys is a copy of digUntilKnownKeys, for the census check.
func DigUntilKnownKeys() []string { return slices.Clone(digUntilKnownKeys[:]) }
