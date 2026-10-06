package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestStaticGatedGrantOffered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key, kind string }{
		{"Evendo, Waking Haven", "static#0.0", "activate"},
		{"The Eternity Elevator", "static#0.0", "activate"},
		{"Kavaron, Memorial World", "static#0.0", "activate"},
		{"Susur Secundi, Void Altar", "static#0.0", "activate"},
		// Max-speed grants are the engine's "granted" option kind, which an
		// "activate" assertion does not match.
		{"Muraganda Raceway", "static#0.0", "granted"},
		{"Amonkhet Raceway", "static#0.0", "granted"},
		{"Endrider Catalyzer", "static#0.0", "granted"},
		{"Howlsquad Heavy", "static#0.1", "granted"},
		// A speed-gated card with its own ETB is cast, not placed.
		{"Kickoff Celebrations", "static#0.0", "granted"},
		{"Perilous Snare", "static#0.0", "granted"},
	} {
		card := tc.card
		t.Run(card, func(t *testing.T) {
			req := counterReq(t, reg, card, tc.key)
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
					if ex.Offered != nil && ex.Offered.Card == "p0:"+card && ex.Offered.Kind == tc.kind && ex.Want != nil && *ex.Want {
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
		{"Debris Field Crusher", "static#0.0", staticGatedSelfETBCounterReason},
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
