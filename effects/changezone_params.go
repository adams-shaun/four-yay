package effects

import (
	"github.com/adams-shaun/gorge/effects/params"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file is api:ChangeZone's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8). compileChangeZone is the ONLY reader of a ChangeZone ability's
// parameters: the resolution (zone_change.go, zone_hand.go, zone_hidden.go,
// zone_search.go, zone_library.go), the rules-side offer and target paths
// (originImpliedTargetZone, targetRemoval, the opening-hand action, the
// ShareLandType$ submit gate) and the planner all read the compiled
// ChangeZoneParams, so a sibling path can no longer read a parameter
// differently from the resolver. internal/codeshape's
// changeZoneParamReadsOutsideCompiler ratchet holds that: no parameter read
// in ChangeZone's own files, and no read of a ChangeZone-only key anywhere
// else in rules/ or effects/.
//
// The move riders ChangeZone shares with Dig and ChangeZoneAll
// (applyFaceDownMarker, GainControl$, Attacking$, Duration$,
// ForgetOtherRemembered$) are compiled here too, as value types: ChangeZone
// embeds them in its compiled struct, and the sibling APIs compile them on the
// stack through the readers at the bottom of this file, so each of those keys
// still has exactly one reader.
//
// Generic machinery the resolver calls with the ability -- the Defined$
// selector resolver, the targeting/cost tier (subAskCandidates, ChooserFor,
// poseTargetsAsk, TargetUnique$), and the AtEOT$/StaticEffect$/ForgetChanged$
// riders every move API shares -- still reads its own keys; those are the
// next tier (the cost/target typed records), not ChangeZone's.

// ParamText is one compiled parameter's text and presence. Parsed parameter
// values are already trimmed (cards' parseParams), so Text is the value as the
// script spells it.
type ParamText = params.ParamText

func paramText(v string, ok bool) ParamText {
	return ParamText{Text: strings.TrimSpace(v), Present: ok}
}

// ZoneMask is a set of state.Zone values, bit z for zone z.
type ZoneMask uint16

// Has reports whether z is in the set.
func (m ZoneMask) Has(z state.Zone) bool { return z < 16 && m&(1<<z) != 0 }

func zoneMaskOf(zones []state.Zone) ZoneMask {
	var m ZoneMask
	for _, z := range zones {
		if z < 16 {
			m |= 1 << z
		}
	}
	return m
}

// handChooserKind is a hidden-hand walk's compiled Chooser$ (handMoveChooserFor).
type handChooserKind uint8

const (
	handChooserOwner handChooserKind = iota // absent, or Owner
	handChooserYou
	handChooserTargeted
	handChooserTriggeredTarget
	handChooserTriggeredPlayer
	handChooserChosenPlayer // ChosenPlayer or Player.Chosen
	handChooserUnknown      // any other spelling: fails closed
)

// ChangeZoneParams is one ChangeZone ability's parameters, compiled once.
type ChangeZoneParams struct {
	// src/n are the Params map the struct was compiled from and its size
	// then (the identity rule cards.SameParamMap applies).
	src map[string]string
	n   int

	// Origin$ alone: present, its text, its parsed zones in script order,
	// Any/All, and whether every token was a known zone.
	OriginPresent bool
	OriginText    string
	OriginOwn     []state.Zone
	OriginOwnAll  bool
	OriginOwnOK   bool
	// Origin is the candidate origin set: Origin$ merged with
	// OriginAlternative$ (Forge's and/or second origin), in script order,
	// with its mask. OriginAll is either side's Any/All.
	Origin     []state.Zone
	OriginMask ZoneMask
	OriginAll  bool
	// OriginAlternative$: present, text and whether every token parsed.
	OriginAltPresent bool
	OriginAltText    string
	OriginAltOK      bool

	// Destination$: the text, the zone ParseZone maps it to (graveyard for an
	// unknown word) and whether the word is a modelled zone.
	DestinationText  string
	Destination      state.Zone
	DestinationKnown bool
	// DestAltSVar$ (with its optional MANDATORY prefix stripped into
	// DestAltMandatory), DestAltSVarCompare$ and DestinationAlternative$.
	DestAltSVarText        string
	DestAltCond            string
	DestAltMandatory       bool
	DestAltSVarCompare     string
	DestinationAltText     string
	DestinationAlt         state.Zone
	DestinationAltKnown    bool
	LibraryPositionText    string
	LibraryPosition        ParamText
	LibraryPositionAltText string
	AlternativeDecider     string

	// Selectors: the targeting half (Defined$, ValidTgts$, TargetMin$,
	// TargetMax$), then the fetch owner and chooser.
	changeZoneTargeting
	DefinedPlayer ParamText
	Chooser       string
	handChooser   handChooserKind
	// ChooseFromDefined$, AttachedTo$, AttachedToPlayer$.
	ChooseFromDefined string
	AttachedTo        string
	AttachedToPlayer  string

	// The filter and the count.
	ChangeType         string // ChangeType$, "Card" when absent
	ChangeNum          ParamText
	WithTotalCMC       ParamText
	MaxRevealed        ParamText
	WithTotalCardTypes string

	// Entry riders.
	WithCountersType   string
	WithCountersAmount ParamText
	LeaveBattlefield   string
	Riders             MoveRiders

	// Optionality and prompts.
	OptionalPresent  bool // Optional$ non-empty
	OptionalYes      bool // Optional$ True or You
	OptionalTrue     bool // Optional$ True
	MandatoryPresent bool // Mandatory$ non-empty
	Mandatory        bool
	ChoiceOptional   bool
	OptionalPrompt   string
	SelectPrompt     string
	SpellDescription string

	// Flags.
	Hidden              bool
	Unimprint           bool
	Imprint             bool
	ImprintLast         bool
	RememberLKI         bool
	RememberChanged     bool
	RememberTargets     bool
	RememberSearched    bool
	ForgetOtherTargets  bool
	Tapped              bool
	AtRandom            bool
	NoLooking           bool
	Reveal              bool
	NoReveal            bool
	DifferentNames      bool
	Exactly             bool
	ShareLandType       bool
	ShuffleTrue         bool // Shuffle$ True
	ShuffleFalse        bool // Shuffle$ False
	NoShuffle           bool
	ShuffleNonMandatory bool
	Reorder             bool

	// Unread are the parameters present on the ability that no ChangeZone
	// reader consumes (changeZoneUnread): the resolver Notes them once per
	// resolution (noteUnread), the loud-degrade contract.
	Unread []string
}

// changeZoneTargeting is the targeting half of a ChangeZone ability's
// parameters: all changeZoneChosenTargets reads, compiled on its own so the
// Effect API's remembered-target prefetch (prefetchRememberedChangeZoneTarget)
// reaches only these keys.
type changeZoneTargeting struct {
	Defined   string
	ValidTgts ParamText
	TargetMin ParamText
	TargetMax ParamText
	// Derived selector facts.
	DefinedImprinted  bool // Defined$ Imprinted
	DefinedRemembered bool // Defined$ Remembered
}

// changeZoneKnownKeys is every parameter key a ChangeZone ability's
// resolution consumes or deliberately ignores, sorted: the keys
// compileChangeZone reads, the keys the shared machinery its resolution runs
// reads (the cast/activation/targeting tier, Resolve's Condition* gate, the
// AtEOT$/StaticEffect$/ForgetChanged$ riders, the Defined$ resolver), the
// presentation/AI-only keys and the structural SubAbility$/Keyword$ tags.
// rules' TestChangeZoneKnownKeysMatchTheCensus holds it equal to the
// parameter census's measured api:ChangeZone read set plus its ignored and
// structural keys, so the compile-time unread detection below and the census
// cannot disagree. It shrinks back into compileChangeZone's own reads as the
// shared tier gets compiled records of its own.
var changeZoneKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "AddType", "AddTypes",
	"AdditionalDesc", "AdditionalDescription", "Affected", "AffectedZone",
	"AlternateCost", "AlternativeCost", "AlternativeDecider", "Announce", "AnnounceTitle",
	"AtEOT", "AtRandom", "AttachedTo", "AttachedToPlayer", "Attacking",
	"BecomeStartingPlayer", "Boast", "ChangeNum", "ChangeType", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceOptional", "ChoiceTitle", "ChoiceZone",
	"Choices", "ChooseFromDefined", "ChooseFromList", "Chooser", "ClassBand",
	"ClearImprinted", "Condition", "ConditionActivationLimit", "ConditionCheckSVar",
	"ConditionCompare", "ConditionDefined", "ConditionDescription",
	"ConditionFirstCombat", "ConditionNotPresent", "ConditionPhases",
	"ConditionPlayerTurn", "ConditionPresent", "ConditionSVarCompare", "CopyCard", "Cost",
	"CostDesc", "Defined", "DefinedCards",
	"DefinedPlayer", "DefinedTarget", "Description", "DestAltSVar", "DestAltSVarCompare",
	"Destination", "DestinationAlternative", "DifferentNames", "Duration",
	"Exactly", "Exclude", "Exhaust", "ExileFaceDown", "FaceDown", "FaceDownPower",
	"FaceDownSetType", "FaceDownToughness", "Foretold", "ForgetChanged",
	"ForgetOtherRemembered", "ForgetOtherTargets", "GainControl", "GameActivationLimit",
	"Hidden", "Image", "Imprint", "ImprintCards", "ImprintLast", "ImprintPlayed",
	"InstantSpeed", "IsCurse", "IsPresent", "KW", "Keyword",
	"KeywordLine", "LeaveBattlefield", "LibraryPosition", "LibraryPositionAlternative",
	"Mandatory", "MaxRevealed", "MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor",
	"ModeCost", "Monstrosity", "NewController", "NoLooking", "NoReveal", "NoShuffle",
	"NumDmg", "OpponentTurn", "Optional",
	"OptionalPrompt", "Origin", "OriginAlternative", "Planeswalker", "PlayCost",
	"PlayerTurn", "PowerUp", "PrecostDesc", "PresentCompare", "PresentDefined",
	"PresentZone", "RandomNumTargets", "ReduceAmount", "ReduceCost", "RememberChanged", "RememberCostMana",
	"RememberLKI", "RememberObjects", "RememberSearched", "RememberTargets", "Reorder",
	"ReplaceColor", "ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana",
	"ReplaceOnly", "ReplaceType", "Reveal", "SVarCompare",
	"SelectPrompt", "SetChosenMode", "SetColor", "ShareLandType", "ShowCards", "Shuffle",
	"ShuffleNonMandatory", "SorcerySpeed", "SpellDescription", "StackDescription",
	"StaticEffect", "StaticEffectCheckSVar", "StaticEffectSVarCompare",
	"SubAbility", "Tapped", "TargetMax", "TargetMin", "TargetType", "TargetUnique",
	"TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls", "TargetsAtRandom",
	"TargetsForEachPlayer", "TargetsWithControllerProperty",
	"TargetsWithDefinedController", "TargetsWithDifferentCMC",
	"TargetsWithDifferentControllers", "TargetsWithDifferentNames",
	"TargetsWithEqualToughness", "TargetsWithSameCardType", "TargetsWithSameController",
	"TargetsWithSameCreatureType", "TargetsWithSharedCardType", "TargetsWithSharedTypes",
	"TgtPrompt", "TgtZone", "TokenScript", "Transformed", "TriggerDescription",
	"Type", "Ultimate", "Unearth",
	"Unimprint", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices",
	"ValidCounterType", "ValidDescription", "ValidTgts", "VoteMessage", "WithCountersAmount", "WithCountersType", "WithMayLook",
	"WithTotalCMC", "WithTotalCardTypes", "WithoutManaCost", "XMax", "XMin",
}

