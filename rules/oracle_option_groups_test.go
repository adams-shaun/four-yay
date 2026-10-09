package rules_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestOracleDecisionRecordsOptionGroups pins the engine half of the
// per-player target-ask routing: Kaya, Spirits' Justice's -2 poses the
// mid-resolution "exile up to one target creature that player controls" ask
// as a KChoose whose options carry the target-controller group, and the
// oracle recording must carry those groups downstream — the compliance
// adapter routes a declined grouped ask to one XMage target skip per
// offered seat, which it can only recognise from the recorded groups.
func TestOracleDecisionRecordsOptionGroups(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Kaya, Spirits' Justice")
	if !ok {
		t.Fatal("precondition: Kaya, Spirits' Justice is not in the corpus")
	}
	var req *levelb.Requirement
	reqs := levelb.Requirements(c)
	for i := range reqs {
		if reqs[i].Key == "activate#0.2" {
			req = &reqs[i]
			break
		}
	}
	if req == nil {
		t.Fatal("precondition: no activate#0.2 requirement for Kaya, Spirits' Justice")
	}
	it, skip := templates.GenerateB(reg, "Kaya, Spirits' Justice", *req)
	if skip != nil {
		t.Fatalf("precondition: scenario does not settle: %s", skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failures: %v", res.Fails)
	}
	found := false
	for _, d := range res.Decisions {
		if d.Kind != "choose_n" || len(d.OptionGroups) == 0 {
			continue
		}
		for _, g := range d.OptionGroups {
			if !strings.HasPrefix(g, "target-controller-") {
				t.Fatalf("grouped choose carries a non-target group %q", g)
			}
		}
		if len(d.OptionRefs) == 0 {
			t.Fatalf("the grouped choose offers no object options: %+v", d)
		}
		found = true
	}
	if !found {
		t.Fatal("no grouped target ask recorded; the engine recording lost the option groups")
	}
}
