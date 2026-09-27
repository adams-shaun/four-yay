package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestTriggerObjectsCardsCountProperties(t *testing.T) {
	h := newHost(t, 2)
	creature := mkCard(t, "Name:Creature\nManaCost:3\nTypes:Creature Beast\nPT:4/4\nOracle:x\n")
	artifact := mkCard(t, "Name:Artifact\nManaCost:5\nTypes:Artifact\nOracle:x\n")
	creatureObj := h.g.AddObject(creature, 0)
	artifactObj := h.g.AddObject(artifact, 0)
	creatureObj.Zone = state.ZGraveyard
	artifactObj.Zone = state.ZGraveyard
	c := &Ctx{Controller: 0, Captured: []state.Target{{Obj: creatureObj.ID}, {Obj: artifactObj.ID}}}
	if len(c.Captured) != 2 || creatureObj.Zone != state.ZGraveyard || artifactObj.Zone != state.ZGraveyard {
		t.Fatal("precondition: captured batch must contain both cards in the graveyard")
	}
	if creatureObj.Face().Cmc() == artifactObj.Face().Cmc() || creatureObj.Face().Types[1] == artifactObj.Face().Types[0] {
		t.Fatal("precondition: batch needs different mana costs and Beast must differ from Artifact")
	}
	for _, tc := range []struct {
		expr string
		want int32
	}{
		{"TriggerObjectsCards$CardTypes", 2},
		{"TriggerObjectsCards$GreatestCardManaCost", 5},
		{"TriggerObjectsCards$CardPower", 4},
	} {
		if got, ok := EvalCountOK(h, c, tc.expr); !ok || got != tc.want {
			t.Errorf("EvalCountOK(%s) = (%d, %v), want (%d, true)", tc.expr, got, ok, tc.want)
		}
	}
}
