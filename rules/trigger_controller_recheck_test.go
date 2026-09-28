package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 603.4 rechecks an ordinary trigger as the controller of the ability
// already on the stack, even if its source changes hands before resolution.
func TestOrdinaryTriggerRecheckKeepsAbilityControllerAfterSourceStolen(t *testing.T) {
	t.Parallel()
	watcher := card(t, "Name:Life Watcher\nManaCost:0\nTypes:Creature\nPT:1/1\n"+
		"T:Mode$ Phase | Phase$ Upkeep | TriggerZones$ Battlefield | LifeTotal$ You | LifeAmount$ GE10 | Execute$ TrigDraw\n"+
		"SVar:TrigDraw:DB$ Draw | Defined$ You\nOracle:x\n")
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, watcher)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: watcher not on seat 0's battlefield: %+v", o)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -15})
	if e.G.Players[0].Life < 10 || e.G.Players[1].Life >= 10 {
		t.Fatalf("precondition: controller lives do not straddle threshold: %d, %d", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != id {
		t.Fatalf("precondition: watcher did not fire: %+v", e.pendingTriggers)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Controller != 0 {
		t.Fatalf("precondition: seat 0's trigger is not on stack: %v", e.G.Stack)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 1})
	if e.G.Obj(id).Controller != 1 || e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("precondition: source was not stolen on battlefield: %+v", e.G.Obj(id))
	}
	pre := countDraw(e)
	e.resolveTop()
	if got := countDraw(e) - pre; got != 1 {
		t.Fatalf("trigger drew %d cards, want 1: its stack controller still satisfies GE10", got)
	}
}
