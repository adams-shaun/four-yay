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
	// parent is a shared lower layer (an engine's token-script programs):
	// a spec is a member when it is in texts or in parent. Membership is all
	// a set decides -- a member's program is a pure function of its text --
	// so the layered set answers exactly as the union of both texts would.
	parent *PredicatePrograms
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
	predicateTermBargained
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
	return CompilePredicateProgramsOver(nil, specs)
}

// CompilePredicatePrograms over parent: the set of specs plus every member
// of parent (nil: none). parent is shared, never modified.
func CompilePredicateProgramsOver(parent *PredicatePrograms, specs []string) *PredicatePrograms {
	texts := append([]string(nil), specs...)
	sort.Strings(texts)
	programs := &PredicatePrograms{parent: parent}
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
	if i < len(ps.texts) && ps.texts[i] == spec {
		return true
	}
	return ps.parent != nil && ps.parent.isMember(spec)
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
	switch compilePredicateBaseCodes.Code(string(base)) {
	case compilePredicateBaseAny:
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
	switch compilePredicateTermCodes.Code(string(term)) {
	case compilePredicateTermYouCtrl:
		kind = predicateTermYouCtrl
	case compilePredicateTermYouDontCtrl:
		kind = predicateTermYouDontCtrl
	case compilePredicateTermYouOwn:
		kind = predicateTermYouOwn
	case compilePredicateTermOppOwn:
		kind = predicateTermOppOwn
	case compilePredicateTermSelf:
		kind = predicateTermSelf
	case compilePredicateTermOther:
		kind = predicateTermOther
	case compilePredicateTermTapped:
		kind = predicateTermTapped
	case compilePredicateTermUntapped:
		return predicateTerm{kind: predicateTermTapped, negated: true}
	case compilePredicateTermAttacking:
		kind = predicateTermAttacking
	case compilePredicateTermToken:
		kind = predicateTermToken
	case compilePredicateTermKicked:
		kind = predicateTermKicked
	case compilePredicateTermBargained:
		kind = predicateTermBargained
	case compilePredicateTermSurged:
		kind = predicateTermSurged
	case compilePredicateTermEscaped:
		kind = predicateTermEscaped
	case compilePredicateTermWasCastFromGraveyard:
		// The graveyard-origin cast bits (FlagFlashback/FlagHarmonize/
		// FlagEscaped) on a never-cast read — the compiled twin of the
		// filter.go entry and of the Count$wasCastFromGraveyard branch head,
		// all three sharing state.ObjectWasCastFromGraveyard (so a stack copy
		// reads false: a copy was put on the stack, never cast, CR 707.10);
		// an explicit case is required because predicateTermFromWord maps
		// only the color/type/colorless word kinds.
		kind = predicateTermWasCastFromGraveyard
	case compilePredicateTermEquippedBy:
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
	case predicateBaseSpell:
		// The textual oracle's twin (the cbSpell arm of filter_compiled.go's
		// matchBase): the derived AsStack override makes the card a
		// cast is announcing (or a may-play permission is offering) read as the
		// spell it is while it is still in its origin zone, so a printed
		// ValidAfterStack$ Spell.<...> grant is not refused by the compiled
		// sidecar with a definite No.
		matched = o.Zone == state.ZStack || sc.AsStack
	case predicateBaseSpellAbility:
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
	case predicateTermBargained:
		matched = o.CastFlags&state.FlagBargained != 0
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

type compilePredicateBaseCode uint16

const (
	compilePredicateBaseAny compilePredicateBaseCode = iota + 1
)

var compilePredicateBaseCodes = state.NewStrCodes(
	state.StrEntry[compilePredicateBaseCode]{Key: "Any", Val: compilePredicateBaseAny},
	state.StrEntry[compilePredicateBaseCode]{Key: "Card", Val: compilePredicateBaseAny},
	state.StrEntry[compilePredicateBaseCode]{Key: "Permanent", Val: compilePredicateBaseAny},
	state.StrEntry[compilePredicateBaseCode]{Key: "PermanentCard", Val: compilePredicateBaseAny},
	state.StrEntry[compilePredicateBaseCode]{Key: "Spell", Val: compilePredicateBaseAny},
	state.StrEntry[compilePredicateBaseCode]{Key: "SpellAbility", Val: compilePredicateBaseAny},
)

type compilePredicateTermCode uint16

const (
	compilePredicateTermYouCtrl compilePredicateTermCode = iota + 1
	compilePredicateTermYouDontCtrl
	compilePredicateTermYouOwn
	compilePredicateTermOppOwn
	compilePredicateTermSelf
	compilePredicateTermOther
	compilePredicateTermTapped
	compilePredicateTermUntapped
	compilePredicateTermAttacking
	compilePredicateTermToken
	compilePredicateTermKicked
	compilePredicateTermBargained
	compilePredicateTermSurged
	compilePredicateTermEscaped
	compilePredicateTermWasCastFromGraveyard
	compilePredicateTermEquippedBy
)

var compilePredicateTermCodes = state.NewStrCodes(
	state.StrEntry[compilePredicateTermCode]{Key: "YouCtrl", Val: compilePredicateTermYouCtrl},
	state.StrEntry[compilePredicateTermCode]{Key: "YouDontCtrl", Val: compilePredicateTermYouDontCtrl},
	state.StrEntry[compilePredicateTermCode]{Key: "OppCtrl", Val: compilePredicateTermYouDontCtrl},
	state.StrEntry[compilePredicateTermCode]{Key: "YouOwn", Val: compilePredicateTermYouOwn},
	state.StrEntry[compilePredicateTermCode]{Key: "OppOwn", Val: compilePredicateTermOppOwn},
	state.StrEntry[compilePredicateTermCode]{Key: "Self", Val: compilePredicateTermSelf},
	state.StrEntry[compilePredicateTermCode]{Key: "Other", Val: compilePredicateTermOther},
	state.StrEntry[compilePredicateTermCode]{Key: "StrictlyOther", Val: compilePredicateTermOther},
	state.StrEntry[compilePredicateTermCode]{Key: "tapped", Val: compilePredicateTermTapped},
	state.StrEntry[compilePredicateTermCode]{Key: "untapped", Val: compilePredicateTermUntapped},
	state.StrEntry[compilePredicateTermCode]{Key: "attacking", Val: compilePredicateTermAttacking},
	state.StrEntry[compilePredicateTermCode]{Key: "token", Val: compilePredicateTermToken},
	state.StrEntry[compilePredicateTermCode]{Key: "kicked", Val: compilePredicateTermKicked},
	state.StrEntry[compilePredicateTermCode]{Key: "bargained", Val: compilePredicateTermBargained},
	state.StrEntry[compilePredicateTermCode]{Key: "surged", Val: compilePredicateTermSurged},
	state.StrEntry[compilePredicateTermCode]{Key: "escaped", Val: compilePredicateTermEscaped},
	state.StrEntry[compilePredicateTermCode]{Key: "wasCastFromGraveyard", Val: compilePredicateTermWasCastFromGraveyard},
	state.StrEntry[compilePredicateTermCode]{Key: "EquippedBy", Val: compilePredicateTermEquippedBy},
	state.StrEntry[compilePredicateTermCode]{Key: "EnchantedBy", Val: compilePredicateTermEquippedBy},
	state.StrEntry[compilePredicateTermCode]{Key: "AttachedBy", Val: compilePredicateTermEquippedBy},
)
