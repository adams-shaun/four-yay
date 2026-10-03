package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// stat:IgnorePlaneswalkerZeroLoyaltyRule (Sanctum Lurker: "Planeswalkers you
// control aren't put into their owners' graveyards for having 0 loyalty.") is
// read by planeswalkerZeroLoyalty, the CR 704.5i sweep: an exempt walker is
// skipped before it joins the batch, exactly as IgnoreLegendRule's exempt
// legends are skipped before grouping (CR 704.5j).
func init() { effects.RegisterNonAPI("stat:IgnorePlaneswalkerZeroLoyaltyRule") }

// zeroLoyaltyExempt reports whether one of the given live
// IgnorePlaneswalkerZeroLoyaltyRule statics exempts walker id from CR 704.5i.
// The statics share the IgnoreLegendRule grammar -- each static's "as long
// as" gate through continuousGateHolds, then ValidCard$ matched against the
// candidate with THAT static's source and controller (so Sanctum Lurker's
// Planeswalker.YouCtrl covers only its controller's walkers), an absent
// ValidCard$ exempting every candidate -- so the one evaluator,
// legendRuleExempt, answers both.
func (e *Engine) zeroLoyaltyExempt(statics []staticView, id state.ObjID) bool {
	return e.legendRuleExempt(statics, id)
}
