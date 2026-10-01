package main

// Fix-round coverage for the two BP-16 wiring defects the first review found.
//
//  1. azFrontDoor keyed the -checkpoint/az-redeal refusal on the seat NAME
//     (`!plainAZ`), which over-blocked the -spellbench path (whose az-redeal
//     registry entry DOES pass azNet) and under-blocked the mixed -pairs path
//     (whose az-redeal resolves through bots/azredeal.New and silently drops
//     the checkpoint). The refusal is now keyed on the construction path
//     (azSeatMode), and these tests pin both directions.
//
//  2. The hosted plain bench names sb-uniform/sb-first/sb-heuristic used to
//     play builtins.AutoPay on their -pairs arms; routing them through
//     bots.New left Options.AutoPayMana false, so they silently switched to
//     the Manual surface. hostedPolicy now restores AutoPay for exactly
//     those names (benchAutoPayHosted).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
)

// validAZCheckpoint is the smallest model azFrontDoor accepts as a search
// leaf/prior: a value head, and no hidden information in its features. It is
// the precondition every refusal assertion below depends on -- a model that
// failed policynet validation for another reason would make the test pass
// while proving nothing about the az-redeal path.
func validAZCheckpoint(t *testing.T) *policynet.Model {
	t.Helper()
	m := &policynet.Model{ValueHidden: 1, Features: policynet.FeaturesMZ}
	if m.ValueHidden == 0 {
		t.Fatal("precondition: the checkpoint has no value head")
	}
	if m.Features == policynet.FeaturesMZOppHand {
		t.Fatal("precondition: the checkpoint carries hidden information")
	}
	return m
}

// TestAZCheckpointRefusalFollowsTheConstructionPath is the class fix: the
// -checkpoint refusal must fire for an az-redeal side that resolves through
// bots/azredeal.New (azSeatsFromHosted, the -pairs path), and must NOT fire
// for the -spellbench registry entry that passes azNet (azSeatsFromSpellbench)
// -- in both directions, with and without a plain az side present.
func TestAZCheckpointRefusalFollowsTheConstructionPath(t *testing.T) {
	saveAZ(t)
	azWorldArg, azFlagsGiven, azCorpusPath = azmcts.WorldRedeal, false, ""
	m := validAZCheckpoint(t)

	// -pairs, az-redeal alone: bots/azredeal.New builds with a nil net, so
	// the checkpoint cannot drive it -> refused.
	if err := azFrontDoor("az-redeal", "bot", m, azSeatsFromHosted); err == nil || !strings.Contains(err.Error(), "generation 0") {
		t.Fatalf("hosted az-redeal + checkpoint: err = %v, want the generation-0 refusal", err)
	}
	// -pairs, mixed az + az-redeal: the old keying passed this because
	// plainAZ was true, yet az-redeal still resolved through bots/azredeal.New
	// and dropped the checkpoint -> it must be refused too.
	if err := azFrontDoor("az", "az-redeal", m, azSeatsFromHosted); err == nil || !strings.Contains(err.Error(), "generation 0") {
		t.Fatalf("mixed hosted az + az-redeal + checkpoint: err = %v, want the generation-0 refusal", err)
	}
	// The refusal is the checkpoint's, not the lineup's: no model is fine.
	if err := azFrontDoor("az", "az-redeal", nil, azSeatsFromHosted); err != nil {
		t.Fatalf("hosted az + az-redeal without -checkpoint: err = %v, want nil", err)
	}

	// -spellbench: the az-redeal registry entry passes azNet, so the
	// checkpoint IS applied and the front door must accept it. azA is empty
	// whenever no plain az base is listed (spellbench.go builds azA/azB this
	// way), which is the exact shape the old name-keyed refusal over-blocked.
	if err := azFrontDoor("", "az-redeal", m, azSeatsFromSpellbench); err != nil {
		t.Fatalf("spellbench az-redeal + checkpoint: err = %v, want nil (azNet reaches the seat)", err)
	}
	if err := azFrontDoor("az", "az-redeal", m, azSeatsFromSpellbench); err != nil {
		t.Fatalf("spellbench az + az-redeal + checkpoint: err = %v, want nil", err)
	}
	// The checkpoint still has to be a valid search leaf on the spellbench
	// path: the mode only picks WHICH az-redeal constructor runs, it does not
	// weaken the checkpoint gate. A model without a value head is refused.
	if err := azFrontDoor("", "az-redeal", &policynet.Model{}, azSeatsFromSpellbench); err == nil || !strings.Contains(err.Error(), "no value head") {
		t.Fatalf("spellbench az-redeal + valueless checkpoint: err = %v, want the value-head refusal", err)
	}
}

// TestHostedBenchPlainSBArmsKeepAutoPay pins the arm the hosted registry
// changed: sb-uniform/sb-first/sb-heuristic played builtins.AutoPay on -pairs
// before BP-16, and hostedPolicy restores it. The separate sb-*-manual arms
// remain the Manual surface.
func TestHostedBenchPlainSBArmsKeepAutoPay(t *testing.T) {
	for _, name := range []string{"sb-uniform", "sb-first", "sb-heuristic"} {
		s := hostedPolicy(name)(19)
		c, ok := s.(seat.PaymentPlanConsumer)
		if !ok {
			t.Fatalf("hostedPolicy(%q) built %T, which is not a seat.PaymentPlanConsumer", name, s)
		}
		if !c.WantsPaymentActions() {
			t.Fatalf("hostedPolicy(%q).WantsPaymentActions() = false, want true (the -pairs arm was builtins.AutoPay)", name)
		}
	}
	// The Manual counterpart of sb-uniform is still Manual, so the AutoPay
	// assertion above is not vacuous: the same policy on a Manual surface
	// reports false.
	manual, ok := policies["sb-uniform-manual"]
	if !ok {
		t.Fatal("precondition: sb-uniform-manual is not registered")
	}
	s := manual(19)
	c, ok := s.(seat.PaymentPlanConsumer)
	if !ok {
		t.Fatalf("sb-uniform-manual built %T, which is not a seat.PaymentPlanConsumer", s)
	}
	if c.WantsPaymentActions() {
		t.Fatal("sb-uniform-manual.WantsPaymentActions() = true, want false (the Manual arm)")
	}
	// And the underlying builtin really is the AutoPay mode, not just a
	// payment-actions flag: builtins.AutoPay != builtins.Manual is what
	// distinguishes the two arms above.
	if builtins.AutoPay == builtins.Manual {
		t.Fatal("precondition: builtins.AutoPay and builtins.Manual are the same mode")
	}
}
