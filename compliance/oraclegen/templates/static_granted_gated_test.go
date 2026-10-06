package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestStaticGatedGrantFallsThroughToNamedGap(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key, reason string }{
		{"Evendo, Waking Haven", "static#0.0", staticGrantManaReason},
		{"Muraganda Raceway", "static#0.0", staticGrantManaReason},
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
