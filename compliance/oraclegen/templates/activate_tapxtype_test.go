package templates

import (
	"strings"
	"testing"
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
					t.Errorf("cost fixture %q absent from battlefield %v", want, seat.Battlefield)
				}
				for _, name := range seat.Tapped {
					if name == want {
						t.Errorf("cost fixture %q starts tapped", want)
					}
				}
				answers := 0
				for _, step := range it.XAnswers {
					for _, answer := range step {
						if answer.Kind == "choice" && strings.EqualFold(answer.Value, want) {
							answers++
						}
					}
				}
				if answers != 1 {
					t.Errorf("XMage cost answer for %q occurs %d times: %+v", want, answers, it.XAnswers)
				}
			}
		})
	}
}
