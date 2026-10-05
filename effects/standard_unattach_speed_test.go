package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestStandardUnattachDefinedEquipment(t *testing.T) {
	h, c, ids := attachBoard(t)
	h.Emit(events.Event{Kind: events.Attach, Obj: ids["eq"], IDs: []state.ObjID{ids["bear"]}})
	if h.g.Obj(ids["eq"]).AttachedTo != ids["bear"] {
		t.Fatal("precondition: equipment not attached")
	}
	c.Remembered = []state.Target{{Obj: ids["eq"]}, {Obj: ids["bear"]}}
	before := len(h.log)
	Resolve(h, c, sa(t, "DB$ Unattach | Defined$ Remembered"))
	if h.g.Obj(ids["eq"]).AttachedTo != 0 || h.g.Obj(ids["eq"]).LastBearer != ids["bear"] {
		t.Fatalf("equipment not detached: %+v", h.g.Obj(ids["eq"]))
	}
	if len(h.log) != before+1 || h.log[before].Kind != events.Unattached || h.log[before].Obj != ids["eq"] || len(h.log[before].IDs) != 1 || h.log[before].IDs[0] != ids["bear"] {
		t.Fatalf("detach events: %+v", h.log[before:])
	}
}

func TestStandardChangeSpeedStartsEnginesAndKeepsMinimum(t *testing.T) {
	h, c, _ := attachBoard(t)
	c.Remembered = []state.Target{{Player: 1, IsPlayer: true}}
	Resolve(h, c, sa(t, "DB$ ChangeSpeed | Mode$ Increase | Defined$ Remembered"))
	if h.g.Players[1].Speed != 1 {
		t.Fatalf("start engines = %d, want 1", h.g.Players[1].Speed)
	}
	before := len(h.log)
	Resolve(h, c, sa(t, "DB$ ChangeSpeed | Mode$ Decrease | Defined$ Remembered"))
	if h.g.Players[1].Speed != 1 || len(h.log) != before {
		t.Fatalf("speed fell below 1: %d, events %+v", h.g.Players[1].Speed, h.log[before:])
	}
}

func TestStandardChangeSpeedGrandPrixStartsOnlyAtZero(t *testing.T) {
	h, c, _ := attachBoard(t)
	c.Remembered = []state.Target{{Player: 1, IsPlayer: true}}
	body := sa(t, "DB$ ChangeSpeed | Mode$ Increase | Defined$ Remembered | ConditionCheckSVar$ PlayerCountDefinedRemembered$Speed | ConditionSVarCompare$ EQ0")
	Resolve(h, c, body)
	if h.g.Players[1].Speed != 1 {
		t.Fatalf("did not start engines: %d", h.g.Players[1].Speed)
	}
	before := len(h.log)
	Resolve(h, c, body)
	if h.g.Players[1].Speed != 1 || len(h.log) != before {
		t.Fatalf("restarted engines: %d, events %+v", h.g.Players[1].Speed, h.log[before:])
	}
}

func TestStandardChangeSpeedHarrierHighestOnly(t *testing.T) {
	h, c, _ := attachBoard(t)
	h.Emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: 2})
	h.Emit(events.Event{Kind: events.SpeedChange, Player: 1, Amount: 2})
	if h.g.Players[0].Speed != 2 || h.g.Players[1].Speed != 2 {
		t.Fatal("precondition: speeds not tied at two")
	}
	c.Targets = []state.Target{{Player: 1, IsPlayer: true}}
	body := sa(t, "DB$ ChangeSpeed | Mode$ Decrease | Defined$ TargetedController | ConditionCheckSVar$ TargetedController$Speed | ConditionSVarCompare$ GTPlayerCountDefinedNonTargetedController$HighestSpeed")
	before := len(h.log)
	Resolve(h, c, body)
	if h.g.Players[1].Speed != 2 || len(h.log) != before {
		t.Fatalf("tie decreased speed: %d, events %+v", h.g.Players[1].Speed, h.log[before:])
	}
	h.Emit(events.Event{Kind: events.SpeedChange, Player: 1, Amount: 1})
	Resolve(h, c, body)
	if h.g.Players[1].Speed != 2 {
		t.Fatalf("highest speed not decreased: %d", h.g.Players[1].Speed)
	}
}

func TestStandardChangeSpeedDecrease(t *testing.T) {
	h, c, _ := attachBoard(t)
	h.Emit(events.Event{Kind: events.SpeedChange, Player: 1, Amount: 3})
	if h.g.Players[1].Speed != 3 {
		t.Fatal("precondition: speed not three")
	}
	c.Targets = []state.Target{{Player: 1, IsPlayer: true}}
	before := len(h.log)
	Resolve(h, c, sa(t, "DB$ ChangeSpeed | Mode$ Decrease | Defined$ TargetedController"))
	if h.g.Players[1].Speed != 2 || h.g.Players[0].Speed != 0 {
		t.Fatalf("speed: %d, %d", h.g.Players[1].Speed, h.g.Players[0].Speed)
	}
	if len(h.log) != before+1 || h.log[before].Kind != events.SpeedChange || h.log[before].Player != 1 || h.log[before].Amount != -1 {
		t.Fatalf("speed events: %+v", h.log[before:])
	}
}
