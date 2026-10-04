package effects

import (
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// This file is the generic TARGETING tier's parameter compiler (W4 step 3's
// cross-API tier, docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md
// section 8: "start with APIs whose params are read on more than one path").
// The targeting parameters -- ValidTgts$, TgtPrompt$, TargetMin$/TargetMax$,
// TgtZone$, TargetType$, TargetUnique$, the TargetsWith*$ set constraints,
// TargetingPlayer$/TargetingPlayerControls$, TargetValidTargeting$ and
// MaxTotalTarget*$ -- are not one API's: every targeting ability carries them whatever its API, and they were
// read on five paths (the offer census, the target ask, the resolution
// recheck, the payment planner and the resolution itself) by ~140 literal
// reads in ~45 files. compileTargets is now their ONLY reader: every path
// reads the compiled TargetParams through TargetsOf, so the offer and the
// recheck can no longer parse a key differently. internal/codeshape's
// targetParamLeaks ratchet holds it: no read of a targeting key anywhere in
// rules/ or effects/ outside this file.
//
// The per-API compilers that need a targeting fact (ChangeZone's ValidTgts$
// and bounds, ChangeZoneAll's targeting flag, Attach's inZone<X> zones) take
// it from the TargetParams NewSAFacts compiled first, so each key still has
// exactly one reader. The API-scoped rider DividedAsYouChoose$ has its own
// single reader here (dividedParam).

// TargetFlag is one compiled boolean fact of an ability's targeting.
type TargetFlag uint32

const (
	// TgtTargeted: ValidTgts$ is non-empty -- the ability targets.
	TgtTargeted TargetFlag = 1 << iota
	// TgtValidPresent: the ValidTgts$ key is present (even empty).
	TgtValidPresent
	// TgtUnique: TargetUnique$ True. TgtUniqueSet: TargetUnique$ non-empty.
	TgtUnique
	TgtUniqueSet
	// TgtSameController: TargetsWithSameController$ True.
	TgtSameController
	// TgtDifferentControllers: TargetsWithDifferentControllers$ True.
	TgtDifferentControllers
	// TgtForEachPlayer: TargetsForEachPlayer$ True.
	TgtForEachPlayer
	// TgtPlayerControls: TargetingPlayerControls$ True;
	// TgtPlayerControlsSet: TargetingPlayerControls$ non-empty.
	TgtPlayerControls
	TgtPlayerControlsSet
	// TgtMinOneEach / TgtMaxOneEach: the bound is the OneEach spelling.
	TgtMinOneEach
	TgtMaxOneEach
	// TgtBoundX: either bound is the bare X.
	TgtBoundX
	// TgtBoundPromisedGift: either bound's text names Count$PromisedGift.
	TgtBoundPromisedGift
	// TgtBoundsDynamic: a bound is present and not a literal integer.
	TgtBoundsDynamic
	// TgtNonTriggeredController: TargetsWithDefinedController$
	// NonTriggeredCardController.
	TgtNonTriggeredController
	// TgtTypeStack: TargetType$ names a stack-object kind.
	TgtTypeStack
	// TgtValidStack: ValidTgts$ names a stack-object kind.
	TgtValidStack
	// TgtValidPlayers: some ValidTgts$ alternative's base can be a player
	// (Player/Any/Opponent/You), SpecTargetsPlayers.
	TgtValidPlayers
	// TgtValidXBound: ValidTgts$ carries a numeric predicate bounded by the
	// paid X (SpecNamesXBound).
	TgtValidXBound
	// TgtDeclares: any of ValidTgts$, TgtPrompt$, TargetType$, TargetMin$,
	// TargetMax$ or TgtZone$ is non-empty -- the ability declares a target
	// in some form (the payment planner's target-declaration probe).
	TgtDeclares
	// TgtAtRandom: TargetsAtRandom$ set and not False -- the targets are
	// chosen at random from the game rng, never by the chooser
	// (RandomTargetsAsk narrows every target ask to the draw).
	TgtAtRandom
	// TgtRandomNum: RandomNumTargets$ True -- with TgtAtRandom, the NUMBER
	// of targets is drawn too, between the bounds (Orcish Catapult).
	TgtRandomNum
)

// TargetParams is one ability's targeting parameters, compiled once.
type TargetParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// Flags are the compiled boolean facts (TargetFlag).
	Flags TargetFlag

	// ValidTgts is ValidTgts$, trimmed ("" when absent).
	ValidTgts string
	// Prompt is TgtPrompt$, trimmed.
	Prompt string
	// Min and Max are TargetMin$/TargetMax$ as written.
	Min, Max ParamText
	// BoundMin and BoundMax are the literal-only bounds (targetBounds'
	// answer): a literal integer bound is honoured, an absent or dynamic one
	// defaults to 1, then min < 0 -> 1, max < 1 -> 1, max < min -> min.
	BoundMin, BoundMax int

	// ZoneText is TgtZone$, trimmed.
	ZoneText string
	// Zones is the target search zones TgtZone$ and TargetType$ name, in
	// declaration order without repeats: TgtZone$'s known tokens, then the
	// stack when TargetType$ names a stack object. Nil when neither names a
	// zone (the caller's API-specific fallback applies). Shared and
	// read-only (len == cap).
	Zones []state.Zone
	// TargetType is TargetType$, trimmed; TypeTokens is its parsed stack-kind
	// token set (state.StackKindTokens: Spell-only when no token names a
	// stack kind). Shared and read-only.
	TargetType string
	TypeTokens []state.StackKindToken

	// TargetingPlayer is TargetingPlayer$, trimmed (who answers the ask).
	TargetingPlayer string
	// DefinedController is TargetsWithDefinedController$, trimmed.
	DefinedController string
	// ControllerProperty is TargetsWithControllerProperty$, trimmed.
	ControllerProperty string
	// ValidTargeting is TargetValidTargeting$, trimmed.
	ValidTargeting string
	// SharedCardType is TargetsWithSharedCardType$, trimmed (a reference
	// object spec); SharedTypes is TargetsWithSharedTypes$ split on ',' into
	// lowercase card-type tokens (nil when absent or empty).
	SharedCardType string
	SharedTypes    []string
	// SetProp and SetPropKind are the target-SET property constraint
	// (TargetsWithSameCardType$, TargetsWithSameCreatureType$,
	// TargetsWithEqualToughness$, TargetsWithDifferentCMC$,
	// TargetsWithDifferentNames$, first True in that order): the mode and the
	// token family ("cardtype", "creaturetype", "toughness", "cmc", "name").
	SetProp     decision.SetPropMode
	SetPropKind string

	// MaxTotalCMC and MaxTotalPower are MaxTotalTargetCMC$ and
	// MaxTotalTargetPower$ as written.
	MaxTotalCMC, MaxTotalPower ParamText
}

