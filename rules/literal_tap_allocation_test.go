package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func literalTapWindow(t *testing.T) *Engine {
	t.Helper()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Spider-Man, To the Rescue", "Grizzly Bears")
	source := searchMoveByName(t, e, "Spider-Man, To the Rescue", state.ZBattlefield)
	searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: source must be untapped on battlefield: %+v", o)
	}
	for i := 0; i < 8; i++ {
		d := passUntilNonPriority(t, e, 40)
		if d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_tap" {
			if e.triggerCost == nil || e.triggerCost.source != source || len(e.triggerCost.amount.TapPermanent) != 1 {
				t.Fatalf("precondition: wrong triggered cost: %+v", e.triggerCost)
			}
			return e
		}
		if d.Kind == decision.KTriggerOptional {
			submitChoices(t, e, 0)
			continue
		}
		t.Fatalf("unexpected ask before tap window: %+v", d)
	}
	t.Fatal("no tap window")
	return nil
}

func assertLiteralTapDeclineOnly(t *testing.T, e *Engine) {
	t.Helper()
	e.pending = nil
	e.triggeredCostPaymentAsk()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Kind != "trigger_cost_decline" {
		t.Fatalf("partial tap payment offered: %+v", d)
	}
}

func TestLiteralTapTriggeredCostUnsupportedRemainders(t *testing.T) {
	for _, name := range []string{"Exert", "RollDice", "Untap", "XMin"} {
		t.Run(name, func(t *testing.T) {
			e := literalTapWindow(t)
			amt := &e.triggerCost.amount
			switch name {
			case "Exert":
				amt.Exert = []CostPart{{N: 1, Spec: "CARDNAME"}}
			case "RollDice":
				amt.RollDice = []CostPart{{N: 1}}
			case "Untap":
				amt.Untap = true
			case "XMin":
				amt.XMin = 1
			}
			if len(amt.TapPermanent) != 1 {
				t.Fatal("precondition: no literal tap part")
			}
			if e.triggeredCostPayable(e.triggerCost) {
				t.Fatalf("%s plus tap incorrectly payable", name)
			}
			assertLiteralTapDeclineOnly(t, e)
		})
	}
}

func TestLiteralTapTriggeredCostDisjointAllocation(t *testing.T) {
	e := literalTapWindow(t)
	tc := e.triggerCost
	var bear state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, tc.player) {
		if id != tc.source && e.G.Obj(id) != nil && e.G.Obj(id).Face() != nil && e.G.Obj(id).Face().Name == "Grizzly Bears" {
			bear = id
			break
		}
	}
	if bear == 0 || e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(bear).Tapped {
		t.Fatalf("precondition: distinct untapped battlefield bear required: %d", bear)
	}
	self := tc.amount.TapPermanent[0]
	// A broad first part shares self with the second, but can pay with bear.
	// A greedy first pick of self would make the second part unpayable.
	tc.amount.TapPermanent = []CostPart{{N: 1, Spec: "Creature"}, self}
	if !e.triggeredCostPayable(tc) {
		t.Fatal("precondition: broad plus self must have a full disjoint allocation")
	}
	e.pending = nil
	e.triggeredCostPaymentAsk()
	d := e.Pending()
	if d == nil || d.Min != 1 || d.Max != 1 || len(d.Options) != 1 || d.Options[0].Obj != bear {
		t.Fatalf("first tap must reserve bear and leave self for next part: %+v", d)
	}
	remaining := literalTapOptions(asPayer(e), tc.player, tc.source, tc.amount.TapPermanent, 1)
	if len(remaining) != 1 || remaining[0] != tc.source || e.G.Obj(tc.source).Tapped || e.G.Obj(bear).Tapped {
		t.Fatalf("precondition: second part needs untapped self, not bear: %+v", remaining)
	}
}

func TestLiteralTapTriggeredCostOffersEveryFeasibleFirstChoice(t *testing.T) {
	e := literalTapWindow(t)
	tc := e.triggerCost
	var bears []state.ObjID
	for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(zone, tc.player) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: zone, To: state.ZBattlefield})
				bears = append(bears, id)
				if len(bears) == 2 {
					break
				}
			}
		}
		if len(bears) == 2 {
			break
		}
	}
	if len(bears) != 2 || bears[0] == bears[1] || e.G.Obj(bears[0]).Tapped || e.G.Obj(bears[1]).Tapped {
		t.Fatalf("precondition: two distinct untapped bears required: %v", bears)
	}
	tc.amount.TapPermanent = []CostPart{{N: 1, Spec: "Creature"}, {N: 1, Spec: "Creature"}}
	want := pay.TapCostCandidates(asPayer(e), tc.player, tc.source, tc.amount.TapPermanent[0])
	if len(want) < 3 {
		t.Fatalf("precondition: at least three overlapping creature candidates required, got %v", want)
	}
	e.pending = nil
	e.triggeredCostPaymentAsk()
	d := e.Pending()
	if d == nil || d.Min != 1 || d.Max != 1 || len(d.Options) != len(want) {
		t.Fatalf("first election omitted feasible choices: %+v; want candidates %v", d, want)
	}
	for _, id := range want {
		if optionIndexForObj(t, d, id) < 0 {
			t.Fatalf("feasible first choice %d not offered: %+v", id, d.Options)
		}
	}
}

func TestLiteralTapTriggeredCostOverlappingParts(t *testing.T) {
	e := literalTapWindow(t)
	tc := e.triggerCost
	part := tc.amount.TapPermanent[0]
	if part.N != 1 || len(literalTapOptions(asPayer(e), tc.player, tc.source, tc.amount.TapPermanent, 0)) != 1 {
		t.Fatalf("precondition: sole self candidate required: %+v", part)
	}
	tc.amount.TapPermanent = append(tc.amount.TapPermanent, part)
	if e.triggeredCostPayable(tc) {
		t.Fatal("two self taps payable with only one untapped self")
	}
	assertLiteralTapDeclineOnly(t, e)
	if e.G.Obj(tc.source).Tapped {
		t.Fatal("source tapped despite unpaid cost")
	}
}
