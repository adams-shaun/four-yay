package pay

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TapCreaturesForMana is the ability's explicit alternative payment, not a
// cost reduction: each chosen untapped creature replaces one generic mana
// while paying that activation. ConvokeAsk records the choice and the usual
// payment commit emits Tap events. Summoning sickness does not bar this tap:
// the creature is not activating its own {T} ability (CR 302.6).
func TapCreaturesForMana(sa *cards.SA) bool {
	if sa == nil || sa.Kind != "AB" {
		return false
	}
	return effects.ActivationOf(sa).Has(effects.ActTapCreaturesForMana)
}

// CreatureManaCandidates is shared by the offer credit and the payment ask.
// The activation source is excluded when its own {T} cost would double-tap it.
func CreatureManaCandidates(g *state.Game, p state.PlayerID, source state.ObjID, sourceTaps bool) []state.ObjID {
	var ids []state.ObjID
	for _, id := range g.Zone(state.ZBattlefield, p) {
		o := g.Obj(id)
		if o == nil || o.Tapped || (sourceTaps && id == source) || o.Face() == nil || o.BestowedAttached() || o.ReconfiguredAttached() || !o.EffectiveIsCreature() {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
