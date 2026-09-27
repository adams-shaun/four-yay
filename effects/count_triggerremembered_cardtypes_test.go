package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestTriggerRememberedCardTypes(t *testing.T) {
	h := newHost(t, 2)
	face := mkCard(t, "Name:Artifact Creature\nTypes:Artifact Creature Golem\nOracle:x\n")
	obj := h.g.AddObject(face, 0)
	obj.Zone = state.ZGraveyard
	c := &Ctx{Controller: 0, Remembered: []state.Target{{Obj: obj.ID}}}
	if obj.Zone != state.ZGraveyard || len(c.Remembered) != 1 || len(obj.Face().Types) != 3 {
		t.Fatal("precondition: remembered graveyard card must have Artifact, Creature, and Golem types")
	}
	if got, ok := EvalCountOK(h, c, "TriggerRemembered$CardTypes"); !ok || got != 2 {
		t.Fatalf("TriggerRemembered$CardTypes = (%d, %v), want (2, true)", got, ok)
	}
}