// changeZoneUnread lists, sorted, the keys present on sa that a ChangeZone
// resolution never reads (compile time only: one map walk per ability).
func changeZoneUnread(sa *cards.SA) []string {
	var out []string
	for _, k := range sa.ParamNames() {
		if _, known := slices.BinarySearch(changeZoneKnownKeys[:], k); !known {
			out = append(out, k)
		}
	}
	return out
}

// noteUnread is the loud degrade for a parameter the compiler found no reader
// for: one Note naming every such key, and the move resolves without them.
func (p *ChangeZoneParams) noteUnread(h Host, c *Ctx) {
	if len(p.Unread) == 0 {
		return
	}
	text := "ChangeZone ignores unread parameter(s)"
	for i, k := range p.Unread {
		if i > 0 {
			text += ","
		}
		text += " " + k + "$"
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, Text: text})
}

// FaceDownRiders are the face-down entry markers applyFaceDownMarker stamps:
// ChangeZone compiles them once, Dig and ChangeZoneAll on the stack per call.
type FaceDownRiders struct {
	FaceDown          bool
	ExileFaceDown     bool
	WithMayLook       bool
	Foretold          bool
	Unearth           bool
	FaceDownSetType   string
	FaceDownPower     ParamText
	FaceDownToughness ParamText
}

