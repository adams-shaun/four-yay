package oraclegen

import "github.com/adams-shaun/gorge/rules"

// IsSetupChoice reports whether d is a setup-placed permanent's as-enters
// colour or type choice: gorge records it before step zero (Step < 0,
// Resume "etb") and XMage poses the same dialog while placing the seeded
// permanent, so the answer must be queued ahead of the first gameplay answer
// or that dialog consumes it. The pick kind is the option kind gorge's ETB ask
// builds: "color" for ChooseColor, "type" for ChooseType (the creature list,
// or another category's list, both string-keyed XMage choices).
func IsSetupChoice(d rules.OracleDecision) bool {
	if d.Step >= 0 || d.Resume != "etb" || d.Kind != "choose_n" || len(d.Picks) != 1 || len(d.PickKinds) != 1 {
		return false
	}
	return d.PickKinds[0] == "color" || d.PickKinds[0] == "type"
}

// HoistSetupChoices moves every setup_choice answer in a step's answer stream
// to its front, preserving their relative order. XAnswers already prepends them
// to step zero, but a template reorders a step's answers afterwards -- the
// activate template hoists the activation-cost picks (Lifecraft Engine's crew
// choice) to the front of the activation step, which would put a gameplay
// answer ahead of the as-enters dialog and let XMage consume it there ("Choice
// key [Colossal Dreadmaw] not found"). Calling this at the end of any such
// reorder restores the invariant that the as-enters queue is answered first,
// whatever the setup kind.
func HoistSetupChoices(answers [][]XAnswer, step int) {
	if step < 0 || step >= len(answers) {
		return
	}
	var setup, rest []XAnswer
	for _, a := range answers[step] {
		if a.Kind == "setup_choice" {
			setup = append(setup, a)
			continue
		}
		rest = append(rest, a)
	}
	if len(setup) == 0 {
		return
	}
	answers[step] = append(setup, rest...)
}
