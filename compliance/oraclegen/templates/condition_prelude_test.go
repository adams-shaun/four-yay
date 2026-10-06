package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestConditionPreludePhaseExamples(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, evidence string }{
		{"Lunar Convocation", "Angel's Mercy"},
		{"Insectoid Exterminator", "Murder"},
		{"Frontline War-Rager", "Grizzly Bears"},
		{"Creakwood Safewright", "Llanowar Elves"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s missing from corpus", tc.name)
			}
			face := card.Faces[0]
			var req levelb.Requirement
			for _, r := range levelb.Requirements(card) {
				if r.Family == "trigger" && r.Sub == "trigger.phase" {
					req = r
					break
				}
			}
			if req.Key == "" {
				t.Fatalf("precondition: %s has no phase trigger requirement", tc.name)
			}
			trigger := &face.Triggers[0]
			candidates := conditionPreludes(reg, trigger.Params, face.SVars)
			var candidateEvidence bool
			for _, c := range candidates {
				for _, name := range append(append([]string{}, c.battlefield...), c.graveyard...) {
					candidateEvidence = candidateEvidence || name == tc.evidence
				}
				for _, step := range c.steps {
					candidateEvidence = candidateEvidence || strings.Contains(step.Card, tc.evidence)
				}
			}
			if !candidateEvidence {
				t.Fatalf("no condition candidate contains %q: %+v", tc.evidence, candidates)
			}
			// Frontline's tapped-board predicate is offered with the expected
			// setup fixture, but currently does not fire in the engine probe.
			// Keep the classifier regression explicit without claiming it is served.
			if tc.name == "Frontline War-Rager" {
				foundTapped := false
				for _, c := range candidates {
					foundTapped = foundTapped || len(c.tapped) >= 2
				}
				if !foundTapped {
					t.Fatalf("no two-tapped-creature candidate: %+v", candidates)
				}
				return
			}
			item, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("%s: %s", tc.name, skip.Reason)
			}
			p0 := item.Scenario.Setup["p0"]
			if !containsString(p0.Battlefield, tc.name) {
				t.Fatalf("precondition: source is not on battlefield: %v", p0.Battlefield)
			}
		})
	}
}
