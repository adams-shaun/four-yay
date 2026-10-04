package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The offer gate's potential-target retry (offerCastableUsing's
// statics.validTarget branch) hands costPotentialTargets' WHOLE candidate
// census to the cost-modifier composition so ValidTarget$/ValidSpell$ can
// match any candidate. A target-relative Amount$ must not read that census:
// Battlefield Thaumaturge's `TargetedObjectsDistinct$Valid
// Creature.inZoneBattlefield` counts the cast's targets, so pricing every
// legal candidate at once reduces the cost by the census size and offers a
// cast the table can never complete. costAmountTargets trims the census to a
// complete legal target assignment (the declaration's own resolved
// TargetMax$, capped at the census) before the amount is evaluated.
//
// These tests pin the assignment size and the offer it admits/withholds.

// thaumaturgeFixture is the Battlefield Thaumaturge static under test. It is
// an ENCHANTMENT so it is never itself a legal target of `ValidTgts$
// Creature` and cannot inflate the candidate census being measured.
const thaumaturgeFixture = "Name:Battlefield Thaumaturge Fixture\nTypes:Enchantment\n" +
	"S:Mode$ ReduceCost | ValidCard$ Instant.YouCtrl | Relative$ True | Type$ Spell | Amount$ ReduceCost | EffectZone$ All\n" +
	"SVar:ReduceCost:TargetedObjectsDistinct$Valid Creature.inZoneBattlefield\nOracle:x\n"

