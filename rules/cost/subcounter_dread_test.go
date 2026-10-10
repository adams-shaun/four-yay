package cost

import "testing"

// TestSubCounterKindIsFoldedToTheCanonicalSpelling pins that a SubCounter cost
// names the counter kind a PutCounter places: Shadows' Lair writes
// `SubCounter<1/Dread>` while Grasping Shadows places `CounterType$ DREAD`, and
// Object.Counter is case-sensitive, so the verbatim spelling paid from zero
// counters. Keyword counters keep their case.
func TestSubCounterKindIsFoldedToTheCanonicalSpelling(t *testing.T) {
	for cost, want := range map[string]string{
		"B T SubCounter<1/Dread>":      "DREAD",
		"SubCounter<1/DREAD>":          "DREAD",
		"SubCounter<2/CHARGE>":         "CHARGE",
		"SubCounter<1/Indestructible>": "Indestructible",
		"RemoveAnyCounter<1/Dread>":    "DREAD",
	} {
		c := ParseCost(cost)
		if len(c.SubCounter) != 1 {
			t.Fatalf("ParseCost(%q): SubCounter = %+v, want one part", cost, c.SubCounter)
		}
		if got := c.SubCounter[0].Spec; got != want {
			t.Errorf("ParseCost(%q) kind = %q, want %q", cost, got, want)
		}
	}
}
