package rules

import (
	"testing"
)

// TestPotentialUnpayableCastsOnlyOnAProof pins the offer builder's
// PotentialUnpayableCasts to the planner's PROOF, not the cast planner's raw
// "insufficient" verdict (fb-20261006T100405Z).
//
// On the TestPotentialPaymentPlansScriptsAFilter board a {W} spell is payable
// only through Heap Gate's filter, a source the planner census cannot price:
// PotentialPaymentPlans reports it payable with a scripted prefix. The cast
// planner's raw verdict for the same cast is "insufficient", because no plan
// exists among the priced alternatives. Marking that raw verdict a proof would
// hide a genuinely playable cast's "(tap other mana first)" row, so the builder
// must apply the same census-completeness gate the planner applies
// (paymentPlanCensusOf + paymentPlanRelaxProof). With the gate the cast is not
// in PotentialUnpayableCasts and its projection stays unannotated; without it
// the row disappears.
func TestPotentialUnpayableCastsOnlyOnAProof(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9414, "Name:Potential White\nManaCost:W\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
	onBoardCard(t, e, 0, corpusCard(t, "Heap Gate"))
	onBoard(t, e, 0, "Name:Potential Wastes\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n")

	// Precondition: the planner really proves this cast payable through an
	// uncovered source's scripted prefix. If it ever stopped doing so, the
	// test would be asserting the wrong thing.
	d := ppPriority(t, e)
	pp := ppFind(t, e.PotentialPaymentPlans(0), "cast", spell)
	if pp.Reason != "" || len(pp.Script) == 0 {
		t.Fatalf("precondition: planner verdict %+v, want a scripted prefix (payable)", pp)
	}

	// The builder must not call that cast one of its proven-unpayable ones.
	e.EnsurePaymentActions()
	if containsObjID(d.PotentialUnpayableCasts, spell) {
		t.Errorf("obj %d: present in PotentialUnpayableCasts, but the planner proves it payable via a script", spell)
	}

	// And the projection must carry no verdict for it, so the client keeps the
	// "(tap other mana first)" row that was correct on main.
	var found bool
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == spell {
			found = true
			if a.Payable != nil {
				t.Errorf("obj %d: Payable = %v, want nil (the planner only proves it payable)", spell, *a.Payable)
			}
		}
	}
	if !found {
		t.Fatalf("precondition: obj %d absent from potential_actions", spell)
	}
}
