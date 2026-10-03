package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestCardBasePowerStopsAtLayer7bAndDrivesCount(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	creature := onBoard(t, e, 0, "Name:Base Reader\nTypes:Creature Human\nPT:2/2\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | SetPower$ 5 | Description$ base set\n"+
		"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | Execute$ TrigDraw | TriggerDescription$ draw base power cards\n"+
		"SVar:TrigDraw:DB$ Draw | NumCards$ Count$CardBasePower | Defined$ You\n"+
		"SVar:X:Count$CardPower/Minus.Count$CardBasePower\nOracle:x\n")
	pump := onBoard(t, e, 0, "Name:Lord\nTypes:Creature\nPT:1/1\n"+
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl+Other | AddPower$ 2 | Description$ pump\nOracle:x\n")
	if e.G.Obj(creature).Zone != state.ZBattlefield || e.G.Obj(pump).Zone != state.ZBattlefield {
		t.Fatal("precondition: test permanents are not both on the battlefield")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: creature, Counter: "P1P1", Amount: 1})
	base, full := e.Chars(creature).BasePower, e.Power(creature)
	if base != 5 || full != 8 || base == full {
		t.Fatalf("precondition/semantics: base power=%d, full power=%d; want distinct 5 and 8", base, full)
	}
	if diff, ok := effects.EvalCountOK(e, &effects.Ctx{Source: creature, Controller: 0,
		SVars: e.G.Obj(creature).Face().SVars}, "SVar$X"); !ok || diff != 3 {
		t.Fatalf("Okinec-shaped Count$CardPower/Minus.Count$CardBasePower = %d, resolved=%v; want 3", diff, ok)
	}

	// The off-zone Chars query remains meaningful and falls back to the face.
	offZone := e.G.AddObject(card(t, "Name:Off Zone\nTypes:Creature\nPT:3/2\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: offZone.ID, From: state.ZLibrary, To: state.ZHand})
	if o := e.G.Obj(offZone.ID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: off-zone source is not in hand: %+v", o)
	}
	if got := e.Chars(offZone.ID).BasePower; got != 3 {
		t.Fatalf("off-zone base power = %d, want printed face 3", got)
	}

	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("upkeep triggers = %d, want the source's draw trigger", len(e.pendingTriggers))
	}
	drawsBefore := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == 0 {
			drawsBefore++
		}
	}
	e.putTriggersOnStack()
	e.resolveTop()
	draws := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws-drawsBefore != 5 {
		t.Fatalf("base-power trigger drew %d cards, want 5", draws-drawsBefore)
	}
}
