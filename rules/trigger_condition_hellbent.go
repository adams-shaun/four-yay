package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// hellbentHolds is the one reading of "you have no cards in hand" (the
// Hellbent ability word): the Continuous static gate (layers_static.go
// condHellbent) and the trigger-side intervening-if below both call it, so
// the two spellings cannot drift apart.
func (e *Engine) hellbentHolds(p state.PlayerID) bool {
	return len(e.G.Zone(state.ZHand, p)) == 0
}

// triggerHellbentHolds evaluates a T: line's Hellbent$ clause as a CR 603.4
// intervening-if ("if you have no cards in hand"). Measured at this pin: 5
// corpus T: lines -- Case of the Crimson Pulse's "To solve" end-step
// trigger, Lyzolda's avatar end step, Headless Specter's and Jagged
// Poppet's combat-damage lines, Slaughterhouse Bouncer's dies line -- which
// before this clause fired UNCONDITIONALLY. An absent clause holds; a value other than True is an
// unreadable shape and fails closed like Threshold$/Metalcraft$. It is
// re-checked at resolution through triggerResolvingCheckHolds.
func (e *Engine) triggerHellbentHolds(t cards.Trigger, you state.PlayerID) bool {
	v, ok := t.Param(cards.PKHellbent)
	if !ok {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(v), "True") && e.hellbentHolds(you)
}
