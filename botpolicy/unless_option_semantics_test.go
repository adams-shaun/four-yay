package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestUnlessPayPolicyUsesSemanticMarkersAfterReordering(t *testing.T) {
	d := manaUnlessDecision("3")
	// Reverse the choices: decline is listed first at index 0, pay second at 1.
	d.Options = []decision.Option{
		{Index: 0, Kind: "mode", Label: "Don't pay", Mode: decision.ModeUnlessDecline},
		{Index: 1, Kind: "mode", Label: "Pay 3", Mode: decision.ModeUnlessPay},
	}
	b := Board{Life: TableOf(map[state.PlayerID]int32{0: 4}),
		Creatures: TableOf(map[state.ObjID]Creature{1: {Power: 5, Toughness: 5, Controller: 1}})}
	if !b.facingLethal(0) || len(d.Options) != 2 || d.Options[0].Mode == d.Options[1].Mode {
		t.Fatal("precondition: lethal threat and distinct pay/decline options required")
	}
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("reordered decline answer invalid: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("lethal board should select semantic decline (index 0): %+v", in)
	}

	// A decline-only decision has no pay marker. Its sole option must remain
	// the answer even though it is index 0.
	d.Options = []decision.Option{{Index: 0, Kind: "mode", Label: "Don't pay", Mode: decision.ModeUnlessDecline}}
	in = Decide(Board{}, d, rng(1))
	if len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("decline-only offer was not selected: %+v", in)
	}
}

func TestUnlessDamagePolicyUsesSemanticMarkersAfterReordering(t *testing.T) {
	obj := state.ObjID(42)
	d := devilOffer(4, obj)
	d.Options = []decision.Option{
		{Index: 0, Kind: "mode", Label: "Refuse — it stays", Obj: obj, Player: 1, Mode: decision.ModeUnlessDecline},
		{Index: 1, Kind: "mode", Label: "Take 4 damage", Obj: obj, Player: 1, Mode: decision.ModeUnlessPay},
	}
	b := Board{Cards: TableOf(map[state.ObjID]Card{obj: {Creature: true, Power: 4, CMC: 1}}), Life: TableOf(map[state.PlayerID]int32{1: 20})}
	if b.cardWorth(obj) < 4 || b.Life.Get(1) <= 4 || d.Options[0].Mode == d.Options[1].Mode {
		t.Fatal("precondition: creature is worth the nonlethal damage and offers differ")
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("reordered damage answer invalid: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("valuable creature should select semantic damage acceptance (index 1): %+v", in)
	}

}
