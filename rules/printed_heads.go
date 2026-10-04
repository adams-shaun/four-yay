package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// printedHeads is the set of PRINTED keyword heads a face carries, over the
// heads the offer walk's hand loop reads off a card's front face through
// cards.Face.KeywordParam (the keyword-cost offer family: kicker, surge,
// entwine, replicate, ... and the face-down cast family). Each of those
// readers answers "absent" -- and prices nothing -- exactly when no entry
// of f.Keywords has a KeywordHead that strings.EqualFold-matches its head,
// so a clear bit lets the walk skip the reader (a keyword lookup and a
// zeroed Cost return per head, per hand card, per walk) without changing
// what it offers. A set bit only means "ask the reader", which still decides.
type printedHeads uint32

const (
	phKicker printedHeads = 1 << iota
	phSurge
	phEntwine
	phReplicate
	phMultikicker
	phSquad
	phEvoke
	phDash
	phOverload
	phWarp
	phEmerge
	phBestow
	phMutate
	phBuyback
	phSuspend
	phPlot
	phMorph
	phMegamorph
	phDisguise
	phMayFlashCost
	phAlternateAdditionalCost
	// phAll answers "maybe" for every head.
	phAll printedHeads = 1<<iota - 1
)

// has reports whether any head in bits may be printed.
func (ph printedHeads) has(bits printedHeads) bool { return ph&bits != 0 }

// printedHeadsOf returns f's printedHeads. The fold is exact: for an
// all-ASCII head, strings.EqualFold against an ASCII name is the ASCII
// lower-case comparison below; a head carrying any non-ASCII byte could
// match through Unicode simple folding (the KELVIN SIGN folds to k), so it
// answers phAll and every reader is asked.
func printedHeadsOf(f *cards.Face) printedHeads {
	var m printedHeads
	for _, k := range f.Keywords {
		h := cards.KeywordHead(k)
		var buf [len("alternateadditionalcost")]byte
		ascii := true
		for i := 0; i < len(h); i++ {
			if h[i] >= 0x80 {
				ascii = false
				break
			}
		}
		if !ascii {
			return phAll
		}
		if len(h) > len(buf) {
			continue // longer than every head: no ASCII head can match
		}
		for i := 0; i < len(h); i++ {
			c := h[i]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			buf[i] = c
		}
		switch printedHeadsOfae71Codes.Code(string(string(buf[:len(h)]))) {
		case printedHeadsOfae71Kicker:
			m |= phKicker
		case printedHeadsOfae71Surge:
			m |= phSurge
		case printedHeadsOfae71Entwine:
			m |= phEntwine
		case printedHeadsOfae71Replicate:
			m |= phReplicate
		case printedHeadsOfae71Multikicker:
			m |= phMultikicker
		case printedHeadsOfae71Squad:
			m |= phSquad
		case printedHeadsOfae71Evoke:
			m |= phEvoke
		case printedHeadsOfae71Dash:
			m |= phDash
		case printedHeadsOfae71Overload:
			m |= phOverload
		case printedHeadsOfae71Warp:
			m |= phWarp
		case printedHeadsOfae71Emerge:
			m |= phEmerge
		case printedHeadsOfae71Bestow:
			m |= phBestow
		case printedHeadsOfae71Mutate:
			m |= phMutate
		case printedHeadsOfae71Buyback:
			m |= phBuyback
		case printedHeadsOfae71Suspend:
			m |= phSuspend
		case printedHeadsOfae71Plot:
			m |= phPlot
		case printedHeadsOfae71Morph:
			m |= phMorph
		case printedHeadsOfae71Megamorph:
			m |= phMegamorph
		case printedHeadsOfae71Disguise:
			m |= phDisguise
		case printedHeadsOfae71Mayflashcost:
			m |= phMayFlashCost
		case printedHeadsOfae71Alternateadditionalcost:
			m |= phAlternateAdditionalCost
		}
	}
	return m
}

// printedHeadNames maps each bit to the head its reader asks KeywordParam
// for (the verify check and the unit test walk it).
var printedHeadNames = [...]struct {
	bit  printedHeads
	head string
}{
	{phKicker, "Kicker"}, {phSurge, "Surge"}, {phEntwine, "Entwine"}, {phReplicate, "Replicate"},
	{phMultikicker, "Multikicker"}, {phSquad, "Squad"}, {phEvoke, "Evoke"}, {phDash, "Dash"},
	{phOverload, "Overload"}, {phWarp, "Warp"}, {phEmerge, "Emerge"}, {phBestow, "Bestow"},
	{phMutate, "Mutate"}, {phBuyback, "Buyback"}, {phSuspend, "Suspend"}, {phPlot, "Plot"},
	{phMorph, "Morph"}, {phMegamorph, "Megamorph"}, {phDisguise, "Disguise"},
	{phMayFlashCost, "MayFlashCost"}, {phAlternateAdditionalCost, "AlternateAdditionalCost"},
}

