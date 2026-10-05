package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestMultiTargetSlotFixturesGenerate: a spell whose cast demands two or more
// targets for one slot (a literal TargetMin$ above one) must still get a
// scenario. The fixture builder used to hand the cast exactly one target per
// slot, so the target decision could not settle and every carrier was skipped
// as "no fixture gorge can cast". Each case asserts the cast names the required
// count of DISTINCT refs and that gorge plays it through.
func TestMultiTargetSlotFixturesGenerate(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	cases := []struct {
		card      string
		wantCount int
		diff      bool // targets must span two seats (different controllers)
	}{
		{"Betrayal at the Vault", 3, false}, // main 1 + sub TargetMin$ 2
		{"Run Away Together", 2, true},      // TargetMin$ 2 | different controllers
		{"Shifting Grift", 2, false},        // charm mode TargetMin$ 2
		{"Trial of Agony", 2, false},        // TargetMin$ 2 | same controller
		{"Violent Ultimatum", 3, false},     // TargetMin$ 3
	}
	for _, tc := range cases {
		it, skip := Generate(reg, tc.card)
		if skip != nil {
			t.Errorf("%s: %s", tc.card, skip.Reason)
			continue
		}
		var targets []string
		for _, st := range it.Scenario.Steps {
			if st.Op == "cast" && st.Card == "p0:"+tc.card {
				targets = st.Targets
				break
			}
		}
		if len(targets) != tc.wantCount {
			t.Errorf("%s: cast has %d targets %v, want %d", tc.card, len(targets), targets, tc.wantCount)
			continue
		}
		seen := map[string]bool{}
		seats := map[string]bool{}
		for _, ref := range targets {
			if seen[ref] {
				t.Errorf("%s: duplicate target ref %q in %v", tc.card, ref, targets)
			}
			seen[ref] = true
			seats[strings.SplitN(ref, ":", 2)[0]] = true
		}
		if tc.diff && len(seats) < 2 {
			t.Errorf("%s: targets %v span %d seat(s), want 2", tc.card, targets, len(seats))
		}
		if n, _, ok := oraclegen.Settle(reg, it.Scenario); !ok || n == 0 {
			t.Errorf("%s: generated scenario does not settle (targets %v)", tc.card, targets)
		}
	}
}
