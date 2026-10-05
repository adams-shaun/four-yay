package oraclegen

import (
	"testing"
)

func TestOptionalCostPaymentFeasibility(t *testing.T) {
	for _, tc := range []struct {
		pool, cost string
		want       bool
	}{
		{"", "2", false},
		{"C", "2", false},
		{"RR", "2", true},
		{"W", "W W", false},
		{"WW", "W W", true},
		{"CW", "1 W", true},
		{"W", "1 W", false},
		{"W", "W/U W", false},
		{"UW", "W/U W", true},
		{"C", "2/W", false},
		{"CC", "2/W", true},
		{"W", "2/W", true},
		{"W", "C", false},
		{"C", "C", true},
	} {
		if got := poolPaysCost(tc.pool, tc.cost); got != tc.want {
			t.Errorf("poolPaysCost(%q, %q) = %t, want %t", tc.pool, tc.cost, got, tc.want)
		}
	}
}
