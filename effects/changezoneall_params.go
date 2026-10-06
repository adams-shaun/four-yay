package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file is api:ChangeZoneAll's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on ChangeZone's pattern (changezone_params.go). compileChangeZoneAll is
// the ONLY reader of a ChangeZoneAll ability's parameters: the sweep
// (zone_changeall.go) and the rules-side removal classifier (targetRemoval)
// read the compiled ChangeZoneAllParams. internal/codeshape's
// changeZoneAllParamLeaks ratchet holds that: no parameter read in the
// sweep's own file, and no read of a ChangeZoneAll-only key anywhere else in
// rules/ or effects/.
//
// The move riders ChangeZoneAll shares with ChangeZone and Dig (face-down
// markers, Transformed$, Attacking$, GainControl$, Duration$,
// ForgetOtherRemembered$) are compiled through compileMoveRiders, their one
// reader. The generic machinery the sweep calls with the ability -- the
// Defined$ resolver, the AtEOT$/StaticEffect$ riders -- still reads its own
// keys (the next tier, as for ChangeZone).

// changeZoneAllEveryZone is the Origin$ Any/All sweep's zone list, in scan
// order.
var changeZoneAllEveryZone = []state.Zone{
	state.ZLibrary, state.ZHand, state.ZBattlefield, state.ZGraveyard,
	state.ZExile, state.ZStack, state.ZCommand, state.ZCeased,
}

// ChangeZoneAllParams is one ChangeZoneAll ability's parameters, compiled
// once.
type ChangeZoneAllParams struct {
	// src/n are the Params map the struct was compiled from and its size
	// then (the identity rule cards.SameParamMap applies).
	src map[string]string
	n   int

	// Origin$: its text, whether every token parsed, and the swept zones in
	// scan order (Any/All already expanded to every zone).
	OriginText string
	OriginOK   bool
	Origin     []state.Zone
	// Destination$: the zone ParseZone maps it to (graveyard for an unknown
	// word) and its trimmed, lower-cased text (targetRemoval's classifier).
	Destination      state.Zone
	DestinationLower string

	// ChangeType$ ("Card" when absent) and ChangeNum$; ChangeNumCapped is a
	// ChangeNum$ that names a cap (present, non-empty, not "All").
	ChangeType      string
	TypeLimit       ParamText
	ChangeNum       ParamText
	ChangeNumCapped bool

	// The player scope (changeZoneAllPlayers): UseAllOriginZones$ True, and
	// the ValidTgts$/Defined$ selectors' presence and text.
	UseAllOriginZones bool
	Targeting         bool
	ValidTgtsText     string
	DefinedPresent    bool
	DefinedText       string

	// The library tail: LibraryPosition$ and Shuffle$ True.
	LibraryPosition string
	ShuffleTrue     bool

	// Flags.
	RandomOrder     bool
	RememberLKI     bool
	RememberChanged bool
	Tapped          bool

	// Riders are the move riders shared with ChangeZone and Dig.
	Riders MoveRiders

	// Unread are the parameters present on the ability that no ChangeZoneAll
	// reader consumes: the sweep Notes them once per resolution.
	Unread []string
}

