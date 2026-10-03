package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// cardfuzz fuzz-1003 planfb "cost_changed" (Call to Heel, Press the Enemy,
// Symbol of Unsummoning, each beside the caster's Battlefield Thaumaturge):
// "Each instant and sorcery spell you cast costs {1} less to cast for each
// creature it targets" is a ReduceCost whose AMOUNT counts the announced
// targets (TargetedObjectsDistinct$). The planner's target-dependence gate
// read only ValidTarget$, so it witnessed the untargeted price ({1}{U}) and
// the executor, re-reading the cost after CR 601.2c, found {U} and fell back
// to the manual window. The offer gate (markCostValidTarget) already knew a
// computed Amount$ may read targets; the planner now declines the same
// target-reading statics (costStaticReadsTargets).
func TestPaymentPlanDeclinesTargetCountingCostAmount(t *testing.T) {
	t.Parallel()
	for i, spell := range []string{"Call to Heel", "Symbol of Unsummoning", "Press the Enemy"} {
		t.Run(spell, func(t *testing.T) {
			t.Parallel()
			e, _, mine, _ := cr601Board(t, uint64(61100+i),
				map[string]state.Zone{spell: state.ZHand, "Battlefield Thaumaturge": state.ZBattlefield},
				map[string]state.Zone{"Grizzly Bears": state.ZBattlefield})
			got := e.PlanCastPayment(0, paymentCast(mine[spell]))
			if got.Plan != nil || got.Reason != "unsupported" || got.Detail != "shape:target_dependent_cost" {
				t.Fatalf("%s beside Battlefield Thaumaturge = %+v, want unsupported shape:target_dependent_cost", spell, got)
			}
		})
	}
}

// TestPaymentPlanKeepsTargetIndependentComputedAmount is the other
// direction: a computed Amount$ that reads no target (Into Thin Air's
// affinity, Count$Valid Artifact.YouCtrl) prices the same for every
// announcement, so a targeting spell under it is still planned.
func TestPaymentPlanKeepsTargetIndependentComputedAmount(t *testing.T) {
	t.Parallel()
	e, _, mine, _ := cr601Board(t, 61110,
		map[string]state.Zone{"Into Thin Air": state.ZHand, "Ornithopter": state.ZBattlefield},
		map[string]state.Zone{"Grizzly Bears": state.ZBattlefield})
	id := mine["Into Thin Air"]
	if d := e.paymentPlanCastShapeDetail(0, id); d != "" {
		t.Fatalf("Into Thin Air's affinity reduction declined the plan shape: %q", d)
	}
}
