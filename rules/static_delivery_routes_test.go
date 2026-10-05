package rules

import (
	"testing"
)

func TestIgnoreHexproofActivatedEffectDelivery(t *testing.T) {
	e := handEngine(t)
	tower := onBoard(t, e, 0, "Name:Test Detection Tower\nTypes:Land\nA:AB$ Effect | Cost$ 1 T | StaticAbilities$ STLoseAB\nSVar:STLoseAB:Mode$ IgnoreHexproof | Activator$ You | ValidEntity$ Creature.OppCtrl\nOracle:x\n")
	hex := onBoard(t, e, 1, "Name:Hex Elf\nTypes:Creature Elf\nPT:2/2\nK:Hexproof\nOracle:x\n")
	if !e.hexproofBlocksTarget(hex, 0, 0) {
		t.Fatal("precondition: hexproof not blocking")
	}
	face := e.G.Obj(tower).Face()
	if face == nil || len(face.Abilities) == 0 {
		t.Fatal("precondition: no activation")
	}
	addMana(t, e, 0, "C")
	e.G.Obj(tower).SummonSick = false
	e.askPriority(0)
	opt := abilityOption(t, e, tower, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 40)
	live := false
	for _, ce := range e.active() {
		if ce.Restriction == "IgnoreHexproof" {
			live = true
		}
	}
	if !live {
		t.Fatal("activated effect did not register IgnoreHexproof")
	}
	if e.hexproofBlocksTarget(hex, 0, 0) {
		t.Fatal("activated tower did not lift hexproof")
	}
	if !e.hexproofBlocksTarget(hex, 2, 0) {
		t.Fatal("activated tower lifted hexproof for another seat")
	}
}