// changeZoneAllKnownKeys is every parameter key a ChangeZoneAll resolution
// consumes or deliberately ignores, sorted (changeZoneKnownKeys' contract):
// rules' TestChangeZoneAllKnownKeysMatchTheCensus holds it equal to the
// parameter census's measured api:ChangeZoneAll read set plus its ignored and
// structural keys.
var changeZoneAllKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment",
	"AITgts", "Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "AtEOT", "Attacking", "Boast",
	"ChangeNum", "ChangeType", "ChangeTypeDesc", "CharacteristicDefining",
	"CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices", "ChooseFromList",
	"ClassBand", "ClearImprinted", "Condition", "ConditionActivationLimit",
	"ConditionCheckSVar", "ConditionCompare", "ConditionCompare2", "ConditionDefined",
	"ConditionDescription", "ConditionFirstCombat", "ConditionNotPresent",
	"ConditionPhases", "ConditionPlayerTurn", "ConditionPresent", "ConditionPresent2",
	"ConditionSVarCompare", "ConditionZone", "CopyCard", "Cost", "CostDesc",
	"Defined", "DefinedCards", "DefinedTarget", "Description", "Destination",
	"Duration", "Exclude", "Exhaust", "ExileFaceDown", "FaceDown",
	"FaceDownPower", "FaceDownSetType", "FaceDownToughness", "Foretold",
	"ForgetOtherRemembered", "ForgetOtherTargets", "GainControl", "GameActivationLimit", "Image",
	"ImprintCards", "ImprintPlayed", "InstantSpeed", "IsCurse",
	"IsPresent", "KW", "Keyword", "KeywordLine", "LibraryPosition",
	"MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor", "ModeCost", "Monstrosity",
	"NewController", "NumDmg", "OpponentTurn",
	"Origin", "Planeswalker", "PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc",
	"PresentCompare", "PresentDefined", "PresentZone", "RandomNumTargets", "RandomOrder", "ReduceAmount",
	"ReduceCost", "RememberAnimated", "RememberChanged", "RememberCostMana", "RememberLKI",
	"RememberObjects", "RememberTargets", "ReplaceColor", "ReplaceGraveyard", "ReplaceGraveyardValid",
	"ReplaceMana", "ReplaceOnly", "ReplaceType", "SVarCompare",
	"SelectPrompt", "SetChosenMode", "SetColor", "ShowCards", "Shuffle",
	"SorcerySpeed", "SpellDescription", "StackDescription",
	"StaticEffect", "StaticEffectCheckSVar", "StaticEffectSVarCompare", "SubAbility",
	"Tapped", "TargetMax", "TargetMin", "TargetType", "TargetUnique",
	"TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls", "TargetsAtRandom",
	"TargetsForEachPlayer", "TargetsWithControllerProperty",
	"TargetsWithDefinedController", "TargetsWithDifferentCMC",
	"TargetsWithDifferentControllers", "TargetsWithDifferentNames",
	"TargetsWithEqualToughness", "TargetsWithSameCardType",
	"TargetsWithSameController", "TargetsWithSameCreatureType",
	"TargetsWithSharedCardType", "TargetsWithSharedTypes", "TgtPrompt", "TgtZone",
	"TokenScript", "Transformed", "TriggerDescription", "Type", "TypeLimit",
	"Ultimate", "Unearth", "UnlessAI", "UnlessCost", "UnlessPayer",
	"UnlessResolveSubs", "UnlessSwitched", "UseAllOriginZones", "ValidCard",
	"ValidCards", "ValidCardsDesc", "ValidChoices", "ValidCounterType",
	"ValidDescription", "ValidTgts", "VoteMessage",
	"WithMayLook", "WithoutManaCost", "XMax", "XMin",
}

// isChangeZoneAllSA reports whether sa resolves as api:ChangeZoneAll.
func isChangeZoneAllSA(sa *cards.SA) bool {
	return sa != nil && (sa.CompiledAPI() == cards.APIChangeZoneAll || sa.API == "ChangeZoneAll")
}

// ChangeZoneAllOf returns sa's compiled ChangeZoneAll parameters: the
// configured record's when it is bound to sa's Params map, else the front
// cache's entry for that map, else a fresh compile stored in the front cache
// (ChangeZoneOf's contract).
func ChangeZoneAllOf(sa *cards.SA) *ChangeZoneAllParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.ChangeZoneAll; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &czaFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileChangeZoneAll(sa, TargetsOf(sa), DefinedOf(sa))
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

func (p *ChangeZoneAllParams) boundTo(m map[string]string) bool {
	return p.n == len(m) && cards.SameParamMap(p.src, m)
}

// czaFront is ChangeZoneAllOf's direct-mapped front cache (czFront's shape).
var czaFront [1 << 10]atomic.Pointer[ChangeZoneAllParams]

