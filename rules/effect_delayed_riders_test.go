package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestEffectDelayedRiderExpiry(t *testing.T) {
	t.Parallel()
	trigger := "Mode$ DamageDone | ValidTarget$ Player | Execute$ Pain"
	t.Run("forget counter", func(t *testing.T) {
		e := layerEngine(t)
		subject := onBoard(t, e, 0, "Name:Countered\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.emit(events.Event{Kind: events.CounterChange, Obj: subject, Counter: "VOW", Amount: 1})
		src := onBoard(t, e, 0, "Name:CounterPromise\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | ForgetCounter$ VOW | RememberObjects$ Remembered\nSVar:Hook:"+trigger+"\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
		face := e.G.Obj(src).Face()
		effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars, Remembered: []state.Target{{Obj: subject}}}, face.Abilities[0])
		if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 1 || e.G.Obj(subject).Counter("VOW") != 1 {
			t.Fatalf("precondition: registered counter-bearing subject: %+v", e.G.Delayed)
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: subject, Counter: "VOW", Amount: -1})
		if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 0 {
			t.Fatalf("last counter did not forget subject: %+v", e.G.Delayed)
		}
	})
	t.Run("forget cast", func(t *testing.T) {
		e, src := armEffectLifetime(t, "", " | ForgetOnCast$ Card", trigger)
		spell := onBoard(t, e, 0, "Name:Spell\nTypes:Creature\nPT:1/1\nOracle:x\n")
		if spell == src || e.G.Obj(spell) == nil {
			t.Fatal("precondition: distinct cast source required")
		}
		e.sweepEffectDelayedCast(events.Event{Kind: events.PutOnStack, Obj: spell, Player: 0})
		if len(e.G.Delayed) != 0 {
			t.Fatalf("completed cast did not retire grant: %+v", e.G.Delayed)
		}
	})
	t.Run("imprinted host", func(t *testing.T) {
		e, src := armEffectLifetime(t, "", " | ImprintOnHost$ True", trigger)
		if !e.G.Delayed[0].ImprintOnHost || e.G.Obj(src).Zone != state.ZBattlefield {
			t.Fatal("precondition: unmarked host")
		}
		e.EndImprintedEffects(src)
		if len(e.G.Delayed) != 0 {
			t.Fatal("host-token exile did not retire trigger")
		}
	})
	t.Run("end combat", func(t *testing.T) {
		e := layerEngine(t)
		e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
		src := onBoard(t, e, 0, "Name:CombatPromise\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | Duration$ UntilEndOfCombat\nSVar:Hook:"+trigger+"\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
		face := e.G.Obj(src).Face()
		effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars}, face.Abilities[0])
		if len(e.G.Delayed) != 1 || e.G.Step != state.StepBeginCombat {
			t.Fatalf("precondition: combat registration absent: %+v", e.G.Delayed)
		}
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
		if len(e.pendingTriggers) != 1 {
			t.Fatal("combat promise did not fire")
		}
		e.pendingTriggers = nil
		e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain2})
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
		if len(e.G.Delayed) != 0 || len(e.pendingTriggers) != 0 {
			t.Fatalf("combat promise survived phase: %+v", e.G.Delayed)
		}
	})
}

func TestEffectDelayedUnsupportedDurationLoud(t *testing.T) {
	t.Parallel()
	for _, dur := range []string{"UntilStateBasedActionChecked", "AsLongAsControl"} {
		e := layerEngine(t)
		src := onBoard(t, e, 0, "Name:Rejected\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | Duration$ "+dur+"\nSVar:Hook:Mode$ DamageDone | ValidTarget$ Player | Execute$ Pain\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
		face := e.G.Obj(src).Face()
		effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars}, face.Abilities[0])
		if len(e.G.Delayed) != 0 || len(effectNotesContaining(e, "unmodelled Effect trigger lifetime")) == 0 {
			t.Fatalf("%s silently accepted: %+v notes=%v", dur, e.G.Delayed, effectNoteTexts(e))
		}
	}
}
