package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The three corpus carriers exercise the same event-batch count head. Keep
// the actual compiled card SVars tied to the evaluator rather than a copied
// literal, while effects/count_triggerobjects_test pins the batch semantics.
func TestRepoTriggerObjectsCardsHeadsCompileAndResolve(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := newSeats(t, 2)
	for _, name := range []string{"Polluted Cistern // Dim Oubliette", "Amzu, Swarms' Hunger", "The Skullspore Nexus"} {
		cd, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus card %q missing", name)
		}
		for _, face := range cd.Faces {
			for _, body := range face.SVars {
				if len(body) < len("TriggerObjectsCards$") || body[:len("TriggerObjectsCards$")] != "TriggerObjectsCards$" {
					continue
				}
				if _, ok := effects.EvalCountOK(e, &effects.Ctx{}, body); !ok {
					t.Errorf("%s SVar body %q did not resolve", name, body)
				}
			}
		}
	}
}
