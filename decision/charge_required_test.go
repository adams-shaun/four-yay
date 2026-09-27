package decision

import (
	"reflect"
	"testing"
)

// TestRequiredQuotaPricesCombinedLifeCharge pins the review-r1 MAJOR at the
// rule's own home: a Required option set whose COMBINED non-mana charge (each
// Phyrexian pip at two life, CR 107.4f) exceeds the published PayerLife must
// have a quota that the answer can actually pay, and FitRequired must return
// exactly that charge-feasible set -- never restore the unpayable option.
func TestRequiredQuotaPricesCombinedLifeCharge(t *testing.T) {
	d := &Decision{Kind: KAttackers, Min: 0, Max: 3, PayerLife: 4, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1, Required: true, CostPhyrexian: 1},
		{Index: 1, Kind: "attacker", Obj: 2, Required: true, CostPhyrexian: 1},
		{Index: 2, Kind: "attacker", Obj: 3, Required: true, CostPhyrexian: 1},
	}}
	// Precondition: the bound binds only because all three are required and
	// the combined charge (6 life) overruns the published total (4).
	if !d.Options[0].Required || !d.Options[1].Required || !d.Options[2].Required {
		t.Fatal("fixture wrong: every option must be Required")
	}
	if q := d.RequiredQuota(); q != 2 {
		t.Fatalf("RequiredQuota = %d, want 2 (third pip exceeds 4 life)", q)
	}
	if !d.ChargeOptionsFit([]int{0, 1}) {
		t.Fatal("precondition: two pips (4 life) must fit")
	}
	if d.ChargeOptionsFit([]int{0, 1, 2}) {
		t.Fatal("precondition: three pips (6 life) must not fit")
	}
	// The repair must drop the unpayable required third, not restore it.
	got := d.FitRequired([]int{0, 1, 2})
	if !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("FitRequired([0 1 2]) = %v, want [0 1]", got)
	}
	if err := d.Validate(Intent{Choices: got}); err != nil {
		t.Fatalf("repaired answer %v failed Validate: %v", got, err)
	}
}

// TestRequiredQuotaKeepsChargeFreeDeclarationsByteIdentical pins the ordinary
// case: with no published bound (PayerLife 0 = not published) the quota and
// the repair are exactly what they were before the charge rule existed.
func TestRequiredQuotaKeepsChargeFreeDeclarationsByteIdentical(t *testing.T) {
	d := &Decision{Kind: KAttackers, Min: 0, Max: 3, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1, Required: true},
		{Index: 1, Kind: "attacker", Obj: 2, Required: true},
		{Index: 2, Kind: "attacker", Obj: 3, Required: true},
	}}
	if q := d.RequiredQuota(); q != 3 {
		t.Fatalf("RequiredQuota = %d, want 3 with no published bound", q)
	}
	if got := d.FitRequired([]int{0, 1, 2}); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("FitRequired = %v, want the answer unchanged", got)
	}
}

// TestRequiredQuotaSharesFixedLifeAndPipBound pins the mixed budget: a
// CostLife option and a pip share the SAME cumulative life bound (2 + 1 pip =
// 4 of 6), so all three survive, while a fourth push overruns.
func TestRequiredQuotaSharesFixedLifeAndPipBound(t *testing.T) {
	d := &Decision{Kind: KAttackers, Min: 0, Max: 3, PayerLife: 5, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1, Required: true, CostLife: 2},
		{Index: 1, Kind: "attacker", Obj: 2, Required: true, CostPhyrexian: 1},
		{Index: 2, Kind: "attacker", Obj: 3, Required: true, CostPhyrexian: 1},
	}}
	if q := d.RequiredQuota(); q != 2 {
		t.Fatalf("RequiredQuota = %d, want 2 (2 + 2 = 4 of 5 life; the third pip overruns)", q)
	}
	if got := d.FitRequired([]int{0, 1, 2}); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("FitRequired = %v, want [0 1]", got)
	}
}

// TestRequiredQuotaDoesNotPriceTapObligations pins the documented split: a
// tapXType obligation is invisible on the wire, so ChargeOptionConstraints
// drops it from the policy's answer, but the required QUOTA must not silently
// stop demanding a required creature because of a tap -- the engine's
// board-aware payment can still meet it, and the pre-existing contract leaves
// a required tap pick to that payment.
func TestRequiredQuotaDoesNotPriceTapObligations(t *testing.T) {
	d := &Decision{Kind: KAttackers, Min: 0, Max: 2, PayerLife: 4, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1, Required: true, CostTaps: 1},
		{Index: 1, Kind: "attacker", Obj: 2, Required: true, CostPhyrexian: 1},
	}}
	if q := d.RequiredQuota(); q != 2 {
		t.Fatalf("RequiredQuota = %d, want 2 (a tap is not a life charge)", q)
	}
}