// verifyPrintedHeads panics when a clear bit of ph is contradicted by
// f.KeywordParam (derivedMemoVerify mode: the offer walk checks every hand
// card it summarises).
func verifyPrintedHeads(f *cards.Face, ph printedHeads) {
	for _, n := range printedHeadNames {
		if _, ok := f.KeywordParam(n.head); ok && !ph.has(n.bit) {
			panic("rules: printed keyword summary of " + f.Name + " misses " + n.head)
		}
	}
}

const (
	printedHeadsOfae71Kicker                  uint16 = 1  // "kicker"
	printedHeadsOfae71Surge                   uint16 = 2  // "surge"
	printedHeadsOfae71Entwine                 uint16 = 3  // "entwine"
	printedHeadsOfae71Replicate               uint16 = 4  // "replicate"
	printedHeadsOfae71Multikicker             uint16 = 5  // "multikicker"
	printedHeadsOfae71Squad                   uint16 = 6  // "squad"
	printedHeadsOfae71Evoke                   uint16 = 7  // "evoke"
	printedHeadsOfae71Dash                    uint16 = 8  // "dash"
	printedHeadsOfae71Overload                uint16 = 9  // "overload"
	printedHeadsOfae71Warp                    uint16 = 10 // "warp"
	printedHeadsOfae71Emerge                  uint16 = 11 // "emerge"
	printedHeadsOfae71Bestow                  uint16 = 12 // "bestow"
	printedHeadsOfae71Mutate                  uint16 = 13 // "mutate"
	printedHeadsOfae71Buyback                 uint16 = 14 // "buyback"
	printedHeadsOfae71Suspend                 uint16 = 15 // "suspend"
	printedHeadsOfae71Plot                    uint16 = 16 // "plot"
	printedHeadsOfae71Morph                   uint16 = 17 // "morph"
	printedHeadsOfae71Megamorph               uint16 = 18 // "megamorph"
	printedHeadsOfae71Disguise                uint16 = 19 // "disguise"
	printedHeadsOfae71Mayflashcost            uint16 = 20 // "mayflashcost"
	printedHeadsOfae71Alternateadditionalcost uint16 = 21 // "alternateadditionalcost"
)

var printedHeadsOfae71Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "kicker", Val: printedHeadsOfae71Kicker},
	state.StrEntry[uint16]{Key: "surge", Val: printedHeadsOfae71Surge},
	state.StrEntry[uint16]{Key: "entwine", Val: printedHeadsOfae71Entwine},
	state.StrEntry[uint16]{Key: "replicate", Val: printedHeadsOfae71Replicate},
	state.StrEntry[uint16]{Key: "multikicker", Val: printedHeadsOfae71Multikicker},
	state.StrEntry[uint16]{Key: "squad", Val: printedHeadsOfae71Squad},
	state.StrEntry[uint16]{Key: "evoke", Val: printedHeadsOfae71Evoke},
	state.StrEntry[uint16]{Key: "dash", Val: printedHeadsOfae71Dash},
	state.StrEntry[uint16]{Key: "overload", Val: printedHeadsOfae71Overload},
	state.StrEntry[uint16]{Key: "warp", Val: printedHeadsOfae71Warp},
	state.StrEntry[uint16]{Key: "emerge", Val: printedHeadsOfae71Emerge},
	state.StrEntry[uint16]{Key: "bestow", Val: printedHeadsOfae71Bestow},
	state.StrEntry[uint16]{Key: "mutate", Val: printedHeadsOfae71Mutate},
	state.StrEntry[uint16]{Key: "buyback", Val: printedHeadsOfae71Buyback},
	state.StrEntry[uint16]{Key: "suspend", Val: printedHeadsOfae71Suspend},
	state.StrEntry[uint16]{Key: "plot", Val: printedHeadsOfae71Plot},
	state.StrEntry[uint16]{Key: "morph", Val: printedHeadsOfae71Morph},
	state.StrEntry[uint16]{Key: "megamorph", Val: printedHeadsOfae71Megamorph},
	state.StrEntry[uint16]{Key: "disguise", Val: printedHeadsOfae71Disguise},
	state.StrEntry[uint16]{Key: "mayflashcost", Val: printedHeadsOfae71Mayflashcost},
	state.StrEntry[uint16]{Key: "alternateadditionalcost", Val: printedHeadsOfae71Alternateadditionalcost},
)
