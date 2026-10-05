package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// foldAndMarkExcess reads the recipient's remaining lethal amount BEFORE
// folding the replacement-adjusted Damage, then publishes its history.
func (e *Engine) foldAndMarkExcess(ev events.Event) events.Event {
	var lethal, victimType int32
	var controller state.PlayerID
	hasLethal := false
	if ev.Kind == events.Damage && ev.Amount > 0 && ev.Obj != 0 {
		if o := e.G.Obj(ev.Obj); o != nil && o.Zone == state.ZBattlefield {
			controller = o.Controller
			creature := e.IsCreature(ev.Obj)
			if creature {
				victimType |= 1
			}
			if f := o.Face(); f != nil && f.IsPlaneswalker() {
				victimType |= 2
			}
			src := e.inFlightDamageSource()
			if src == 0 {
				src = e.damaging
			}
			lethal, hasLethal = excessDamageLethal(o, creature, e.Toughness(ev.Obj), src != 0 && e.hasKeywordH(src, kwhDeathtouch))
		}
	}
	stored, _ := e.foldEntryMove(ev)
	if stored.Kind == events.Damage && hasLethal && stored.Amount > lethal {
		e.emit(events.Event{Kind: events.ExcessDamage, Obj: stored.Obj, Player: controller, Amount: victimType})
	}
	return stored
}
