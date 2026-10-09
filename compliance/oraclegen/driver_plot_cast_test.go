package oraclegen_test

import (
	"os"
	"strings"
	"testing"
)

// TestDriverPlotCast pins the driver's cast_mode "plot" wiring: the plot is
// XMage's PlotAbility special action (no stack, not a cast), so the cast arm
// must pay the step's mana and activate the step's xmage_ability text instead
// of calling castSpell, reject a plot step with no text, and keep rejecting
// "plot_cast" (the later cast of a plotted card).
func TestDriverPlotCast(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)

	cast, ok := caseBody(java, "cast")
	if !ok {
		t.Fatal("ScenarioReplay.java has no cast case")
	}
	// Precondition: the arm adds the step's mana before the plot branch, and
	// the ordinary cast path (castSpell) exists after it for the order check.
	manaAt := strings.Index(cast, `addPool(pl, g, mana)`)
	plotAt := strings.Index(cast, `if ("plot".equals(castMode))`)
	castAt := strings.Index(cast, `castSpell(turn, phase, p, card)`)
	if manaAt < 0 || castAt < 0 {
		t.Fatalf("cast arm lost its mana or castSpell path; the assertions below would be vacuous:\n%s", cast)
	}
	if plotAt < 0 {
		t.Fatal(`cast arm has no "plot" branch`)
	}
	if !(manaAt < plotAt && plotAt < castAt) {
		t.Errorf("plot branch must follow the mana add (%d) and precede the ordinary cast (%d); it is at %d", manaAt, castAt, plotAt)
	}
	branchEnd := strings.Index(cast[plotAt:], "return;")
	if branchEnd < 0 {
		t.Fatal(`the "plot" branch does not return`)
	}
	branch := cast[plotAt : plotAt+branchEnd]
	if !strings.Contains(branch, "xabilityAt(stepIdx)") ||
		!strings.Contains(branch, `has no xmage_ability`) ||
		!strings.Contains(branch, "activateAbility(turn, phase, p, plotText)") {
		t.Errorf("plot branch must read xmage_ability, reject an empty one, and activateAbility:\n%s", branch)
	}
	if strings.Contains(branch, "castSpell") {
		t.Errorf("plot is a special action, not a cast; the branch calls castSpell:\n%s", branch)
	}

	// castModeSupported admits plot and only plot (not plot_cast).
	i := strings.Index(java, "static boolean castModeSupported(String mode)")
	if i < 0 {
		t.Fatal("castModeSupported not found")
	}
	helper := java[i : i+strings.Index(java[i:], "}\n")]
	if !strings.Contains(helper, `mode.equals("plot")`) {
		t.Error(`castModeSupported does not admit "plot"`)
	}
	if strings.Contains(helper, "plot_cast") {
		t.Error(`castModeSupported must keep rejecting "plot_cast"`)
	}
}
