package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func assertStepCard(t *testing.T, steps []oraclegen.Step, card string) {
	t.Helper()
	for _, step := range steps {
		if step.Card == "p0:"+card {
			return
		}
	}
	t.Fatalf("expected %s cast prelude, got %+v", card, steps)
}

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
			idx, err := strconv.Atoi(req.Slot)
			if err != nil || idx < 0 || idx >= len(face.Triggers) {
				t.Fatalf("invalid trigger slot %q", req.Slot)
			}
			trigger := &face.Triggers[idx]
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
			item, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("%s: %s", tc.name, skip.Reason)
			}
			p0 := item.Scenario.Setup["p0"]
			if !containsString(p0.Battlefield, tc.name) {
				t.Fatalf("precondition: source is not on battlefield: %v", p0.Battlefield)
			}
			switch tc.name {
			case "Lunar Convocation":
				assertStepCard(t, item.Scenario.Steps, "Angel's Mercy")
			case "Insectoid Exterminator":
				assertStepCard(t, item.Scenario.Steps, "Murder")
			case "Frontline War-Rager":
				foundAttack := false
				for _, step := range item.Scenario.Steps {
					foundAttack = foundAttack || step.Op == "attack"
				}
				if !foundAttack {
					t.Fatalf("expected attack prelude for tapped-board condition: %+v", item.Scenario.Steps)
				}
			case "Creakwood Safewright":
				if !containsString(p0.Graveyard, "Llanowar Elves") {
					t.Fatalf("expected Elf graveyard fixture, got %v", p0.Graveyard)
				}
			}

		})
	}
}
