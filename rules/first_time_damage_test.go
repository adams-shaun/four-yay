package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// FirstTime$ on DamageDoneOnce admits one damage batch per recipient per turn.
func TestDamageDoneOnceFirstTimeOnlyTriggersOnFirstDamage(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	src := "Name:First-Hit Counter\nManaCost:1 G\nTypes:Creature\nPT:2/5\n" +
		"T:Mode$ DamageDoneOnce | ValidTarget$ Card.Self | FirstTime$ True | TriggerZones$ Battlefield | Execute$ TrigPump\n" +
		"SVar:TrigPump:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ X\n" +
		"SVar:X:TriggerCount$DamageAmount\nOracle:x\n"
	e, cfg := combatTriggerBoard(t, reg, nil, []string{src}, nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	carrier := findBattlefield(t, e, 0, "First-Hit Counter", 0)
	if o := e.G.Obj(carrier); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("trigger carrier = %+v, want battlefield object", o)
	}

	e.emit(events.Event{Kind: events.Damage, Obj: carrier, Amount: 2})
	if got := e.G.Obj(carrier).Damage; got != 2 {
		t.Fatalf("first damage = %d, want 2", got)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pending triggers after first hit = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if got := e.G.Obj(carrier).Counter("P1P1"); got != 2 {
		t.Fatalf("counters after first hit = %d, want 2", got)
	}

	e.emit(events.Event{Kind: events.Damage, Obj: carrier, Amount: 3})
	if got := e.G.Obj(carrier).Damage; got != 5 {
		t.Fatalf("damage after both hits = %d, want 5 (both damage events must apply)", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("pending triggers after second hit = %d, want 0 (FirstTime$ must suppress the later batch)", len(e.pendingTriggers))
	}
	if got := e.G.Obj(carrier).Counter("P1P1"); got != 2 {
		t.Fatalf("counters after second hit = %d, want unchanged 2", got)
	}
	replayCheck(t, e, cfg)
}