// MoveRiders are the entry/exit riders ChangeZone shares with the other move
// APIs (Dig, ChangeZoneAll): compiled once into ChangeZoneParams; a sibling
// API reads each through its single-key reader below.
type MoveRiders struct {
	FaceDownRiders
	Transformed bool
	// Attacking$ (classifyAttackingEntry's input).
	Attacking string
	// GainControl$.
	GainControl ParamText
	// Duration$ (exileHostGone / recordExileReturn).
	Duration ParamText
	// ForgetOtherRemembered$ True.
	ForgetOtherRemembered bool
}

func isTrue(v string) bool { return strings.EqualFold(strings.TrimSpace(v), "True") }

// isChangeZoneSA reports whether sa resolves as api:ChangeZone.
func isChangeZoneSA(sa *cards.SA) bool {
	return sa != nil && (sa.CompiledAPI() == cards.APIChangeZone || sa.API == "ChangeZone")
}

// ChangeZoneOf returns sa's compiled ChangeZone parameters: the configured
// record's when it is bound to sa's Params map, else the front cache's entry
// for that map, else a fresh compile stored in the front cache. The cache
// covers abilities no configured face reaches -- chiefly cards.ResolveSVar
// copies (delayed-trigger and Charm bodies), which share their template's
// Params map -- without writing anything reachable from the SA (a slot write
// on a template would make two otherwise identical stack objects compare
// unequal under reflect.DeepEqual). A hit is two pointer loads; it never
// changes an answer, only how often the compile runs.
func ChangeZoneOf(sa *cards.SA) *ChangeZoneParams {
	if f := LoadSAFacts(sa); f != nil {
		if cz := f.ChangeZone; cz != nil && cz.boundTo(sa.Params) {
			return cz
		}
	}
	slot := &czFront[paramMapSlot(sa.Params)]
	if cz := slot.Load(); cz != nil && cz.boundTo(sa.Params) {
		return cz
	}
	cz := compileChangeZone(sa, TargetsOf(sa), DefinedOf(sa))
	if sa.Params != nil {
		slot.Store(cz)
	}
	return cz
}