// Has reports whether every flag in f is set.
func (p *TargetParams) Has(f TargetFlag) bool { return p.Flags&f == f }

// Targeted reports whether the ability targets (ValidTgts$ non-empty).
func (p *TargetParams) Targeted() bool { return p.Flags&TgtTargeted != 0 }

// noTargets answers TargetsOf for a nil ability or one with no parameters.
var noTargets = TargetParams{BoundMin: 1, BoundMax: 1, TypeTokens: spellOnlyTokens}

// spellOnlyTokens is state.StackKindTokens' default (a TargetType$ naming no
// stack kind), shared.
var spellOnlyTokens = func() []state.StackKindToken {
	t := state.StackKindTokens("")
	return t[:len(t):len(t)]
}()

// TargetsOf returns sa's compiled targeting parameters: the configured
// record's when it is bound to sa's Params map, else the front cache's entry
// for that map, else a fresh compile stored in the front cache (ChangeZoneOf's
// contract). A nil ability, or one with no parameters, answers the shared
// untargeted record. Never nil; the result is read-only.
func TargetsOf(sa *cards.SA) *TargetParams {
	if sa == nil || len(sa.Params) == 0 {
		return &noTargets
	}
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Targets; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &targetsFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileTargets(sa)
	slot.Store(p)
	return p
}

// targetsFront is TargetsOf's direct-mapped front cache (czFront's shape).
var targetsFront [1 << 10]atomic.Pointer[TargetParams]

