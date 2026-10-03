package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The K:Devour expansion (CR 702.83), pinned on real corpus carriers:
//
//   - Gorger Wurm (Devour 1, creatures): the ETB replacement poses the real
//     "sacrifice any number of creatures" KChoose (Optional$ -> Min 0, Max =
//     eligible count), the answered creatures are sacrificed, and the wurm
//     enters with one counter per devoured creature; a decline enters it
//     plain.
//   - Thunder-Thrash Elder (Devour 3) and Thromok the Insatiable (Devour X):
//     the per-devouree multiplier -- Times.3, and Times.X, whose X resolves
//     the face's own SVar (Count$RememberedSize, the devoured count), so n
//     devoured creatures give n counters each, n² in all.
//   - Feasting Hobbit (Devour 3 Food): the typed filter -- only Foods are
//     offered and counted, non-Food permanents are not.

// devourFodder places the named cards (already in seat 0's hand) on the
// battlefield and returns their ids in battlefield zone order.
func devourFodder(t *testing.T, e *Engine, names ...string) []state.ObjID {
	t.Helper()
	var ids []state.ObjID
	for _, name := range names {
		id := searchMoveByName(t, e, name, state.ZHand)
		placeOnBattlefield(t, e, id)
		ids = append(ids, id)
	}
	return ids
}

// enterDevourer emits the devourer's hand->battlefield entry (the ETB
// replacement fires on the Move) and returns the pending decision -- the
// sacrifice ask the replacement's body poses.
func enterDevourer(t *testing.T, e *Engine, name string) *decision.Decision {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	return e.Pending()
}

// idOf finds a seat-0 battlefield object by face name.
func idOf(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("no battlefield object named %q", name)
	return 0
}
