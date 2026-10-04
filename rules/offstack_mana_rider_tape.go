package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// tapeManaRiderMayAsk reports whether activating obj's mana ability at
// priority may pose a rider ask: one of its available mana abilities carries
// a SubAbility$ chain the text allowlist says may ask (a target, a mode, a
// name, an arrangement). A mana ability never uses the stack (CR 605.3), so
// the rider resolves inside the activating Submit; that Submit is then a
// tape run (StartsResolution) so the rider's ask is posed and answered from
// the tape instead of taking its no-run default. A plain mana ability, or
// one whose rider is ask-free (a painland's damage), is no run at all.
func tapeManaRiderMayAsk(e *Engine, p state.PlayerID, obj state.ObjID) bool {
	o := e.G.Obj(obj)
	if o == nil {
		return false
	}
	f := o.Face()
	var svars map[string]string
	if f != nil {
		svars = f.SVars
	}
	for _, ma := range e.availableManaAbilitiesForWindow(p, obj, true) {
		if ma != nil && ma.Sub != nil && cards.SAChainMayAsk(ma.Sub, svars, f, false) {
			return true
		}
	}
	return false
}

// offStackTapeServed reports whether an ask inside the open off-stack mana
// frame is served from the tape: the frame was opened at the top level of a
// watched run (a priority mana activation tapeManaRiderMayAsk made a run, or
// a resolution activating one) with no payment window or cast open around
// it. Inside a window (cumulative upkeep, a triggered or unless cost) or a
// cast's payment the frame keeps its own routing.
func offStackTapeServed(e *Engine) bool {
	f := e.offStackMana
	return f != nil && e.tape.Watching() && e.cast == nil && !e.tapeWindowAsking &&
		!f.baseUnless && !f.baseCumulative && !f.baseTriggerCost && e.echo == nil
}
