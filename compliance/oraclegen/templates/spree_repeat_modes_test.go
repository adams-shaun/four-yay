package templates

import (
	"testing"
)

// TestSpreeAndRepeatModeAnswersReachTheCast pins the generator contract the
// XMage driver consumes: cast-step modes are numeric mode-queue answers, and a
// repeatable modal choice preserves duplicate picks in order.
func TestSpreeAndRepeatModeAnswersReachTheCast(t *testing.T) {
	reg := loadGenRegistry(t)
	spree := []string{
		"Dance of the Tumbleweeds", "Getaway Glamer", "Great Train Heist",
		"Insatiable Avarice", "Jailbreak Scheme", "Lively Dirge",
		"Metamorphic Blast", "Rush of Dread", "Shifting Grift",
		"Smuggler's Surprise", "Unfortunate Accident",
	}
	for _, name := range spree {
		t.Run(name, func(t *testing.T) {
			it, skip := Generate(reg, name)
			if skip != nil {
				t.Fatalf("scenario missing (vacuous test): %s", skip.Reason)
			}
			if len(it.Steps) == 0 || it.Steps[0].Op != "cast" {
				t.Fatalf("precondition: expected cast as first step, got %+v", it.Steps)
			}
			answers := stepAnswers(it.XAnswers, 0)
			if len(answers) == 0 {
				t.Fatalf("no cast-step XMage answers: %+v", it.XAnswers)
			}
			foundMode := false
			for _, a := range answers {
				if a.Kind == "mode" {
					foundMode = true
					if a.Value == "" || a.Value == "yes" || a.Value == "no" {
						t.Fatalf("Spree mode must use numeric mode queue: %+v", a)
					}
				}
			}
			if !foundMode {
				t.Fatalf("Spree cast has no mode answer: %+v", answers)
			}
		})
	}

	it, skip := Generate(reg, "Cosmium Confluence")
	if skip != nil {
		t.Fatalf("Cosmium Confluence scenario missing (vacuous test): %s", skip.Reason)
	}
	if len(it.Steps) == 0 || it.Steps[0].Op != "cast" {
		t.Fatalf("precondition: expected Cosmium cast as first step, got %+v", it.Steps)
	}
	var modes []string
	for _, a := range stepAnswers(it.XAnswers, 0) {
		if a.Kind == "mode" {
			modes = append(modes, a.Value)
		}
	}
	if len(modes) != 3 {
		t.Fatalf("Cosmium must emit three numeric mode picks, got %v (all answers %+v)", modes, stepAnswers(it.XAnswers, 0))
	}
	for _, mode := range modes {
		if mode != "1" && mode != "2" && mode != "3" {
			t.Fatalf("Cosmium mode = %q, want source mode 1, 2, or 3: %v", mode, modes)
		}
	}
}
