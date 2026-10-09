package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestKeywordActionTriggerClassification pins the routing this ticket
// (agent-20261009T160937Z-c135e2b0) added: a trigger whose cause is a keyword
// action (Forage, GiveGift, Explores, CollectEvidence, ManifestDread,
// Discover, ElementalBend, BecomesPlotted, BecomesSaddled) now names a
// servable sub-family instead of its trigger.gap:<Mode> bucket. The engine's
// trigmatch matchers already exist for every one; only the level-B cause was
// missing.
func TestKeywordActionTriggerClassification(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Corpseberry Cultivator", "trigger#0.1", "trigger.forage"},
		{"Jolly Gerbils", "trigger#0.0", "trigger.give-gift"},
		{"Merfolk Cave-Diver", "trigger#0.0", "trigger.explores"},
		{"Nicanzil, Current Conductor", "trigger#0.0", "trigger.explores"},
		{"Nicanzil, Current Conductor", "trigger#0.1", "trigger.explores"},
		{"Evidence Examiner", "trigger#0.1", "trigger.collect-evidence"},
		{"Surveillance Monitor", "trigger#0.1", "trigger.collect-evidence"},
		{"Paranormal Analyst", "trigger#0.0", "trigger.manifest-dread"},
		{"Curator of Sun's Creation", "trigger#0.0", "trigger.discover"},
		{"Avatar Aang", "trigger#0.0", "trigger.elemental-bend"},
		{"Aloe Alchemist", "trigger#0.0", "trigger.becomes-plotted"},
		{"Longhorn Sharpshooter", "trigger#0.0", "trigger.becomes-plotted"},
		{"Stubborn Burrowfiend", "trigger#0.0", "trigger.becomes-saddled"},
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s not in the corpus", tc.name)
		}
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Key != tc.key {
				continue
			}
			found = true
			if r.Sub != tc.sub || r.Gap != "" {
				t.Fatalf("%s %s classified sub=%q gap=%q, want %q with no gap", tc.name, tc.key, r.Sub, r.Gap, tc.sub)
			}
		}
		if !found {
			t.Fatalf("%s carries no requirement %s", tc.name, tc.key)
		}
	}
}
