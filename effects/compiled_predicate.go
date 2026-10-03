package effects

import (
	"sort"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// PredicateResult describes what a compiled predicate program can prove about
// one candidate. Maybe deliberately leaves the textual matcher authoritative.
type PredicateResult uint8

const (
	PredicateMaybe PredicateResult = iota
	PredicateNo
	PredicateYes
)

// PredicatePrograms is an immutable collection of programs keyed by their
// original Forge text. It is built before a game begins and never changes
// during matching, cloning, or replay.
type PredicatePrograms struct {
	// texts is the member spec texts, sorted and deduplicated. A retained
	// spec's program rides its compiledSpec; only a spec the process-wide
	// compiled-spec cache did not retain (compiledSpec.id 0) is answered by
	// binary search here, with its program compiled on the spot.
	texts []string
	// known/member are membership bitsets over compiledSpec.id, filled on a
	// spec's first evaluation against this set: known marks an id whose
	// membership was decided, member the ones that are members. Bits are
	// only ever set (member before known), so a concurrent reader that sees
	// known also sees member, and every answer is the texts search's.
	known  [specIDWords]atomic.Uint64
	member [specIDWords]atomic.Uint64
}

// specIDWords covers every compiledSpec.id (1..compiledSpecCacheMax).
const specIDWords = (compiledSpecCacheMax + 64) / 64

type predicateProgram struct {
	alternatives []predicateAlternative
}

type predicateAlternative struct {
	base      predicateBase
	baseMaybe bool
	terms     []predicateTerm
}

type predicateBaseKind uint8

const (
	predicateBaseAny predicateBaseKind = iota
	predicateBaseCard
	predicateBasePermanent
	predicateBasePermanentCard
	predicateBaseSpell
	predicateBaseSpellAbility
	predicateBaseType
)

type predicateBase struct {
	kind    predicateBaseKind
	arg     string
	argID   cards.TypeWordID
	negated bool
}

type predicateTermKind uint8

const (
	predicateTermYouCtrl predicateTermKind = iota
	predicateTermYouDontCtrl
	predicateTermYouOwn
	predicateTermOppOwn
	predicateTermSelf
	predicateTermOther
	predicateTermTapped
	predicateTermAttacking
	predicateTermToken
	predicateTermKicked
	predicateTermSurged
	predicateTermEscaped
	predicateTermWasCastFromGraveyard
	predicateTermColor
	predicateTermType
	predicateTermColorless
	predicateTermAttachedBy
)

type predicateTerm struct {
	kind predicateTermKind
	arg  string
	// argID is arg's interned type-word ordinal for a single-word type term
	// (0 for a multi-word subtype, which splits at match time).
	argID   cards.TypeWordID
	negated bool
	maybe   bool
}

// CompilePredicatePrograms compiles the subset of filter grammar that can be
// evaluated using only the candidate object and SpecContext. The compiler
// preserves unknown text as maybe rather than treating it as a non-match.
func CompilePredicatePrograms(specs []string) *PredicatePrograms {
	texts := append([]string(nil), specs...)
	sort.Strings(texts)
	programs := &PredicatePrograms{}
	for _, spec := range texts {
		if spec == "" {
			continue
		}
		if n := len(programs.texts); n > 0 && programs.texts[n-1] == spec {
			continue
		}
		programs.texts = append(programs.texts, spec)
	}
	return programs
}

// isMember is the text membership search.
func (ps *PredicatePrograms) isMember(spec string) bool {
	i := sort.SearchStrings(ps.texts, spec)
	return i < len(ps.texts) && ps.texts[i] == spec
}

// lookup is isMember with the program: spec's program when it is a member
// (compiled fresh: only the unretained-spec path asks).
func (ps *PredicatePrograms) lookup(spec string) (*predicateProgram, bool) {
	if !ps.isMember(spec) {
		return nil, false
	}
	p := compilePredicateProgram(spec)
	return &p, true
}

// programFor is lookup through the compiled spec's dense id: after the
// first evaluation of a retained spec against this set, membership is two
// bit tests and the program is the one the spec carries.
func (ps *PredicatePrograms) programFor(cs *compiledSpec, spec string) (*predicateProgram, bool) {
	if cs == nil || cs.id == 0 {
		return ps.lookup(spec)
	}
	w, b := cs.id>>6, uint64(1)<<(cs.id&63)
	if ps.known[w].Load()&b != 0 {
		if ps.member[w].Load()&b != 0 {
			return &cs.prog, true
		}
		return nil, false
	}
	ok := ps.isMember(spec)
	if ok {
		ps.member[w].Or(b)
	}
	ps.known[w].Or(b)
	if ok {
		return &cs.prog, true
	}
	return nil, false
}

func compilePredicateProgram(spec string) predicateProgram {
	var p predicateProgram
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, rest, _ := strings.Cut(alt, ".")
		compiledBase, ok := compilePredicateBase(base)
		a := predicateAlternative{base: compiledBase, baseMaybe: !ok}
		// Forge's base-qualified Spell.IsTargeting form is never lowered to
		// sidecar terms: its whole argument is a target spec, so splitting it
		// at '+' would evaluate the target-side conjunction against the
		// candidate. baseMaybe forces a Maybe answer, which leaves the shared
		// compiled/textual matcher (compiledMatch) authoritative for the
		// alternative -- including an argument this grammar cannot answer,
		// which that path fails closed.
		if _, _, shape := spellIsTargetingShape(base, rest); shape {
			a.baseMaybe = true
			p.alternatives = append(p.alternatives, a)
			continue
		}
		for term := range strings.SplitSeq(rest, "+") {
			if term == "" {
				continue
			}
			a.terms = append(a.terms, compilePredicateTerm(term))
		}
		p.alternatives = append(p.alternatives, a)
	}
	return p
}