func (p *ChangeZoneParams) boundTo(m map[string]string) bool {
	return p.n == len(m) && cards.SameParamMap(p.src, m)
}

// czFront is ChangeZoneOf's direct-mapped front cache, keyed by the Params
// map's identity (a collision only costs a recompile).
var czFront [1 << 10]atomic.Pointer[ChangeZoneParams]

func paramMapSlot(m map[string]string) uint {
	h := uint64(cards.ParamMapIdentity(m)>>3) ^ uint64(len(m))*0x9e3779b97f4a7c15
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	return uint(h) & (1<<10 - 1)
}

// compileChangeZone is the one reader of a ChangeZone ability's parameters.
func compileChangeZone(sa *cards.SA, tp *TargetParams, dr *DefinedParams) *ChangeZoneParams {
	p := &ChangeZoneParams{src: sa.Params, n: len(sa.Params)}

	// Origin$ and OriginAlternative$.
	if from, ok := sa.Param(cards.PKOrigin); ok {
		p.OriginPresent = true
		p.OriginText = from
		p.OriginOwn, p.OriginOwnAll, p.OriginOwnOK = ParseZones(from)
		p.Origin = append([]state.Zone(nil), p.OriginOwn...)
		p.OriginAll = p.OriginOwnAll
		if alt, hasAlt := sa.Param(cards.PKOriginAlternative); hasAlt {
			p.OriginAltPresent = true
			p.OriginAltText = alt
			altZones, altAll, altValid := ParseZones(alt)
			for _, z := range altZones {
				if !zoneIn(p.Origin, z) {
					p.Origin = append(p.Origin, z)
				}
			}
			p.OriginAll = p.OriginAll || altAll
			p.OriginAltOK = altValid
		}
		p.Origin = p.Origin[:len(p.Origin):len(p.Origin)]
		p.OriginMask = zoneMaskOf(p.Origin)
	}

	// Destination$ and its alternatives.
	p.DestinationText = strings.TrimSpace(sa.ParamStr(cards.PKDestination))
	p.Destination, p.DestinationKnown = parseZone(p.DestinationText)
	if !p.DestinationKnown {
		p.Destination = state.ZGraveyard
	}
	p.DestAltSVarText = strings.TrimSpace(sa.ParamStr(cards.PKDestAltSVar))
	p.DestAltCond = p.DestAltSVarText
	if rest, ok := strings.CutPrefix(p.DestAltCond, "MANDATORY "); ok {
		p.DestAltMandatory = true
		p.DestAltCond = strings.TrimSpace(rest)
	}
	p.DestAltSVarCompare = strings.TrimSpace(sa.ParamStr(cards.PKDestAltSVarCompare))
	p.DestinationAltText = strings.TrimSpace(sa.ParamStr(cards.PKDestinationAlternative))
	p.DestinationAlt, p.DestinationAltKnown = parseZone(p.DestinationAltText)
	lp, lpOK := sa.Param(cards.PKLibraryPosition)
	p.LibraryPosition = paramText(lp, lpOK)
	p.LibraryPositionText = p.LibraryPosition.Text
	p.LibraryPositionAltText = strings.TrimSpace(sa.ParamStr(cards.PKLibraryPositionAlternative))
	p.AlternativeDecider = strings.TrimSpace(sa.ParamStr(cards.PKAlternativeDecider))

	// Selectors.
	p.changeZoneTargeting = compileChangeZoneTargeting(tp, dr)
	p.DefinedPlayer = definedPlayerRef(sa).Param()
	p.Chooser = strings.TrimSpace(sa.ParamStr(cards.PKChooser))
	switch compileChangeZoneCodes.Code(string(p.Chooser)) {
	case compileChangeZoneOwner:
		p.handChooser = handChooserOwner
	case compileChangeZoneYou:
		p.handChooser = handChooserYou
	case compileChangeZoneTargeted:
		p.handChooser = handChooserTargeted
	case compileChangeZoneTriggeredTarget:
		p.handChooser = handChooserTriggeredTarget
	case compileChangeZoneTriggeredPlayer:
		p.handChooser = handChooserTriggeredPlayer
	case compileChangeZoneChosenPlayer:
		p.handChooser = handChooserChosenPlayer
	default:
		p.handChooser = handChooserUnknown
	}
	p.ChooseFromDefined = strings.TrimSpace(sa.ParamStr(cards.PKChooseFromDefined))
	p.AttachedTo = strings.TrimSpace(sa.ParamStr(cards.PKAttachedTo))
	p.AttachedToPlayer = strings.TrimSpace(sa.ParamStr(cards.PKAttachedToPlayer))

	// The filter and the count.
	p.ChangeType = sa.ParamStr(cards.PKChangeType)
	if p.ChangeType == "" {
		p.ChangeType = "Card"
	}
	cn, cnOK := sa.Param(cards.PKChangeNum)
	p.ChangeNum = paramText(cn, cnOK)
	tc, tcOK := sa.Param(cards.PKWithTotalCMC)
	p.WithTotalCMC = paramText(tc, tcOK)
	mr, mrOK := sa.Param(cards.PKMaxRevealed)
	p.MaxRevealed = paramText(mr, mrOK)
	p.WithTotalCardTypes = strings.TrimSpace(sa.ParamStr(cards.PKWithTotalCardTypes))

	// Entry riders.
	p.WithCountersType = sa.ParamStr(cards.PKWithCountersType)
	wa, waOK := sa.Param(cards.PKWithCountersAmount)
	p.WithCountersAmount = paramText(wa, waOK)
	p.LeaveBattlefield = sa.ParamStr(cards.PKLeaveBattlefield)
	p.Riders = compileMoveRiders(sa)

	// Optionality and prompts.
	opt := strings.TrimSpace(sa.ParamStr(cards.PKOptional))
	p.OptionalPresent = opt != ""
	p.OptionalTrue = strings.EqualFold(opt, "True")
	p.OptionalYes = p.OptionalTrue || strings.EqualFold(opt, "You")
	mand := strings.TrimSpace(sa.ParamStr(cards.PKMandatory))
	p.MandatoryPresent = mand != ""
	p.Mandatory = strings.EqualFold(mand, "True")
	p.ChoiceOptional = isTrue(sa.ParamStr(cards.PKChoiceOptional))
	p.OptionalPrompt = strings.TrimSpace(sa.ParamStr(cards.PKOptionalPrompt))
	p.SelectPrompt = strings.TrimSpace(sa.ParamStr(cards.PKSelectPrompt))
	p.SpellDescription = sa.ParamStr(cards.PKSpellDescription)

	// Flags.
	p.Hidden = isTrue(sa.ParamStr(cards.PKHidden))
	p.Unimprint = isTrue(sa.ParamStr(cards.PKUnimprint))
	p.Imprint = isTrue(sa.ParamStr(cards.PKImprint))
	p.ImprintLast = isTrue(sa.ParamStr(cards.PKImprintLast))
	p.RememberLKI = isTrue(sa.ParamStr(cards.PKRememberLKI))
	p.RememberChanged = isTrue(sa.ParamStr(cards.PKRememberChanged))
	p.RememberTargets = isTrue(sa.ParamStr(cards.PKRememberTargets))
	p.RememberSearched = isTrue(sa.ParamStr(cards.PKRememberSearched))
	p.ForgetOtherTargets = isTrue(sa.ParamStr(cards.PKForgetOtherTargets))
	p.Tapped = isTrue(sa.ParamStr(cards.PKTapped))
	p.AtRandom = isTrue(sa.ParamStr(cards.PKAtRandom))
	p.NoLooking = isTrue(sa.ParamStr(cards.PKNoLooking))
	p.Reveal = isTrue(sa.ParamStr(cards.PKReveal))
	p.NoReveal = isTrue(sa.ParamStr(cards.PKNoReveal))
	p.DifferentNames = isTrue(sa.ParamStr(cards.PKDifferentNames))
	p.Exactly = isTrue(sa.ParamStr(cards.PKExactly))
	p.ShareLandType = isTrue(sa.ParamStr(cards.PKShareLandType))
	shuffle := strings.TrimSpace(sa.ParamStr(cards.PKShuffle))
	p.ShuffleTrue = strings.EqualFold(shuffle, "True")
	p.ShuffleFalse = strings.EqualFold(shuffle, "False")
	p.NoShuffle = isTrue(sa.ParamStr(cards.PKNoShuffle))
	p.ShuffleNonMandatory = isTrue(sa.ParamStr(cards.PKShuffleNonMandatory))
	p.Reorder = isTrue(sa.ParamStr(cards.PKReorder))
	p.Unread = changeZoneUnread(sa)
	return p
}