// compileChangeZoneAll is the one reader of a ChangeZoneAll ability's
// parameters.
func compileChangeZoneRandomOrder(sa *cards.SA) bool {
	return isTrue(sa.ParamStr(cards.PKRandomOrder))
}

func compileChangeZoneAll(sa *cards.SA, tp *TargetParams, dp *DefinedParams) *ChangeZoneAllParams {
	p := &ChangeZoneAllParams{src: sa.Params, n: len(sa.Params)}

	p.OriginText = sa.ParamStr(cards.PKOrigin)
	zones, all, ok := ParseZones(p.OriginText)
	p.OriginOK = ok
	if all {
		zones = changeZoneAllEveryZone
	}
	p.Origin = zones[:len(zones):len(zones)]
	dest := sa.ParamStr(cards.PKDestination)
	p.Destination = ParseZone(dest)
	p.DestinationLower = strings.ToLower(strings.TrimSpace(dest))

	p.ChangeType = sa.ParamStr(cards.PKChangeType)
	if p.ChangeType == "" {
		p.ChangeType = "Card"
	}
	cn, cnOK := sa.Param(cards.PKChangeNum)
	p.ChangeNum = paramText(cn, cnOK)
	p.ChangeNumCapped = p.ChangeNum.Text != "" && !strings.EqualFold(p.ChangeNum.Text, "All")
	typeLimit, typeLimitOK := sa.Param(cards.PKTypeLimit)
	p.TypeLimit = paramText(typeLimit, typeLimitOK)

	p.UseAllOriginZones = isTrue(sa.ParamStr(cards.PKUseAllOriginZones))
	p.ValidTgtsText, p.Targeting = tp.ValidTgts, tp.Has(TgtValidPresent)
	p.DefinedText, p.DefinedPresent = dp.Defined.Raw, dp.Defined.Present()

	p.LibraryPosition = strings.TrimSpace(sa.ParamStr(cards.PKLibraryPosition))
	p.ShuffleTrue = strings.EqualFold(sa.ParamStr(cards.PKShuffle), "True")

	p.RandomOrder = compileChangeZoneRandomOrder(sa)
	p.RememberLKI = isTrue(sa.ParamStr(cards.PKRememberLKI))
	p.RememberChanged = isTrue(sa.ParamStr(cards.PKRememberChanged))
	p.Tapped = isTrue(sa.ParamStr(cards.PKTapped))

	p.Riders = compileMoveRiders(sa)
	p.Unread = unreadKeys(sa, changeZoneAllKnownKeys[:])
	return p
}

// unreadKeys lists, sorted, the keys present on sa that are not in known (a
// sorted table): an API compiler's unread report (one map walk per compile).
// The walk is unsorted and the result sorted, so map order never reaches it,
// and an ability with nothing unread allocates nothing (a runtime copy -- a
// mana ability's rewritten Produced$ -- recompiles per copy).
func unreadKeys(sa *cards.SA, known []string) []string {
	var out []string
	for k := range sa.Params {
		// This generic activation-cost permission is consumed before resolution,
		// not by any particular API. Ignore it only on an activated ability
		// whose typed activation record actually enables the permission; an
		// unrelated or malformed use must still be reported as unread.
		if k == cards.PKTapCreaturesForMana.String() &&
			ActivationOf(sa).Has(ActTapCreaturesForMana) {
			continue
		}
		if _, ok := slices.BinarySearch(known, k); !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// noteUnreadParams is the loud degrade for parameters an API compiler found
// no reader for: one Note naming every such key; the effect resolves without
// them.
func noteUnreadParams(h Host, c *Ctx, api string, unread []string) {
	if len(unread) == 0 {
		return
	}
	text := api + " ignores unread parameter(s)"
	for i, k := range unread {
		if i > 0 {
			text += ","
		}
		text += " " + k + "$"
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, Text: text})
}

// ChangeZoneAllKnownKeys is a copy of changeZoneAllKnownKeys, for the census
// check.
func ChangeZoneAllKnownKeys() []string { return slices.Clone(changeZoneAllKnownKeys[:]) }