// compileTargets is the one reader of an ability's targeting parameters.
func compileTargets(sa *cards.SA) *TargetParams {
	p := &TargetParams{paramBinding: bindParams(sa), TypeTokens: spellOnlyTokens}
	vt, vtOK := sa.Param(cards.PKValidTgts)
	p.ValidTgts = strings.TrimSpace(vt)
	if vtOK {
		p.Flags |= TgtValidPresent
	}
	if p.ValidTgts != "" {
		p.Flags |= TgtTargeted
		if SpecTargetsStack(p.ValidTgts) {
			p.Flags |= TgtValidStack
		}
		if SpecTargetsPlayers(p.ValidTgts) {
			p.Flags |= TgtValidPlayers
		}
		if SpecNamesXBound(p.ValidTgts) {
			p.Flags |= TgtValidXBound
		}
	}
	p.Prompt = strings.TrimSpace(sa.ParamStr(cards.PKTgtPrompt))

	tmin, tminOK := sa.Param(cards.PKTargetMin)
	tmax, tmaxOK := sa.Param(cards.PKTargetMax)
	p.Min = ParamText{Text: tmin, Present: tminOK}
	p.Max = ParamText{Text: tmax, Present: tmaxOK}
	p.BoundMin, p.BoundMax = literalTargetBounds(p.Min, p.Max)
	if strings.EqualFold(tmin, "OneEach") {
		p.Flags |= TgtMinOneEach
	}
	if strings.EqualFold(tmax, "OneEach") {
		p.Flags |= TgtMaxOneEach
	}
	if strings.EqualFold(strings.TrimSpace(tmin), "X") || strings.EqualFold(strings.TrimSpace(tmax), "X") {
		p.Flags |= TgtBoundX
	}
	if strings.Contains(tmin, "Count$PromisedGift") || strings.Contains(tmax, "Count$PromisedGift") {
		p.Flags |= TgtBoundPromisedGift
	}
	if (tminOK && !isLiteralInt(tmin)) || (tmaxOK && !isLiteralInt(tmax)) {
		p.Flags |= TgtBoundsDynamic
	}

	p.ZoneText = strings.TrimSpace(sa.ParamStr(cards.PKTgtZone))
	p.TargetType = strings.TrimSpace(sa.ParamStr(cards.PKTargetType))
	if p.TargetType != "" {
		toks := state.StackKindTokens(p.TargetType)
		p.TypeTokens = toks[:len(toks):len(toks)]
		if SpecTargetsStack(p.TargetType) {
			p.Flags |= TgtTypeStack
		}
	}
	p.Zones = targetZoneDecl(p.ZoneText, p.Flags&TgtTypeStack != 0)

	if isTrue(sa.ParamStr(cards.PKTargetUnique)) {
		p.Flags |= TgtUnique
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKTargetUnique)) != "" {
		p.Flags |= TgtUniqueSet
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKTargetsAtRandom)); v != "" && !strings.EqualFold(v, "False") {
		p.Flags |= TgtAtRandom
	}
	if isTrue(sa.ParamStr(cards.PKRandomNumTargets)) {
		p.Flags |= TgtRandomNum
	}
	if strings.EqualFold(sa.ParamStr(cards.PKTargetsWithSameController), "True") {
		p.Flags |= TgtSameController
	}
	if strings.EqualFold(sa.ParamStr(cards.PKTargetsWithDifferentControllers), "True") {
		p.Flags |= TgtDifferentControllers
	}
	if isTrue(sa.ParamStr(cards.PKTargetsForEachPlayer)) {
		p.Flags |= TgtForEachPlayer
	}
	tpc := strings.TrimSpace(sa.ParamStr(cards.PKTargetingPlayerControls))
	if strings.EqualFold(tpc, "True") {
		p.Flags |= TgtPlayerControls
	}
	if tpc != "" {
		p.Flags |= TgtPlayerControlsSet
	}
	p.TargetingPlayer = strings.TrimSpace(sa.ParamStr(cards.PKTargetingPlayer))
	p.DefinedController = strings.TrimSpace(sa.ParamStr(cards.PKTargetsWithDefinedController))
	if p.DefinedController == "NonTriggeredCardController" {
		p.Flags |= TgtNonTriggeredController
	}
	p.ControllerProperty = strings.TrimSpace(sa.ParamStr(cards.PKTargetsWithControllerProperty))
	p.ValidTargeting = strings.TrimSpace(sa.ParamStr(cards.PKTargetValidTargeting))
	p.SharedCardType = strings.TrimSpace(sa.ParamStr(cards.PKTargetsWithSharedCardType))
	p.SharedTypes = sharedTypesList(sa.ParamStr(cards.PKTargetsWithSharedTypes))

	switch {
	case strings.EqualFold(sa.ParamStr(cards.PKTargetsWithSameCardType), "True"):
		p.SetProp, p.SetPropKind = decision.SetPropShared, "cardtype"
	case strings.EqualFold(sa.ParamStr(cards.PKTargetsWithSameCreatureType), "True"):
		p.SetProp, p.SetPropKind = decision.SetPropShared, "creaturetype"
	case strings.EqualFold(sa.ParamStr(cards.PKTargetsWithEqualToughness), "True"):
		p.SetProp, p.SetPropKind = decision.SetPropShared, "toughness"
	case strings.EqualFold(sa.ParamStr(cards.PKTargetsWithDifferentCMC), "True"):
		p.SetProp, p.SetPropKind = decision.SetPropDistinct, "cmc"
	case strings.EqualFold(sa.ParamStr(cards.PKTargetsWithDifferentNames), "True"):
		p.SetProp, p.SetPropKind = decision.SetPropDistinct, "name"
	}

	cmc, cmcOK := sa.Param(cards.PKMaxTotalTargetCMC)
	p.MaxTotalCMC = ParamText{Text: cmc, Present: cmcOK}
	pow, powOK := sa.Param(cards.PKMaxTotalTargetPower)
	p.MaxTotalPower = ParamText{Text: pow, Present: powOK}
	if p.ValidTgts != "" || p.Prompt != "" || p.TargetType != "" || strings.TrimSpace(tmin) != "" ||
		strings.TrimSpace(tmax) != "" || p.ZoneText != "" {
		p.Flags |= TgtDeclares
	}
	return p
}