// compileMoveRiders is the one reader of the move riders ChangeZone shares
// with Dig and ChangeZoneAll. It returns a value: a sibling API compiles it on
// the stack per call (zero allocation), ChangeZone once per ability.
func compileMoveRiders(sa *cards.SA) MoveRiders {
	var r MoveRiders
	r.FaceDownRiders = compileFaceDownRiders(sa)
	r.Transformed = isTrue(sa.ParamStr(cards.PKTransformed))
	r.Attacking = attackingParam(sa)
	r.GainControl = gainControlParam(sa)
	r.Duration = durationParam(sa)
	r.ForgetOtherRemembered = forgetOtherRememberedParam(sa)
	return r
}

// compileFaceDownRiders is the one reader of the face-down entry markers.
func compileFaceDownRiders(sa *cards.SA) FaceDownRiders {
	var r FaceDownRiders
	r.FaceDown = isTrue(sa.ParamStr(cards.PKFaceDown))
	r.ExileFaceDown = isTrue(sa.ParamStr(cards.PKExileFaceDown))
	r.WithMayLook = isTrue(sa.ParamStr(cards.PKWithMayLook))
	r.Foretold = isTrue(sa.ParamStr(cards.PKForetold))
	r.Unearth = isTrue(sa.ParamStr(cards.PKUnearth))
	r.FaceDownSetType = strings.TrimSpace(sa.ParamStr(cards.PKFaceDownSetType))
	fp, fpOK := sa.Param(cards.PKFaceDownPower)
	r.FaceDownPower = paramText(fp, fpOK)
	ft, ftOK := sa.Param(cards.PKFaceDownToughness)
	r.FaceDownToughness = paramText(ft, ftOK)
	return r
}