func compilePredicateBase(base string) (predicateBase, bool) {
	if base == "CARDNAME" {
		return predicateBase{}, false
	}
	negated := false
	if trimmed := strings.TrimPrefix(base, "non"); trimmed != base {
		base, negated = trimmed, true
	}
	switch compilePredicateBasecf11Codes.Code(string(base)) {
	case compilePredicateBasecf11Any:
		kind := map[string]predicateBaseKind{
			"Any": predicateBaseAny, "Card": predicateBaseCard,
			"Permanent": predicateBasePermanent, "PermanentCard": predicateBasePermanentCard,
			"Spell": predicateBaseSpell, "SpellAbility": predicateBaseSpellAbility,
		}[base]
		return predicateBase{kind: kind, negated: negated}, true
	}
	if predicateTypeWords[base] {
		return predicateBase{kind: predicateBaseType, arg: base, argID: cards.InternTypeWord(base), negated: negated}, true
	}
	return predicateBase{}, false
}

func compilePredicateTerm(term string) predicateTerm {
	if rest, ok := strings.CutPrefix(term, "!"); ok {
		if rest == "" {
			return predicateTerm{maybe: true}
		}
		compiled := compilePredicateTerm(rest)
		if !compiled.maybe {
			compiled.negated = !compiled.negated
		}
		return compiled
	}
	var kind predicateTermKind
	switch compilePredicateTermcf12Codes.Code(string(term)) {
	case compilePredicateTermcf12YouCtrl:
		kind = predicateTermYouCtrl
	case compilePredicateTermcf12YouDontCtrl:
		kind = predicateTermYouDontCtrl
	case compilePredicateTermcf12YouOwn:
		kind = predicateTermYouOwn
	case compilePredicateTermcf12OppOwn:
		kind = predicateTermOppOwn
	case compilePredicateTermcf12Self:
		kind = predicateTermSelf
	case compilePredicateTermcf12Other:
		kind = predicateTermOther
	case compilePredicateTermcf12Tapped:
		kind = predicateTermTapped
	case compilePredicateTermcf12Untapped:
		return predicateTerm{kind: predicateTermTapped, negated: true}
	case compilePredicateTermcf12Attacking:
		kind = predicateTermAttacking
	case compilePredicateTermcf12Token:
		kind = predicateTermToken
	case compilePredicateTermcf12Kicked:
		kind = predicateTermKicked
	case compilePredicateTermcf12Surged:
		kind = predicateTermSurged
	case compilePredicateTermcf12Escaped:
		kind = predicateTermEscaped
	case compilePredicateTermcf12WasCastFromGraveyard:
		// The graveyard-origin cast bits (FlagFlashback/FlagHarmonize/
		// FlagEscaped) on a never-cast read — the compiled twin of the
		// filter.go entry and of the Count$wasCastFromGraveyard branch head,
		// all three sharing state.ObjectWasCastFromGraveyard (so a stack copy
		// reads false: a copy was put on the stack, never cast, CR 707.10);
		// an explicit case is required because predicateTermFromWord maps
		// only the color/type/colorless word kinds.
		kind = predicateTermWasCastFromGraveyard
	case compilePredicateTermcf12EquippedBy:
		kind = predicateTermAttachedBy
	default:
		if wordKind, key, ok := nonPredicate(term); ok {
			if kind, ok := predicateTermFromWord(wordKind); ok {
				return predicateTerm{kind: kind, arg: key, argID: typeTermID(kind, key), negated: true}
			}
		}
		if wordKind, key := wordPredicate(term); wordKind != wordUnknown {
			if kind, ok := predicateTermFromWord(wordKind); ok {
				return predicateTerm{kind: kind, arg: key, argID: typeTermID(kind, key)}
			}
		}
		return predicateTerm{maybe: true}
	}
	return predicateTerm{kind: kind}
}

