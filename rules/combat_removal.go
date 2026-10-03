package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// removeNoncreatureCombatants applies CR 506.4's "an attacking or blocking
// creature that ... stops being a creature" removal: Imprisoned in the Moon
// enchanting an attacking Craw Wurm makes it a noncreature land, which stops
// attacking and deals no combat damage. CR 506.4's other cases are already
// folded where they happen: leaving the battlefield (events.Move), a control
// change (changeControl), phasing out (the PhaseOut fold), regeneration and an
// effect that removes it (EndCombatReset{Obj}).
//
// It runs in the state-based-action pass loop, which every priority window
// and every step's turn-based actions go through, so the removal lands before
// anyone acts on the changed board and before combat damage is assigned. It
// is not itself a state-based action, but it shares the property that makes
// that home correct: it reads only the current derived types and combat
// state, poses no choice and its result is idempotent. Each removed permanent
// gets the same EndCombatReset{Obj} event regeneration and RemoveFromCombat
// use -- it stops attacking/blocking, and an attacker it blocked stays
// blocked (CR 506.4 / 509.1h). The scan is in battlefield seat order, so the
// emitted order is deterministic.
func (e *Engine) removeNoncreatureCombatants() bool {
	var blockers []state.ObjID
	if e.G.BlockersLive() {
		for _, p := range e.G.AliveFrom(0) {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				if o := e.G.Obj(id); o != nil {
					for _, b := range o.BlockedBy {
						if b != 0 {
							blockers = append(blockers, b)
						}
					}
				}
			}
		}
	}
	var out []state.ObjID
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.PhasedOut {
				continue
			}
			if !o.IsAttacking && !containsObjID(blockers, id) {
				continue
			}
			if e.IsCreature(id) {
				continue
			}
			out = append(out, id)
		}
	}
	for _, id := range out {
		e.emit(events.Event{Kind: events.EndCombatReset, Obj: id})
	}
	return len(out) > 0
}
