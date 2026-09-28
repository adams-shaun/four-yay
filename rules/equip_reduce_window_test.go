package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// A live mana window is not itself proof that a weaker target's repriced
// equip cost is payable. The Mountain can add only {1} to Belt's {6} pool;
// the 2/2 would cost {8}, while the 4/4 costs {6}.
func TestBeltOfGiantStrengthEquipWeakTargetWithInsufficientWindow(t *testing.T) {
	t.Parallel()
	e, cfg, beltID, bruteID, smallID := beltGame(t, 515)
	if got := e.Power(bruteID); got != 4 {
		t.Fatalf("brute power = %d, want 4", got)
	}
	if got := e.Power(smallID); got != 2 {
		t.Fatalf("small power = %d, want 2 (different equip cost)", got)
	}
	mountain := findAndMoveToHand(t, e, 0, "Mountain")
	moveToBattlefield(t, e, mountain)
	if o := e.G.Obj(mountain); o.Zone != state.ZBattlefield || o.Tapped || !e.untappedManaSource(0, mountain) {
		t.Fatalf("fixture Mountain must be an untapped battlefield mana source: %+v", o)
	}
	// The untapped source can produce just one, not the two needed by the
	// small target. Assert that the affordability distinction is real.
	if got := e.attackBudget(0); got != 1 {
		t.Fatalf("pre-float generic mana reach = %d, want 1", got)
	}
	addMana(t, e, 0, "CCCCCC")
	if got := e.attackBudget(0); got != 7 {
		t.Fatalf("pool plus Mountain reach = %d, want 7 (between target costs 6 and 8)", got)
	}
	opt, ok := findAbilityOption(e, beltID, 0)
	if !ok {
		t.Fatal("Belt equip not offered from the 4/4's {6} price")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want equip target decision, got %+v", d)
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == smallID {
			t.Fatalf("unpayable 2/2 offered at {6} with only one Mountain available: %+v", d.Options)
		}
		if o.Obj == bruteID {
			found = true
		}
	}
	if !found {
		t.Fatalf("payable 4/4 not offered: %+v", d.Options)
	}
	targetObject(t, e, bruteID)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(beltID).AttachedTo != bruteID {
		t.Fatalf("Belt attached to %d, want %d", e.G.Obj(beltID).AttachedTo, bruteID)
	}
	replayCheck(t, e, cfg)
}

// A second Mountain covers the entire delta; target filtering must not
// withhold the 2/2 just because its price exceeds the floating pool.
func TestBeltOfGiantStrengthEquipWeakTargetWithPayableWindow(t *testing.T) {
	t.Parallel()
	e, cfg, beltID, bruteID, smallID := beltGame(t, 516)
	if e.Power(bruteID) != 4 || e.Power(smallID) != 2 {
		t.Fatalf("target powers must differ (4 vs 2): %d vs %d", e.Power(bruteID), e.Power(smallID))
	}
	for i := 0; i < 2; i++ {
		id := findAndMoveToHand(t, e, 0, "Mountain")
		moveToBattlefield(t, e, id)
		if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.Tapped || !e.untappedManaSource(0, id) {
			t.Fatalf("Mountain %d must be an untapped battlefield mana source: %+v", i, o)
		}
	}
	addMana(t, e, 0, "CCCCCC")
	if got := e.attackBudget(0); got != 8 {
		t.Fatalf("pool plus two Mountains = %d, want 8 (the 2/2's actual cost)", got)
	}
	opt, ok := findAbilityOption(e, beltID, 0)
	if !ok {
		t.Fatal("Belt equip not offered at {6}")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want equip target decision, got %+v", d)
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == smallID {
			found = true
		}
	}
	if !found {
		t.Fatalf("2/2 at price {8} not offered with {6} plus two Mountains: %+v", d.Options)
	}
	targetObject(t, e, smallID)
	// The pending activation's 601.2g window must offer both Mountains. The
	// actual payment path, not just a projection, must be able to finish it.
	for i := 0; i < 2; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("want payment-window ask %d, got %+v", i, d)
		}
		chosen := -1
		for _, o := range d.Options {
			if o.Kind == "activate" {
				chosen = o.Index
				break
			}
		}
		if chosen < 0 {
			t.Fatalf("payment window %d has no mana source: %+v", i, d.Options)
		}
		submitChoices(t, e, chosen)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(beltID).AttachedTo != smallID || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("equip payment did not complete at {8}: attached=%d pool=%d", e.G.Obj(beltID).AttachedTo, e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}