// typeTermID interns a single-word type term's argument; a multi-word
// subtype (Time Lord) keeps 0 and splits at match time.
func typeTermID(kind predicateTermKind, arg string) cards.TypeWordID {
	if kind != predicateTermType || arg == "" || strings.IndexByte(arg, ' ') >= 0 {
		return 0
	}
	return cards.InternTypeWord(arg)
}

func predicateTermFromWord(kind wordKind) (predicateTermKind, bool) {
	switch kind {
	case wordColor:
		return predicateTermColor, true
	case wordType:
		return predicateTermType, true
	case wordColorless:
		return predicateTermColorless, true
	}
	return 0, false
}

// Evaluate returns Maybe when spec was not compiled or when an alternative
// that is not already false contains unsupported grammar.
func (ps *PredicatePrograms) Evaluate(spec string, g *state.Game, o *state.Object, sc SpecContext) PredicateResult {
	return ps.evaluate(spec, g, o, &sc)
}

// evaluate is Evaluate with the (large) context passed by pointer, for the
// hot matcher path.
func (ps *PredicatePrograms) evaluate(spec string, g *state.Game, o *state.Object, sc *SpecContext) PredicateResult {
	if ps == nil || o == nil {
		return PredicateMaybe
	}
	return ps.evaluateCS(compiledSpecFor(spec), spec, g, o, sc)
}

// evaluateCS is evaluate for a caller already holding spec's compiled form.
func (ps *PredicatePrograms) evaluateCS(cs *compiledSpec, spec string, g *state.Game, o *state.Object, sc *SpecContext) PredicateResult {
	if ps == nil || o == nil {
		return PredicateMaybe
	}
	p, ok := ps.programFor(cs, spec)
	if !ok {
		return PredicateMaybe
	}
	maybe := false
	for _, alt := range p.alternatives {
		if alt.baseMaybe {
			maybe = true
			continue
		}
		baseOK := matchesCompiledBase(alt.base, o, sc)
		if !baseOK {
			continue
		}
		all := true
		altMaybe := false
		for _, term := range alt.terms {
			if term.maybe {
				altMaybe = true
				continue
			}
			// A colour-bearing term on a face-down battlefield permanent
			// (CR 708.5: its printed characteristics do not exist) is left to
			// the textual oracle: the compiled colour path and the text colour
			// path both read ColorsOf's printed face today, so a definite
			// answer here would be a new printed-colour claim for a context
			// that has no colours. Maybe keeps that answer textual.
			if (term.kind == predicateTermColor || term.kind == predicateTermColorless) &&
				o.FaceDown && o.Zone == state.ZBattlefield {
				altMaybe = true
				continue
			}
			matched := matchesCompiledTerm(term, g, o, sc)
			if !matched {
				all = false
				break
			}
		}
		if !all {
			continue
		}
		if !altMaybe {
			return PredicateYes
		}
		maybe = true
	}
	if maybe {
		return PredicateMaybe
	}
	return PredicateNo
}

