package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// costStaticSelfOnly reports whether sv is a cost-modifier static whose
// ValidCard$ is exactly `Card.Self` -- the spelling costStaticGate refuses,
// target-independently, for every priced object other than the static's own
// Source. Any other spelling (`Card.Self+nonCreature`, a class, a missing
// ValidCard$) is not self-only: the gate chain must run for it.
func costStaticSelfOnly(sv staticView) bool {
	spec, has := sv.Param(cards.PKValidCard)
	return has && spec == "Card.Self"
}

// markCostSelfOnly sets staticView.selfOnly on every raise/reduce/set member
// of out (the three lists costModifiersCompose walks). The bit is a pure
// function of the view's own parameters, so every collection path that
// builds a costStaticViews marks it through markCostValidTarget.
func markCostSelfOnly(out *costStaticViews) {
	for _, list := range [...][]staticView{out.raise, out.reduce, out.set} {
		for i := range list {
			list[i].selfOnly = costStaticSelfOnly(list[i])
		}
	}
}

// costStaticsInertFor reports that no raise/reduce/set member can touch the
// pricing of object id: every member is self-only and sourced by some other
// object, so costStaticGate denies each one target-independently and the
// composition is the zero costMods whatever the targets (see
// offerFloorRefuses). An empty snapshot is trivially inert. A view that was
// never marked (selfOnly false) is never inert, so a hand-built snapshot
// falls back to the full composition.
func costStaticsInertFor(statics *costStaticViews, id state.ObjID) bool {
	for _, list := range [...][]staticView{statics.raise, statics.reduce, statics.set} {
		for i := range list {
			if !list[i].selfOnly || list[i].Source == id {
				return false
			}
		}
	}
	return true
}
