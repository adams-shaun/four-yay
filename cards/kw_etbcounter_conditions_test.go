package cards

import "testing"

func TestEtbCounterCompilesConditionFields(t *testing.T) {
	for _, tc := range []struct {
		name, field, key, value string
	}{
		{"revolt", "Revolt$ True", "Revolt", "True"},
		{"adamant", "Adamant$ Red", "CheckSVar", "Count$Adamant_3.Red.1.0"},
		{"presence", "IsPresent$ Permanent.YouCtrl+cmcGE4", "IsPresent", "Permanent.YouCtrl+cmcGE4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := expanded(t, "Name:Condition Counter\nTypes:Creature\nPT:1/1\nK:etbCounter:P1P1:1:"+tc.field+":description\nOracle:x\n")
			if len(f.Repls) != 1 {
				t.Fatalf("replacement count = %d, want 1", len(f.Repls))
			}
			got := f.Repls[0].Params[tc.key]
			if got != tc.value {
				t.Fatalf("%s = %q, want %q; params=%v", tc.key, got, tc.value, f.Repls[0].Params)
			}
		})
	}
}
