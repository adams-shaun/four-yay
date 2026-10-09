package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestSpellCastProvenanceCauseFires pins the cast-provenance causes: a
// wasCastFromExile / !wasCastFromYourHand trigger fires when the probe is cast
// out of p0's exile, and a Card.Adventure trigger fires when an Adventure
// spell face is cast from p0's hand. Before the cause existed each row skipped
// with "spell-cast unsupported <x> provenance" (spellCastNarrowSkip).
//
// Each subtest asserts the PRECONDITION its firing depends on -- the trigger
// really names the provenance predicate and the cause's own probe is in the
// zone the provenance needs -- so a vacuous setup fails loudly instead of
// passing on a different cause.
func TestSpellCastProvenanceCauseFires(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name       string
		reqKey     string
		filterWant string // the provenance token the trigger's ValidCard$ must carry
		probe      string // the card the cause casts
		exile      bool   // the probe starts in p0's exile (else p0's hand)
	}{
		{"Quintorius Kand", "trigger#0.0", "wascastfromexile", "Misthollow Griffin", true},
		{"Kellan, the Kid", "trigger#0.0", "!wascastfromyourhand", "Misthollow Griffin", true},
		{"Shadow of the Goblin", "trigger#0.3", "!wascastfromyourhand", "Misthollow Griffin", true},
		{"Chancellor of Tales", "trigger#0.0", "adventure", "Beanstalk Wurm", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 || len(card.Faces[0].Triggers) == 0 {
				t.Fatalf("precondition: %s trigger is unavailable", tc.name)
			}
			var req levelb.Requirement
			for _, r := range levelb.Requirements(card) {
				if r.Key == tc.reqKey {
					req = r
					break
				}
			}
			if req.Key == "" {
				t.Fatalf("precondition: %s %s requirement missing", tc.name, tc.reqKey)
			}
			// The requirement must actually be the provenance-gated trigger.
			idx, err := strconv.Atoi(req.Slot)
			if err != nil || idx < 0 || idx >= len(card.Faces[0].Triggers) {
				t.Fatalf("precondition: %s slot %q: %v", tc.name, req.Slot, err)
			}
			filter := strings.ToLower(strings.ReplaceAll(
				card.Faces[0].Triggers[idx].ParamStr(cards.PKValidCard)+","+
					card.Faces[0].Triggers[idx].ParamStr(cards.PKValidSAonCard), " ", ""))
			if !strings.Contains(filter, tc.filterWant) {
				t.Fatalf("precondition: %s filter %q lacks %q", tc.name, filter, tc.filterWant)
			}
			item, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("%s still skipped: %s", tc.name, skip.Reason)
			}
			// The cause's own probe must be placed in the zone the provenance
			// needs, or the trigger could not have fired from this cause.
			setup := item.Scenario.Setup["p0"]
			if tc.exile {
				if !containsString(setup.Exile, tc.probe) {
					t.Fatalf("precondition: probe %s not in p0 exile %v", tc.probe, setup.Exile)
				}
			} else if !containsString(setup.Hand, tc.probe) {
				t.Fatalf("precondition: probe %s not in p0 hand %v", tc.probe, setup.Hand)
			}
			res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
			if err != nil {
				t.Fatalf("%s replay err=%v", tc.name, err)
			}
			if !abilityOnStack(res.Snapshots, tc.name, card.Faces[0].Name, stackSlot(req)) {
				t.Fatalf("%s trigger never reached the stack", tc.name)
			}
		})
	}
}
