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
// (CR 716). The carrier is the ClassLevelChange designation event, not
// CounterChange: gaining a level never adds a counter or fires CounterAdded.
// ClassLevel$ N fires on a crossing from below N to at least N; a later
// change above N does not re-fire. ValidCard$/ValidPlayer$ ride the
// shared eventCardAndPlayerMatch, and TriggerZones$ is the shared zoneGate the
// matcher dispatch already ran.
func classLevelGainedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.ClassLevelChange || ev.Amount <= 0 {
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
		after := o.ClassLevel()
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
