package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("HealDamage", effHealDamage)
}

// HealMarkedDamage removes all damage marked on id by emitting the negative
// Damage event the Damage fold applies (the same "heal" mutation a
// regeneration shield and umbra armor perform, CR 701.19b/702.90). It is a
// no-op on an object with no marked damage or no object at all, so the caller
// never needs a precondition. Marked damage only lives on a battlefield
// permanent, so no zone gate is needed: an object in any other zone carries
// zero and the helper emits nothing.
func HealMarkedDamage(h Host, id state.ObjID) {
	o := h.Game().Obj(id)
	if o == nil || o.Damage <= 0 {
		return
	}
	h.Emit(events.Event{Kind: events.Damage, Obj: id, Amount: -o.Damage})
}

// effHealDamage is Forge's HealDamage effect: "all (other) damage already
// dealt to [the defined object] is healed." Two corpus carriers use it —
// Wolverine, Fierce Fighter's damage-replacement ("If damage would be dealt
// to NICKNAME, instead that damage is dealt, but all other damage already
// dealt to him is healed", Defined$ ReplacedTarget) and Pyramids (Defined$
// ReplacedCard). Targets come through the ordinary Defined read, so every
// spelling resolves the same way a sibling primitive's does.
//
// The mutation is a marked-damage reduction routed through events.Apply via
// Host.Emit (AGENTS.md: all state goes through events), never a direct write
// to state.Object.Damage.
func effHealDamage(h Host, c *Ctx, sa *cards.SA) {
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		HealMarkedDamage(h, t.Obj)
	}
}
