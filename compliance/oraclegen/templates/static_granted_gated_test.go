package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestStaticGatedGrantOffered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, card := range []string{
		"Evendo, Waking Haven", "The Eternity Elevator", "Kavaron, Memorial World",
		"Susur Secundi, Void Altar",
	} {
		t.Run(card, func(t *testing.T) {
			req := counterReq(t, reg, card, "static#0.0")
			item, skip := GenerateB(reg, card, req)
			if skip != nil {
				t.Fatalf("GenerateB skip = %v; want a served self offered observation", skip)
			}
			if len(item.Scenario.Steps) == 0 {
				t.Fatal("served item has no offered-observation steps")
			}
			found := false
			for _, step := range item.Scenario.Steps {
				for _, ex := range step.Expect {
					if ex.Offered != nil && ex.Offered.Card == "p0:"+card && ex.Offered.Kind == "activate" && ex.Want != nil && *ex.Want {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("scenario does not assert an offered self activation: %+v", item.Scenario.Steps)
			}
		})
	}
}

func TestStaticGatedGrantFallsThroughToNamedGap(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key, reason string }{
		{"Muraganda Raceway", "static#0.0", staticGrantSelfManaReason},
		{"Amonkhet Raceway", "static#0.0", staticGrantActivateReason},
		{"Dawnsire, Sunstar Dreadnought", "static#0.0", staticGrantTriggerReason},
		{"Far Fortune, End Boss", "static#0.0", staticGrantReplacementReason},
	} {
		t.Run(tc.card, func(t *testing.T) {
			req := counterReq(t, reg, tc.card, tc.key)
			_, skip := GenerateB(reg, tc.card, req)
			if skip == nil || skip.Reason != "static "+tc.reason {
				t.Fatalf("skip = %v, want static %q", skip, tc.reason)
			}
		})
	}
}
