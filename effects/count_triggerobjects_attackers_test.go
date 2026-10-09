package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestTriggerObjectsAttackersCountRef pins the TriggerObjectsAttackers count
// ref (task tek1): the attackers that passed the firing trigger's own
// ValidAttackers$ filter, captured at queue time on the TriggerContext and
// read here directly off a hand-built Ctx. The Earth King's
// SVar:X:TriggerObjectsAttackers$Amount ("up to that many basic land cards")
// is the corpus carrier; Witch-King Sky Scourge's $CardPower and Basri Ket's /
// Lulu's $Valid shapes share the same referent set.
func TestTriggerObjectsAttackersCountRef(t *testing.T) {
	h := newHost(t, 2)
	asp := mkCard(t, "Name:Asp\nManaCost:4 G\nTypes:Creature Snake\nPT:4/5\nOracle:x\n")
	bear := mkCard(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	aspObj := h.g.AddObject(asp, 0)
	bearObj := h.g.AddObject(bear, 0)
	aspObj.Zone = state.ZBattlefield
	bearObj.Zone = state.ZBattlefield
	aspObj.Controller = 0
	bearObj.Controller = 0
	if aspObj.Zone != state.ZBattlefield || bearObj.Zone != state.ZBattlefield {
		t.Fatalf("precondition: both attackers must be on the battlefield, got %v / %v", aspObj.Zone, bearObj.Zone)
	}
	if p := aspObj.Face().Power(); p != 4 {
		t.Fatalf("precondition: Asp power = %d, want 4", p)
	}
	if p := bearObj.Face().Power(); p != 2 {
		t.Fatalf("precondition: Bear power = %d, want 2", p)
	}
	c := &Ctx{Controller: 0, TriggerContext: TriggerContext{TriggerAttackers: []state.ObjID{aspObj.ID}}}
	for _, tc := range []struct {
		expr string
		want int32
	}{
		{"TriggerObjectsAttackers$Amount", 1},
		{"TriggerObjectsAttackers$CardPower", 4},
		{"TriggerObjectsAttackers$Valid Creature.powerGE3", 1},
		{"TriggerObjectsAttackers$Valid Creature.powerLE3", 0},
		{"TriggerObjectsAttackers$Amount/Twice", 2},
	} {
		if got, ok := EvalCountOK(h, c, tc.expr); !ok || got != tc.want {
			t.Errorf("EvalCountOK(%s) = (%d, %v), want (%d, true)", tc.expr, got, ok, tc.want)
		}
	}
	// The whole matched batch: both attackers admitted, so the properties sum
	// over both and the filter splits them 1/1.
	c2 := &Ctx{Controller: 0, TriggerContext: TriggerContext{TriggerAttackers: []state.ObjID{aspObj.ID, bearObj.ID}}}
	for _, tc := range []struct {
		expr string
		want int32
	}{
		{"TriggerObjectsAttackers$Amount", 2},
		{"TriggerObjectsAttackers$CardPower", 6},
		{"TriggerObjectsAttackers$Valid Creature", 2},
		{"TriggerObjectsAttackers$Valid Creature.powerGE3", 1},
		{"TriggerObjectsAttackers$Valid Creature.powerLE3", 1},
	} {
		if got, ok := EvalCountOK(h, c2, tc.expr); !ok || got != tc.want {
			t.Errorf("batch: EvalCountOK(%s) = (%d, %v), want (%d, true)", tc.expr, got, ok, tc.want)
		}
	}
	// An UNBOUND context fails closed to zero, never to the remembered set:
	// Remembered for a batch AttackersDeclared trigger is the whole declared
	// batch plus the defending player, so a fallback would read 3 (2 objects
	// + 1 player) where the ref must read 0.
	c3 := &Ctx{Controller: 0, Remembered: []state.Target{
		{Obj: aspObj.ID}, {Obj: bearObj.ID}, {IsPlayer: true, Player: 1},
	}}
	if len(c3.Remembered) != 3 {
		t.Fatalf("precondition: the Remembered decoy must hold 3 entries, got %d", len(c3.Remembered))
	}
	for _, expr := range []string{
		"TriggerObjectsAttackers$Amount",
		"TriggerObjectsAttackers$CardPower",
		"TriggerObjectsAttackers$Valid Creature",
	} {
		got, ok := EvalCountOK(h, c3, expr)
		if got != 0 {
			t.Errorf("unbound: EvalCountOK(%s) = %d, want 0 (not the Remembered fallback)", expr, got)
		}
		if !ok {
			t.Errorf("unbound: EvalCountOK(%s) not resolvable; the empty capture is a legitimate zero", expr)
		}
	}
}
