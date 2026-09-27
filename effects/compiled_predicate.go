package effects

import (
	"sort"
	"strings"

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
	byText map[string]predicateProgram
}

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
	kind    predicateTermKind
	arg     string
	negated bool
	maybe   bool
}

// CompilePredicatePrograms compiles the subset of filter grammar that can be
// evaluated using only the candidate object and SpecContext. The compiler
// preserves unknown text as maybe rather than treating it as a non-match.
func CompilePredicatePrograms(specs []string) *PredicatePrograms {
	texts := append([]string(nil), specs...)
	sort.Strings(texts)
	programs := &PredicatePrograms{byText: make(map[string]predicateProgram, len(texts))}
	for _, spec := range texts {
		if spec == "" {
			continue
		}
		if _, exists := programs.byText[spec]; exists {
			continue
		}
		programs.byText[spec] = compilePredicateProgram(spec)
	}
	return programs
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
	switch base {
	case "Any", "Card", "Permanent", "PermanentCard", "Spell", "SpellAbility":
		kind := map[string]predicateBaseKind{
			"Any": predicateBaseAny, "Card": predicateBaseCard,
			"Permanent": predicateBasePermanent, "PermanentCard": predicateBasePermanentCard,
			"Spell": predicateBaseSpell, "SpellAbility": predicateBaseSpellAbility,
		}[base]
		return predicateBase{kind: kind, negated: negated}, true
	}
	if predicateTypeWords[base] {
		return predicateBase{kind: predicateBaseType, arg: base, negated: negated}, true
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
	switch term {
	case "YouCtrl":
		kind = predicateTermYouCtrl
	case "YouDontCtrl", "OppCtrl":
		kind = predicateTermYouDontCtrl
	case "YouOwn":
		kind = predicateTermYouOwn
	case "OppOwn":
		kind = predicateTermOppOwn
	case "Self":
		kind = predicateTermSelf
	case "Other", "StrictlyOther":
		kind = predicateTermOther
	case "tapped":
		kind = predicateTermTapped
	case "untapped":
		return predicateTerm{kind: predicateTermTapped, negated: true}
	case "attacking":
		kind = predicateTermAttacking
	case "token":
		kind = predicateTermToken
	case "kicked":
		kind = predicateTermKicked
	case "surged":
		kind = predicateTermSurged
	case "escaped":
		kind = predicateTermEscaped
	case "wasCastFromGraveyard":
		// The graveyard-origin cast bits (FlagFlashback/FlagHarmonize/
		// FlagEscaped) on a never-cast read — the compiled twin of the
		// filter.go entry and of the Count$wasCastFromGraveyard branch head,
		// all three sharing state.ObjectWasCastFromGraveyard (so a stack copy
		// reads false: a copy was put on the stack, never cast, CR 707.10);
		// an explicit case is required because predicateTermFromWord maps
		// only the color/type/colorless word kinds.
		kind = predicateTermWasCastFromGraveyard
	case "EquippedBy", "EnchantedBy", "AttachedBy":
		kind = predicateTermAttachedBy
	default:
		if wordKind, key, ok := nonPredicate(term); ok {
			if kind, ok := predicateTermFromWord(wordKind); ok {
				return predicateTerm{kind: kind, arg: key, negated: true}
			}
		}
		if wordKind, key := wordPredicate(term); wordKind != wordUnknown {
			if kind, ok := predicateTermFromWord(wordKind); ok {
				return predicateTerm{kind: kind, arg: key}
			}
		}
		return predicateTerm{maybe: true}
	}
	return predicateTerm{kind: kind}
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
	p, ok := ps.byText[spec]
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
		matched = hasTypeCtx(o, "Creature", *sc) || hasTypeCtx(o, "Planeswalker", *sc) || hasTypeCtx(o, "Battle", *sc)
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
		matched = hasTypeCtx(o, base.arg, *sc)
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
		matched = hasTypePredicateCtx(o, term.arg, *sc)
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
	return len(ps.byText)
}
