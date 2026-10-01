package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The trigger-walk plan: a whole-board answer for an event no object is
// referent of.
//
// A live walk (forEachTriggerObject with kindOnly) validates every summary of
// every living seat's walked zones before it visits anything, so at its end
// the summaries describe the whole board: every object that can act on some
// event from where it sits is in a hotIDs list, with its signature. The plan
// records, after such a walk, the zone list headers the summaries were
// confirmed against, the summary generation (trigZoneGen: bumped by every
// summary change), and the union of every hot signature. A later walk whose
// catch-up changed no summary and whose zone lists are still those headers
// reads the same summaries; when its event has no referent, the walk can
// visit only hot objects whose signature admits the event -- and when the
// union does not admit it, nothing at all, so the walk returns before
// touching a single summary. A StepChange into a step the battlefield is
// walked for (stepWalksBattlefield) is never answered by the plan: the
// synthesized step triggers read derived state no signature records.
//
// trigZoneSkipVerify runs the full walk instead and panics if it visits.

// trigPlanSlots bounds the plan's header record: 4 seats of
// trigZoneSlots zones (the stack once). A larger table never builds a plan.
const trigPlanSlots = 4 * trigZoneSlots

type trigPlanHead struct {
	data *state.ObjID
	n    int32
	seat state.PlayerID
}

func trigHeadOf(seat state.PlayerID, cur []state.ObjID) trigPlanHead {
	h := trigPlanHead{n: int32(len(cur)), seat: seat}
	if len(cur) > 0 {
		h.data = &cur[0]
	}
	return h
}

// trigPlan is the Engine's plan record (see above). A recorded header's
// array cannot be recycled for another list while the plan holds: at build
// each header is its summary's own live header, which pins the array
// (trigZoneSummary.live), and every change to a summary -- its live header
// included -- bumps trigZoneGen, which the plan must still match.
type trigPlan struct {
	ok    bool
	gen   uint64
	n     int
	union trigSig
	heads [trigPlanSlots]trigPlanHead
}

// trigPlanHolds reports whether the recorded plan still describes the board:
// no summary changed since it was built and every walked zone list is the
// recorded one.
func (e *Engine) trigPlanHolds() bool {
	pl := &e.trigPlan
	if !pl.ok || pl.gen != e.trigZoneGen {
		return false
	}
	i, first := 0, true
	for si := range e.G.Players {
		p := state.PlayerID(si)
		if e.G.Players[p].Lost {
			continue
		}
		for _, z := range objectWalkZones {
			if z == state.ZStack && !first {
				continue
			}
			if i >= pl.n || pl.heads[i] != trigHeadOf(p, e.G.Zone(z, p)) {
				return false
			}
			i++
		}
		first = false
	}
	return i == pl.n
}

// trigPlanBuild records the plan after a fully validated live walk.
func (e *Engine) trigPlanBuild() {
	pl := &e.trigPlan
	pl.ok = false
	var union trigSig
	i, first := 0, true
	for si := range e.G.Players {
		p := state.PlayerID(si)
		if e.G.Players[p].Lost {
			continue
		}
		for _, z := range objectWalkZones {
			if z == state.ZStack && !first {
				continue
			}
			if i >= trigPlanSlots {
				return
			}
			cur := e.G.Zone(z, p)
			k := int(p)*trigZoneSlots + trigZoneSlot(z)
			if k >= len(e.trigZones) || !e.trigZones[k].valid || !sameZoneList(e.trigZones[k].live, cur) {
				return
			}
			union = union.or(e.trigZones[k].union)
			pl.heads[i] = trigHeadOf(p, cur)
			i++
		}
		first = false
	}
	pl.n, pl.union, pl.gen, pl.ok = i, union, e.trigZoneGen, true
}

// trigNoReferent reports whether ev names no object.
func trigNoReferent(ev *events.Event) bool {
	return ev.Obj == 0 && len(ev.IDs) == 0 && len(ev.Pairs) == 0
}
