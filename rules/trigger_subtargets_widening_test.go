package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// A ChangeZone chain link owns its ordinary resolution-time ask, but the
// trigger placement flow records the announcement so the same link consumes
// it instead. Its mandatory no-legal-target case also removes the trigger.
func TestTriggerChangeZoneChainAskedAtPlacementAndMandatoryNoTargetRemovesAbility(t *testing.T) {
	makeTrigger := func(t *testing.T, withOpponent bool) (*Engine, state.ObjID) {
		t.Helper()
		e := layerEngine(t)
		src := onBoard(t, e, 0,
			"Name:Zone Chain Trigger\nTypes:Creature\nPT:1/1\n"+
				"T:Mode$ ChangesZone | ValidCard$ Card.Self | Destination$ Battlefield | Execute$ Root | TriggerDescription$ x\n"+
				"SVar:Root:DB$ Pump | SubAbility$ Move\n"+
				"SVar:Move:DB$ ChangeZone | ValidTgts$ Creature.OppCtrl | Origin$ Battlefield | Destination$ Exile | ChangeType$ Creature\nOracle:x\n")
		if withOpponent {
			onBoard(t, e, 1, "Name:Opponent Target\nTypes:Creature\nPT:2/2\nOracle:x\n")
		}
		o := e.G.Obj(src)
		if o == nil || o.Face() == nil || len(o.Face().Triggers) != 1 {
			t.Fatalf("precondition: printed trigger compiled, got %+v", o)
		}
		root := o.Face().Triggers[0].Effect
		if root == nil || root.Sub == nil || root.Sub.API != "ChangeZone" {
			t.Fatalf("precondition: trigger has a ChangeZone chain link, root=%+v sub=%+v api=%v", root, root.Sub, root.Sub.CompiledAPI())
		}
		return e, src
	}

	e, src := makeTrigger(t, true)
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Idx: 0,
		SA: e.G.Obj(src).Face().Triggers[0].Effect})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" || len(d.Options) != 1 {
		t.Fatalf("ChangeZone chain ask = %+v, want placement KTarget trig_sub before priority", d)
	}
	if target := d.Options[0].Obj; target == 0 || e.G.Obj(target).Controller != 1 {
		t.Fatalf("ChangeZone chain target = %d, want opponent's creature", target)
	}

	e, src = makeTrigger(t, false)
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Idx: 0,
		SA: e.G.Obj(src).Face().Triggers[0].Effect})
	removed := false
	for _, ev := range e.L.Events {
		if ev.Text == "countered: no legal targets" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("mandatory ChangeZone link with no legal target did not remove the ability; pending=%+v stack=%v", e.Pending(), e.G.Stack)
	}
}

// A non-modal printed trigger with no root target and no Defined$ Targeted
// tail still announces its targeted SubAbility$ before priority (CR 603.3d).
func TestTriggerChainWithoutTargetedTailAskedAtPlacement(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0,
		"Name:Chain Trigger\nTypes:Creature\nPT:1/1\n"+
			"T:Mode$ ChangesZone | ValidCard$ Card.Self | Destination$ Battlefield | Execute$ Root | TriggerDescription$ x\n"+
			"SVar:Root:DB$ Pump | SubAbility$ TargetOne\n"+
			"SVar:TargetOne:DB$ Pump | ValidTgts$ Creature.YouCtrl\nOracle:x\n")
	target := onBoard(t, e, 0, "Name:Target\nTypes:Creature\nPT:2/2\nOracle:x\n")
	o := e.G.Obj(src)
	if o == nil || o.Face() == nil || len(o.Face().Triggers) != 1 {
		t.Fatalf("precondition: printed trigger compiled, got %+v", o)
	}
	root := o.Face().Triggers[0].Effect
	if root == nil || root.Sub == nil || root.Sub.Params["ValidTgts"] == "" {
		t.Fatalf("precondition: trigger has a targeting chain link: %+v", root)
	}
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Idx: 0, SA: root})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" {
		t.Fatalf("chain target ask = %+v, want placement KTarget trig_sub before priority", d)
	}
	if indexOfObjOption(d, target) < 0 {
		t.Fatalf("chain target options = %+v, want target %d", d.Options, target)
	}
}

// A mandatory chain target with no legal candidate counters the triggered
// ability at placement, rather than leaving it to fail during resolution.
func TestTriggerChainMandatoryNoLegalTargetRemovesAbility(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0,
		"Name:Chain Trigger\nTypes:Creature\nPT:1/1\n"+
			"T:Mode$ ChangesZone | ValidCard$ Card.Self | Destination$ Battlefield | Execute$ Root | TriggerDescription$ x\n"+
			"SVar:Root:DB$ Pump | SubAbility$ TargetOpponent\n"+
			"SVar:TargetOpponent:DB$ Pump | ValidTgts$ Creature.OppCtrl | TargetMin$ 1\nOracle:x\n")
	_ = onBoard(t, e, 0, "Name:Own Target\nTypes:Creature\nPT:2/2\nOracle:x\n")
	o := e.G.Obj(src)
	if o == nil || o.Face() == nil || len(o.Face().Triggers) != 1 || o.Face().Triggers[0].Effect == nil {
		t.Fatalf("precondition: printed trigger compiled, got %+v", o)
	}
	root := o.Face().Triggers[0].Effect
	if root.Sub == nil || root.Sub.Params["TargetMin"] != "1" || root.Sub.Params["ValidTgts"] != "Creature.OppCtrl" {
		t.Fatalf("precondition: mandatory opponent-target chain link, got %+v", root.Sub)
	}
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Idx: 0, SA: root})
	removed := false
	for _, ev := range e.L.Events {
		if ev.Text == "countered: no legal targets" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("mandatory-target exit did not remove the ability; pending=%+v stack=%v events=%+v", e.Pending(), e.G.Stack, e.L.Events)
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("no target should be asked after the mandatory link has no legal target: %+v", d)
	}
}