// DividedAsYouChoose$ is a targeting rider that only some APIs honour
// (DealDamage, PutCounter and PreventDamage divide), so compileTargets --
// which every ability's resolution reaches -- does not read it: the
// parameter census attributes a read to the APIs whose code reaches it, and
// a generic read would mark the key read for every API. It has one reader
// below, called only from those APIs' paths. TargetsAtRandom$ (and its
// RandomNumTargets$ rider) is the opposite case: every target ask honours it
// whatever the API (RandomTargetsAsk, effects/atrandom.go), so it is a
// compiled flag (TgtAtRandom, TgtRandomNum) like the rest of the tier.

// dividedParam is DividedAsYouChoose$ as written; ok reports it non-empty
// (the ability divides its amount among its targets).
func dividedParam(sa *cards.SA) (p ParamText, ok bool) {
	v, present := sa.Param(cards.PKDividedAsYouChoose)
	return ParamText{Text: v, Present: present}, strings.TrimSpace(v) != ""
}

// literalTargetBounds is the literal-only bound pair (TargetParams.BoundMin/
// BoundMax): a present literal integer is honoured, anything else defaults
// to 1, then the clamps min < 0 -> 1, max < 1 -> 1, max < min -> min.
func literalTargetBounds(tmin, tmax ParamText) (int, int) {
	min, max := 1, 1
	if n, ok := literalInt(tmin); ok {
		min = n
	}
	if n, ok := literalInt(tmax); ok {
		max = n
	}
	if min < 0 {
		min = 1
	}
	if max < 1 {
		max = 1
	}
	if max < min {
		max = min
	}
	return min, max
}

func literalInt(p ParamText) (int, bool) {
	if !p.Present {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(p.Text))
	return n, err == nil
}

func isLiteralInt(v string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(v))
	return err == nil
}

// targetZoneDecl is TargetParams.Zones: TgtZone$'s known comma tokens in
// order without repeats, then the stack when TargetType$ names a stack kind.
func targetZoneDecl(tgtZone string, typeStack bool) []state.Zone {
	var zones []state.Zone
	add := func(z state.Zone) {
		for _, have := range zones {
			if have == z {
				return
			}
		}
		zones = append(zones, z)
	}
	for z := range strings.SplitSeq(tgtZone, ",") {
		switch targetZoneDeclCodes.Code(string(strings.TrimSpace(z))) {
		case targetZoneDeclBattlefield:
			add(state.ZBattlefield)
		case targetZoneDeclGraveyard:
			add(state.ZGraveyard)
		case targetZoneDeclHand:
			add(state.ZHand)
		case targetZoneDeclExile:
			add(state.ZExile)
		case targetZoneDeclStack:
			add(state.ZStack)
		}
	}
	if typeStack {
		add(state.ZStack)
	}
	if len(zones) == 0 {
		return nil
	}
	return zones[:len(zones):len(zones)]
}