func creaturePerm(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	return battlePerm(t, e, 0, "Name:"+name+"\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
}

// TestRelativeReduceCostSingleTargetAssignmentNotOvercounted pins the
// over-count direction with two assertions on the SAME board: a single-target
// {3} spell with three legal creatures is payable from a {2} pool (the honest
// {3}-{1}) but must NOT be offered from a {1} pool. The old full-census read
// priced it at {3}-{3}={0} and offered it from {1}.
func TestRelativeReduceCostSingleTargetAssignmentNotOvercounted(t *testing.T) {
	t.Parallel()
	spell := card(t, "Name:Single Target Spell\nManaCost:3\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +1 | NumDef$ +1 | SpellDescription$ Target creature gets +1/+1.\nOracle:x\n")
	e := handEngine(t, spell)
	spellID := e.G.Zone(state.ZHand, 0)[0]
	reducer := battlePerm(t, e, 0, thaumaturgeFixture)
	b1 := creaturePerm(t, e, "Single Bear 1")
	b2 := creaturePerm(t, e, "Single Bear 2")
	b3 := creaturePerm(t, e, "Single Bear 3")
	if e.G.Obj(spellID).Zone != state.ZHand || e.G.Obj(reducer).Zone != state.ZBattlefield {
		t.Fatal("precondition: spell in hand, reducer on battlefield")
	}
	if e.G.Obj(b1).Face().IsCreature() != true || e.G.Obj(b2).Face().IsCreature() != true || e.G.Obj(b3).Face().IsCreature() != true {
		t.Fatal("precondition: the three candidates must all be creatures")
	}
	census := e.costPotentialTargets(0, spellID, spellScope(""))
	if len(census) != 3 {
		t.Fatalf("precondition: want a 3-creature census, got %d", len(census))
	}
	// The assignment the amount is evaluated against is the single forced
	// target, not the census.
	if got := e.costAmountTargets(0, spellID, spellScope(""), census); len(got) != 1 {
		t.Fatalf("single-target assignment size = %d, want 1 (census %d)", len(got), len(census))
	}
	// The honest price of the one legal target choice is {3}-{1}={2}.
	if got := e.costModifiersForTargets(0, spellID, spellScope(""), []state.Target{{Obj: b1}}).Apply(e.parseCost("3")).CMC(); got != 2 {
		t.Fatalf("single-target-bound price = %d, want 2", got)
	}
	// {2} pool: offered (this proves the reduction still admits the cast).
	e.G.Players[0].Pool[state.MC] = 2
	if !hasCastOption(e.legalActions(0), spellID) {
		t.Fatal("{3} single-target spell must be offered from a {2} pool after the {1} reduction")
	}
	// {1} pool: withheld (the over-count would have offered it for {0}).
	e.G.Players[0].Pool[state.MC] = 1
	if hasCastOption(e.legalActions(0), spellID) {
		t.Fatal("{3} single-target spell wrongly offered from a {1} pool: the potential amount counted the whole census")
	}
}

// TestRelativeReduceCostMultiTargetAssignmentAdmitsCastOffer pins the
// multi-target direction: a {3} spell that must target TWO creatures, on a
// board with three legal creatures, is priced {3}-{2}={1} and offered from a
// {1} pool -- and NOT offered from an empty pool (the census-size read would
// have priced it at {0}).
func TestRelativeReduceCostMultiTargetAssignmentAdmitsCastOffer(t *testing.T) {
	t.Parallel()
	spell := card(t, "Name:Two Target Spell\nManaCost:3\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | TargetMin$ 2 | TargetMax$ 2 | NumAtt$ +1 | NumDef$ +1 | SpellDescription$ Target creatures get +1/+1.\nOracle:x\n")
	e := handEngine(t, spell)
	spellID := e.G.Zone(state.ZHand, 0)[0]
	reducer := battlePerm(t, e, 0, thaumaturgeFixture)
	b1 := creaturePerm(t, e, "Pair Bear 1")
	b2 := creaturePerm(t, e, "Pair Bear 2")
	_ = creaturePerm(t, e, "Pair Bear 3")
	if e.G.Obj(spellID).Zone != state.ZHand || e.G.Obj(reducer).Zone != state.ZBattlefield {
		t.Fatal("precondition: spell in hand, reducer on battlefield")
	}
	statics := e.collectCostStatics()
	if !statics.validTarget {
		t.Fatal("precondition: Relative$ reducer must activate the potential-target retry")
	}
	census := e.costPotentialTargets(0, spellID, spellScope(""))
	if len(census) != 3 {
		t.Fatalf("precondition: want a 3-creature census, got %d", len(census))
	}
	if got := e.costAmountTargets(0, spellID, spellScope(""), census); len(got) != 2 {
		t.Fatalf("two-target assignment size = %d, want 2 (census %d)", len(got), len(census))
	}
	// The legal two-target cast costs {3}-{2}={1}.
	if got := e.costModifiersForTargets(0, spellID, spellScope(""), []state.Target{{Obj: b1}, {Obj: b2}}).Apply(e.parseCost("3")).CMC(); got != 1 {
		t.Fatalf("two-target-bound price = %d, want 1", got)
	}
	// Empty pool: the honest price is {1}, so it must be withheld; the
	// census-size read would have priced {0} and offered it.
	if hasCastOption(e.legalActions(0), spellID) {
		t.Fatal("{3} two-target spell wrongly offered from an empty pool: the potential amount counted the whole census")
	}
	// {1} pool: the reduced cast is offered.
	e.G.Players[0].Pool[state.MC] = 1
	if !hasCastOption(e.legalActions(0), spellID) {
		t.Fatal("{3} two-target spell must be offered from a {1} pool after the {2} reduction")
	}
}

// TestRelativeReduceCostAssignmentCappedAtCensus pins the cap: a declaration
// whose maximum exceeds the legal candidates is priced against every legal
// candidate (there is no target to invent), so a two-required-target spell on
// a board with exactly two creatures reduces by {2}.
func TestRelativeReduceCostAssignmentCappedAtCensus(t *testing.T) {
	t.Parallel()
	spell := card(t, "Name:Two Target Spell\nManaCost:3\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | TargetMin$ 2 | TargetMax$ 2 | NumAtt$ +1 | NumDef$ +1\nOracle:x\n")
	e := handEngine(t, spell)
	spellID := e.G.Zone(state.ZHand, 0)[0]
	_ = battlePerm(t, e, 0, thaumaturgeFixture)
	b1 := creaturePerm(t, e, "Exactly Bear 1")
	b2 := creaturePerm(t, e, "Exactly Bear 2")
	if e.G.Obj(b1).Zone != state.ZBattlefield || e.G.Obj(b2).Zone != state.ZBattlefield {
		t.Fatal("precondition: both candidates on the battlefield")
	}
	census := e.costPotentialTargets(0, spellID, spellScope(""))
	if len(census) != 2 {
		t.Fatalf("precondition: want a 2-creature census, got %d", len(census))
	}
	if got := e.costAmountTargets(0, spellID, spellScope(""), census); len(got) != 2 {
		t.Fatalf("assignment size = %d, want 2 (capped at census)", len(got))
	}
	if got := e.costModifiersForTargets(0, spellID, spellScope(""), []state.Target{{Obj: b1}, {Obj: b2}}).Apply(e.parseCost("3")).CMC(); got != 1 {
		t.Fatalf("two-target-bound price = %d, want 1", got)
	}
}
