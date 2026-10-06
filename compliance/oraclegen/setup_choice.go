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