func matchesCompiledBase(base predicateBase, o *state.Object, sc *SpecContext) bool {
	var matched bool
	switch base.kind {
	case predicateBaseAny:
		matched = hasTypeCtxPtrID(o, "Creature", twCreature, sc) || hasTypeCtxPtrID(o, "Planeswalker", twPlaneswalker, sc) || hasTypeCtxPtrID(o, "Battle", twBattle, sc)
	case predicateBaseCard:
		matched = true
	case predicateBasePermanent:
		matched = o.Zone == state.ZBattlefield
	case predicateBasePermanentCard:
		// The textual oracle's twin (effects/filter.go matchesBase): a
		// permanent CARD wherever the object sits, including a permanent
		// spell on the stack (CR 109.2). The compiled sidecar and the text
		// must not disagree.
		matched = o.Face() != nil && o.Face().IsPermanent()
	case predicateBaseSpell, predicateBaseSpellAbility:
		matched = o.Zone == state.ZStack
	case predicateBaseType:
		// hasTypeCtx, not hasType: the layer walk binds its types-so-far
		// list through ExtraTypes, and the published layer-4 table through
		// DerivedTypes. The textual oracle answers both through this same
		// helper, so the compiled base cannot return a definite No for a
		// derived type the text path grants (a manifested Forest under
		// Maskwood Nexus, or an animated manland).
		matched = hasTypeCtxPtrID(o, base.arg, base.argID, sc)
	}
	if base.negated {
		return !matched
	}
	return matched
}

func matchesCompiledTerm(term predicateTerm, g *state.Game, o *state.Object, sc *SpecContext) bool {
	var matched bool
	switch term.kind {
	case predicateTermYouCtrl:
		matched = o.Controller == sc.You
	case predicateTermYouDontCtrl:
		matched = o.Controller != sc.You
	case predicateTermYouOwn:
		matched = o.Owner == sc.You
	case predicateTermOppOwn:
		matched = o.Owner != sc.You
	case predicateTermSelf:
		matched = o.ID == sc.Source
	case predicateTermOther:
		matched = o.ID != sc.Source
	case predicateTermTapped:
		matched = o.Tapped
	case predicateTermAttacking:
		matched = o.IsAttacking
	case predicateTermToken:
		matched = o.IsToken
	case predicateTermKicked:
		matched = o.CastFlags&state.FlagKicked != 0
	case predicateTermSurged:
		matched = o.CastFlags&state.FlagSurged != 0
	case predicateTermEscaped:
		matched = o.CastFlags&state.FlagEscaped != 0
	case predicateTermWasCastFromGraveyard:
		matched = state.ObjectWasCastFromGraveyard(o)
	case predicateTermColor:
		matched = strings.Contains(ColorsOf(o), term.arg)
	case predicateTermType:
		if term.argID != 0 {
			// A single ASCII word: hasTypePredicateCtxPtr's one-word walk is
			// exactly hasTypeCtxPtr of that word.
			matched = hasTypeCtxPtrID(o, term.arg, term.argID, sc)
		} else {
			matched = hasTypePredicateCtxPtr(o, term.arg, sc)
		}
	case predicateTermColorless:
		matched = ColorsOf(o) == ""
	case predicateTermAttachedBy:
		matched = attachedBy(g, o, sc.You, sc.Source)
	}
	if term.negated {
		return !matched
	}
	return matched
}

// Len reports the number of unique non-empty source strings compiled.
func (ps *PredicatePrograms) Len() int {
	if ps == nil {
		return 0
	}
	return len(ps.texts)
}

const (
	compilePredicateBasecf11Any uint16 = 1 // "Any", "Card", "Permanent", "PermanentCard", "Spell", "SpellAbility"
)

