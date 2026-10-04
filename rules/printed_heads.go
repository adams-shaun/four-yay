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
		switch printedHeadsOfCodes.Code(string(string(buf[:len(h)]))) {
		case printedHeadsOfKicker:
			m |= phKicker
		case printedHeadsOfSurge:
			m |= phSurge
		case printedHeadsOfEntwine:
			m |= phEntwine
		case printedHeadsOfReplicate:
			m |= phReplicate
		case printedHeadsOfMultikicker:
			m |= phMultikicker
		case printedHeadsOfSquad:
			m |= phSquad
		case printedHeadsOfEvoke:
			m |= phEvoke
		case printedHeadsOfDash:
			m |= phDash
		case printedHeadsOfOverload:
			m |= phOverload
		case printedHeadsOfWarp:
			m |= phWarp
		case printedHeadsOfEmerge:
			m |= phEmerge
		case printedHeadsOfBestow:
			m |= phBestow
		case printedHeadsOfMutate:
			m |= phMutate
		case printedHeadsOfBuyback:
			m |= phBuyback
		case printedHeadsOfSuspend:
			m |= phSuspend
		case printedHeadsOfPlot:
			m |= phPlot
		case printedHeadsOfMorph:
			m |= phMorph
		case printedHeadsOfMegamorph:
			m |= phMegamorph
		case printedHeadsOfDisguise:
			m |= phDisguise
		case printedHeadsOfMayflashcost:
			m |= phMayFlashCost
		case printedHeadsOfAlternateadditionalcost:
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

type printedHeadsOfCode uint16

const (
	printedHeadsOfKicker printedHeadsOfCode = iota + 1
	printedHeadsOfSurge
	printedHeadsOfEntwine
	printedHeadsOfReplicate
	printedHeadsOfMultikicker
	printedHeadsOfSquad
	printedHeadsOfEvoke
	printedHeadsOfDash
	printedHeadsOfOverload
	printedHeadsOfWarp
	printedHeadsOfEmerge
	printedHeadsOfBestow
	printedHeadsOfMutate
	printedHeadsOfBuyback
	printedHeadsOfSuspend
	printedHeadsOfPlot
	printedHeadsOfMorph
	printedHeadsOfMegamorph
	printedHeadsOfDisguise
	printedHeadsOfMayflashcost
	printedHeadsOfAlternateadditionalcost
)

var printedHeadsOfCodes = state.NewStrCodes(
	state.StrEntry[printedHeadsOfCode]{Key: "kicker", Val: printedHeadsOfKicker},
	state.StrEntry[printedHeadsOfCode]{Key: "surge", Val: printedHeadsOfSurge},
	state.StrEntry[printedHeadsOfCode]{Key: "entwine", Val: printedHeadsOfEntwine},
	state.StrEntry[printedHeadsOfCode]{Key: "replicate", Val: printedHeadsOfReplicate},
	state.StrEntry[printedHeadsOfCode]{Key: "multikicker", Val: printedHeadsOfMultikicker},
	state.StrEntry[printedHeadsOfCode]{Key: "squad", Val: printedHeadsOfSquad},
	state.StrEntry[printedHeadsOfCode]{Key: "evoke", Val: printedHeadsOfEvoke},
	state.StrEntry[printedHeadsOfCode]{Key: "dash", Val: printedHeadsOfDash},
	state.StrEntry[printedHeadsOfCode]{Key: "overload", Val: printedHeadsOfOverload},
	state.StrEntry[printedHeadsOfCode]{Key: "warp", Val: printedHeadsOfWarp},
	state.StrEntry[printedHeadsOfCode]{Key: "emerge", Val: printedHeadsOfEmerge},
	state.StrEntry[printedHeadsOfCode]{Key: "bestow", Val: printedHeadsOfBestow},
	state.StrEntry[printedHeadsOfCode]{Key: "mutate", Val: printedHeadsOfMutate},
	state.StrEntry[printedHeadsOfCode]{Key: "buyback", Val: printedHeadsOfBuyback},
	state.StrEntry[printedHeadsOfCode]{Key: "suspend", Val: printedHeadsOfSuspend},
	state.StrEntry[printedHeadsOfCode]{Key: "plot", Val: printedHeadsOfPlot},
	state.StrEntry[printedHeadsOfCode]{Key: "morph", Val: printedHeadsOfMorph},
	state.StrEntry[printedHeadsOfCode]{Key: "megamorph", Val: printedHeadsOfMegamorph},
	state.StrEntry[printedHeadsOfCode]{Key: "disguise", Val: printedHeadsOfDisguise},
	state.StrEntry[printedHeadsOfCode]{Key: "mayflashcost", Val: printedHeadsOfMayflashcost},
	state.StrEntry[printedHeadsOfCode]{Key: "alternateadditionalcost", Val: printedHeadsOfAlternateadditionalcost},
)
