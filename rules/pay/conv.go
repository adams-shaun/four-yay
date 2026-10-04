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
	switch manaColourFromCodes.Code(string(strings.ToLower(name))) {
	case manaColourFromAnytype:
		base = []int{state.MW, state.MU, state.MB, state.MR, state.MG, state.MC}
	case manaColourFromW:
		base = []int{state.MW}
	case manaColourFromU:
		base = []int{state.MU}
	case manaColourFromB:
		base = []int{state.MB}
	case manaColourFromR:
		base = []int{state.MR}
	case manaColourFromG:
		base = []int{state.MG}
	case manaColourFromC:
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
	switch applyManaConversionToCodes.Code(string(name)) {
	case applyManaConversionToAnycolor:
		for _, i := range from {
			conv.Wild[i] = true
		}
	case applyManaConversionToAnytype:
		for _, i := range from {
			conv.Wild[i] = true
		}
		conv.WildC = true
	case applyManaConversionToC:
		// "as though it were colorless mana" as a GRANT: mana of the "from"
		// colours may additionally pay {C} pips. Not a corpus shape today,
		// but the same grammar the restriction side uses; kept for symmetry.
		for _, i := range from {
			conv.To[i][state.MC] = true
		}
	case applyManaConversionToW:
		for _, i := range from {
			conv.To[i][state.MW] = true
		}
	case applyManaConversionToU:
		for _, i := range from {
			conv.To[i][state.MU] = true
		}
	case applyManaConversionToB:
		for _, i := range from {
			conv.To[i][state.MB] = true
		}
	case applyManaConversionToR:
		for _, i := range from {
			conv.To[i][state.MR] = true
		}
	case applyManaConversionToG:
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
		switch staticSAKindMatchesCodes.Code(string(kind)) {
		case staticSAKindMatchesSpell:
			if !ability {
				return true
			}
		case staticSAKindMatchesActivated:
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

type manaColourFromCode uint16

const (
	manaColourFromAnytype manaColourFromCode = iota + 1
	manaColourFromW
	manaColourFromU
	manaColourFromB
	manaColourFromR
	manaColourFromG
	manaColourFromC
)

var manaColourFromCodes = state.NewStrCodes(
	state.StrEntry[manaColourFromCode]{Key: "anytype", Val: manaColourFromAnytype},
	state.StrEntry[manaColourFromCode]{Key: "w", Val: manaColourFromW},
	state.StrEntry[manaColourFromCode]{Key: "white", Val: manaColourFromW},
	state.StrEntry[manaColourFromCode]{Key: "u", Val: manaColourFromU},
	state.StrEntry[manaColourFromCode]{Key: "blue", Val: manaColourFromU},
	state.StrEntry[manaColourFromCode]{Key: "b", Val: manaColourFromB},
	state.StrEntry[manaColourFromCode]{Key: "black", Val: manaColourFromB},
	state.StrEntry[manaColourFromCode]{Key: "r", Val: manaColourFromR},
	state.StrEntry[manaColourFromCode]{Key: "red", Val: manaColourFromR},
	state.StrEntry[manaColourFromCode]{Key: "g", Val: manaColourFromG},
	state.StrEntry[manaColourFromCode]{Key: "green", Val: manaColourFromG},
	state.StrEntry[manaColourFromCode]{Key: "c", Val: manaColourFromC},
	state.StrEntry[manaColourFromCode]{Key: "colorless", Val: manaColourFromC},
)

type applyManaConversionToCode uint16

const (
	applyManaConversionToAnycolor applyManaConversionToCode = iota + 1
	applyManaConversionToAnytype
	applyManaConversionToC
	applyManaConversionToW
	applyManaConversionToU
	applyManaConversionToB
	applyManaConversionToR
	applyManaConversionToG
)

var applyManaConversionToCodes = state.NewStrCodes(
	state.StrEntry[applyManaConversionToCode]{Key: "anycolor", Val: applyManaConversionToAnycolor},
	state.StrEntry[applyManaConversionToCode]{Key: "anytype", Val: applyManaConversionToAnytype},
	state.StrEntry[applyManaConversionToCode]{Key: "c", Val: applyManaConversionToC},
	state.StrEntry[applyManaConversionToCode]{Key: "colorless", Val: applyManaConversionToC},
	state.StrEntry[applyManaConversionToCode]{Key: "w", Val: applyManaConversionToW},
	state.StrEntry[applyManaConversionToCode]{Key: "white", Val: applyManaConversionToW},
	state.StrEntry[applyManaConversionToCode]{Key: "u", Val: applyManaConversionToU},
	state.StrEntry[applyManaConversionToCode]{Key: "blue", Val: applyManaConversionToU},
	state.StrEntry[applyManaConversionToCode]{Key: "b", Val: applyManaConversionToB},
	state.StrEntry[applyManaConversionToCode]{Key: "black", Val: applyManaConversionToB},
	state.StrEntry[applyManaConversionToCode]{Key: "r", Val: applyManaConversionToR},
	state.StrEntry[applyManaConversionToCode]{Key: "red", Val: applyManaConversionToR},
	state.StrEntry[applyManaConversionToCode]{Key: "g", Val: applyManaConversionToG},
	state.StrEntry[applyManaConversionToCode]{Key: "green", Val: applyManaConversionToG},
)

type staticSAKindMatchesCode uint16

const (
	staticSAKindMatchesSpell staticSAKindMatchesCode = iota + 1
	staticSAKindMatchesActivated
)

var staticSAKindMatchesCodes = state.NewStrCodes(
	state.StrEntry[staticSAKindMatchesCode]{Key: "Spell", Val: staticSAKindMatchesSpell},
	state.StrEntry[staticSAKindMatchesCode]{Key: "Activated", Val: staticSAKindMatchesActivated},
)
