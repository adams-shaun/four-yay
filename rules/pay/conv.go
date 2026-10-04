package pay

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// conv.go is the stat:ManaConvert grammar (moved from package rules'
// mana_convert.go, E7): the ManaConversion$ from/to words and the ValidSA$
// payment-kind scoping that build one payment's Conv. Pure.

// stat:ManaConvert (CR 608.2i, "spend mana as though it were mana of any
// color"): the S:Mode$ ManaConvert statics. This file owns the grammar;
// rules' manaConversionParts builds the one per-payment conversion set
// from the board, and ResolveMana consults it through the *Conv parameter on the payment call sites, so an
// offered cost (castable / manaAbilityPayable / the mana window / the X ask)
// and the cost actually charged can never disagree about what the payer may
// spend.
//
// The conversion is a fact about the PAYER'S mana for ONE payment, never
// about the cost: "you may spend white mana as though it were red mana"
// leaves the {R} pip exactly what it was and lets the pool's white mana pay
// it. That is why the model below annotates pool colours, not pips.

// ManaColourFrom maps a ManaConversion$ "from" word to the pool-colour
// indexes it names: a colour name or letter is that colour; "AnyType" is all
// six (any type includes colourless); "non<name>" is every colour except the
// named one. An unknown word names nothing -- a conversion this build cannot
// parse is silently inert rather than guessed wide.
func ManaColourFrom(word string) []int {
	name := strings.TrimSpace(word)
	complement := false
	if rest, ok := strings.CutPrefix(name, "non"); ok && rest != "" {
		name, complement = rest, true
	}
	var base []int
	switch manaColourFromcf21Codes.Code(string(strings.ToLower(name))) {
	case manaColourFromcf21Anytype:
		base = []int{state.MW, state.MU, state.MB, state.MR, state.MG, state.MC}
	case manaColourFromcf21W:
		base = []int{state.MW}
	case manaColourFromcf21U:
		base = []int{state.MU}
	case manaColourFromcf21B:
		base = []int{state.MB}
	case manaColourFromcf21R:
		base = []int{state.MR}
	case manaColourFromcf21G:
		base = []int{state.MG}
	case manaColourFromcf21C:
		base = []int{state.MC}
	default:
		return nil
	}
	if !complement {
		return base
	}
	excl := map[int]bool{}
	for _, i := range base {
		excl[i] = true
	}
	var out []int
	for i := range ManaLetters {
		if !excl[i] {
			out = append(out, i)
		}
	}
	return out
}

// ApplyConversionTo applies a ManaConversion$ "to" word to the pip it makes
// acceptable: a colour name or letter is that pip; "AnyColor" is the wild
// (any colour, not {C}) grant; "AnyType" is wild plus the {C} pips. The
// second return reports whether the word resolved at all, so an unknown
// conversion target leaves the static inert rather than granting everything.
func ApplyConversionTo(conv *Conv, from []int, word string) bool {
	name := strings.ToLower(strings.TrimSpace(word))
	switch applyManaConversionTocf22Codes.Code(string(name)) {
	case applyManaConversionTocf22Anycolor:
		for _, i := range from {
			conv.Wild[i] = true
		}
	case applyManaConversionTocf22Anytype:
		for _, i := range from {
			conv.Wild[i] = true
		}
		conv.WildC = true
	case applyManaConversionTocf22C:
		// "as though it were colorless mana" as a GRANT: mana of the "from"
		// colours may additionally pay {C} pips. Not a corpus shape today,
		// but the same grammar the restriction side uses; kept for symmetry.
		for _, i := range from {
			conv.To[i][state.MC] = true
		}
	case applyManaConversionTocf22W:
		for _, i := range from {
			conv.To[i][state.MW] = true
		}
	case applyManaConversionTocf22U:
		for _, i := range from {
			conv.To[i][state.MU] = true
		}
	case applyManaConversionTocf22B:
		for _, i := range from {
			conv.To[i][state.MB] = true
		}
	case applyManaConversionTocf22R:
		for _, i := range from {
			conv.To[i][state.MR] = true
		}
	case applyManaConversionTocf22G:
		for _, i := range from {
			conv.To[i][state.MG] = true
		}
	default:
		return false
	}
	return true
}

// StaticSAKindMatches reports whether a ValidSA$ value on a ManaConvert
// static admits this kind of payment. The value is Forge's comma-separated
// OR list of "<kind>[.<qualifier>]" ("Spell", "Activated", "Spell.MayPlaySource",
// "Spell,Activated"); an absent value admits both. An alternative whose kind
// is neither Spell nor Activated (a Loyalty/other qualifier this build
// cannot evaluate) is skipped rather than guessed: a conversion the engine
// cannot scope never grants, the fail-closed direction every other
// restriction-class reader here uses.
func StaticSAKindMatches(validSA string, ability bool) bool {
	v := strings.TrimSpace(validSA)
	if v == "" {
		return true
	}
	for alt := range strings.SplitSeq(v, ",") {
		kind := strings.TrimSpace(alt)
		if i := strings.IndexByte(kind, '.'); i >= 0 {
			kind = kind[:i]
		}
		switch staticSAKindMatchescf23Codes.Code(string(kind)) {
		case staticSAKindMatchescf23Spell:
			if !ability {
				return true
			}
		case staticSAKindMatchescf23Activated:
			if ability {
				return true
			}
		}
	}
	return false
}

