package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestTargetSpareManaCountsSickBasicSource(t *testing.T) {
	green := pendingGreen()
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		1: {OnBattlefield: true, Basic: true, Produces: green},
		2: {OnBattlefield: true, Basic: true, Produces: green},
		3: {OnBattlefield: true, Basic: true, Produces: green},
		9: {Castable: true, InstantSpeed: true, ManaCost: "G G", CMC: CmcOf("G G")},
	})}
	if !b.hasSpareMana() {
		t.Fatal("precondition: two healthy Forests preserve the {G}{G} reserve")
	}
	sick := b.Cards.Get(3)
	sick.Sick = true
	b.Cards.Set(3, sick)
	if !sick.OnBattlefield || !sick.Basic || !sick.Sick || sick.Produces.Colour[state.MG] != 1 {
		t.Fatalf("source 3 is not a sick basic Forest: %+v", sick)
	}
	if !b.hasSpareMana() {
		t.Fatal("one healthy plus two sick/healthy Forests must preserve a {G}{G} reserve; sickness does not prohibit a basic land's {T} mana ability")
	}
	b.Cards.Delete(3)
	if b.hasSpareMana() {
		t.Fatal("precondition: without source 3, two Forests cannot survive the reserve probe")
	}
}