// sharedTypesList splits TargetsWithSharedTypes$ ("Artifact,Creature,Land")
// into lowercase card-type tokens, nil when absent or empty.
func sharedTypesList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out[:len(out):len(out)]
}

// SpecTargetsStack reports whether a Forge TargetType$ or ValidTgts$ value
// names a target that lives on the stack: a spell (Spell/Instant/Sorcery),
// or an activated/triggered/spell-ability object. The shared state parser
// keeps this census aligned with stack target-kind legality, including
// Forge's Ability alias.
func SpecTargetsStack(spec string) bool {
	for token := range strings.SplitSeq(spec, ",") {
		if _, ok := state.StackKindTokenOf(strings.TrimSpace(token)); ok {
			return true
		}
	}
	return false
}

// SpecTargetsPlayers reports whether a spec can name a player as a target,
// by looking at each alternative's BASE type (the part before any "."),
// never a substring scan: "Any" targets either an object or a player,
// "Player"/"Opponent"/"You" a player, while a predicate like
// "Creature.YouCtrl" IS an object filter (its ".YouCtrl" clause scopes the
// creature's controller, not the target being a player). The old substring
// form matched the "You" inside "YouCtrl" and wrongly offered players as
// Equip targets (Task 14 found it: an Equip onto Creature.YouCtrl offered
// both players as options, which effAttach then had to refuse).
func SpecTargetsPlayers(spec string) bool {
	for alt := range strings.SplitSeq(spec, ",") {
		switch base, _, _ := strings.Cut(strings.TrimSpace(alt), "."); base {
		case "Player", "Any", "Opponent", "You":
			return true
		}
	}
	return false
}

// xBoundRe matches a ValidTgts$ numeric predicate whose right-hand side is
// the paid {X}: the <field><CMP>X family numericPred resolves through
// SpecContext.Resolve (powerGEX, cmcEQX, toughnessLTX, counters_GTX_<KIND>).
// Only these shapes are dynamic in X; a literal bound (cmcGE3) is static and
// stays gated at offer time.
var xBoundRe = regexp.MustCompile(`(?i)(power|toughness|cmc)(LE|GE|EQ|LT|GT)X|counters_(?:LE|GE|EQ|LT|GT)X_`)

// SpecNamesXBound reports whether spec carries an X-bounded numeric
// predicate (xBoundRe).
func SpecNamesXBound(spec string) bool { return xBoundRe.MatchString(spec) }

// NumTextResolved is NumResolved over a compiled parameter (a typed
// parameter struct's ParamText) instead of a key read.
func NumTextResolved(h Host, c *Ctx, p ParamText, def int32) (int32, bool) {
	return numResolvedText(h, c, p, def)
}

// NumTextResolvedStrict is NumResolvedStrict over a compiled parameter.
func NumTextResolvedStrict(h Host, c *Ctx, p ParamText, def int32) (int32, bool) {
	return numResolvedStrictText(h, c, p, def)
}

type targetZoneDeclCode uint16

const (
	targetZoneDeclBattlefield targetZoneDeclCode = iota + 1
	targetZoneDeclGraveyard
	targetZoneDeclHand
	targetZoneDeclExile
	targetZoneDeclStack
)

var targetZoneDeclCodes = state.NewStrCodes(
	state.StrEntry[targetZoneDeclCode]{Key: "Battlefield", Val: targetZoneDeclBattlefield},
	state.StrEntry[targetZoneDeclCode]{Key: "Graveyard", Val: targetZoneDeclGraveyard},
	state.StrEntry[targetZoneDeclCode]{Key: "Hand", Val: targetZoneDeclHand},
	state.StrEntry[targetZoneDeclCode]{Key: "Exile", Val: targetZoneDeclExile},
	state.StrEntry[targetZoneDeclCode]{Key: "Stack", Val: targetZoneDeclStack},
)
