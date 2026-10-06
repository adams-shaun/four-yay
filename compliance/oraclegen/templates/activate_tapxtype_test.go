package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestActivateTapXTypeFixtures(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name, key, prefix string
		want              []string
	}{
		{"Great Gilded Boat", "activate#0.0", "Crew 2", []string{"Colossal Dreadmaw"}},
		{"Alacrian Jaguar", "activate#0.0", "Saddle 1", []string{"Colossal Dreadmaw"}},
		{"Kithkeeper", "activate#0.0", "Tap three untapped creatures you control", []string{"Grizzly Bears", "Llanowar Elves", "Nessian Asp"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 || len(card.Faces[0].Abilities) == 0 || !card.Faces[0].Abilities[0].IsActivated() {
				t.Fatalf("precondition: %s activated ability is missing from corpus", tc.name)
			}
			assertActivateItem(t, reg, tc.name, tc.key, tc.prefix)
			it, _ := activateRequirement(t, reg, tc.name, tc.key)
			seat := it.Scenario.Setup["p0"]
			for _, want := range tc.want {
				found := false
				for _, name := range seat.Battlefield {
					if name == want {
						found = true
					}
				}
				if !found {
					t.Fatalf("precondition: cost fixture %q absent from battlefield %v", want, seat.Battlefield)
				}
				for _, name := range seat.Tapped {
					if name == want {
						t.Fatalf("precondition: cost fixture %q starts tapped", want)
					}
				}
			}

			step := activateStepIndex(it.Steps)
			answers := map[string]bool{}
			for _, answer := range it.XAnswers[step] {
				if answer.Seat != 0 || answer.Kind != "choice" || strings.Contains(answer.Value, "^") {
					continue
				}
				key := strings.ToLower(answer.Value)
				if answers[key] {
					t.Fatalf("conflicting duplicate XMage cost answer %q: %+v", answer.Value, it.XAnswers[step])
				}
				answers[key] = true
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Snapshots) == 0 {
				t.Fatal("precondition: generated activation must play through to a final snapshot")
			}
			before := map[string]bool{}
			for _, name := range seat.Tapped {
				before[strings.ToLower(name)] = true
			}
			actual := map[string]bool{}
			for _, permanent := range res.Snapshots[len(res.Snapshots)-1].Permanents {
				if permanent.Controller == 0 && permanent.Tapped && !before[strings.ToLower(permanent.Name)] {
					actual[strings.ToLower(permanent.Name)] = true
				}
			}
			if len(actual) != len(answers) {
				t.Fatalf("cost answers %v do not match gorge's newly tapped permanents %v", answers, actual)
			}
			for name := range actual {
				if !answers[name] {
					t.Errorf("gorge tapped %q but XMage cost answers do not name it: %v", name, answers)
				}
			}
			for name := range answers {
				if !actual[name] {
					t.Errorf("XMage cost answer names %q but gorge did not tap it: %v", name, actual)
				}
			}
		})
	}
}
