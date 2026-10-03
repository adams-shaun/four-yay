package effects

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// This file is the generic ACTIVATION/CONDITION tier's parameter compiler
// (W4 step 4, docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md
// section 8: "start with APIs whose params are read on more than one path
// (costs, targets, Defined$)"). The keys it compiles are not one API's: every
// AB$/SP$/DB$ ability carries them whatever its API --
//
//   - the cost: Cost$;
//   - the activation restrictions: Activation$, ActivationZone$,
//     ActivationPhases$ with its riders ActivationFirstCombat$/
//     ActivationAfterBlockers$/PlayerTurn$/OpponentTurn$, ActivationLimit$,
//     GameActivationLimit$, ActivationGameTypes$, Activator$ (the AnyPlayer
//     gate: `Activator$ Player`), SorcerySpeed$ and InstantSpeed$;
//   - the presence/SVar gates: IsPresent$/PresentCompare$/PresentZone$/
//     PresentDefined$ and CheckSVar$/SVarCompare$;
//   - the resolution conditions: Condition$ and the Condition* family
//     conditionMet evaluates (ConditionDefined$, ConditionPresent$,
//     ConditionNotPresent$, ConditionCompare$, ConditionCheckSVar$,
//     ConditionSVarCompare$, ConditionPlayerTurn$, ConditionPhases$,
//     ConditionFirstCombat$, ConditionActivationLimit$);
//   - the unless-pay rider: UnlessCost$, UnlessPayer$, UnlessSwitched$.
//
// They were read on the offer, payment-planner, mana-window and resolution
// paths by ~200 reads in ~60 files, each trimming (or not), case-folding (or
// not) and parsing compare operators on its own. compileActivation is now
// their ONLY reader: every path reads the compiled ActivationParams through
// ActivationOf. internal/codeshape's activationParamLeaks ratchet holds it:
// no read of an activation-tier key on an ability's parameters anywhere in
// rules/ or effects/ outside this file (a trigger's, static's or
// replacement's own keys of the same names are their own tiers' business).
//
// Every key here was already counted read for every API by the parameter
// census (the generic offer/legality machinery and effects.Resolve's
// conditionMet read them for whatever ability runs), so compiling them for
// every ability changes no known-key table -- with two exceptions handled
// honestly rather than masked: PresentZone$ was not honoured by the mana
// abilities' presence gate (it now is: rules' one presence evaluator serves
// both), and ConditionZone$ is honoured only by api:Untap's battlefield
// condition, so it is NOT compiled for every ability -- a generic read would
// make every per-API known-key table demand it. It has its own single reader
// here instead (conditionZoneParam), the targeting tier's
// DividedAsYouChoose$ shape.
//
// Compare operators and literal numbers are compiled once (Compare); a
// compare whose right-hand side names an SVar keeps its text, resolved by
// the evaluator against the live game.

// CmpOp is a compiled two-letter Forge comparison operator.
type CmpOp uint8

const (
	// CmpNone: no operator (absent, too short, or not one of the six).
	CmpNone CmpOp = iota
	CmpEQ
	CmpNE
	CmpLT
	CmpLE
	CmpGT
	CmpGE
)

// cmpOpOf classifies a two-byte operator. fold selects a case-insensitive
// match (the SVar and condition evaluators' ToUpper reading); without it the
// match is exact (the presence compare's reading). It allocates nothing.
func cmpOpOf(a, b byte, fold bool) CmpOp {
	if fold {
		if 'a' <= a && a <= 'z' {
			a -= 'a' - 'A'
		}
		if 'a' <= b && b <= 'z' {
			b -= 'a' - 'A'
		}
	}
	switch uint16(a)<<8 | uint16(b) {
	case 'E'<<8 | 'Q':
		return CmpEQ
	case 'N'<<8 | 'E':
		return CmpNE
	case 'L'<<8 | 'T':
		return CmpLT
	case 'L'<<8 | 'E':
		return CmpLE
	case 'G'<<8 | 'T':
		return CmpGT
	case 'G'<<8 | 'E':
		return CmpGE
	}
	return CmpNone
}