// compileChangeZoneTargeting is the one reader of ChangeZone's targeting half.
// The targeting keys come from the generic tier's compiled TargetParams.
func compileChangeZoneTargeting(tp *TargetParams, dp *DefinedParams) changeZoneTargeting {
	var t changeZoneTargeting
	t.Defined = dp.Defined.Text
	t.DefinedImprinted = dp.Defined.Is(RefImprinted)
	t.DefinedRemembered = dp.Defined.Is(RefRemembered)
	t.ValidTgts = ParamText{Text: tp.ValidTgts, Present: tp.Has(TgtValidPresent)}
	t.TargetMin = paramText(tp.Min.Text, tp.Min.Present)
	t.TargetMax = paramText(tp.Max.Text, tp.Max.Present)
	return t
}

// The single-key readers below are the riders' one read each, shared by
// compileMoveRiders and the sibling APIs' sa-form wrappers (classifyAttackingEntry,
// applyGainControl, exileHostGone, recordExileReturn, forgetOtherRemembered),
// which have no compiled record yet.

// attackingParam is Attacking$ (classifyAttackingEntry's input).
func attackingParam(sa *cards.SA) string { return strings.TrimSpace(sa.ParamStr(cards.PKAttacking)) }

// gainControlParam is GainControl$ as written (gainControlOf trims itself).
func gainControlParam(sa *cards.SA) ParamText {
	gc, ok := sa.Param(cards.PKGainControl)
	return ParamText{Text: gc, Present: ok}
}

