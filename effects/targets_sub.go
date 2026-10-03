package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// isLaterTargetedSub reports whether sa is a ValidTgts$ SubAbility$ reached
// during a resolution whose placement/announcement ask covered a DIFFERENT
// SA (Ctx.OfferedSA names it): Ctx.Targets then belong to that earlier SA --
// they are this sub's PARENT targets, never its own.
func isLaterTargetedSub(c *Ctx, sa *cards.SA) bool {
	return c.OfferedSA != nil && sa.Line != c.OfferedSA.Line
}

// subAskCandidates is the target census both mid-resolution ValidTgts$ asks
// (chosenTargetsFor, changeZoneChosenTargets) offer from. For a later sub of
// a targeted parent it binds the parent's chosen targets, so a spec that
// names them -- `Equipment.AttachedTo ParentTarget`, `Creature.ControlledBy
// ParentTarget`, `Any.NotDefinedParentTarget` -- is judged against the object
// the earlier instruction targeted instead of failing closed to no candidate
// (CR 115.1: each target is chosen as its own instruction's "target").
func subAskCandidates(h Host, c *Ctx, sa *cards.SA) []state.Target {
	if isLaterTargetedSub(c, sa) {
		return h.LegalSubTargets(c.Controller, c.Source, sa, c.Targets)
	}
	return h.LegalTargets(c.Controller, c.Source, sa)
}

// noSubTargets is the "nothing eligible" outcome of a mid-resolution
// ValidTgts$ ask. For a later sub of a targeted parent it is a HANDLED empty
// set: the caller's Defined fallthrough reads Ctx.Targets, which are the
// PARENT's, so declining to handle it made the sub act on the parent's target
// -- Fiery Annihilation's "exile up to one target Equipment" exiled the
// creature it had just damaged whenever no Equipment could be chosen. Every
// other shape keeps the caller's own Defined behaviour.
func noSubTargets(c *Ctx, sa *cards.SA) ([]state.Target, bool) {
	if isLaterTargetedSub(c, sa) {
		return []state.Target{}, true
	}
	return nil, false
}
