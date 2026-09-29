package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// A delayed trigger's PresentDefined group must use its own captured context,
// not the source object's unrelated persistent Remembered list.
func TestTriggerPresentDefinedUsesTriggerContext(t *testing.T) {
	e := layerEngine(t)
	source := onBoard(t, e, 0, "Name:Source\nTypes:Artifact\nOracle:x\n")
	remembered := onBoard(t, e, 0, "Name:Persistent memory\nTypes:Land\nOracle:x\n")
	captured := onBoard(t, e, 1, "Name:Delayed capture\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.G.Obj(source).Remembered = []state.Target{{Obj: remembered}}
	if e.G.Obj(source).Zone != state.ZBattlefield || e.G.Obj(remembered).Zone != state.ZBattlefield || e.G.Obj(captured).Zone != state.ZBattlefield {
		t.Fatal("precondition: source, persistent memory, and delayed capture must all be on the battlefield")
	}
	if e.IsCreature(remembered) || !e.IsCreature(captured) {
		t.Fatal("precondition: persistent memory must be a land and the registration capture a creature")
	}
	if len(e.G.Obj(source).Remembered) != 1 || e.G.Obj(source).Remembered[0].Obj != remembered || remembered == captured {
		t.Fatal("precondition: source persistent memory and trigger capture must be distinct")
	}
	trigger := cards.Trigger{Params: map[string]string{
		"PresentDefined": "TriggeredSource",
		"IsPresent":      "Creature",
		"PresentCompare": "EQ1",
	}}
	tc := effects.TriggerContext{
		TriggerSource:     captured,
		DelayedRemembered: []state.Target{{Obj: captured}},
	}
	if !e.presentClauseHolds(trigger, source, 0, &tc, "IsPresent", "PresentCompare", "PresentDefined", "PresentZone") {
		t.Fatal("present clause did not count the creature in the trigger's TriggerSource context (EQ1)")
	}
}
