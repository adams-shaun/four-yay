// Class trigger mode.
//
// Mode$ ClassLevelGained (CR 702.118c): "When this Class becomes level N".
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package trigmatch

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// classLevelGainedMatches implements "When this Class becomes level N"
// (CR 702.118c). The carrier event is the CounterChange that put the level
// counter -- the same event the Class's own level-up activator emits, and the
// same event Mode$ CounterAdded reads -- so the trigger needs no Class-specific
// event kind. ClassLevel$ N is the crossing gate the oracle text means: the
// trigger fires when the put takes the permanent's LEVEL total from below N to
// at least N, exactly the crossing semantics counterAddedMatches uses for
// CounterAmount$ (a batch that overshoots fires; a later put that lands on a
// level already reached does not re-fire). ValidCard$/ValidPlayer$ ride the
// shared eventCardAndPlayerMatch, and TriggerZones$ is the shared zoneGate the
// matcher dispatch already ran.
func classLevelGainedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.CounterChange || ev.Amount <= 0 {
		return false
	}
	if !strings.EqualFold(ev.Counter, "LEVEL") {
		return false
	}
	o := e.Game().Obj(ev.Obj)
	if o == nil {
		return false
	}
	if !eventCardAndPlayerMatch(e, t, source, ev.Obj, o.Controller) {
		return false
	}
	if raw := strings.TrimSpace(t.ParamStr(cards.PKClassLevel)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return false
		}
		after := o.Counter(ev.Counter)
		before := after - ev.Amount
		if before < 0 {
			before = 0
		}
		if !(before < int32(n) && after >= int32(n)) {
			return false
		}
	}
	return true
}

func init() {
	registerTrigMatcher(classLevelGainedMatches, "ClassLevelGained")
}
