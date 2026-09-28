package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestRelativeReduceCostTargetCountAdmitsCastOffer(t *testing.T) {
	t.Parallel()
	spell := card(t, "Name:Target Count Spell\nManaCost:3\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +1 | NumDef$ +1 | SpellDescription$ Target creature gets +1/+1.\nOracle:x\n")
	e := handEngine(t, spell)
	spellID := e.G.Zone(state.ZHand, 0)[0]
	bear := battlePerm(t, e, 0, "Name:Target Count Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	reducer := battlePerm(t, e, 0, "Name:Battlefield Thaumaturge Fixture\nTypes:Creature Wizard\nPT:2/2\n"+
		"S:Mode$ ReduceCost | ValidCard$ Instant.YouCtrl | Relative$ True | Type$ Spell | Amount$ ReduceCost | EffectZone$ All\n"+
		"SVar:ReduceCost:TargetedObjectsDistinct$Valid Creature.inZoneBattlefield\nOracle:x\n")
	if e.G.Obj(spellID).Zone != state.ZHand || e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(reducer).Zone != state.ZBattlefield {
		t.Fatal("precondition: spell must be in hand and creature/reducer on battlefield")
	}
	if e.G.Obj(bear).Face().IsCreature() != true {
		t.Fatal("precondition: the offered target must be a creature")
	}
	statics := e.collectCostStatics()
	if !statics.validTarget {
		t.Fatal("precondition: Relative$ reducer must activate the potential-target retry")
	}
	if got := e.costModifiersForTargets(0, spellID, spellScope(""), []state.Target{{Obj: bear}}).apply(e.parseCost("3")).CMC(); got != 2 {
		t.Fatalf("target-bound price = %d, want 2", got)
	}
	e.G.Players[0].Pool[state.MC] = 2
	if !hasCastOption(e.legalActions(0), spellID) {
		t.Fatal("{3} spell was not offered from a {2} pool with one creature target and the Relative$ reduction")
	}
}
