package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// hellbentHolds is the one reading of "you have no cards in hand" (the
// Hellbent ability word): the Continuous static gate (layers_static.go
// condHellbent) and the trigger-side intervening-if below both call it, so
// the two spellings cannot drift apart.
func hellbentHolds(g *state.Game, p state.PlayerID) bool {
	return len(g.Zone(state.ZHand, p)) == 0
}

// triggerHellbentHolds evaluates a T: line's Hellbent$ clause as a CR 603.4
// intervening-if ("if you have no cards in hand"). Measured at this pin: 5
// corpus T: lines -- Case of the Crimson Pulse's "To solve" end-step
// trigger, Lyzolda's avatar end step, Headless Specter's and Jagged
// Poppet's combat-damage lines, Slaughterhouse Bouncer's dies line -- which
// before this clause fired UNCONDITIONALLY. An absent clause holds; a value
// other than True is an unreadable shape and fails closed like
// Threshold$/Metalcraft$. It is re-checked at resolution through
// triggerResolvingCheckHolds.
func triggerHellbentHolds(g *state.Game, t cards.Trigger, you state.PlayerID) bool {
	if _, ok := t.Param(cards.PKHellbent); !ok {
		return true
	}
	return trigmatch.ParamTrue(t, cards.PKHellbent) && hellbentHolds(g, you)
}
