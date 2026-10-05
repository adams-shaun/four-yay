package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestLiteralTapTriggeredCostSpiderManToTheRescue(t *testing.T) {
	t.Parallel()
	literalTapTriggeredCostCase(t, "Spider-Man, To the Rescue")
}

func TestLiteralTapTriggeredCostFireNationDrill(t *testing.T) {
	t.Parallel()
	literalTapTriggeredCostCase(t, "The Fire Nation Drill")
}

func literalTapTriggeredCostCase(t *testing.T, name string) {
	t.Helper()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, name, "Grizzly Bears")
	source := searchMoveByName(t, e, name, state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	src := e.G.Obj(source)
	if src == nil || src.Zone != state.ZBattlefield || src.Tapped {
		t.Fatalf("precondition: %s must be an untapped battlefield permanent, got %+v", name, src)
	}
	if e.G.Obj(bear) == nil || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatalf("precondition: target creature %d is not on battlefield", bear)
	}

	var d *decision.Decision
	for i := 0; i < 8; i++ {
		d = passUntilNonPriority(t, e, 40)
		if d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_tap" {
			break
		}
		if d.Kind == decision.KTriggerOptional {
			submitChoices(t, e, 0)
			continue
		}
		t.Fatalf("unexpected decision before literal tap election: %+v", d)
	}
	if d == nil || d.Min != 1 || d.Max != 1 {
		t.Fatalf("literal tap election = %+v, want exactly one choice", d)
	}
	found := false
	for _, option := range d.Options {
		if option.Kind == "trigger_cost_tap" && option.Obj == source {
			found = true
		}
	}
	if !found {
		t.Fatalf("literal tap election omitted self source %d: %+v", source, d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, optionIndexForObj(t, d, source))
	if !e.G.Obj(source).Tapped {
		t.Fatal("source was not tapped before the immediate-trigger continuation")
	}
	var tappedAt, continuationAt = -1, -1
	for i := mark; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		if ev.Kind == events.Tap && ev.Obj == source {
			tappedAt = i
		}
		if ev.Kind == events.LifeChange || ev.Kind == events.MoveZone {
			continuationAt = i
			break
		}
	}
	if tappedAt < mark {
		t.Fatalf("no events.Tap for source after election: %+v", e.L.Events[mark:])
	}
	if d = e.Pending(); d == nil {
		t.Fatal("immediate-trigger continuation did not proceed to its target decision")
	}
	if d.Kind != decision.KTarget {
		t.Fatalf("after tapping cost, continuation decision = %+v, want target ask", d)
	}
	if continuationAt >= 0 && tappedAt >= continuationAt {
		t.Fatalf("cost tap event at %d followed continuation event at %d", tappedAt, continuationAt)
	}
}

func optionIndexForObj(t *testing.T, d *decision.Decision, id state.ObjID) int {
	t.Helper()
	for _, option := range d.Options {
		if option.Kind == "trigger_cost_tap" && option.Obj == id {
			return option.Index
		}
	}
	t.Fatalf("decision has no tap option for object %d: %+v", id, d.Options)
	return -1
}
