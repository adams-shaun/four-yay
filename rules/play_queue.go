package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// queuedPlays is the not-yet-begun remainder of one answered Play (Etali,
// Primal Storm's Amount$ All "cast any number of spells from among them"),
// with the riders every card of that answer is begun under.
type queuedPlays struct {
	player           state.PlayerID
	ids              []state.ObjID
	free             bool
	playCost         string
	replaceGraveyard bool
	copyCard         bool
	// imprintOn is the ImprintPlayed$ True source (0 = no imprint): every
	// card the play actually begins is imprinted on it.
	imprintOn state.ObjID
	// cont is the parked resolution continuation (kind "play_resume") the
	// queue resumes once every chosen card's cast is complete.
	cont *resumePoint
}

func (q *queuedPlays) clone() *queuedPlays {
	if q == nil {
		return nil
	}
	c := *q
	c.ids = append([]state.ObjID(nil), q.ids...)
	c.cont = cloneResume(q.cont)
	return &c
}

// runPlays begins q's cards in order. A cast that parks on its own question
// (e.cast still in flight, a pending decision, or a suspended resolution)
// stops the walk; q stays in e.queuedPlays while it still holds cards to
// begin or a parked continuation. It reports whether any card was begun.
func (e *Engine) runPlays(q *queuedPlays) bool {
	begun := false
	for len(q.ids) > 0 {
		id := q.ids[0]
		q.ids = q.ids[1:]
		from := state.Zone(0)
		if o := e.G.Obj(id); o != nil {
			from = o.Zone
		}
		e.beginPlay(q.player, id, q.free, q.playCost, q.replaceGraveyard, q.copyCard)
		begun = true
		if q.imprintOn != 0 && from.Valid() {
			if o := e.G.Obj(id); o != nil && o.Zone != from {
				e.emit(events.Event{Kind: events.Imprint, Obj: q.imprintOn,
					IDs: []state.ObjID{id}})
			}
		}
		if e.Suspended() || e.cast != nil || e.pending != nil {
			break
		}
	}
	if len(q.ids) > 0 || q.cont != nil {
		e.queuedPlays = q
	} else {
		e.queuedPlays = nil
	}
	return begun
}

// startQueuedPlay drains e.queuedPlays before priority, the suspendedCasts /
// defeatedCasts shape (rules/turn.go): once the parked cast that stopped
// runPlays has completed and nothing is pending, the next chosen card is
// begun; when none is left, the parked resolution continuation resumes.
func (e *Engine) startQueuedPlay() bool {
	q := e.queuedPlays
	if q == nil || e.cast != nil || e.pending != nil || e.Suspended() {
		return false
	}
	if len(q.ids) > 0 {
		e.runPlays(q)
		return true
	}
	e.queuedPlays = nil
	if q.cont != nil {
		e.resumeResolution(q.cont, nil)
	}
	return true
}