var compilePredicateBasecf11Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Any", Val: compilePredicateBasecf11Any},
	state.StrEntry[uint16]{Key: "Card", Val: compilePredicateBasecf11Any},
	state.StrEntry[uint16]{Key: "Permanent", Val: compilePredicateBasecf11Any},
	state.StrEntry[uint16]{Key: "PermanentCard", Val: compilePredicateBasecf11Any},
	state.StrEntry[uint16]{Key: "Spell", Val: compilePredicateBasecf11Any},
	state.StrEntry[uint16]{Key: "SpellAbility", Val: compilePredicateBasecf11Any},
)

const (
	compilePredicateTermcf12YouCtrl              uint16 = 1  // "YouCtrl"
	compilePredicateTermcf12YouDontCtrl          uint16 = 2  // "YouDontCtrl", "OppCtrl"
	compilePredicateTermcf12YouOwn               uint16 = 3  // "YouOwn"
	compilePredicateTermcf12OppOwn               uint16 = 4  // "OppOwn"
	compilePredicateTermcf12Self                 uint16 = 5  // "Self"
	compilePredicateTermcf12Other                uint16 = 6  // "Other", "StrictlyOther"
	compilePredicateTermcf12Tapped               uint16 = 7  // "tapped"
	compilePredicateTermcf12Untapped             uint16 = 8  // "untapped"
	compilePredicateTermcf12Attacking            uint16 = 9  // "attacking"
	compilePredicateTermcf12Token                uint16 = 10 // "token"
	compilePredicateTermcf12Kicked               uint16 = 11 // "kicked"
	compilePredicateTermcf12Surged               uint16 = 12 // "surged"
	compilePredicateTermcf12Escaped              uint16 = 13 // "escaped"
	compilePredicateTermcf12WasCastFromGraveyard uint16 = 14 // "wasCastFromGraveyard"
	compilePredicateTermcf12EquippedBy           uint16 = 15 // "EquippedBy", "EnchantedBy", "AttachedBy"
)

var compilePredicateTermcf12Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "YouCtrl", Val: compilePredicateTermcf12YouCtrl},
	state.StrEntry[uint16]{Key: "YouDontCtrl", Val: compilePredicateTermcf12YouDontCtrl},
	state.StrEntry[uint16]{Key: "OppCtrl", Val: compilePredicateTermcf12YouDontCtrl},
	state.StrEntry[uint16]{Key: "YouOwn", Val: compilePredicateTermcf12YouOwn},
	state.StrEntry[uint16]{Key: "OppOwn", Val: compilePredicateTermcf12OppOwn},
	state.StrEntry[uint16]{Key: "Self", Val: compilePredicateTermcf12Self},
	state.StrEntry[uint16]{Key: "Other", Val: compilePredicateTermcf12Other},
	state.StrEntry[uint16]{Key: "StrictlyOther", Val: compilePredicateTermcf12Other},
	state.StrEntry[uint16]{Key: "tapped", Val: compilePredicateTermcf12Tapped},
	state.StrEntry[uint16]{Key: "untapped", Val: compilePredicateTermcf12Untapped},
	state.StrEntry[uint16]{Key: "attacking", Val: compilePredicateTermcf12Attacking},
	state.StrEntry[uint16]{Key: "token", Val: compilePredicateTermcf12Token},
	state.StrEntry[uint16]{Key: "kicked", Val: compilePredicateTermcf12Kicked},
	state.StrEntry[uint16]{Key: "surged", Val: compilePredicateTermcf12Surged},
	state.StrEntry[uint16]{Key: "escaped", Val: compilePredicateTermcf12Escaped},
	state.StrEntry[uint16]{Key: "wasCastFromGraveyard", Val: compilePredicateTermcf12WasCastFromGraveyard},
	state.StrEntry[uint16]{Key: "EquippedBy", Val: compilePredicateTermcf12EquippedBy},
	state.StrEntry[uint16]{Key: "EnchantedBy", Val: compilePredicateTermcf12EquippedBy},
	state.StrEntry[uint16]{Key: "AttachedBy", Val: compilePredicateTermcf12EquippedBy},
)
