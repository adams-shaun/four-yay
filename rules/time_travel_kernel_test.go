package rules

// Kernel-era restoration of effects/time_travel_test.go (deleted with the W3
// legacy removal): Time Travel asks once per object with a time counter (and
// per suspended card, even at zero counters) and applies each answer to the
// object it was asked about.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	kr3TimeTravelSrc = "Name:Time Travel Spell\nManaCost:U\nTypes:Sorcery\nA:SP$ TimeTravel\nOracle:x\n"
	kr3SuspendSrc    = "Name:Suspended Card\nManaCost:5 R\nTypes:Sorcery\nK:Suspend:2:R\nA:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
)

// kr3Suspend suspends seat 0's Suspended Card through its real suspend cast
// option and returns it (exiled, suspended, two TIME counters).
func kr3Suspend(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	id := kr3Move(t, e, 0, "Suspended Card", state.ZHand)
	e.pending = nil
	addMana(t, e, 0, "R")
	submitChoices(t, e, castModeOption(t, e, id, "suspend"))
	if o := e.G.Obj(id); o.Zone != state.ZExile || o.Counter("TIME") != 2 || o.CastFlags&state.FlagSuspend == 0 {
		t.Fatalf("precondition: the suspend did not exile a marked card with 2 TIME: %+v", o)
	}
	return id
}

// TestTimeTravelAsksPerObjectAndAppliesAnswer: a suspended card with time
// counters gets a time_travel ask (skip / add / remove); the add answer puts
// a third counter on it, through the registered handler.
func TestTimeTravelAsksPerObjectAndAppliesAnswer(t *testing.T) {
	t.Parallel()
	e, cfg := kr3Game(t, 221, kr3Cards(t, kr3TimeTravelSrc, kr3SuspendSrc), nil)
	kr3Move(t, e, 0, "Time Travel Spell", state.ZHand)
	sus := kr3Suspend(t, e)
	mark := len(e.L.Events)
	d := kr3Cast(t, e, "Time Travel Spell", "U", -1)
	if d == nil || d.ResumeKind != "time_travel" || d.Options[0].Obj != sus {
		t.Fatalf("decision = %+v, want a time_travel ask about the suspended card", d)
	}
	if d.Options[1].Kind != "time_travel_add" || d.Options[2].Kind != "time_travel_remove" {
		t.Fatalf("options = %+v, want add/remove choices", d.Options)
	}
	if d := kr3Answer(t, e, kr3OptionKind(t, d, "time_travel_add")); d != nil {
		t.Fatalf("unexpected follow-up %+v", d)
	}
	if got := e.G.Obj(sus).Counter("TIME"); got != 3 {
		t.Fatalf("TIME = %d, want 3 after the add answer", got)
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Note && ev.Text == "unimplemented API TimeTravel" {
			t.Fatal("TimeTravel fell through the unimplemented handler")
		}
	}
	replayCheck(t, e, cfg)
}

// TestTimeTravelKeepsObjectIdentityWhenRemovalShrinksEligibility: three
// permanents with one TIME counter each are asked about in order; removing
// the first one's counter (which drops it out of the eligible set) must not
// shift the later asks -- the second ask is about B, the third about C, and
// each answer lands on the object it named.
func TestTimeTravelKeepsObjectIdentityWhenRemovalShrinksEligibility(t *testing.T) {
	t.Parallel()
	e, cfg := kr3Game(t, 222, kr3Cards(t, kr3TimeTravelSrc, kr3Creature("A"), kr3Creature("B"), kr3Creature("C")), nil)
	kr3Move(t, e, 0, "Time Travel Spell", state.ZHand)
	var ids []state.ObjID
	for _, n := range []string{"A", "B", "C"} {
		id := kr3Move(t, e, 0, n, state.ZBattlefield)
		e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: 1})
		ids = append(ids, id)
	}
	e.pending = nil
	d := kr3Cast(t, e, "Time Travel Spell", "U", -1)
	for i, want := range []struct {
		obj    state.ObjID
		answer string
	}{{ids[0], "time_travel_remove"}, {ids[1], "time_travel_remove"}, {ids[2], "time_travel_skip"}} {
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "time_travel" || d.Options[0].Obj != want.obj {
			t.Fatalf("ask %d = %+v, want one about %d", i, d, want.obj)
		}
		d = kr3Answer(t, e, kr3OptionKind(t, d, want.answer))
	}
	if d != nil {
		t.Fatalf("a fourth ask: %+v", d)
	}
	if a, b, c := e.G.Obj(ids[0]).Counter("TIME"), e.G.Obj(ids[1]).Counter("TIME"), e.G.Obj(ids[2]).Counter("TIME"); a != 0 || b != 0 || c != 1 {
		t.Fatalf("TIME counters = %d,%d,%d, want 0,0,1 (answers drifted after a removal)", a, b, c)
	}
	replayCheck(t, e, cfg)
}

// TestTimeTravelOffersZeroCounterSuspendedCard: a suspended card with no
// time counters left (its may-cast declined) is still offered -- it is
// suspended -- and add puts one on.
func TestTimeTravelOffersZeroCounterSuspendedCard(t *testing.T) {
	t.Parallel()
	e, cfg := kr3Game(t, 223, kr3Cards(t, kr3TimeTravelSrc, kr3SuspendSrc), nil)
	kr3Move(t, e, 0, "Time Travel Spell", state.ZHand)
	sus := kr3Suspend(t, e)
	e.emit(events.Event{Kind: events.CounterChange, Obj: sus, Counter: "TIME", Amount: -2})
	// Removing the last counter triggers the CR 702.62a may-cast; declining
	// it leaves the card suspended in exile at zero counters.
	e.pending = nil
	e.Advance()
	if d := kr3Next(t, e); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("decision = %+v, want the last-counter may-cast", d)
	} else if d := kr3Answer(t, e, kr3OptionKind(t, d, "suspend_cast_no")); d != nil {
		t.Fatalf("unexpected follow-up %+v", d)
	}
	if o := e.G.Obj(sus); o.Counter("TIME") != 0 || o.Zone != state.ZExile || o.CastFlags&state.FlagSuspend == 0 {
		t.Fatalf("precondition: want a suspended exiled card at 0 TIME, got %+v", o)
	}
	d := kr3Cast(t, e, "Time Travel Spell", "U", -1)
	if d == nil || d.ResumeKind != "time_travel" || d.Options[0].Obj != sus {
		t.Fatalf("decision = %+v, want the zero-counter suspended card %d offered", d, sus)
	}
	if d := kr3Answer(t, e, kr3OptionKind(t, d, "time_travel_add")); d != nil {
		t.Fatalf("unexpected follow-up %+v", d)
	}
	if got := e.G.Obj(sus).Counter("TIME"); got != 1 {
		t.Fatalf("TIME = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}
