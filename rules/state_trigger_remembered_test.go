package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestStateTriggerRemembersItsSourceNotTheCheckingEvent pins the round-10
// paymirror finding (random4 seed 12468 seq 7211, Homarid Spawning Bed:
// state_differs on G.Objs[*].Remembered[*].Obj, eventA/eventB trigger_push).
// A CR 603.8 state trigger (Mode$ Always) has no triggering event; the engine
// checks its condition on whichever event is being emitted when the state
// first matches. Veiled Crocodile ("when a player has no cards in hand")
// remembered the Island tapped inside the cast's CR 601.2g payment window
// on the planned route, and itself on the float route, where the check ran
// on an event naming no object. Its Remembered is now its source whatever
// event ran the check (stateTriggerRemembered).
func TestStateTriggerRemembersItsSourceNotTheCheckingEvent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		ev   func(land state.ObjID) events.Event
	}{
		{"tap_event_names_a_land", func(land state.ObjID) events.Event { return events.Event{Kind: events.Tap, Obj: land} }},
		{"event_names_no_object", func(state.ObjID) events.Event { return events.Event{Kind: events.Priority} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			watcher := onBoard(t, e, 0, "Name:State watcher\nTypes:Enchantment\n"+
				"T:Mode$ Always | LifeTotal$ You | LifeAmount$ GE1 | Execute$ Gain\n"+
				"SVar:Gain:DB$ GainLife | LifeAmount$ 1 | Defined$ You\nOracle:x\n")
			land := onBoard(t, e, 0, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n")
			e.checkFaceTriggers(e, tc.ev(land), nil, 0, 0, false, false, false)
			if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != watcher {
				t.Fatalf("Always queue = %+v, want one instance from %d", e.pendingTriggers, watcher)
			}
			ctx := e.pendingTriggers[0].Ctx
			want := []state.Target{{Obj: watcher}}
			if len(ctx.Remembered) != 1 || ctx.Remembered[0] != want[0] || len(ctx.Captured) != 1 || ctx.Captured[0] != want[0] {
				t.Fatalf("state trigger Remembered %v Captured %v, want %v (the source; the checking event's object is incidental)", ctx.Remembered, ctx.Captured, want)
			}
		})
	}
}
