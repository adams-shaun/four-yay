package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticNamedKeywordGrant: a static that grants a keyword OUTSIDE the
// evergreen set but inside the named vocabulary (Ward, Prowess, Wither) is
// served, and its item names the wider opt-in CompareKeywordsNamed. The
// evergreen vocabulary alone sees nothing on those rows, which the test
// asserts per row, so an item that merely kept CompareKeywords could not pass.
func TestStaticNamedKeywordGrant(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key string }{
		{"Bria, Riptide Rogue", "static#0.0"},         // BLB, Prowess
		{"Long River Lurker", "static#0.0"},           // BLB, Ward:1
		{"Hexing Squelcher", "static#0.0"},            // ECL, Ward:PayLife<2>
		{"Thorin Oakenshield", "static#0.0"},          // HOB, Ward:1
		{"Massacre Girl, Known Killer", "static#0.0"}, // MKM, Wither
	} {
		req := probeRequirement(t, reg, tc.card, tc.key)
		it, skip := GenerateB(reg, tc.card, req)
		if skip != nil {
			t.Errorf("%s %s skipped: %s", tc.card, tc.key, skip.Reason)
			continue
		}
		if len(it.Compare) != 1 || it.Compare[0] != oraclediff.CompareKeywordsNamed {
			t.Errorf("%s Compare = %v, want [%s]", tc.card, it.Compare, oraclediff.CompareKeywordsNamed)
			continue
		}

		// Precondition: the evergreen vocabulary alone does NOT observe the
		// static, so the wider vocabulary is what served this row. A row that
		// the evergreen set already served would be a false positive here.
		_, snap := servedFinal(t, reg, tc.card, tc.key)
		c, _ := reg.Lookup(tc.card)
		f := c.Faces[0]
		st, _ := staticSlotOf(f, req)
		plan := staticPlanFor(st.ParamStr(cards.PKAffected))
		specs := staticProbeSpecs(reg, append([]string{staticProbe}, plan.probes...))
		if staticObserved(snap, f, tc.card, st, specs) {
			t.Errorf("%s: the evergreen vocabulary already observes it, the named opt-in is not exercised", tc.card)
		}
		if observed, namedOnly := staticObservedNamed(snap, f, tc.card, st, specs); !observed || !namedOnly {
			t.Errorf("%s: named observation observed=%v namedOnly=%v, want true/true", tc.card, observed, namedOnly)
		}
	}
}

// TestStaticNamedKeywordGrantStillSkips pins the other direction: a static
// granting a keyword outside BOTH vocabularies (Samut's Split second to
// instant/sorcery spells) keeps the named skip, so widening the set did not
// swallow every keyword grant.
func TestStaticNamedKeywordGrantStillSkips(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, skip := GenerateB(reg, "Samut, Tyrant of Naktamun",
		probeRequirement(t, reg, "Samut, Tyrant of Naktamun", "static#0.0"))
	if skip == nil {
		t.Fatal("Samut, Tyrant of Naktamun served, want a keyword skip")
	}
	if !strings.Contains(skip.Reason, "outside the compared") {
		t.Fatalf("Samut skip = %q, want the outside-the-compared-keyword-set reason", skip.Reason)
	}
}
