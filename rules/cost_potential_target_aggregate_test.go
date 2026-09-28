package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestPotentialTargetCostPricesOneCandidateNotTheUnion is the regression for
// the offer gate's target-relative Amount$ read (finding on commit
// 56a9cf895): the potential-target retry holds the list of EVERY legal
// candidate target, and a target-count reduction must be priced as ONE
// possible announcement (the best single candidate), never the union of all
// candidates. Battlefield Thaumaturge is the real corpus carrier
// (`Mode$ ReduceCost | ValidCard$ Instant.YouCtrl,Sorcery.YouCtrl |
// Relative$ True | Amount$ ReduceCost` over
// `SVar:ReduceCost:TargetedObjectsDistinct$Valid Creature.inZoneBattlefield`):
// each instant/sorcery costs {1} less per creature it targets. On a {3} spell
// with three legal creature targets, the union would reduce by 3 ({3} -> {0})
// while no single target choice reduces by more than 1 ({3} -> {2}); pricing
// the union admits an offer that affordableTargetCandidates then finds no
// single candidate can pay. The offer gate must match the menu.
func TestPotentialTargetCostPricesOneCandidateNotTheUnion(t *testing.T) {
	t.Parallel()
	blast := card(t, "Name:Test Blast\nManaCost:3\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1\nOracle:x\n")
	e := handEngine(t, blast)

	// Battlefield Thaumaturge's static supplies the reduction; it is also a
	// creature, so the two extra bears make three legal creature targets.
	th := corpusAlternativeCard(t, "Battlefield Thaumaturge")
	o := e.G.AddObject(th, 0)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})
	battlePerm(t, e, 0, "Name:Creature One\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	battlePerm(t, e, 0, "Name:Creature Two\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	spell := e.G.Zone(state.ZHand, 0)[0]
	// Preconditions: the spell is a {3} instant in the zone the offer reads,
	// the static's source is on the battlefield, and there are several legal
	// creature targets -- without all three the aggregation this test pins
	// cannot arise.
	if s := e.G.Obj(spell).Face(); s == nil || !s.IsInstant() {
		t.Fatalf("precondition: the fixture spell must be an instant: %+v", s)
	}
	if e.G.Obj(o.ID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Battlefield Thaumaturge is in %s, want battlefield", e.G.Obj(o.ID).Zone)
	}
	cands := e.costPotentialTargets(0, spell, spellScope(""))
	if len(cands) < 2 {
		t.Fatalf("precondition: legal candidate targets = %d, want >=2 for a union to differ from a single pick", len(cands))
	}
	for _, cand := range cands {
		if cand.IsPlayer || e.G.Obj(cand.Obj) == nil || e.G.Obj(cand.Obj).Zone != state.ZBattlefield {
			t.Fatalf("precondition: candidate %+v is not a battlefield object", cand)
		}
	}

	base := e.parseCost("3")
	if base.Generic != 3 {
		t.Fatalf("precondition: base cost generic = %d, want 3", base.Generic)
	}
	// The static is really active: one candidate alone earns exactly {1} of
	// reduction ({3} -> {2}). This is the positive control -- the assertion
	// below would also "pass" if the whole static were never applied.
	single := e.costModifiersForPotentialTargets(0, spell, spellScope(""), cands[:1]).apply(base)
	if single.Generic != 2 {
		t.Fatalf("precondition/control: one candidate reduction wrong: {3} -> {%d}, want {2}", single.Generic)
	}
	// The offer gate with the whole candidate list must equal the best single
	// candidate, not the union. Before the fix this read {0} (the union of all
	// three candidates), admitting an offer no single target can pay.
	all := e.costModifiersForPotentialTargets(0, spell, spellScope(""), cands).apply(base)
	if all.Generic != single.Generic {
		t.Errorf("offer gate aggregated candidate targets: full list prices {%d}, one candidate prices {%d}; must price one announcement",
			all.Generic, single.Generic)
	}
}
