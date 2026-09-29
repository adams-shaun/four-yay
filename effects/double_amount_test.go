package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestDoubleAmountResolvesPerAffectedObject pins the per-object semantics of
// Forge's bare `NumAtt$ Double` / `NumDef$ Double` amount (Mightform
// Harmonizer, Wolverine Claws Out, Unnatural Growth, Zopandrel, ...): the
// amount is the affected object's OWN current characteristic, resolved at
// resolution time. The corpus's dominant carriers affect more than one
// creature, either with a multi-object `Defined$ Valid Creature.YouCtrl` (the
// Pump form checked first, double_trouble / unnatural_growth / zopandrel) or
// with PumpAll's `ValidCards$ Creature` sweep (roar_of_endless_song). A scalar
// resolver that reads one object applies that object's stat to all of them.
//
// The board deliberately holds two creatures of DIFFERENT power (Bear 2/2,
// Flier 1/1): if the amounts collapse to a single value the two registrations
// disagree, so the test cannot pass vacuously.
func TestDoubleAmountResolvesPerAffectedObject(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"PumpDefinedValid", "DB$ Pump | Defined$ Valid Creature.YouCtrl | NumAtt$ Double | NumDef$ Double"},
		{"PumpAllValidCards", "DB$ PumpAll | ValidCards$ Creature.YouCtrl | NumAtt$ Double | NumDef$ Double"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, ids := board(t)
			h := &fakeHost{g: g}
			// Precondition: the two affected creatures must have different
			// power, or "per object" and "first object" are indistinguishable.
			// myBear is 2/2, myFlier is 1/1.
			if bp, fp := h.Power(ids["myBear"]), h.Power(ids["myFlier"]); bp == fp {
				t.Fatalf("precondition: bear/flier power must differ, both %d", bp)
			}
			Resolve(h, &Ctx{Controller: 0}, sa(t, tc.body))
			got := map[state.ObjID][2]int32{}
			for _, ce := range h.continuous {
				if ce.Layer == state.LPT {
					got[ce.Source] = [2]int32{ce.AddPower, ce.AddToughness}
				}
			}
			if len(got) != 2 {
				t.Fatalf("P/T registrations = %d, want 2 (one per affected creature): %+v", len(got), h.continuous)
			}
			// The bear must get +2/+2 and the flier +1/+1. A scalar resolver
			// that read the first object would give BOTH +2/+2 (or, for the
			// PumpAll form before the fix, no registration at all).
			if want := [2]int32{2, 2}; got[ids["myBear"]] != want {
				t.Errorf("bear Double = %v, want %v (its own 2/2)", got[ids["myBear"]], want)
			}
			if want := [2]int32{1, 1}; got[ids["myFlier"]] != want {
				t.Errorf("flier Double = %v, want %v (its own 1/1, not the bear's)", got[ids["myFlier"]], want)
			}
		})
	}
}

// TestDoubleAmountScalarDegradesToZero keeps the scalar contract: without an
// object to read, `Double` is not resolvable and Num degrades it to the
// present-but-unresolvable zero -- never to an arbitrary object's stat. This is
// the fail-closed half of removing the old first-Defined read, and it also
// pins that NumForObject is a drop-in for Num on ordinary values.
func TestDoubleAmountScalarDegradesToZero(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	if got := Num(h, &Ctx{}, sa(t, "SP$ Pump | NumAtt$ Double"), "NumAtt", 7); got != 0 {
		t.Fatalf("scalar NumAtt$ Double = %d, want 0 (present-but-unresolvable degrades to zero)", got)
	}
	// A per-object call with no object keeps the same degrade.
	if got := NumForObject(h, &Ctx{}, sa(t, "SP$ Pump | NumAtt$ Double"), "NumAtt", 0, 0); got != 0 {
		t.Fatalf("NumForObject NumAtt$ Double with no object = %d, want 0", got)
	}
	// A non-Double value must behave exactly as Num (the drop-in superset).
	if got := NumForObject(h, &Ctx{}, sa(t, "SP$ Pump | NumAtt$ +3"), "NumAtt", 0, 1); got != 3 {
		t.Fatalf("NumForObject NumAtt$ +3 = %d, want 3", got)
	}
}

// TestDoubleAmountToughnessKey covers the NumDef side of the key table so an
// edit cannot leave toughness doubling reading power: on the 1/1 flier the
// doubled toughness is 1.
func TestDoubleAmountToughnessKey(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	if got := NumForObject(h, &Ctx{}, sa(t, "SP$ Pump | NumDef$ Double"), "NumDef", 0, ids["myFlier"]); got != 1 {
		t.Fatalf("NumDef$ Double on the 1/1 flier = %d, want 1 (its toughness)", got)
	}
}
