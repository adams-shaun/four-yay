package templates

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestStaticContinuousProbeCapSpiderHam pins the capped probe cluster and
// documents that Spider-Ham's uniform pump remains observable on a retained
// probe. If the retained probes cannot observe an effect, the cap's dropped
// filter words produce a specific skip instead of the generic mystery skip.
func TestStaticContinuousProbeCapSpiderHam(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const card, key = "Spider-Ham, Peter Porker", "static#0.0"
	req := probeRequirement(t, reg, card, key)
	c, ok := reg.Lookup(card)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("precondition: %s is absent from the corpus", card)
	}
	if req.Face < 0 || req.Face >= len(c.Faces) {
		t.Fatalf("precondition: %s %s has invalid face %d", card, key, req.Face)
	}
	slot, err := strconv.Atoi(req.Slot)
	if err != nil || slot < 0 || slot >= len(c.Faces[req.Face].Statics) {
		t.Fatalf("precondition: %s %s has invalid static slot %q", card, key, req.Slot)
	}
	plan := staticPlanFor(c.Faces[req.Face].Statics[slot].ParamStr(cards.PKAffected))
	wantProbes := []string{"Mineshaft Spider", "Lifecreed Duo", "Savannah Lions"}
	if !slices.Equal(plan.probes, wantProbes) {
		t.Fatalf("retained probes = %v, want first capped probes %v", plan.probes, wantProbes)
	}
	wantDropped := []string{"Frog", "Mouse", "Rabbit", "Squirrel", "Turtle"}
	if !slices.Equal(plan.dropped, wantDropped) {
		t.Fatalf("dropped filter words = %v, want %v", plan.dropped, wantDropped)
	}
	gap := staticProbeCapGap(plan)
	if !strings.Contains(gap, "5 dropped:") {
		t.Fatalf("truncation reason %q does not name the count", gap)
	}
	for _, word := range wantDropped {
		if !strings.Contains(gap, word) {
			t.Fatalf("truncation reason %q does not name dropped word %q", gap, word)
		}
	}

	const retainedProbe = "Mineshaft Spider"
	probeItem, why := staticBase(reg, c, c.Faces[req.Face], card, req, plan, nil)
	if why != "" {
		t.Fatalf("retained-probe scenario unavailable: %s", why)
	}
	if !slices.Contains(probeItem.Setup["p0"].Battlefield, retainedProbe) {
		t.Fatalf("precondition: p0 battlefield %v lacks retained probe %s", probeItem.Setup["p0"].Battlefield, retainedProbe)
	}
	res, err := rules.RunOracleScenarioJSON(reg, probeItem.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("retained-probe scenario does not replay: err=%v fails=%v", err, res.Fails)
	}
	p, ok := permanent(res.Snapshots[len(res.Snapshots)-1], 0, retainedProbe)
	if !ok {
		t.Fatalf("precondition: %s is not on p0's final battlefield", retainedProbe)
	}
	printed := printedPT(t, reg, retainedProbe)
	if p.PT == printed {
		t.Fatalf("Spider-Ham's uniform +1/+1 was not observed: %s remains %s", retainedProbe, printed)
	}
	if _, final := servedFinal(t, reg, card, key); len(final.Permanents) == 0 {
		t.Fatalf("precondition: generated %s scenario has no final permanents", card)
	}
}
