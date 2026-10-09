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
// the static's CONTROLLER has max speed. This scan reads the condition itself
// rather than going through the layer walk, which deliberately does not emit a
// MaxSpeed AddAbility$ (rules/speed.go's maxSpeedAbilities owns the `granted`
// priority offer). Every other Condition$ keeps the scan's pre-existing
// behaviour: not read here.
func manaGrantConditionHolds(g *state.Game, sv staticView) bool {
	if !isMaxSpeedCondition(sv.ParamStr(cards.PKCondition)) {
		return true
	}
	return int(sv.Controller) < len(g.Players) && g.Players[sv.Controller].Speed >= maxSpeed
}
