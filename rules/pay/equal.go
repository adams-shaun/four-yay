package pay

import (
	"github.com/adams-shaun/gorge/decision"
)

// Typed reflect.DeepEqual replacements for this package's verify checks.
// Each keeps DeepEqual's semantics exactly: a nil slice equals only a nil
// slice, and a pointer equals when it is the same address or both pointees
// are equal. equal_test.go holds the field lists to the structs.

const (
	planOutcomeFieldCount       = 4
	paymentPlanFieldCount       = 6
	paymentActivationFieldCount = 5
)

// PlanOutcomeEqual is reflect.DeepEqual(a, b) for two plan outcomes.
func PlanOutcomeEqual(a, b PlanOutcome) bool {
	return a.Reason == b.Reason && a.Detail == b.Detail && a.Nodes == b.Nodes && paymentPlanEqual(a.Plan, b.Plan)
}

func paymentPlanEqual(a, b *decision.PaymentPlan) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Version != b.Version || a.ID != b.ID || a.Cost != b.Cost || a.PoolSpend != b.PoolSpend || a.PoolAfter != b.PoolAfter {
		return false
	}
	if (a.Activations == nil) != (b.Activations == nil) || len(a.Activations) != len(b.Activations) {
		return false
	}
	for i := range a.Activations {
		x, y := &a.Activations[i], &b.Activations[i]
		if x.Source != y.Source || x.SourceZoneSeq != y.SourceZoneSeq || x.Ability != y.Ability || x.Produces != y.Produces {
			return false
		}
		if x.Consequence != y.Consequence && (x.Consequence == nil || y.Consequence == nil || *x.Consequence != *y.Consequence) {
			return false
		}
	}
	return true
}

// intSlicesEqual is reflect.DeepEqual for two [][]int.
func intSlicesEqual(a, b [][]int) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for i := range a {
		if (a[i] == nil) != (b[i] == nil) || len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}
