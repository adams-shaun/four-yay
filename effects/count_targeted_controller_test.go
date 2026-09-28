package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestTargetedControllerRefProperty pins the TargetedController$<Property>
// count head (agent-20260928T043626Z-b7e271c1): before the fix the ref fell
// through evalPlayerRefProperty's default arm, whose property confinement
// (LifeTotal only) returned (0, false) for every property, so Lullmage's
// Domination's ReduceCost static read Count$Compare ... GE8 as 0 and never
// discounted. The referenced players are the CONTROLLERS of the target list;
// the target creature is seat 1's, so every value read must be seat 1's, and
// seat 0's zones are deliberately different sizes so reading the resolving
// controller (a wrong perspective) cannot pass.
func TestTargetedControllerRefProperty(t *testing.T) {
	g, ids := board(t)
	// Seat 0 is the resolving controller; give it a distinct life and zones
	// so a wrong-perspective read (seat 0) fails loudly. `board` already puts
	// two cards in seat 0's graveyard, plus the one below makes three.
	g.Players[0].Life = 11
	g.Players[1].Life = 17
	g.Players[1].Counters = []state.Counter{{Kind: "POISON", N: 3}}
	for i := 0; i < 2; i++ {
		mkPlayerZoneCard(t, g, 1, state.ZHand, "Name:Doodad\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	}
	for i := 0; i < 3; i++ {
		mkPlayerZoneCard(t, g, 1, state.ZLibrary, "Name:Filler\nManaCost:2\nTypes:Sorcery\nOracle:x\n")
	}
	for i := 0; i < 4; i++ {
		mkPlayerZoneCard(t, g, 1, state.ZGraveyard, "Name:Past\nManaCost:U\nTypes:Instant\nOracle:x\n")
	}
	// Seat 0's own graveyard is a non-zero but DIFFERENT size (board's two
	// cards plus one here = 3), so the controller distinction is real: a read
	// that used the resolving controller would count 3, not 4.
	mkPlayerZoneCard(t, g, 0, state.ZGraveyard, "Name:Mine\nManaCost:U\nTypes:Instant\nOracle:x\n")

	h := &fakeHost{g: g}
	obj := state.Target{Obj: ids["theirBig"]} // controlled by seat 1
	base := &Ctx{Source: ids["myBear"], Controller: 0, Targets: []state.Target{obj}}

	// Precondition: the target object is really seat 1's battlefield creature
	// and the two graveyard sizes genuinely differ -- without both the
	// assertions below are vacuous.
	if o := g.Obj(obj.Obj); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: target %v zone=%v controller=%v, want battlefield seat 1", obj.Obj, o.Zone, o.Controller)
	}
	if len(g.Zone(state.ZGraveyard, 1)) == len(g.Zone(state.ZGraveyard, 0)) {
		t.Fatalf("precondition: the two graveyards must differ in size")
	}

	for _, tc := range []struct {
		expr string
		want int32
	}{
		{"TargetedController$CardsInGraveyard", 4},
		{"TargetedController$CardsInHand", 2},
		{"TargetedController$CardsInLibrary", 3},
		{"TargetedController$Counters.Poison", 3},
		{"TargetedController$LifeTotal", 17},
		{"TargetedController$CreaturesInPlay", 1}, // theirBig, the target's only creature
		// /Op suffixes apply through the ordinary arithmetic.
		{"TargetedController$CardsInGraveyard/Twice", 8},
	} {
		if got := EvalCount(h, base, tc.expr); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.expr, got, tc.want)
		}
	}
	// PickedTargets outranks Targets: a picked list of a DIFFERENT controller
	// (seat 0's own creature) must answer seat 0's graveyard size (3).
	picked := &Ctx{Source: ids["myBear"], Controller: 0,
		Targets:       []state.Target{obj},
		PickedTargets: []state.Target{{Obj: ids["myBear"]}}}
	if got := EvalCount(h, picked, "TargetedController$CardsInGraveyard"); got != 3 {
		t.Errorf("PickedTargets precedence: TargetedController$CardsInGraveyard = %d, want 3", got)
	}
	// No targets: no controllers, modelled zero.
	none := &Ctx{Source: ids["myBear"], Controller: 0}
	if got := EvalCount(h, none, "TargetedController$CardsInGraveyard"); got != 0 {
		t.Errorf("no targets: TargetedController$CardsInGraveyard = %d, want 0", got)
	}
	// Unmodelled property fails closed.
	if got := EvalCount(h, base, "TargetedController$Speed"); got != 0 {
		t.Errorf("unmodelled TargetedController$Speed = %d, want 0", got)
	}
}
