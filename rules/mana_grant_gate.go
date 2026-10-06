package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// manaGrantConditionHolds reports whether a Continuous static's own existence
// condition lets its AddAbility$ mana member join the authoritative mana
// membership (appendAvailableManaAbilitiesGate's direct scan). The scan is the
// one home both the pass-scoped offer walk and the fresh activation/payment
// readers go through, so gating here keeps them in agreement; ignorePayable
// skips payability only and never reaches this gate.
//
// A "Max speed --" grant (CR 702.179e, Condition$ MaxSpeed) exists only while
// the static's CONTROLLER has max speed. continuousConditionHolds cannot be
// reused for it: that helper deliberately fails closed for MaxSpeed (the
// layer walk must not emit these grants -- rules/speed.go's maxSpeedAbilities
// owns the `granted` priority offer), so it would drop the legal speed-4
// member as well. Every other Condition$ keeps the scan's pre-existing
// behaviour: not read here.
func manaGrantConditionHolds(g *state.Game, sv staticView) bool {
	if !isMaxSpeedCondition(sv.ParamStr(cards.PKCondition)) {
		return true
	}
	return int(sv.Controller) < len(g.Players) && g.Players[sv.Controller].Speed >= maxSpeed
}
