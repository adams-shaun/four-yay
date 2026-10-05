package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// emitChangeZoneCounters applies every counter kind in Forge's comma-delimited
// WithCountersType$ list. The order is the script order, keeping replay and
// replacement processing deterministic.
func emitChangeZoneCounters(h Host, id state.ObjID, kinds string, amount int32) {
	for _, kind := range strings.Split(kinds, ",") {
		if kind = strings.TrimSpace(kind); kind != "" {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: kind, Amount: amount})
		}
	}
}
