package state

import (
	"strconv"
	"strings"
)

// StackObjKind classifies one stack object for stack-spec legality (the
// TargetType$ grammar and Defined$ ValidStack's kind tokens): a card object
// (Face != nil) is a spell; an ability object is the triggered/activated
// split both the CR (603.1/602.1) and the view's StackView.Kind serve. The
// split is TriggerOf -- membership in the source face's T: lines is a
// TriggerPush mint (a triggered ability); membership in its AB$ list is an
// AbilityPush mint (an activated ability); anything else is a DelayedPush
// mint, whose Ability was resolved from the registration's Execute$ SVar
// rather than copied from either list -- and a delayed trigger IS a
// triggered ability (CR 603.7), so that branch counts as triggered. When the
// source or its face is gone the split is unknowable and the object counts
// as activated (the view's own "ability" verdict) only for legacy or
// manually-constructed objects without a stamped kind. Event-minted stack
// objects carry the CR kind through source disappearance, so a
// Triggered-only counter still reaches a triggered ability. Pointer identity
// is sound for the membership tests: every mint copies
// the parsed slice's pointer (TriggerPush/AbilityPush), StackCopy re-copies
// it, and ResolveSVar always parses a fresh SA, so a delayed trigger's
// Ability can never alias a face-list entry.
//
// The classifier lives in state -- not rules, where its first consumer (the
// target census) sat -- because a second consumer exists at a LOWER package
// level: effects' Defined$ ValidStack arm must admit exactly the objects
// target legality admits, and effects cannot import rules. Three consumers
// share this one classifier: rules (which delegates to it,
// rules/target_legal.go), effects' Defined$ ValidStack arm
// (effects/context.go, effects/misc.go), and the view's StackView.Kind
// (view/stack.go), which reads this verdict -- its stamped-kind authority --
// instead of keeping its own TriggerOf call.
func StackKindOf(g *Game, o *Object) StackObjKind {
	if o == nil {
		return StackKindActivated
	}
	if o.StackKindKnown {
		return o.StackKind
	}
	if o.Face() != nil {
		return StackKindSpell
	}
	if _, ok := TriggerOf(g, o); ok {
		return StackKindTriggered
	}
	if src := g.Obj(o.Source); src != nil {
		if f := src.Face(); f != nil {
			for _, ab := range f.Abilities {
				if ab == o.Ability {
					return StackKindActivated
				}
			}
			// A real source face that lists neither a trigger nor this
			// ability minted it from an SVar: DelayedPush (CR 603.7).
			return StackKindTriggered
		}
	}
	return StackKindActivated
}

// StackObjKind is the stack-spec-relevant kind of a stack object.
type StackObjKind uint8

const (
	StackKindSpell     StackObjKind = iota // a card object (a Face) on the stack
	StackKindActivated                     // an ability object minted by AbilityPush
	StackKindTriggered                     // an ability object minted by TriggerPush/DelayedPush
)

// StackKindToken is one comma-separated token of a stack spec (a TargetType$
// value or a Defined$ ValidStack value): which stack object kinds its base
// admits, the controller qualifier read off the qualifiers after the base
// ("YouCtrl" -- controller must be the chooser; "OppCtrl" -- controller must
// not be; e.g. Weaver of Harmony's `Activated.YouCtrl,Triggered.YouCtrl`,
// Kang Dynasty's `Spell.OppCtrl`), and the card-type restriction the base or
// a qualifier can impose: the bases "Instant" and "Sorcery" (Spider Sense's
// `Instant,Sorcery,Triggered`) and the Spell qualifiers "Instant"/"Sorcery"
// (Sister of Silence's `Spell.Instant,Spell.Sorcery,Activated,Triggered`)
// restrict the Spell kind to instant/sorcery CARD objects -- a creature
// spell is not admitted. This parser also records the TargetType$ qualifiers
// whose live characteristic and target-count checks belong to rules. A
// Defined$ ValidStack spec may carry ValidStack-only qualifiers (Other,
// sharesNameWith); those are NOT read here -- its consumer parses them
// alongside and must NOT treat their presence as unknown, so the two
// grammars share this struct and each reads the qualifiers it knows.
type StackKindToken struct {
	Kinds        [3]bool // indexed by StackObjKind; only lookups, never ranged
	InstantOnly  bool    // the StackKindSpell kind admits only Instant cards
	SorceryOnly  bool    // the StackKindSpell kind admits only Sorcery cards
	YouCtrl      bool
	OppCtrl      bool
	YouDontCtrl  bool
	SingleTarget bool
	NumTargetsOp string
	NumTargets   int
	NonCreature  bool
	Colorless    bool
	Legendary    bool
}

