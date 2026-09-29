// Affinity delivered as a GRANTED AddKeyword$ (CR 702.41a): the second
// carrier (Mycosynth Golem, affinity for ARTIFACTS — a different count spec
// than the Witherbloom, the Balancer carrier pinned in setaudit_sos_test.go)
// and the negative shape (a spell outside the grant's Affected$ spec is not
// reduced). The grant-half arm itself lives in rules/statics.go's
// appendEffectCostStatics / affinityGrantCostStatics; these tests exist to
// keep its spec derivation and its host gate honest.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const affinityGearSrc = "Name:Test Gear\nManaCost:2\nTypes:Artifact Creature Golem\nPT:2/2\nOracle:x\n"
const affinityRelicSrc = "Name:Test Relic\nManaCost:1\nTypes:Artifact\nOracle:x\n"
const affinityPlainSrc = "Name:Test Plain Golem\nManaCost:3\nTypes:Creature Golem\nPT:2/2\nOracle:x\n"

// TestAffinityGrant_MycosynthGolem_ArtifactCreature prices the SECOND
// corpus carrier shape: Mycosynth Golem's stack static (Affected$
// Artifact.Creature+wasCastByYou | AffectedZone$ Stack | AddKeyword$
// Affinity:Artifact) must give an artifact-creature spell you cast the
// affinity-for-artifacts reduction. Two artifacts you control (the Golem
// itself and a plain relic) take {2} off {2}: free.
func TestAffinityGrant_MycosynthGolem_ArtifactCreature(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 943, []string{"Mycosynth Golem"},
		[]string{affinityGearSrc, affinityRelicSrc, affinityPlainSrc}, nil)
	toMain1(t, e)
	gear := findAndMoveToHand(t, e, 0, "Test Gear")
	if gear == 0 {
		t.Fatal("precondition: the gear must open in hand")
	}
	if plain := sosHandCardByName(e, 0, "Test Plain Golem"); plain != 0 {
		t.Fatal("precondition shape: the plain golem unexpectedly opened in hand before the hand setup")
	}
	if got := searchMoveByName(t, e, "Mycosynth Golem", state.ZBattlefield); got == 0 {
		t.Fatal("precondition: Mycosynth Golem not found in the library")
	}
	moveSeeded(t, e, 0, affinityRelicSrc, state.ZBattlefield)
	e.priorityRound()
	if o := e.G.Obj(gear); o == nil || o.Zone != state.ZHand {
		t.Fatal("precondition: the gear is not in hand")
	}
	for _, name := range []string{"Mycosynth Golem", "Test Relic"} {
		if sosBattlefieldByName(e, name) == 0 {
			t.Fatalf("precondition: %s is not on the battlefield", name)
		}
	}
	if got := reduceOf(t, e, 0, gear); got != 2 {
		t.Fatalf("affinity grant (Artifact spec): the gear's reduction = %d, want 2 (golem + relic, CR 702.41a via the granted keyword)", got)
	}
	// The reduction is real money on the offer: {2} is castable for free
	// with an empty pool.
	opt := castOptionFor(t, e, gear)
	submitChoices(t, e, opt.Index)
	if o := e.G.Obj(gear); o == nil || o.Zone != state.ZStack {
		t.Fatal("the gear is not on the stack after the cast")
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after the free cast = %d, want 0 (the granted affinity took {2} off {2})", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestAffinityGrant_Negative_NonArtifactSpellNotReduced: the same live
// grant, a spell that does NOT match Affected$ Artifact.Creature — a plain
// (non-artifact) creature — draws no reduction and stays uncastable on the
// discount-affected budget. TestAffinityGrant_MycosynthGolem_ArtifactCreature
// is the proof the grant arm itself runs, so a silent registration failure
// cannot make this test pass vacuously.
func TestAffinityGrant_Negative_NonArtifactSpellNotReduced(t *testing.T) {
	t.Parallel()
	e, _, _ := altCostEngine(t, 944, []string{"Mycosynth Golem"},
		[]string{affinityGearSrc, affinityPlainSrc}, nil)
	toMain1(t, e)
	plain := findAndMoveToHand(t, e, 0, "Test Plain Golem")
	if plain == 0 {
		t.Fatal("precondition: the plain golem must open in hand")
	}
	if got := searchMoveByName(t, e, "Mycosynth Golem", state.ZBattlefield); got == 0 {
		t.Fatal("precondition: Mycosynth Golem not found in the library")
	}
	e.priorityRound()
	if o := e.G.Obj(plain); o == nil || o.Zone != state.ZHand {
		t.Fatal("precondition: the plain golem is not in hand")
	}
	if sosBattlefieldByName(e, "Mycosynth Golem") == 0 {
		t.Fatal("precondition: Mycosynth Golem is not on the battlefield")
	}
	if got := reduceOf(t, e, 0, plain); got != 0 {
		t.Fatalf("affinity grant (Artifact spec): the plain creature's reduction = %d, want 0 (it is not an artifact creature)", got)
	}
	// And the discount does not leak into the price: {3} stays {3}, so with
	// one mana in the pool the cast is not offered at all.
	addMana(t, e, 0, "C")
	if opt := castByName(t, e, 0, "Test Plain Golem"); opt != nil {
		t.Fatalf("the plain golem was castable with %s reduction: %+v", "none", opt)
	}
}

// sosBattlefieldByName returns the id of seat 0's battlefield object named
// name, or 0.
func sosBattlefieldByName(e *Engine, name string) state.ObjID {
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return 0
}
