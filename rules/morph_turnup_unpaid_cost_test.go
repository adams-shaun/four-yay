package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Parsed costs outside turnUpPay's explicit settlement set must not be
// offered or accepted as a stale turn-up action. These two fixtures cover
// known parser fields (not Cost.Unknown): battlefield/graveyard exile and
// source counter removal.
func TestMorphTurnUpRejectsParsedButUnpaidCostParts(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Grizzly Bears")

	t.Run("ExileFromGrave", func(t *testing.T) {
		id := onBoard(t, e, 0, "Name:Exile-cost morph\nTypes:Creature\nK:Morph:ExileFromGrave<1/Card>\nOracle:x\n")
		e.emit(events.Event{Kind: events.TurnFaceDown, Obj: id})
		e.emit(events.Event{Kind: events.CastInfo, Obj: id, Counter: events.FlagsString(state.FlagMorphed)})
		grave := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
		if o := e.G.Obj(grave); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition: exile-cost candidate not in graveyard")
		}
		if o := e.G.Obj(id); o == nil || !o.FaceDown || o.Controller != 0 || e.turnFaceUpCantHappen(id) {
			t.Fatalf("precondition: source not a legal face-down turn-up candidate: %+v", o)
		}
		c := ParseCost("ExileFromGrave<1/Card>")
		if len(c.Unknown) != 0 || len(c.Exile) != 1 {
			t.Fatalf("precondition: expected recognized Exile cost, got %+v", c)
		}
		if !e.nonManaCastable(0, id, c, false) {
			t.Fatalf("precondition: graveyard candidate does not make parsed Exile cost payable: %+v", c.Exile[0])
		}
		if _, ok := morphFaceUpCost(e.G.Obj(id)); ok {
			t.Fatalf("morphFaceUpCost accepted parsed-but-unpaid ExileFromGrave")
		}
		mark := len(e.L.Events)
		e.turnFaceUp(0, decision.Option{Kind: "turn_face_up", Obj: id})
		if o := e.G.Obj(id); o == nil || !o.FaceDown {
			t.Fatalf("stale turn-up action bypassed the unpaid exile cost")
		}
		for _, ev := range e.L.Events[mark:] {
			if ev.Kind == events.TurnFaceUp {
				t.Fatalf("unpaid exile-cost turn-up emitted TurnFaceUp")
			}
		}
	})

	t.Run("SubCounter", func(t *testing.T) {
		id := onBoard(t, e, 0, "Name:Counter-cost morph\nTypes:Creature\nK:Morph:SubCounter<1/P1P1>\nOracle:x\n")
		e.emit(events.Event{Kind: events.TurnFaceDown, Obj: id})
		e.emit(events.Event{Kind: events.CastInfo, Obj: id, Counter: events.FlagsString(state.FlagMorphed)})
		e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 1})
		if o := e.G.Obj(id); o == nil || !o.FaceDown || o.Controller != 0 || e.turnFaceUpCantHappen(id) {
			t.Fatalf("precondition: source not a legal face-down turn-up candidate: %+v", o)
		}
		counter := int32(0)
		for _, c := range e.G.Obj(id).Counters {
			if c.Kind == "P1P1" {
				counter = c.N
			}
		}
		if counter != 1 {
			t.Fatalf("precondition: source P1P1 counters = %d, want 1", counter)
		}
		c := ParseCost("SubCounter<1/P1P1>")
		if len(c.Unknown) != 0 || len(c.SubCounter) != 1 {
			t.Fatalf("precondition: expected recognized SubCounter cost, got %+v", c)
		}
		if !e.nonManaCastable(0, id, c, false) {
			t.Fatalf("precondition: source counter does not make parsed SubCounter cost payable: %+v", c.SubCounter[0])
		}
		if _, ok := morphFaceUpCost(e.G.Obj(id)); ok {
			t.Fatalf("morphFaceUpCost accepted parsed-but-unpaid SubCounter")
		}
		mark := len(e.L.Events)
		e.turnFaceUp(0, decision.Option{Kind: "turn_face_up", Obj: id})
		o := e.G.Obj(id)
		remaining := int32(0)
		for _, c := range o.Counters {
			if c.Kind == "P1P1" {
				remaining = c.N
			}
		}
		if o == nil || !o.FaceDown || remaining != 1 {
			t.Fatalf("stale turn-up bypassed SubCounter: object=%+v", o)
		}
		for _, ev := range e.L.Events[mark:] {
			if ev.Kind == events.TurnFaceUp {
				t.Fatalf("unpaid SubCounter turn-up emitted TurnFaceUp")
			}
		}
	})
}
