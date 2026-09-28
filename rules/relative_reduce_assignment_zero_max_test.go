package rules

// The resolved-zero sibling of costAmountTargets. The prior round trimmed a
// potential-target census to the declaration's own resolved TargetMax$, but
// kept a `len(targets) <= 1` fast path BEFORE resolving the bound. With a
// single legal candidate and a dynamic TargetMin$/TargetMax$ pair that
// resolves to 0 -- the "instead" idiom this helper's own doc claims to handle
// -- that path returned the candidate, so a target-relative Amount$ could
// count a target the cast is not allowed to announce and the offer retry
// could admit or display a price the later announced-target reprice refuses.
// The bound is now resolved before the size check, so a resolved 0 yields the
// empty assignment for a census of any size.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// resolvedZeroAmountSpell is the "instead" shape: BOTH bounds are the dynamic
// token X, which reads `Count$Valid Creature.YouCtrl` and resolves to 0 when
// the caster controls no creatures. A single creature controlled by an
// OPPONENT is the one legal candidate, so the census length is 1 while the
// declaration's resolved maximum is 0.
const resolvedZeroAmountSpell = "Name:Instead Target Spell\nManaCost:3\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Creature | TargetMin$ X | TargetMax$ X | NumAtt$ +1 | NumDef$ +1\n" +
	"SVar:X:Count$Valid Creature.YouCtrl\n" +
	"Oracle:x\n"

// TestRelativeReduceCostResolvedZeroMaxEmptyOnSingleCandidate pins the
// reviewer's MAJOR: one legal candidate plus a dynamically resolved zero bound
// must hand the amount evaluator NO targets, so a target-count reduction
// cannot apply for a target the cast may not announce. The sibling assertion
// with a resolved max of 1 and the same one-candidate census still returns the
// candidate, so the fix did not collapse the length-1 case into "always
// empty".
func TestRelativeReduceCostResolvedZeroMaxEmptyOnSingleCandidate(t *testing.T) {
	t.Parallel()
	spell := card(t, resolvedZeroAmountSpell)
	e := handEngine(t, spell)
	spellID := e.G.Zone(state.ZHand, 0)[0]
	_ = battlePerm(t, e, 0, thaumaturgeFixture)
	// The single candidate is controlled by the OPPONENT, so the caster's
	// own `Count$Valid Creature.YouCtrl` is 0 and the bound resolves to 0.
	foeBear := battlePerm(t, e, 1, "Name:Instead Foe Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.G.Obj(spellID).Zone != state.ZHand {
		t.Fatal("precondition: spell not in hand")
	}
	if o := e.G.Obj(foeBear); o == nil || o.Zone != state.ZBattlefield || !o.Face().IsCreature() {
		t.Fatalf("precondition: opponent's candidate must be a creature on the battlefield: %+v", o)
	}
	if n := countType(t, e, 0); n != 0 {
		t.Fatalf("precondition: caster must control 0 creatures (X=0), got %d", n)
	}
	sa := e.costTargetingSA(spellID, spellScope(""))
	if sa == nil {
		t.Fatal("precondition: spell has no targeting SA")
	}
	if _, max := e.resolvedTargetBounds(0, spellID, sa, 0); max != 0 {
		t.Fatalf("precondition: dynamic bound must resolve to 0, got max=%d", max)
	}
	census := e.costPotentialTargets(0, spellID, spellScope(""))
	if len(census) != 1 {
		t.Fatalf("precondition: want exactly 1 legal candidate, got %d", len(census))
	}
	if got := e.costAmountTargets(0, spellID, spellScope(""), census); len(got) != 0 {
		t.Fatalf("resolved-zero assignment size = %d, want 0 with one candidate (census %d)", len(got), len(census))
	}
}

// TestRelativeReduceCostResolvedOneMaxKeepsSingleCandidate is the control: the
// same one-candidate census with a resolved max of 1 (the caster controls the
// one creature) still yields that candidate, so the empty result above is the
// resolved zero and not the length-1 case being dropped.
func TestRelativeReduceCostResolvedOneMaxKeepsSingleCandidate(t *testing.T) {
	t.Parallel()
	spell := card(t, resolvedZeroAmountSpell)
	e := handEngine(t, spell)
	spellID := e.G.Zone(state.ZHand, 0)[0]
	_ = battlePerm(t, e, 0, thaumaturgeFixture)
	// The one candidate is the CASTER's own creature, so Count$Valid
	// Creature.YouCtrl = 1 and the bound resolves to 1.
	mine := battlePerm(t, e, 0, "Name:Instead Mine Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if n := countType(t, e, 0); n != 1 {
		t.Fatalf("precondition: caster must control exactly 1 creature (X=1), got %d", n)
	}
	sa := e.costTargetingSA(spellID, spellScope(""))
	if sa == nil {
		t.Fatal("precondition: spell has no targeting SA")
	}
	if _, max := e.resolvedTargetBounds(0, spellID, sa, 0); max != 1 {
		t.Fatalf("precondition: dynamic bound must resolve to 1, got max=%d", max)
	}
	census := e.costPotentialTargets(0, spellID, spellScope(""))
	if len(census) != 1 {
		t.Fatalf("precondition: want exactly 1 legal candidate, got %d", len(census))
	}
	got := e.costAmountTargets(0, spellID, spellScope(""), census)
	if len(got) != 1 || got[0].Obj != mine {
		t.Fatalf("resolved-one assignment = %+v, want exactly the one candidate %d", got, mine)
	}
}