// MergeConv ORs src into dst: the union of two conversion sets.
func MergeConv(dst *Conv, src Conv) {
	for i := range dst.Wild {
		dst.Wild[i] = dst.Wild[i] || src.Wild[i]
		dst.OnlyC[i] = dst.OnlyC[i] || src.OnlyC[i]
		for j := range dst.To[i] {
			dst.To[i][j] = dst.To[i][j] || src.To[i][j]
		}
	}
	dst.WildC = dst.WildC || src.WildC
}

const (
	manaColourFromcf21Anytype uint16 = 1 // "anytype"
	manaColourFromcf21W       uint16 = 2 // "w", "white"
	manaColourFromcf21U       uint16 = 3 // "u", "blue"
	manaColourFromcf21B       uint16 = 4 // "b", "black"
	manaColourFromcf21R       uint16 = 5 // "r", "red"
	manaColourFromcf21G       uint16 = 6 // "g", "green"
	manaColourFromcf21C       uint16 = 7 // "c", "colorless"
)

var manaColourFromcf21Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "anytype", Val: manaColourFromcf21Anytype},
	state.StrEntry[uint16]{Key: "w", Val: manaColourFromcf21W},
	state.StrEntry[uint16]{Key: "white", Val: manaColourFromcf21W},
	state.StrEntry[uint16]{Key: "u", Val: manaColourFromcf21U},
	state.StrEntry[uint16]{Key: "blue", Val: manaColourFromcf21U},
	state.StrEntry[uint16]{Key: "b", Val: manaColourFromcf21B},
	state.StrEntry[uint16]{Key: "black", Val: manaColourFromcf21B},
	state.StrEntry[uint16]{Key: "r", Val: manaColourFromcf21R},
	state.StrEntry[uint16]{Key: "red", Val: manaColourFromcf21R},
	state.StrEntry[uint16]{Key: "g", Val: manaColourFromcf21G},
	state.StrEntry[uint16]{Key: "green", Val: manaColourFromcf21G},
	state.StrEntry[uint16]{Key: "c", Val: manaColourFromcf21C},
	state.StrEntry[uint16]{Key: "colorless", Val: manaColourFromcf21C},
)

const (
	applyManaConversionTocf22Anycolor uint16 = 1 // "anycolor"
	applyManaConversionTocf22Anytype  uint16 = 2 // "anytype"
	applyManaConversionTocf22C        uint16 = 3 // "c", "colorless"
	applyManaConversionTocf22W        uint16 = 4 // "w", "white"
	applyManaConversionTocf22U        uint16 = 5 // "u", "blue"
	applyManaConversionTocf22B        uint16 = 6 // "b", "black"
	applyManaConversionTocf22R        uint16 = 7 // "r", "red"
	applyManaConversionTocf22G        uint16 = 8 // "g", "green"
)

var applyManaConversionTocf22Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "anycolor", Val: applyManaConversionTocf22Anycolor},
	state.StrEntry[uint16]{Key: "anytype", Val: applyManaConversionTocf22Anytype},
	state.StrEntry[uint16]{Key: "c", Val: applyManaConversionTocf22C},
	state.StrEntry[uint16]{Key: "colorless", Val: applyManaConversionTocf22C},
	state.StrEntry[uint16]{Key: "w", Val: applyManaConversionTocf22W},
	state.StrEntry[uint16]{Key: "white", Val: applyManaConversionTocf22W},
	state.StrEntry[uint16]{Key: "u", Val: applyManaConversionTocf22U},
	state.StrEntry[uint16]{Key: "blue", Val: applyManaConversionTocf22U},
	state.StrEntry[uint16]{Key: "b", Val: applyManaConversionTocf22B},
	state.StrEntry[uint16]{Key: "black", Val: applyManaConversionTocf22B},
	state.StrEntry[uint16]{Key: "r", Val: applyManaConversionTocf22R},
	state.StrEntry[uint16]{Key: "red", Val: applyManaConversionTocf22R},
	state.StrEntry[uint16]{Key: "g", Val: applyManaConversionTocf22G},
	state.StrEntry[uint16]{Key: "green", Val: applyManaConversionTocf22G},
)

const (
	staticSAKindMatchescf23Spell     uint16 = 1 // "Spell"
	staticSAKindMatchescf23Activated uint16 = 2 // "Activated"
)

var staticSAKindMatchescf23Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Spell", Val: staticSAKindMatchescf23Spell},
	state.StrEntry[uint16]{Key: "Activated", Val: staticSAKindMatchescf23Activated},
)
