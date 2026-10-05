package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestConditionCheckSVarConjunctionIsAnd pins the `ConditionCheckSVar$` +
// `ConditionDefined$`/`ConditionPresent$` conjunction as a real AND, not a
// replace: the SVar gate and the presence group must BOTH pass for the rider
// to run. Coiling Rebirth's DBCopy is the measured carrier
// (`ConditionCheckSVar$ X | ConditionDefined$ Remembered | ConditionPresent$
// Card.nonLegendary`), where X is `Count$PromisedGift.1.0`; the shape is
// exercised here with a controllable X (`Count$Valid Creature.YouCtrl`) and a
// plain `Card` presence spec so each half can be made to fail on its own.
//
// Three cases:
//   - both halves pass  -> resolved, met, body runs;
//   - SVar passes, group empty -> NOT met (the group still gates);
//   - SVar fails, group non-empty -> NOT met (the SVar still gates).
func TestConditionCheckSVarConjunctionIsAnd(t *testing.T) {
	line := "DB$ GainLife | LifeAmount$ 2 | ConditionCheckSVar$ X | ConditionSVarCompare$ GE1 | ConditionDefined$ Remembered | ConditionPresent$ Card"

	// newBoard builds a source spell with SVar X = Count$Valid Creature.YouCtrl
	// and returns (host, sourceID, referentID). When controlled is true the
	// referent is on seat 0's battlefield (so X counts it); otherwise it is a
	// graveyard card (X does not count it).
	newBoard := func(controlled bool) (*fakeHost, state.ObjID, state.ObjID) {
		h := newHost(t, 2)
		spell := mkCard(t, "Name:Conjure\nTypes:Sorcery\nOracle:x\nSVar:X:Count$Valid Creature.YouCtrl\n")
		sID := h.g.AddObject(spell, 0).ID
		h.g.Obj(sID).Zone = state.ZStack
		creature := mkCard(t, "Name:Bear\nTypes:Creature\nOracle:x\n")
		cID := h.g.AddObject(creature, 0).ID
		if controlled {
			h.g.Obj(cID).Zone = state.ZBattlefield
			h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), cID))
		} else {
			h.g.Obj(cID).Zone = state.ZGraveyard
		}
		return h, sID, cID
	}
	svarX := func(h *fakeHost, sID state.ObjID, want int32) {
		face := h.g.Obj(sID).Face()
		if face == nil || face.SVars["X"] == "" {
			t.Fatalf("precondition: source X is empty; face=%v", face)
		}
		if got, ok := EvalCountOK(h, &Ctx{Source: sID, Controller: 0}, face.SVars["X"]); !ok || got != want {
			t.Fatalf("precondition: SVar X=%d evaluated=%v, want %d", got, ok, want)
		}
	}

	// Case 1: one creature you control (X>=1, SVar holds) and one remembered
	// referent -> the conjunction holds and Resolve runs the body.
	h, sID, cID := newBoard(true)
	svarX(h, sID, 1)
	ctx := &Ctx{Source: sID, Controller: 0, Remembered: []state.Target{{Obj: cID}}}
	if met, resolved := conditionMet(h, ctx, sa(t, line)); !met || !resolved {
		t.Fatalf("both halves pass: met=%v resolved=%v, want true true", met, resolved)
	}
	Resolve(h, ctx, sa(t, line))
	if !sawLifeChange(h.log) {
		t.Fatalf("both halves pass: the conjunction must run the body; log=%+v", h.log)
	}

	// Case 2: SVar holds but the remembered group is empty -> the group half
	// still gates, so the body must not run.
	h2, sID2, _ := newBoard(true)
	svarX(h2, sID2, 1)
	ctx2 := &Ctx{Source: sID2, Controller: 0}
	if met, resolved := conditionMet(h2, ctx2, sa(t, line)); met || !resolved {
		t.Fatalf("empty group with holding SVar: met=%v resolved=%v, want false true", met, resolved)
	}
	Resolve(h2, ctx2, sa(t, line))
	if sawLifeChange(h2.log) {
		t.Fatalf("empty group must gate even when the SVar holds; log=%+v", h2.log)
	}

	// Case 3: the group half would pass (one remembered referent) but the SVar
	// fails because no creature you control is on the battlefield -> the SVar
	// half still gates.
	h3, sID3, cID3 := newBoard(false)
	svarX(h3, sID3, 0)
	ctx3 := &Ctx{Source: sID3, Controller: 0, Remembered: []state.Target{{Obj: cID3}}}
	if met, resolved := conditionMet(h3, ctx3, sa(t, line)); met || !resolved {
		t.Fatalf("failing SVar with non-empty group: met=%v resolved=%v, want false true", met, resolved)
	}
	Resolve(h3, ctx3, sa(t, line))
	if sawLifeChange(h3.log) {
		t.Fatalf("failing SVar must gate even when the group passes; log=%+v", h3.log)
	}
}

func sawLifeChange(log []events.Event) bool {
	for _, ev := range log {
		if ev.Kind == events.LifeChange {
			return true
		}
	}
	return false
}