// durationParam is Duration$ (exileHostGone / recordExileReturn).
func durationParam(sa *cards.SA) ParamText {
	du, ok := sa.Param(cards.PKDuration)
	return paramText(du, ok)
}

// forgetOtherRememberedParam is ForgetOtherRemembered$ True.
func forgetOtherRememberedParam(sa *cards.SA) bool {
	return isTrue(sa.ParamStr(cards.PKForgetOtherRemembered))
}

// OriginExactly reports whether Origin$ alone (OriginAlternative$ aside)
// names exactly zone z: present, every token known, no Any/All, one zone.
func (p *ChangeZoneParams) OriginExactly(z state.Zone) bool {
	return p.OriginPresent && p.OriginOwnOK && !p.OriginOwnAll && len(p.OriginOwn) == 1 && p.OriginOwn[0] == z
}

// DestinationIs reports whether Destination$ names the modelled zone z.
func (p *ChangeZoneParams) DestinationIs(z state.Zone) bool {
	return p.DestinationKnown && p.Destination == z
}

// fetchSelectors are the whose-zones selectors of a fetch (searchPlayers,
// hiddenPickPlayers): DefinedPlayer$, Defined$ and ValidTgts$.
type fetchSelectors struct {
	DefinedPlayer ParamText
	Defined       ParamText
	ValidTgts     ParamText
}

