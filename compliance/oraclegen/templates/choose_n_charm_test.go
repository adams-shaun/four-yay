package templates

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Some charms permit fewer than CharmNum$ picks. The minimum must be tried
// first: requiring all three modes can demand mutually unsatisfiable targets.
func TestGenerateOptionalChooseNCharmModes(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	for _, tc := range []struct {
		name string
		max  string
	}{
		{"Shifting Grift", "3"},
		{"Choreographed Sparks", "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("missing corpus card %s", tc.name)
			}
			face := card.Faces[0]
			var charmFound bool
			for _, ability := range face.Abilities {
				if ability.Kind == "SP" && ability.API == "Charm" {
					charmFound = true
					if ability.Params["MinCharmNum"] != "1" || ability.Params["CharmNum"] != tc.max {
						t.Fatalf("corpus pick range changed: %+v", ability.Params)
					}
				}
			}
			if !charmFound {
				t.Fatal("corpus no longer has a Charm spell")
			}
			combos := oraclegen.CharmCombinations(face)
			if len(combos) == 0 || len(combos[0].Modes) != 1 {
				t.Fatalf("first legal combination should select one mode: %+v", combos)
			}
			item, skip := Generate(reg, tc.name)
			if skip != nil {
				t.Fatalf("Generate: %s", skip.Reason)
			}
			var picks []string
			for _, step := range item.Steps {
				for _, answer := range step.Answers {
					if answer.Kind == "modes" {
						picks = answer.Pick
					}
				}
			}
			if len(picks) != 1 {
				t.Fatalf("expected one legal mode pick, got %v", picks)
			}
			if _, ok := oraclegen.PlaysThrough(reg, item.Scenario); !ok {
				t.Fatalf("scenario does not replay: %v", picks)
			}
		})
	}
}

func TestGenerateChooseNCharmModes(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	cases := []struct {
		name   string
		count  int
		repeat bool
	}{
		{"Ashling's Command", 2, false},
		{"Brigid's Command", 2, false},
		{"Grub's Command", 2, false},
		{"Sygg's Command", 2, false},
		{"Trystan's Command", 2, false},
		{"Return from the Wilds", 2, false},
		{"Cosmium Confluence", 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("card %q missing from loaded corpus", tc.name)
			}
			face := card.Faces[0]
			foundPrecondition := false
			for _, ability := range face.Abilities {
				if ability.Kind == "SP" && ability.API == "Charm" {
					foundPrecondition = ability.Params["CharmNum"] == strconv.Itoa(tc.count) && strings.EqualFold(ability.Params["CanRepeatModes"], "True") == tc.repeat
					break
				}
			}
			if !foundPrecondition {
				t.Fatalf("corpus CharmNum$/CanRepeatModes$ precondition changed for %s", tc.name)
			}
			faceModes := oraclegen.CharmCombinations(face)
			if !reflect.DeepEqual(faceModes, oraclegen.CharmCombinations(face)) {
				t.Fatal("mode combination order is not deterministic")
			}
			modes := oraclegen.CharmModes(face)
			if len(faceModes) == 0 || faceModes[0].Modes[0].Label() != modes[0].Label() {
				t.Fatalf("first combination does not start with first Choices$ mode: %+v", faceModes)
			}
			foundRepeatedCombination := false
			for _, combo := range faceModes {
				seen := map[string]bool{}
				repeated := false
				for _, mode := range combo.Modes {
					if seen[mode.Label()] {
						repeated = true
					}
					seen[mode.Label()] = true
				}
				if repeated && !tc.repeat {
					t.Fatalf("non-repeat Charm enumerated repeated picks: %+v", combo.Modes)
				}
				foundRepeatedCombination = foundRepeatedCombination || repeated
			}
			if foundRepeatedCombination != tc.repeat {
				t.Fatalf("repeated-combination availability = %v, CanRepeatModes$ = %v", foundRepeatedCombination, tc.repeat)
			}
			item, skip := Generate(reg, tc.name)
			if skip != nil {
				t.Fatalf("Generate: %s", skip.Reason)
			}
			var picks []string
			for _, answer := range item.Steps[0].Answers {
				if answer.Kind == "modes" {
					picks = answer.Pick
					break
				}
			}
			if len(picks) != tc.count {
				t.Fatalf("modes answer %v has %d picks, want %d", picks, len(picks), tc.count)
			}
			if !tc.repeat {
				seen := map[string]bool{}
				for _, pick := range picks {
					if seen[pick] {
						t.Fatalf("non-repeat Charm selected mode twice: %v", picks)
					}
					seen[pick] = true
				}
			}
			if _, ok := oraclegen.PlaysThrough(reg, item.Scenario); !ok {
				t.Fatalf("generated scenario does not replay: modes %v", picks)
			}
		})
	}
}
