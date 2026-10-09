package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDoranBesiegedPumpsDifference drives the real corpus card. Its pump is
// "+X/+X where X is the difference between its power and toughness", spelled
// X1 = SVar$Y1/Abs, Y1 = TriggeredAttacker$CardPower/Minus.Z1, Z1 = the
// toughness. Before the named /Minus operand and /Abs were modelled the attack
// pumped +0/+0 (Doran stayed 0/5) and a 2/2 blocker pumped +2/+2.
func TestDoranBesiegedPumpsDifference(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bears := card(t, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	t.Run("attack pumps by |power-toughness|", func(t *testing.T) {
		e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Doran, Besieged by Time")}, nil)
		doran := moveByName(t, e, 0, "Doran, Besieged by Time", state.ZBattlefield)
		o := e.G.Obj(doran)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
			t.Fatalf("precondition: Doran not on seat 0's battlefield: %+v", o)
		}
		if p, tg := e.Power(doran), e.Toughness(doran); p != 0 || tg != 5 {
			t.Fatalf("precondition: Doran is %d/%d before attacking, want 0/5", p, tg)
		}
		declareAttackersOnly(t, e, 0, 1, doran)
		if !settleDoranTriggers(t, e) {
			t.Fatal("Doran's attack trigger never queued or resolved")
		}
		if p, tg := e.Power(doran), e.Toughness(doran); p != 5 || tg != 10 {
			t.Fatalf("Doran after attacking is %d/%d, want 5/10", p, tg)
		}
		// "Until end of turn": pass to the next turn and the pump is gone.
		turn := e.G.Turn
		for n := 0; n < 200 && e.G.Turn == turn && !e.G.Over; n++ {
			d := e.Pending()
			if d == nil || d.Kind != decision.KPriority {
				break
			}
			submitChoices(t, e, doranPassIndex(t, d))
		}
		if e.G.Turn == turn {
			t.Fatalf("precondition: the turn never ended (step %v)", e.G.Step)
		}
		if p, tg := e.Power(doran), e.Toughness(doran); p != 0 || tg != 5 {
			t.Fatalf("Doran next turn is %d/%d, want 0/5 (pump lasts until end of turn)", p, tg)
		}
	})

	t.Run("block of a 2/2 pumps by zero", func(t *testing.T) {
		e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Doran, Besieged by Time"), bears}, []*cards.Card{bears})
		moveByName(t, e, 0, "Doran, Besieged by Time", state.ZBattlefield)
		blocker := moveByName(t, e, 0, "Test Bear", state.ZBattlefield)
		attacker := moveByName(t, e, 1, "Test Bear", state.ZBattlefield)
		for _, id := range []state.ObjID{blocker, attacker} {
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: %d not on the battlefield: %+v", id, o)
			}
		}
		if p, tg := e.Power(blocker), e.Toughness(blocker); p != 2 || tg != 2 {
			t.Fatalf("precondition: blocker is %d/%d, want 2/2", p, tg)
		}
		e.G.Obj(attacker).SummonSick = false
		e.G.Active = 1
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
		e.G.Step = state.StepDeclareBlockers
		e.askBlockers()
		submitBlockersOnly(t, e, blocker)
		if !settleDoranTriggers(t, e) {
			t.Fatal("Doran's block trigger never queued or resolved")
		}
		if p, tg := e.Power(blocker), e.Toughness(blocker); p != 2 || tg != 2 {
			t.Fatalf("2/2 blocker after the pump is %d/%d, want 2/2", p, tg)
		}
	})
}

func doranPassIndex(t *testing.T, d *decision.Decision) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o.Index
		}
	}
	t.Fatalf("priority decision with no pass option: %+v", d)
	return -1
}

// settleDoranTriggers passes priority while a trigger is queued or on the
// stack, so Doran's pump is pushed and resolves, then stops. It reports
// whether a trigger was ever seen, so a "+0" outcome cannot pass with the
// trigger unregistered.
func settleDoranTriggers(t *testing.T, e *Engine) bool {
	t.Helper()
	saw := false
	for n := 0; n < 30; n++ {
		d := e.Pending()
		if d == nil || (len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0) {
			return saw
		}
		saw = true
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision while settling: %+v", d)
		}
		submitChoices(t, e, doranPassIndex(t, d))
	}
	t.Fatalf("never settled (stack %d, triggers %d)", len(e.G.Stack), len(e.pendingTriggers))
	return saw
}
