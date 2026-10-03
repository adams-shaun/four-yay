package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// LegalSubTargets implements effects.Host: the target census for a
// SubAbility$'s own ValidTgts$ asked while its parent resolves, with the
// parent's already-chosen targets bound for the Targeted*/ParentTarget
// referents (effects.SpecContext.ParentTargets). The binding is engine
// scratch set and restored inside this one synchronous census.
func (e *Engine) LegalSubTargets(chooser state.PlayerID, source state.ObjID, sa *cards.SA, parent []state.Target) []state.Target {
	saved, savedBound := e.subOfferParent, e.subOfferBound
	e.subOfferParent, e.subOfferBound = parent, true
	defer func() { e.subOfferParent, e.subOfferBound = saved, savedBound }()
	return e.LegalTargets(chooser, source, sa)
}
