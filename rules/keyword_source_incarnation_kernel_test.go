package rules

// Kernel-era restorations of the keyword_source_incarnation_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestGrantedWardSurvivesSourceRemoval is the CR 112.7a regression: a
// layer-6 granted Ward's ability is a real triggered ability, and its body
// counters the targeting spell (which it reads off its TriggerContext), never
// the source permanent. Removing the warded creature in response to its own
// ward trigger -- and returning it as a NEW incarnation (CR 400.7) -- must
// not fizzle the ability. Without the fix events.Apply stamps every keyword
// trigger except Gift, so resolveTop's incarnation gate drops this one and
// the ward never asks.
func TestGrantedWardSurvivesSourceRemoval(t *testing.T) {
	t.Parallel()
	e, bear := hexingEngine(t)

	// Precondition: this is the GRANTED ward (a printed K:Ward expands to a
	// face trigger, TriggerPush, and never mints this stack object).
	if !e.HasKeyword(bear, "Ward") {
		t.Fatal("bear has no granted Ward; the scenario is not the granted path")
	}
	if e.G.Obj(bear).Face().HasKeyword("Ward") {
		t.Fatal("bear prints Ward; the grant scenario is not the granted path")
	}

	// Put a real targeting spell on the stack at the warded bear so the
	// granted Ward trigger queues (the printed-ward fixture's low-level
	// emission, which isolates the trigger from the cast flow's own asks).
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	wardID := stackAbilityForSource(e, bear, "Ward")
	if wardID == 0 {
		t.Fatalf("no granted Ward ability on the stack: %v", e.G.Stack)
	}
	// The correct KeywordTriggerPush body: the rebuilt DB$ Ward carries the
	// PayLife<2> UnlessCost and is sourced by the warded creature.
	if got := e.G.Obj(wardID).Ability.Params["UnlessCost"]; got != "PayLife<2>" {
		t.Fatalf("ward ability UnlessCost = %q, want PayLife<2>", got)
	}

	// Source leaves and returns as a new object (CR 400.7) before resolution.
	before := e.G.Obj(bear).Incarnation
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZGraveyard, To: state.ZBattlefield})
	if after := e.G.Obj(bear).Incarnation; after == before {
		t.Fatalf("precondition: incarnation did not change (%d -> %d)", before, after)
	}

	kr6ResolveTop(e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.Player != 1 {
		t.Fatalf("granted ward fizzled on source removal: no pay ask (pending %+v, stack %v)", d, e.G.Stack)
	}
	// Decline: the ward itself counters the spell it captured.
	submitChoices(t, e, d.Options[len(d.Options)-1].Index)
	passUntilStackEmpty(t, e, 20)

	if zone := e.G.Obj(cause).Zone; zone != state.ZGraveyard {
		t.Fatalf("targeting spell zone = %s, want graveyard (warded out)", zone)
	}
	counteredByWard := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == cause && ev.Text == "countered by ward" {
			counteredByWard = true
		}
	}
	if !counteredByWard {
		t.Fatal("no 'countered by ward' move: the outcome was not the ward's triggered effect")
	}
	if zone := e.G.Obj(bear).Zone; zone != state.ZBattlefield {
		t.Fatalf("warded creature zone = %s, want battlefield (the bolt was countered)", zone)
	}
}
