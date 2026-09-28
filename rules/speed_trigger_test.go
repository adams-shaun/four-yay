package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// CR 702.179d: the ability has no source, is controlled by the active
// speed-holding player, triggers once on that player's turn and uses the stack.
func TestSpeedInherentTrigger(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngineThree(t, reg,
		[]*cards.Card{lookup(t, reg, "Amonkhet Raceway")},
		[]*cards.Card{lookup(t, reg, "Amonkhet Raceway")}, nil)
	moveByName(t, e, 0, "Amonkhet Raceway", state.ZBattlefield)
	moveByName(t, e, 1, "Amonkhet Raceway", state.ZBattlefield)
	if e.G.Active != 0 || e.G.Players[0].Speed != 1 || e.G.Players[1].Speed != 1 {
		t.Fatalf("precondition: active=%d speeds=%d,%d", e.G.Active, e.G.Players[0].Speed, e.G.Players[1].Speed)
	}
	// On seat 0's turn, seat 1 cannot gain speed even if seat 2 loses life.
	e.emit(events.Event{Kind: events.LifeChange, Player: 2, Amount: -1})
	if len(e.pendingTriggers) != 1 || !e.pendingTriggers[0].SpeedIncrease || e.pendingTriggers[0].Controller != 0 {
		t.Fatalf("precondition: missing active seat's trigger: %+v", e.pendingTriggers)
	}
	if e.G.Players[0].Speed != 1 || e.G.Players[1].Speed != 1 {
		t.Fatalf("speed changed before resolution: %d,%d", e.G.Players[0].Speed, e.G.Players[1].Speed)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 2, Amount: -1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("second loss queued %d triggers, want 1", len(e.pendingTriggers))
	}
	e.priorityRound()
	if len(e.G.Stack) != 1 {
		t.Fatalf("trigger not on stack: %+v", e.G.Stack)
	}
	o := e.G.Obj(e.G.Stack[0])
	if o == nil || o.Source != 0 || o.Controller != 0 || o.Ability == nil || o.Ability.API != "SpeedIncrease" {
		t.Fatalf("precondition: inherent trigger not source-less/respondable: %+v", o)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 2, Amount: -1})
	if len(e.pendingTriggers) != 0 || len(e.G.Stack) != 1 {
		t.Fatalf("second trigger after placement: queue=%d stack=%d", len(e.pendingTriggers), len(e.G.Stack))
	}
	passUntilStackEmpty(t, e, 30)
	if e.G.Players[0].Speed != 2 || e.G.Players[1].Speed != 1 {
		t.Fatalf("resolved speeds=%d,%d, want 2,1", e.G.Players[0].Speed, e.G.Players[1].Speed)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 2, Amount: -1})
	if len(e.pendingTriggers) != 0 || e.G.Players[0].Speed != 2 {
		t.Fatalf("second trigger after resolution: queue=%d speed=%d", len(e.pendingTriggers), e.G.Players[0].Speed)
	}
	// Turn 2: seat 0 is an opponent, but only seat 1 may trigger.
	for driveToTurn(t, e, 2, 1) {
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 2, Amount: -1})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Controller != 1 {
		t.Fatalf("wrong turn's trigger: %+v", e.pendingTriggers)
	}
	e.priorityRound()
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority before speed resolves: %+v", d)
	}
	passUntilStackEmpty(t, e, 30)
	if e.G.Players[0].Speed != 2 || e.G.Players[1].Speed != 2 {
		t.Fatalf("opponent-turn speed increase: %d,%d", e.G.Players[0].Speed, e.G.Players[1].Speed)
	}
}