// Apply compares have against n under the operator (false for CmpNone).
func (op CmpOp) Apply(have, n int) bool {
	switch op {
	case CmpEQ:
		return have == n
	case CmpNE:
		return have != n
	case CmpLT:
		return have < n
	case CmpLE:
		return have <= n
	case CmpGT:
		return have > n
	case CmpGE:
		return have >= n
	}
	return false
}

// Compare is one compiled Forge comparison ("GE2", "EQ0", "LTX").
type Compare struct {
	// Text is the comparison as written, trimmed ("" when absent).
	Text string
	// N is the literal right-hand side when Lit.
	N int
	// Op is the operator matched exactly; Fold the same match ignoring case.
	Op, Fold CmpOp
	// Lit: Text is at least three bytes and its right-hand side (Text[2:],
	// as written) is a decimal integer.
	Lit bool
}

// CompareOf compiles a comparison written as text. It allocates nothing.
func CompareOf(raw string) Compare {
	c := Compare{Text: strings.TrimSpace(raw)}
	if len(c.Text) < 3 {
		return c
	}
	c.Op = cmpOpOf(c.Text[0], c.Text[1], false)
	c.Fold = cmpOpOf(c.Text[0], c.Text[1], true)
	if n, err := strconv.Atoi(c.Text[2:]); err == nil {
		c.N, c.Lit = n, true
	}
	return c
}

// Set reports whether a comparison is written.
func (c Compare) Set() bool { return c.Text != "" }

// Rhs is the right-hand side as written ("" when Text is under three bytes).
func (c Compare) Rhs() string {
	if len(c.Text) < 3 {
		return ""
	}
	return c.Text[2:]
}

// Holds is the literal presence compare (rules' comparePresent reading): the
// exact-case operator over a literal right-hand side; anything else is false.
func (c Compare) Holds(have int) bool { return c.Lit && c.Op.Apply(have, c.N) }

// ActFlag is one compiled boolean fact of an ability's activation tier.
type ActFlag uint32

const (
	// ActSorcerySpeed: SorcerySpeed$ is exactly True.
	ActSorcerySpeed ActFlag = 1 << iota
	// ActInstantSpeed: InstantSpeed$ True (trimmed, any case).
	ActInstantSpeed
	// ActPlayerTurnTrue / ActOpponentTurnTrue: PlayerTurn$ / OpponentTurn$
	// True (trimmed, any case); presence is the ParamText's.
	ActPlayerTurnTrue
	ActOpponentTurnTrue
	// ActFirstCombat / ActFirstCombatTrue: ActivationFirstCombat$ present /
	// True (trimmed, any case).
	ActFirstCombat
	ActFirstCombatTrue
	// ActAfterBlockers / ActAfterBlockersTrue: ActivationAfterBlockers$
	// present / True (trimmed, any case).
	ActAfterBlockers
	ActAfterBlockersTrue
	// ActPhaseGate: any of ActivationPhases$, ActivationFirstCombat$,
	// ActivationAfterBlockers$, PlayerTurn$ or OpponentTurn$ is present --
	// the activation window gate has something to check.
	ActPhaseGate
	// ActUnlessSwitched: UnlessSwitched$ True (trimmed, any case).
	ActUnlessSwitched
	// ActConditionOther: a Condition* key outside conditionMet's supported
	// set (ConditionDescription$ aside) is present -- the shape is
	// unsupported and the gate runs the sub unconditionally.
	ActConditionOther
)

// ConditionParams is the resolution-condition half of the tier: the keys
// conditionMet evaluates, trimmed.
type ConditionParams struct {
	// Bare is Condition$ (Kicked, Foretold, Revolt, ...).
	Bare string
	// Defined is ConditionDefined$ (the group); Present ConditionPresent$
	// (the spec; Present.Present is the key's presence); NotPresent
	// ConditionNotPresent$; Compare ConditionCompare$.
	Defined    string
	Present    ParamText
	NotPresent string
	Compare    Compare
	// CheckSVar and SVarCompare are ConditionCheckSVar$/ConditionSVarCompare$.
	CheckSVar   string
	SVarCompare Compare
	// PlayerTurn is ConditionPlayerTurn$; Phases ConditionPhases$ with its
	// parse (PhaseSet, PhasesOK: every element resolved and the set is
	// non-empty); FirstCombat ConditionFirstCombat$; ActivationLimit
	// ConditionActivationLimit$.
	PlayerTurn      string
	Phases          string
	PhaseSet        state.StepSet
	PhasesOK        bool
	FirstCombat     string
	ActivationLimit string
}