// fetch is p's compiled fetch selectors.
func (p *ChangeZoneParams) fetch() fetchSelectors {
	return fetchSelectors{DefinedPlayer: p.DefinedPlayer,
		Defined: ParamText{Text: p.Defined, Present: p.Defined != ""}, ValidTgts: p.ValidTgts}
}

// fetchSelectorsParam is the fetch selectors of an API with no compiled
// record yet (Manifest's searchPlayers call).
func fetchSelectorsParam(sa *cards.SA) fetchSelectors {
	tp := TargetsOf(sa)
	return fetchSelectors{DefinedPlayer: definedPlayerRef(sa).Param(), Defined: DefinedRefOf(sa).Param(),
		ValidTgts: ParamText{Text: tp.ValidTgts, Present: tp.Has(TgtValidPresent)}}
}

// ChangeZoneKnownKeys is a copy of changeZoneKnownKeys, for the census check.
func ChangeZoneKnownKeys() []string { return slices.Clone(changeZoneKnownKeys[:]) }

type compileChangeZoneCode uint16

const (
	compileChangeZoneOwner compileChangeZoneCode = iota + 1
	compileChangeZoneYou
	compileChangeZoneTargeted
	compileChangeZoneTriggeredTarget
	compileChangeZoneTriggeredPlayer
	compileChangeZoneChosenPlayer
)

var compileChangeZoneCodes = state.NewStrCodes(
	state.StrEntry[compileChangeZoneCode]{Key: "", Val: compileChangeZoneOwner},
	state.StrEntry[compileChangeZoneCode]{Key: "Owner", Val: compileChangeZoneOwner},
	state.StrEntry[compileChangeZoneCode]{Key: "You", Val: compileChangeZoneYou},
	state.StrEntry[compileChangeZoneCode]{Key: "Targeted", Val: compileChangeZoneTargeted},
	state.StrEntry[compileChangeZoneCode]{Key: "TriggeredTarget", Val: compileChangeZoneTriggeredTarget},
	state.StrEntry[compileChangeZoneCode]{Key: "TriggeredPlayer", Val: compileChangeZoneTriggeredPlayer},
	state.StrEntry[compileChangeZoneCode]{Key: "ChosenPlayer", Val: compileChangeZoneChosenPlayer},
	state.StrEntry[compileChangeZoneCode]{Key: "Player.Chosen", Val: compileChangeZoneChosenPlayer},
)
