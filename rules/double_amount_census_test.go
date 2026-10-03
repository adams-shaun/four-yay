package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestDoubleAmountCarrierCensus is the durable census for the brief that
// implemented Forge's bare `NumAtt$ Double` / `NumDef$ Double` P/T amount
// (effects.NumForObject). It walks the compiled corpus and asserts the
// MECHANISM class of every carrier: each face that names `Double` on a P/T
// amount must do so on a Pump/PumpAll body, with the amount on NumAtt$ and/or
// NumDef$ -- the two keys NumForObject's key table maps to power/toughness.
// If a future Forge pin writes `Double` on some other primitive/key, this
// test fails so the new shape is classified rather than silently resolving to
// zero.
//
// The carrier COUNT is pinned as a ratchet (like the other corpus-count
// tests): a `FORGE_REF` move that changes it is a real corpus change and this
// test is where it is noticed -- re-measure with `make compile-cards` and
// update the constant in the same commit. Measured at FORGE_REF
// fb4d8091126051b0c579db5f3bfdcb7e03aae63d: 37 card files (36 at 95f04e8).
func TestDoubleAmountCarrierCensus(t *testing.T) {
	const doubleCarrierFiles = 37
	reg := testutil.CorpusRegistry(t)
	var carriers, bad []string
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			seen := false
			classify := func(sa *cards.SA) {
				if !paramsDouble(sa, "NumAtt") && !paramsDouble(sa, "NumDef") {
					return
				}
				seen = true
				if sa.API != "Pump" && sa.API != "PumpAll" {
					bad = append(bad, f.Name+": api:"+sa.API)
				}
			}
			for _, a := range f.Abilities {
				classify(a)
			}
			for _, tr := range f.Triggers {
				classify(tr.Effect)
			}
			f.EachSVarAbility(classify)
			if seen {
				carriers = append(carriers, f.Name)
			}
		}
	}
	if len(bad) > 0 {
		t.Fatalf("Double P/T amount on an unclassified primitive: %v (NumForObject only models Pump/PumpAll's NumAtt$/NumDef$)", bad)
	}
	if len(carriers) != doubleCarrierFiles {
		t.Errorf("Double P/T carriers = %d, want %d (%v); a FORGE_REF move changes this -- re-measure and update",
			len(carriers), doubleCarrierFiles, carriers)
	}
}

func paramsDouble(sa *cards.SA, key string) bool {
	return sa != nil && sa.Params[key] == "Double"
}
