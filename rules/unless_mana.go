package rules

import (
	"github.com/adams-shaun/gorge/state"
)

// unlessManaWindowNeeded reports whether a mana-only unless cost should open
// the CR 601.2g mana-activation window: the cost has a mana component, the
// payer's pool (under the payment's own conversion set) cannot already pay
// it, and the payer controls at least one untapped mana source the window
// could supply. An already-payable cost, a non-mana cost, or a payer with no
// untapped source keeps today's exact path (charge straight from the pool),
// so no existing game moves merely because the window exists.
//
// Before this the mid-resolution unless arm charged the floating pool only:
// a payer with an untapped source and an empty pool could never pay, and a
// converted colour (CR 106.6, stat:ManaConvert) could never be produced by
// tapping.
func (e *Engine) unlessManaWindowNeeded(p state.PlayerID, cost Cost, obj state.ObjID) bool {
	if !cost.hasManaPayment() {
		return false
	}
	d := paymentDescriptor{id: obj, class: paymentOther, cost: &cost}
	// Ask whether the POOL ALONE pays (lifeGrant false): a {B} pip K'rrik's
	// PayLifeInsteadOf:B grant could settle with 2 life is not yet covered by
	// the pool, so the window must still open to offer the untapped source.
	// The grant-bearing payment paths (payUnlessCost, advanceUnlessPayment's
	// charge) keep the grant, so answering Done still spends the life.
	if e.costPayableClassLife(p, d, pipRider{}, cost, false) {
		return false
	}
	return e.hasUntappedManaSource(p)
}

// askUnlessWardMana opens the unless-cost activation window by delegating to
// the ONE mid-resolution mana-window owner (askWardMana), which owns the
// resume-state writes (ruling T21-e). The window's Done answer charges the
// cost through payUnlessCost -- with the resolving object's ManaConvert
// conversion -- and re-enters the asking SA with ctx.UnlessPay set, exactly
// as the arm's ordinary pool-only charge does.
func (e *Engine) askUnlessWardMana(payer state.PlayerID, cost Cost, rp *resumePoint) {
	e.askWardMana(rp, &wardManaPayment{payer: payer, cost: cost, target: rp.target,
		resumeKind: "unless_mana", prompt: "Activate mana abilities to pay the unless cost",
		unless: true})
}
