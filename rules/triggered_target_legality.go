package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TargetableObjects publishes the battlefield objects a triggering spell can
// legally target. effects consumes this immutable data snapshot rather than
// calling back into rules from a filter or storing an Engine pointer in Game.
func (e *Engine) TargetableObjects(triggerCard state.ObjID) []state.ObjID {
	spell := e.G.Obj(triggerCard)
	if spell == nil || spell.Zone != state.ZStack {
		return nil
	}
	var sa *cards.SA
	if spell.Ability != nil {
		if effects.TargetsOf(spell.Ability).Targeted() {
			sa = spell.Ability
		}
	} else if face := spell.Face(); face != nil {
		sa = face.SpellAbility()
	}
	if sa == nil || !effects.TargetsOf(sa).Targeted() {
		return nil
	}
	candidates := e.legalTargetCandidates(spell.Controller, spell.ID, spell.ID, sa)
	out := make([]state.ObjID, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.kind == "permanent" {
			out = append(out, candidate.obj)
		}
	}
	return out
}
