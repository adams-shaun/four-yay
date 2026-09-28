package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A nested replacement body must not overwrite the outer body's remembered
// slice before its remaining ReplaceEffect instruction rewrites the held event.
func TestHeroicSacrificeNestedRememberedBindingRestored(t *testing.T) {
	e := newSeats(t, 2)
	outer := onBoard(t, e, 0, "Name:Outer Recipient\nTypes:Creature\nPT:4/4\nOracle:x\n")
	inner := onBoard(t, e, 0, "Name:Inner Recipient\nTypes:Creature\nPT:4/4\nOracle:x\n")
	if outer == inner || e.G.Obj(outer).Zone != state.ZBattlefield || e.G.Obj(inner).Zone != state.ZBattlefield {
		t.Fatalf("distinct battlefield referents required: outer=%d inner=%d", outer, inner)
	}

	// Emulate the paused outer body immediately before it enters a nested
	// runReplaceWith: the saved slice has its own backing array (and spare
	// capacity), as it does after the outer body's Remembered walk. The
	// outer held event is restored after the nested invocation.
	e.replRemembered = make([]state.ObjID, 1, 2)
	e.replRemembered[0] = outer
	outerHeld := events.Event{Kind: events.Damage, Player: 1, Amount: 2}
	e.replacingEvent = &outerHeld
	innerHeld := events.Event{Kind: events.Damage, Player: 0, Amount: 1}
	body := &cards.SA{Kind: "DB", API: "ReplaceEffect", Params: map[string]string{
		"VarName": "Affected", "VarValue": "Remembered",
	}}
	e.runReplaceWith(&effects.Ctx{Source: inner, Controller: 0,
		Remembered: []state.Target{{Obj: inner}}}, 0, body, &innerHeld)
	if innerHeld.Obj != inner || innerHeld.Player != 0 {
		t.Fatalf("inner held event = %+v, want nested body redirected to %d", innerHeld, inner)
	}
	if e.replacingEvent != &outerHeld || len(e.replRemembered) != 1 {
		t.Fatalf("outer state not restored: event=%p remembered=%v", e.replacingEvent, e.replRemembered)
	}
	// The outer body now performs its own Affected rewrite. If the nested
	// body reused its slice backing array, the outer referent is now inner.
	e.ReplaceEvent("Affected", "Remembered", 0)
	if outerHeld.Obj != outer || outerHeld.Player != 0 {
		t.Fatalf("outer held event = %+v, want original outer referent %d after nested body", outerHeld, outer)
	}
}
