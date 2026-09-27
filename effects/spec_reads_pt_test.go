package effects

import "testing"

// TestSpecReadsPT pins the P/T dependency test that gates Count$Valid's
// derived-P/T bind (effects/count.go zoneCountFold.visit). The test is
// load-bearing in BOTH directions: a false negative drops the bind for a
// spec that reads derived P/T (a real corpus shape -- Creature.powerGE4,
// Creature.basePowerEQ1), silently evaluating every candidate off its
// object-alone read; a false positive reinstates the factorial layer walk
// the gate exists to avoid. The shape list mirrors numericPred's field
// vocabulary, so a new P/T field must be added in both places (ptNumericFields).
func TestSpecReadsPT(t *testing.T) {
	cases := []struct {
		spec string
		want bool
	}{
		// The spelling the fix exists for: a count spec that reads no P/T.
		{"Artifact.YouCtrl", false},
		{"Creature.YouCtrl", false},
		{"Creature.token", false},
		// The four comparison fields, lowercase LHS spellings.
		{"Creature.powerGE4", true},
		{"Creature.toughnessLE1", true},
		{"Creature.basePowerEQ1", true},
		{"Creature.baseToughnessGT0", true},
		// Two-characteristic comparisons and negation.
		{"Creature.powerGTbasePower", true},
		{"Creature.!powerGE4", true},
		{"Creature.Artifact+powerGE4", true},
		// OR alternatives: any branch reading P/T makes the whole spec read.
		{"Creature.powerGE4,Artifact.YouCtrl", true},
		{"Artifact.YouCtrl,Creature.toughnessGE4", true},
		// cmc is the evaluator's one non-P/T field: it reads the printed mana
		// cost, so it must NOT trigger the bind.
		{"Creature.cmcGE4", false},
		// The extreme classifiers read power through their own host method,
		// not SpecContext.DerivedPower, and are not count-spec bodies anyway.
		{"Creature.greatestPower", false},
	}
	for _, tc := range cases {
		if got := SpecReadsPT(tc.spec); got != tc.want {
			t.Errorf("SpecReadsPT(%q) = %v, want %v", tc.spec, got, tc.want)
		}
	}
}
