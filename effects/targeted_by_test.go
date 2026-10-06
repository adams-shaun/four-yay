package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestTrialOfAgonyCardChoicesAreItsTargets(t *testing.T) {
	card, choose := corpusSA(t, "Trial of Agony", "DBChoose")
	if choose.API != "ChooseCard" || choose.Params["Choices"] != "Creature.targetedBy" {
		t.Fatalf("Trial of Agony DBChoose changed: %+v", choose)
	}
	h := newHost(t, 2)
	src := h.g.AddObject(card, 0)
	bear := h.g.AddObject(mkCard(t, "Name:Targeted Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
	angel := h.g.AddObject(mkCard(t, "Name:Targeted Angel\nTypes:Creature Angel\nPT:4/4\nOracle:x\n"), 1)
	for _, o := range []*state.Object{src, bear, angel} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	if h.g.Obj(bear.ID).Zone != state.ZBattlefield || h.g.Obj(angel.ID).Zone != state.ZBattlefield {
		t.Fatal("precondition: both candidates must be on the battlefield")
	}
	ctx := &Ctx{Source: src.ID, Controller: 0, Targets: []state.Target{{Obj: bear.ID}, {Obj: angel.ID}}}
	got := cardChoices(h, ctx, choose, 1)
	if len(got) != 2 || got[0].Obj != bear.ID || got[1].Obj != angel.ID {
		t.Fatalf("Trial of Agony choices = %+v, want targeted bear %d and angel %d", got, bear.ID, angel.ID)
	}
	if unknown := UnknownPredicates("Creature.targetedBy"); len(unknown) != 0 {
		t.Fatalf("UnknownPredicates(Creature.targetedBy) = %v, want empty", unknown)
	}
}