// StackKindTokenOf parses ONE comma-separated token of a stack spec. ok is
// false when the token's base names no stack kind at all -- a non-stack
// token never contributes a stack kind (rules' census skips it; Defined$
// ValidStack's own parser does the same).
func StackKindTokenOf(t string) (StackKindToken, bool) {
	base, rest, _ := strings.Cut(strings.TrimSpace(t), ".")
	var tok StackKindToken
	switch base {
	case "Spell":
		tok.Kinds[StackKindSpell] = true
	case "Instant":
		tok.Kinds[StackKindSpell] = true
		tok.InstantOnly = true
	case "Sorcery":
		tok.Kinds[StackKindSpell] = true
		tok.SorceryOnly = true
	case "Activated":
		tok.Kinds[StackKindActivated] = true
	case "Triggered":
		tok.Kinds[StackKindTriggered] = true
	case "SpellAbility":
		tok.Kinds[StackKindSpell] = true
		tok.Kinds[StackKindActivated] = true
		tok.Kinds[StackKindTriggered] = true
	case "Ability":
		// Forge's alias for "activated + triggered ability objects" -- the
		// mirror of SpellAbility minus Spell. Ulalek, Fused Atrocity's
		// sub-copy is the corpus carrier (exactly 1 Defined$ ValidStack line;
		// 0 TargetType$ lines carry the base, so the TargetType$ census is
		// unmoved). Mana abilities need no exclusion here: this engine never
		// puts a mana-ability wrapper on the stack (CR 605.3a -- the offer
		// loop skips AB$ Mana), so Activated+Triggered coverage is exact.
		tok.Kinds[StackKindActivated] = true
		tok.Kinds[StackKindTriggered] = true
	default:
		return tok, false // a non-stack token never contributes a stack kind
	}
	for part := range strings.SplitSeq(rest, ".") {
		for q := range strings.SplitSeq(strings.TrimSpace(part), "+") {
			switch strings.TrimSpace(q) {
			case "YouCtrl":
				tok.YouCtrl = true
			case "OppCtrl":
				tok.OppCtrl = true
			case "YouDontCtrl":
				tok.YouDontCtrl = true
			case "singleTarget":
				tok.SingleTarget = true
			case "nonCreature":
				tok.NonCreature = true
			case "Colorless":
				tok.Colorless = true
			case "Legendary":
				tok.Legendary = true
			case "Instant": // Sister of Silence's Spell.Instant shape
				tok.InstantOnly = true
			case "Sorcery":
				tok.SorceryOnly = true
			default:
				fields := strings.Fields(strings.TrimSpace(q))
				if len(fields) != 2 || fields[0] != "numTargets" || len(fields[1]) < 3 {
					continue
				}
				switch fields[1][:2] {
				case "EQ", "NE", "GE", "GT", "LE", "LT":
					op := fields[1][:2]
					if n, err := strconv.Atoi(fields[1][2:]); err == nil {
						tok.NumTargetsOp, tok.NumTargets = op, n
					}
				}
			}
		}
	}
	return tok, true
}

// StackKindTokens parses a whole TargetType$ value into its kind tokens. A
// parameter that is absent -- or whose tokens name no stack kind at all --
// defaults to Spell-only (today's behaviour, deliberately narrow: a spec
// that never said it wants abilities does not get them).
func StackKindTokens(tt string) []StackKindToken {
	spellOnly := StackKindToken{Kinds: [3]bool{StackKindSpell: true}}
	var toks []StackKindToken
	for t := range strings.SplitSeq(tt, ",") {
		tok, ok := StackKindTokenOf(t)
		if !ok {
			continue
		}
		toks = append(toks, tok)
	}
	if len(toks) == 0 {
		return []StackKindToken{spellOnly}
	}
	return toks
}

// StackKindAdmits reports whether any stack-spec token admits the stack
// object o (of kind k) controlled by controller, from chooser you's
// perspective. Token semantics are OR, matching ValidTgts$ alternatives:
// the object is offered when SOME token whose kind set contains k admits it
// under that token's own controller and card-type restriction. An
// InstantOnly/SorceryOnly token checks the card object's Face, so a creature
// spell is never admitted by Spider Sense's `Instant,Sorcery,Triggered` or
// Sister of Silence's `Spell.Instant,Spell.Sorcery,...`; a Face-less object
// (never reachable for StackKindSpell, since StackObjKind only classifies a
// Face-bearing object as a spell) fails closed.
func StackKindAdmits(toks []StackKindToken, k StackObjKind, o *Object, controller, you PlayerID) bool {
	for _, tok := range toks {
		if !tok.Kinds[k] {
			continue
		}
		if tok.InstantOnly || tok.SorceryOnly {
			f := o.Face()
			if f == nil {
				continue
			}
			if tok.InstantOnly && !f.IsInstant() {
				continue
			}
			if tok.SorceryOnly && !f.IsSorcery() {
				continue
			}
		}
		if tok.YouCtrl {
			if controller == you {
				return true
			}
			continue
		}
		if tok.OppCtrl {
			if controller == you {
				continue
			}
			return true
		}
		if tok.YouDontCtrl {
			if controller == you {
				continue
			}
			return true
		}
		return true
	}
	return false
}