// Any reports whether any key conditionMet evaluates is written (a lone
// ConditionSVarCompare$ compares nothing and does not count).
func (c *ConditionParams) Any() bool {
	return c.Defined != "" || c.Present.Text != "" || c.NotPresent != "" || c.Compare.Text != "" ||
		c.CheckSVar != "" || c.Bare != "" || c.PlayerTurn != "" || c.Phases != "" ||
		c.FirstCombat != "" || c.ActivationLimit != ""
}

// ActivationParams is one ability's activation/condition-tier parameters,
// compiled once.
type ActivationParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// Flags are the compiled boolean facts (ActFlag).
	Flags ActFlag

	// Cost is Cost$ as written ("" when absent).
	Cost string

	// Activation is Activation$, trimmed (Hellbent, Threshold, ...).
	Activation string
	// Zone is ActivationZone$; Zones the zones the ability may be activated
	// from, bit z for zone z: the battlefield when the key is absent,
	// nothing for an unknown zone word.
	Zone  ParamText
	Zones uint32
	// Phases is ActivationPhases$ with its parse (PhaseSet, PhasesValid:
	// every element resolved).
	Phases      ParamText
	PhaseSet    state.StepSet
	PhasesValid bool
	// PlayerTurn and OpponentTurn are PlayerTurn$/OpponentTurn$.
	PlayerTurn, OpponentTurn ParamText
	// GameTypes is ActivationGameTypes$.
	GameTypes ParamText
	// Limit and GameLimit are ActivationLimit$/GameActivationLimit$.
	Limit, GameLimit ParamText
	// Activator is Activator$ (who may activate; absent = the controller).
	Activator ParamText

	// IsPresent, PresentCompare, PresentZone and PresentDefined are the
	// presence gate's IsPresent$/PresentCompare$/PresentZone$/PresentDefined$.
	IsPresent      ParamText
	PresentCompare Compare
	PresentZone    ParamText
	PresentDefined string
	// CheckSVar and SVarCompare are the SVar gate's CheckSVar$/SVarCompare$.
	CheckSVar   ParamText
	SVarCompare Compare

	// Cond is the resolution-condition half.
	Cond ConditionParams

	// UnlessCost is UnlessCost$ as written; UnlessPayer UnlessPayer$, trimmed.
	UnlessCost  string
	UnlessPayer string
}

// Has reports whether every flag in f is set.
func (p *ActivationParams) Has(f ActFlag) bool { return p.Flags&f == f }

// Unless reports whether the ability carries an UnlessCost$ (non-blank).
func (p *ActivationParams) Unless() bool { return strings.TrimSpace(p.UnlessCost) != "" }

// ZoneOK reports whether the ability may be activated from zone z.
func (p *ActivationParams) ZoneOK(z state.Zone) bool { return z < 32 && p.Zones&(1<<z) != 0 }

// noActivation answers ActivationOf for a nil ability or one with no
// parameters: no key present, the battlefield default zone.
var noActivation = ActivationParams{Zones: 1 << state.ZBattlefield}

// ActivationOf returns sa's compiled activation-tier parameters: the
// configured record's when it is bound to sa's Params map, else the front
// cache's entry for that map, else a fresh compile stored in the front cache
// (TargetsOf's contract). A nil ability, or one with no parameters, answers
// the shared empty record. Never nil; the result is read-only.
func ActivationOf(sa *cards.SA) *ActivationParams {
	if sa == nil || len(sa.Params) == 0 {
		return &noActivation
	}
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Activation; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &activationFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileActivation(sa)
	slot.Store(p)
	return p
}

// activationFront is ActivationOf's direct-mapped front cache (czFront's
// shape).
var activationFront [1 << 10]atomic.Pointer[ActivationParams]

