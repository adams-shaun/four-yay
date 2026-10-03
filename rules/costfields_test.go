package rules

import (
	"strings"
	"testing"
)

// costFieldsEqual is exactly equality of the strings.Fields-normalised forms.
func TestCostFieldsEqualMatchesNormalisedCompare(t *testing.T) {
	cases := []string{"", " ", "1 R", " 1  R ", "1 R\t", "1 RR", "1", "R 1", "2 W W", "2 W  W", "\t", "Sac<1/CARDNAME>", "X X R"}
	for _, a := range cases {
		for _, b := range cases {
			want := strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
			if got := costFieldsEqual(a, b); got != want {
				t.Errorf("costFieldsEqual(%q, %q) = %v, want %v", a, b, got, want)
			}
		}
	}
}
