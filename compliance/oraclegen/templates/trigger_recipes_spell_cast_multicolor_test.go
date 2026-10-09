package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestSpellCastMulticolourCauseFires pins the multicolour spell-cast cause:
// a ValidCard$ Card.MultiColor trigger fires from a plain hand cast of
// Terminate, the multicoloured instant the cause prepends. Before the cause
// existed every preferred probe was mono, so the row skipped with
// "spell-cast unsupported cast-from-hand provenance" (spellCastNarrowSkip,
// Lilah, Undefeated Slickshot OTJ).
//
// The subtests assert the PRECONDITIONS the firing depends on: the trigger's
// filter really carries MultiColor, the probe really is a multicoloured
// instant whose spell needs a creature target, and the target creature setup
// places is where the cause aims the cast.
func TestSpellCastMulticolourCauseFires(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Lilah, Undefeated Slickshot")
	if !ok || len(card.Faces) == 0 || len(card.Faces[0].Triggers) == 0 {
		t.Fatalf("precondition: Lilah, Undefeated Slickshot trigger is unavailable")
	}
	probe, ok := reg.Lookup(multicolourProbe)
	if !ok || len(probe.Faces) == 0 {
		t.Fatalf("precondition: probe %s is unavailable", multicolourProbe)
	}
	pface := probe.Faces[0]
	if !pface.IsInstant() || popcount(cards.ManaCostColours(pface.ManaCost)) < 2 {
		t.Fatalf("precondition: %s is not a multicoloured instant (%s %q)", multicolourProbe, pface.ManaCost, pface.Types)
	}
	if !spellProbePlayerTargetBlocked(pface) {
		t.Fatalf("precondition: %s accepts a player target, the blocked-target shape this cause exists for", multicolourProbe)
	}
	var req levelb.Requirement
	for _, r := range levelb.Requirements(card) {
		if r.Key == "trigger#0.0" {
			req = r
			break
		}
	}
	if req.Key == "" {
		t.Fatalf("precondition: Lilah trigger#0.0 requirement missing")
	}
	filter := strings.ToLower(card.Faces[0].Triggers[0].ParamStr(cards.PKValidCard))
	if !strings.Contains(filter, "multicolor") {
		t.Fatalf("precondition: Lilah trigger filter %q lacks multicolor", filter)
	}
	item, skip := GenerateB(reg, "Lilah, Undefeated Slickshot", req)
	if skip != nil {
		t.Fatalf("Lilah still skipped: %s", skip.Reason)
	}
	setup := item.Scenario.Setup["p0"]
	if !containsString(setup.Hand, multicolourProbe) {
		t.Fatalf("precondition: probe %s not in p0 hand %v", multicolourProbe, setup.Hand)
	}
	if !containsString(setup.Battlefield, bearsProbe) {
		t.Fatalf("precondition: target creature %s not on p0 battlefield %v", bearsProbe, setup.Battlefield)
	}
	if !containsString(setup.Battlefield, "Lilah, Undefeated Slickshot") {
		t.Fatalf("precondition: source not on p0 battlefield %v", setup.Battlefield)
	}
	res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
	if err != nil {
		t.Fatalf("replay err=%v", err)
	}
	if !abilityOnStack(res.Snapshots, "Lilah, Undefeated Slickshot", card.Faces[0].Name, stackSlot(req)) {
		t.Fatalf("Lilah's trigger never reached the stack")
	}
}

// popcount counts the set bits of a colour mask.
func popcount(m uint8) int {
	n := 0
	for m != 0 {
		n += int(m & 1)
		m >>= 1
	}
	return n
}
