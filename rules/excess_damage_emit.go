package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// excessDamageHost is the narrow pre-fold layer/fold surface needed to measure
// lethal damage. The game itself is changed only by foldEntryMove and emit.
type excessDamageHost interface {
	Game() *state.Game
	IsCreature(state.ObjID) bool
	Toughness(state.ObjID) int32
	inFlightDamageSource() state.ObjID
	hasKeywordH(state.ObjID, kwHead) bool
	foldEntryMove(events.Event) (events.Event, []string)
	emit(events.Event) events.Event
}

// foldAndMarkExcess reads the recipient's remaining lethal amount BEFORE
// folding the replacement-adjusted Damage, then publishes its history.
func foldAndMarkExcess(h excessDamageHost, ev events.Event, damaging state.ObjID) events.Event {
	var lethal, victimType int32
	var controller state.PlayerID
	hasLethal := false
	if ev.Kind == events.Damage && ev.Amount > 0 && ev.Obj != 0 {
		if o := h.Game().Obj(ev.Obj); o != nil && o.Zone == state.ZBattlefield {
			controller = o.Controller
			creature := h.IsCreature(ev.Obj)
			if creature {
				victimType |= 1
			}
			if f := o.Face(); f != nil && f.IsPlaneswalker() {
				victimType |= 2
			}
			src := h.inFlightDamageSource()
			if src == 0 {
				src = damaging
			}
			lethal, hasLethal = excessDamageLethal(o, creature, h.Toughness(ev.Obj), src != 0 && h.hasKeywordH(src, kwhDeathtouch))
		}
	}
	stored, _ := h.foldEntryMove(ev)
	if stored.Kind == events.Damage && hasLethal && stored.Amount > lethal {
		h.emit(events.Event{Kind: events.ExcessDamage, Obj: stored.Obj, Player: controller, Amount: victimType})
	}
	return stored
}
