package templates

// Regression test for the repeat-mode Charm plan order: Unite the Coalition
// (CharmNum$ 5, CanRepeatModes$ True) used to try its 125 repeating plans
// before the one all-distinct plan, and EVERY plan failed the runner's
// one-use-per-label mode matcher, so the generator spent ~45 s discovering no
// fixture. With the repeat-free combination first and the runner resubmitting
// a repeated index, generation succeeds at the first or second plan.

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// generatedModesAnswer returns the modes pick of a generated item.
func generatedModesAnswer(item oraclegen.Item) []string {
	for _, step := range item.Steps {
		for _, answer := range step.Answers {
			if answer.Kind == "modes" {
				return answer.Pick
			}
		}
	}
	return nil
}

func TestGenerateUniteTheCoalitionSucceedsAtFirstPlan(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	card, ok := reg.Lookup("Unite the Coalition")
	if !ok || len(card.Faces) == 0 {
		t.Fatal("Unite the Coalition missing from corpus")
	}
	face := card.Faces[0]
	var charmFound bool
	for _, ab := range face.Abilities {
		if ab.Kind == "SP" && ab.API == "Charm" {
			charmFound = true
			if ab.Params["CharmNum"] != "5" || ab.Params["CanRepeatModes"] != "True" {
				t.Fatalf("precondition: corpus charm parameters changed: %+v", ab.Params)
			}
		}
	}
	if !charmFound {
		t.Fatal("precondition: Unite the Coalition has no Charm ability")
	}

	item, skip := Generate(reg, "Unite the Coalition")
	if skip != nil {
		t.Fatalf("Generate: %s", skip.Reason)
	}
	picks := generatedModesAnswer(item)
	if len(picks) != 5 {
		t.Fatalf("modes answer %v has %d picks, want 5", picks, len(picks))
	}
	if _, ok := oraclegen.PlaysThrough(reg, item.Scenario); !ok {
		t.Fatalf("generated scenario does not replay: modes %v", picks)
	}

	// The emitted plan must be the first or second combination. Plan order is
	// CharmCombinations order: the all-distinct combination is first, so a
	// success at index <= 1 is exactly "first or second plan".
	combos := oraclegen.CharmCombinations(face)
	if len(combos) == 0 {
		t.Fatal("precondition: no charm combinations")
	}
	planIndex := -1
	for i, combo := range combos {
		labels := make([]string, 0, len(combo.Modes))
		for _, m := range combo.Modes {
			labels = append(labels, m.Label())
		}
		if reflect.DeepEqual(labels, picks) {
			planIndex = i
			break
		}
	}
	if planIndex < 0 {
		t.Fatalf("the generated modes pick %v is not a charm combination", picks)
	}
	if planIndex > 1 {
		t.Fatalf("generation reached plan %d, want the first or second plan (combos[0]=%v)", planIndex, combos[0].Modes)
	}
}
