package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The Alhammarret-shaped selector: ChooseFromDefinedCards$ narrows the
// ordinary ValidCards offer to the printed names of the Defined referents
// (the cards the reveal remembered). All fixtures use inline scripts, never
// committed Forge data.

// TestNameCardChooseFromDefinedOffersOnlyRememberedName is the positive
// control for the two fail-closed tests below: the SAME wiring (universe,
// memory, ValidCards$ Card.nonLand) poses the ask when a remembered
// nonland is eligible, offering exactly that name.
func TestNameCardChooseFromDefinedOffersOnlyRememberedName(t *testing.T) {
	h := &askHost{}
	h.g = namecardGameWithUniverse(t)
	bear := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	forest := h.g.AddObject(mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"), 1)
	if got := h.g.Obj(bear.ID).Face().Name; got != "Bear" {
		t.Fatalf("precondition: remembered face name = %q, want Bear", got)
	}
	if got := h.g.Obj(forest.ID).Face().Name; got != "Forest" {
		t.Fatalf("precondition: excluded face name = %q, want Forest", got)
	}
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	Resolve(h, &Ctx{Controller: 0, Source: src,
		Remembered: []state.Target{{Obj: bear.ID}, {Obj: forest.ID}}},
		sa(t, "SP$ NameCard | ValidCards$ Card.nonLand | ChooseFromDefinedCards$ Remembered"))
	if h.asked == nil {
		t.Fatal("NameCard did not ask for a remembered nonland name")
	}
	if len(h.asked.Options) != 1 || !containsLabel(h.asked.Options, "Bear") {
		t.Fatalf("options = %+v, want exactly the remembered nonland Bear", h.asked.Options)
	}
	if containsLabel(h.asked.Options, "Forest") {
		t.Fatalf("options = %+v, the remembered land Forest must not be offered", h.asked.Options)
	}
}