// isTrueParam reports a present value reading True (trimmed, any case).
func isTrueParam(v string, ok bool) bool { return ok && isTrue(v) }

// compileActivation is the one reader of an ability's activation-tier
// parameters.
func compileActivation(sa *cards.SA) *ActivationParams {
	p := &ActivationParams{paramBinding: bindParams(sa)}
	p.Cost = sa.ParamStr(cards.PKCost)

	p.Activation = strings.TrimSpace(sa.ParamStr(cards.PKActivation))
	az, azOK := sa.Param(cards.PKActivationZone)
	p.Zone = paramText(az, azOK)
	p.Zones = activationZoneMask(p.Zone)
	ph, phOK := sa.Param(cards.PKActivationPhases)
	p.Phases = paramText(ph, phOK)
	if p.Phases.Text != "" {
		set, unknown := state.ParsePhases(p.Phases.Text)
		p.PhaseSet, p.PhasesValid = set, len(unknown) == 0
	}
	pt, ptOK := sa.Param(cards.PKPlayerTurn)
	p.PlayerTurn = paramText(pt, ptOK)
	ot, otOK := sa.Param(cards.PKOpponentTurn)
	p.OpponentTurn = paramText(ot, otOK)
	fc, fcOK := sa.Param(cards.PKActivationFirstCombat)
	ab, abOK := sa.Param(cards.PKActivationAfterBlockers)
	for _, f := range [...]struct {
		on   bool
		flag ActFlag
	}{
		{isTrueParam(pt, ptOK), ActPlayerTurnTrue},
		{isTrueParam(ot, otOK), ActOpponentTurnTrue},
		{fcOK, ActFirstCombat},
		{isTrueParam(fc, fcOK), ActFirstCombatTrue},
		{abOK, ActAfterBlockers},
		{isTrueParam(ab, abOK), ActAfterBlockersTrue},
		{phOK || fcOK || abOK || ptOK || otOK, ActPhaseGate},
		{sa.ParamStr(cards.PKSorcerySpeed) == "True", ActSorcerySpeed},
		{isTrue(sa.ParamStr(cards.PKInstantSpeed)), ActInstantSpeed},
		{isTrue(sa.ParamStr(cards.PKUnlessSwitched)), ActUnlessSwitched},
		{conditionOtherKey(sa), ActConditionOther},
	} {
		if f.on {
			p.Flags |= f.flag
		}
	}
	gt, gtOK := sa.Param(cards.PKActivationGameTypes)
	p.GameTypes = paramText(gt, gtOK)
	lim, limOK := sa.Param(cards.PKActivationLimit)
	p.Limit = paramText(lim, limOK)
	glim, glimOK := sa.Param(cards.PKGameActivationLimit)
	p.GameLimit = paramText(glim, glimOK)
	act, actOK := sa.Param(cards.PKActivator)
	p.Activator = paramText(act, actOK)

	ip, ipOK := sa.Param(cards.PKIsPresent)
	p.IsPresent = paramText(ip, ipOK)
	p.PresentCompare = CompareOf(sa.ParamStr(cards.PKPresentCompare))
	pz, pzOK := sa.Param(cards.PKPresentZone)
	p.PresentZone = paramText(pz, pzOK)
	p.PresentDefined = strings.TrimSpace(sa.ParamStr(cards.PKPresentDefined))
	ck, ckOK := sa.Param(cards.PKCheckSVar)
	p.CheckSVar = paramText(ck, ckOK)
	p.SVarCompare = CompareOf(sa.ParamStr(cards.PKSVarCompare))

	c := &p.Cond
	c.Bare = strings.TrimSpace(sa.ParamStr(cards.PKCondition))
	c.Defined = strings.TrimSpace(sa.ParamStr(cards.PKConditionDefined))
	cp, cpOK := sa.Param(cards.PKConditionPresent)
	c.Present = paramText(cp, cpOK)
	c.NotPresent = strings.TrimSpace(sa.ParamStr(cards.PKConditionNotPresent))
	c.Compare = CompareOf(sa.ParamStr(cards.PKConditionCompare))
	c.CheckSVar = strings.TrimSpace(sa.ParamStr(cards.PKConditionCheckSVar))
	c.SVarCompare = CompareOf(sa.ParamStr(cards.PKConditionSVarCompare))
	c.PlayerTurn = strings.TrimSpace(sa.ParamStr(cards.PKConditionPlayerTurn))
	c.Phases = strings.TrimSpace(sa.ParamStr(cards.PKConditionPhases))
	if c.Phases != "" {
		set, unknown := state.ParsePhases(c.Phases)
		c.PhaseSet, c.PhasesOK = set, len(unknown) == 0 && set != 0
	}
	c.FirstCombat = strings.TrimSpace(sa.ParamStr(cards.PKConditionFirstCombat))
	c.ActivationLimit = strings.TrimSpace(sa.ParamStr(cards.PKConditionActivationLimit))

	p.UnlessCost = sa.ParamStr(cards.PKUnlessCost)
	p.UnlessPayer = strings.TrimSpace(sa.ParamStr(cards.PKUnlessPayer))
	return p
}

