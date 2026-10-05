package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Gift of Estates compares our land count to the source spell's X SVar,
// which counts the opponent's lands, not to the cast's mana X payment.
func TestConditionCompareSourceSVar(t *testing.T) {
	h := newHost(t, 2)
	spell := mkCard(t, "Name:Gift of Estates\nTypes:Sorcery\nOracle:x\nSVar:X:Count$Valid Land.OppCtrl\n")
	id := h.g.AddObject(spell, 0).ID
	h.g.Obj(id).Zone = state.ZStack
	land := mkCard(t, "Name:Forest\nTypes:Land\nOracle:x\n")
	for _, owner := range []state.PlayerID{0, 1, 1} {
		l := h.g.AddObject(land, owner).ID
		h.g.Obj(l).Zone = state.ZBattlefield
		h.g.SetZone(state.ZBattlefield, owner, append(h.g.Zone(state.ZBattlefield, owner), l))
	}
	ability := sa(t, "SP$ GainLife | LifeAmount$ 2 | ConditionPresent$ Land.YouCtrl | ConditionCompare$ LTX")
	ctx := &Ctx{Source: id, Controller: 0}
	if face := h.g.Obj(id).Face(); face == nil || face.SVars["X"] == "" {
		t.Fatalf("precondition: source X = %v", face)
	} else if got, ok := EvalCountOK(h, ctx, face.SVars["X"]); !ok || got != 2 {
		t.Fatalf("precondition: X=%d evaluated=%v, want 2", got, ok)
	}
	if met, ok := conditionMet(h, ctx, ability); !ok || !met {
		t.Fatalf("one land against two: met=%v resolved=%v", met, ok)
	}
	Resolve(h, ctx, ability)
	var gain bool
	for _, ev := range h.log {
		if ev.Kind == events.LifeChange {
			gain = true
		}
	}
	if !gain {
		t.Fatalf("source X comparison must run the body; log=%+v", h.log)
	}
	l := h.g.AddObject(land, 0).ID
	h.g.Obj(l).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), l))
	if met, ok := conditionMet(h, ctx, ability); !ok || met {
		t.Fatalf("two lands against two: met=%v resolved=%v", met, ok)
	}
}

func TestConditionCompareMissingSVarFailsClosed(t *testing.T) {
	h := newHost(t, 2)
	card := mkCard(t, "Name:Source\nTypes:Creature\nOracle:x\n")
	id := h.g.AddObject(card, 0).ID
	h.g.Obj(id).Zone = state.ZBattlefield
	ability := sa(t, "DB$ GainLife | LifeAmount$ 2 | ConditionPresent$ Card.Self | ConditionCompare$ LTX")
	Resolve(h, &Ctx{Source: id, Controller: 0}, ability)
	var note bool
	for _, ev := range h.log {
		if ev.Kind == events.LifeChange {
			t.Fatalf("unresolved X ran body: %+v", h.log)
		}
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unmodelled condition") {
			note = true
		}
	}
	if !note {
		t.Fatalf("missing X should emit a Note: %+v", h.log)
	}
}
