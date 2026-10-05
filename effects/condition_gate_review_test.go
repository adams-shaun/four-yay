package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestConditionPresent2AloneEvaluates(t *testing.T) {
	h := newHost(t, 2)
	self := mkCard(t, "Name:Self\nTypes:Creature\nOracle:x\n")
	id := h.g.AddObject(self, 0).ID
	h.g.Obj(id).Zone = state.ZBattlefield
	sa := sa(t, "DB$ GainLife | LifeAmount$ 2 | ConditionPresent2$ Card.Self | ConditionCompare2$ EQ1")
	if met, resolved := conditionMet(h, &Ctx{Controller: 0, Source: id}, sa); !met || !resolved {
		t.Fatalf("lone second presence group: met=%v resolved=%v, want true true", met, resolved)
	}
}

func TestConditionZoneBattlefieldFiltersDefinedGroup(t *testing.T) {
	h := newHost(t, 2)
	card := mkCard(t, "Name:Referent\nTypes:Creature\nOracle:x\n")
	id := h.g.AddObject(card, 0).ID
	h.g.Obj(id).Zone = state.ZBattlefield
	sa := sa(t, "DB$ GainLife | LifeAmount$ 2 | ConditionDefined$ Remembered | ConditionPresent$ Card | ConditionZone$ Battlefield")
	ctx := &Ctx{Controller: 0, Source: id, Remembered: []state.Target{{Obj: id}}}
	if met, resolved := conditionMet(h, ctx, sa); !met || !resolved {
		t.Fatalf("battlefield referent: met=%v resolved=%v, want true true", met, resolved)
	}
	h.g.Obj(id).Zone = state.ZGraveyard
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("graveyard referent with Battlefield gate: met=%v resolved=%v, want false true", met, resolved)
	}
}

func TestUnsupportedConditionValuesFailClosedWithNote(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{name: "zone", line: "DB$ GainLife | LifeAmount$ 2 | ConditionPresent$ Card | ConditionZone$ Nowhere"},
		{name: "compare", line: "DB$ GainLife | LifeAmount$ 2 | ConditionPresent$ Card | ConditionCompare$ EQX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t, 2)
			card := mkCard(t, "Name:Source\nTypes:Creature\nOracle:x\n")
			id := h.g.AddObject(card, 0).ID
			h.g.Obj(id).Zone = state.ZBattlefield
			sa := sa(t, tc.line)
			if _, ok := unmodelledConditionDetail(sa); !ok {
				t.Fatal("precondition: unsupported Condition* value must be classified")
			}
			Resolve(h, &Ctx{Controller: 0, Source: id}, sa)
			var note, bodyRan bool
			for _, ev := range h.log {
				if ev.Kind == events.Note && strings.Contains(ev.Text, "unmodelled condition") {
					note = true
				}
				if ev.Kind == events.LifeChange {
					bodyRan = true
				}
			}
			if !note || bodyRan {
				t.Fatalf("unsupported condition: note=%v bodyRan=%v log=%+v", note, bodyRan, h.log)
			}
		})
	}
}
