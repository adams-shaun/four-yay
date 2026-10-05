package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Unattach", effUnattach) }

// effUnattach detaches each defined attachment still fastened to a permanent.
// The former bearer travels on the event for Unattached trigger LKI and replay.
func effUnattach(h Host, c *Ctx, sa *cards.SA) {
	selector := DefinedRefOf(sa).Raw
	targets, ok := knownDefinedTargets(h, c, selector)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "unimplemented Unattach selector: " + selector})
		return
	}
	seen := make(map[state.ObjID]bool)
	for _, target := range targets {
		if target.IsPlayer || target.Obj == 0 || seen[target.Obj] {
			continue
		}
		seen[target.Obj] = true
		o := h.Game().Obj(target.Obj)
		if o == nil || o.Zone != state.ZBattlefield || o.AttachedTo == 0 {
			continue
		}
		h.Emit(events.Event{Kind: events.Unattached, Obj: target.Obj, IDs: []state.ObjID{o.AttachedTo}})
	}
}
