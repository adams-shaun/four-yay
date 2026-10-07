package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The Joo Dee defect, at the generator boundary: its activated ability creates
// a token copy of itself and then sacrifices an artifact or creature, so the
// sacrifice asks gorge to choose between the original and the token copy,
// which share a name. The emitted answer must distinguish them. The original
// is the card, so it carries XMage's isCopy() filter "[no copy]"; the token
// copy carries "[only copy]". A bare name (the pre-fix answer) leaves two
// candidates and XMage's choice matcher then picks whichever it iterates
// first -- the 2/2 flake this ticket fixes.
//
// This runs the real generator over the real corpus, so it pins the exact
// answer the driver receives, not a synthetic decision.
func TestJooDeeSacrificeAnswerDistinguishesOriginalFromCopy(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	const card = "Joo Dee, One of Many"
	cd, ok := reg.Lookup(card)
	if !ok {
		t.Skipf("%s is not in the corpus at this pin", card)
	}
	// The defect is the level-B activated-ability scenario (activate#0.0):
	// Surveil 1, create a token copy, then sacrifice an artifact or creature.
	var items []oraclegen.Item
	for _, r := range levelb.Requirements(cd) {
		if r.Gap != "" || !strings.Contains(r.Key, "activate#0.0") {
			continue
		}
		it, skip := GenerateB(reg, card, r)
		if skip != nil {
			t.Fatalf("%s/%s generated no item: %v", card, r.Key, skip.Reason)
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		t.Fatalf("%s no longer offers the activate#0.0 level-B requirement", card)
	}
	// Precondition: the scenario must actually pose a same-name choice, else
	// the assertion below would pass on a scenario that never asks.
	found := false
	for _, it := range items {
		for _, answers := range it.XAnswers {
			for _, a := range answers {
				if a.Kind != "choice" || !strings.Contains(a.Value, card) {
					continue
				}
				found = true
				t.Logf("item %s step answers include choice %q", it.ID, a.Value)
				if !strings.HasSuffix(a.Value, "[no copy]") && !strings.HasSuffix(a.Value, "[only copy]") {
					t.Fatalf("item %q emits a bare same-name choice %q; it cannot distinguish the card from its token copy", it.ID, a.Value)
				}
			}
		}
	}
	if !found {
		t.Fatalf("%s/activate#0.0 no longer poses a choice among same-named %s objects; regenerate the fixture meaning", card, card)
	}
}