// activationZoneMask is the zone mask an ActivationZone$ value admits: the
// battlefield when the key is absent; Battlefield, Graveyard, Hand, Exile or
// Stack when it names one; nothing otherwise (CR 602.1b: an ability whose
// zone this build cannot name is never offered).
func activationZoneMask(az ParamText) uint32 {
	if !az.Present {
		return 1 << state.ZBattlefield
	}
	if v, ok := activationZoneMaskTab1.Get(az.Text); ok {
		return v
	}
	return 0
}

// conditionOtherKey reports whether sa carries a Condition* key conditionMet
// does not evaluate (ConditionZone$, ConditionManaSpent$, ...).
// ConditionDescription$ is display text, never part of the evaluation. The
// answer is order-independent, so ranging the map cannot leak iteration
// order.
func conditionOtherKey(sa *cards.SA) bool {
	for k := range sa.Params {
		if !strings.HasPrefix(k, "Condition") || k == "ConditionDescription" {
			continue
		}
		switch k {
		case "ConditionDefined", "ConditionPresent", "ConditionNotPresent", "ConditionCompare",
			"ConditionCheckSVar", "ConditionSVarCompare", "Condition",
			"ConditionPlayerTurn", "ConditionPhases", "ConditionFirstCombat", "ConditionActivationLimit":
		default:
			return true
		}
	}
	return false
}

// conditionZoneParam is ConditionZone$, trimmed: the one reader of the key.
// It is not part of compileActivation (see the file comment): only api:Untap's
// battlefield condition honours it.
func conditionZoneParam(sa *cards.SA) string {
	return strings.TrimSpace(sa.ParamStr(cards.PKConditionZone))
}

// ActivationTierKeys are the keys compileActivation reads for every ability
// (the rules census check holds them read for every API).
func ActivationTierKeys() []string {
	return []string{
		"Activation", "ActivationAfterBlockers", "ActivationFirstCombat", "ActivationGameTypes",
		"ActivationLimit", "ActivationPhases", "ActivationZone", "Activator", "CheckSVar",
		"Condition", "ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
		"ConditionDefined", "ConditionFirstCombat", "ConditionNotPresent", "ConditionPhases",
		"ConditionPlayerTurn", "ConditionPresent", "ConditionSVarCompare", "Cost",
		"GameActivationLimit", "InstantSpeed", "IsPresent", "OpponentTurn", "PlayerTurn",
		"PresentCompare", "PresentDefined", "PresentZone", "SVarCompare", "SorcerySpeed",
		"UnlessCost", "UnlessPayer", "UnlessSwitched",
	}
}

var activationZoneMaskTab1 = cards.NewStrTable[uint32](
	cards.StrEntry[uint32]{Key: "Battlefield", Val: 1 << state.ZBattlefield},
	cards.StrEntry[uint32]{Key: "Graveyard", Val: 1 << state.ZGraveyard},
	cards.StrEntry[uint32]{Key: "Hand", Val: 1 << state.ZHand},
	cards.StrEntry[uint32]{Key: "Exile", Val: 1 << state.ZExile},
	cards.StrEntry[uint32]{Key: "Stack", Val: 1 << state.ZStack},
)
