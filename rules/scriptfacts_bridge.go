package rules

import (
	"github.com/adams-shaun/gorge/cards"
	scriptfacts "github.com/adams-shaun/gorge/rules/scriptfacts"
)

// This file is package rules' one bridge to the card-fact leaf,
// rules/scriptfacts (the 2026-10-06 rules-split plan's first slice). The
// printed-head type is a defined type over scriptfacts.Heads so rules keeps
// its historical unexported name AND its `has` method, and every function
// below is a one-line forwarder the compiler inlines: rules keeps its
// call sites untouched while the definitions live in a leaf that holds no
// *Engine and reads no game state.
//
// Add a forwarder here only for a name rules actually calls.

type printedHeads scriptfacts.Heads

const (
	phKicker                  = printedHeads(scriptfacts.Kicker)
	phSurge                   = printedHeads(scriptfacts.Surge)
	phEntwine                 = printedHeads(scriptfacts.Entwine)
	phReplicate               = printedHeads(scriptfacts.Replicate)
	phMultikicker             = printedHeads(scriptfacts.Multikicker)
	phSquad                   = printedHeads(scriptfacts.Squad)
	phEvoke                   = printedHeads(scriptfacts.Evoke)
	phDash                    = printedHeads(scriptfacts.Dash)
	phOverload                = printedHeads(scriptfacts.Overload)
	phWarp                    = printedHeads(scriptfacts.Warp)
	phEmerge                  = printedHeads(scriptfacts.Emerge)
	phBestow                  = printedHeads(scriptfacts.Bestow)
	phMutate                  = printedHeads(scriptfacts.Mutate)
	phBuyback                 = printedHeads(scriptfacts.Buyback)
	phSuspend                 = printedHeads(scriptfacts.Suspend)
	phPlot                    = printedHeads(scriptfacts.Plot)
	phMorph                   = printedHeads(scriptfacts.Morph)
	phMegamorph               = printedHeads(scriptfacts.Megamorph)
	phDisguise                = printedHeads(scriptfacts.Disguise)
	phMayFlashCost            = printedHeads(scriptfacts.MayFlashCost)
	phAlternateAdditionalCost = printedHeads(scriptfacts.AlternateAdditionalCost)
	phImpending               = printedHeads(scriptfacts.Impending)
	phAll                     = printedHeads(scriptfacts.All)
)

// has reports whether any head in bits may be printed.
func (ph printedHeads) has(bits printedHeads) bool {
	return scriptfacts.Heads(ph).Has(scriptfacts.Heads(bits))
}

// printedHeadsOf returns f's printedHeads (scriptfacts.Of).
func printedHeadsOf(f *cards.Face) printedHeads { return printedHeads(scriptfacts.Of(f)) }

// verifyPrintedHeads panics when a clear bit of ph is contradicted by
// f.KeywordParam (scriptfacts.Verify).
func verifyPrintedHeads(f *cards.Face, ph printedHeads) {
	scriptfacts.Verify(f, scriptfacts.Heads(ph))
}

// foretellCost is the foretell alternative cost of a face
// (scriptfacts.ForetellCost).
func foretellCost(f *cards.Face) (Cost, bool) { return scriptfacts.ForetellCost(f) }

// triggerOptionalSpec is a printed trigger's CR 603.5 optionality spec
// (scriptfacts.TriggerOptionalSpec).
func triggerOptionalSpec(t cards.Trigger) string { return scriptfacts.TriggerOptionalSpec(t) }

// grantedTriggerHeads reports whether a head can be synthesized
// (scriptfacts.GrantedTriggerHeads).
func grantedTriggerHeads(head string) bool { return scriptfacts.GrantedTriggerHeads(head) }

// grantedKeywordTrigger synthesizes a granted keyword line's triggered
// ability (scriptfacts.GrantedKeywordTrigger).
func grantedKeywordTrigger(line string) *cards.Trigger {
	return scriptfacts.GrantedKeywordTrigger(line)
}
