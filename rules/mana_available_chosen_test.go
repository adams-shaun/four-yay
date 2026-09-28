package rules

// The available-mana projection must read the source's RECORDED as-enters
// choice before parsing its Produced$ value. A "Combo R Chosen" permanent
// (a thriving/gate land: "Add {R} or one mana of the chosen color") reports
// its real possibilities only after the recorded ChosenColor is substituted
// into the Chosen token; feeding the raw value to ProducedCounts advertised
// all five colours, so the seat rail claimed a green pip the source cannot
// actually produce. The substitution is the same read the activation path
// performs (substituteChosenProduced), now applied in addAvailable too.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestAvailableManaReadsRecordedChosenColour pins the source-aware answer for
// the Combo-Chosen family: with R recorded the source offers R alone; with the
// differing G recorded it offers R and G, and neither reports the other three
// colours. Both branches are asserted so a hard-coded colour cannot satisfy it.
func TestAvailableManaReadsRecordedChosenColour(t *testing.T) {
	t.Parallel()
	src := "Name:Thriving Bluff\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo R Chosen | Oracle:x\n"

	eR := layerEngine(t)
	idR := onBoard(t, eR, 0, src)
	oR := eR.G.Obj(idR)
	if oR.Zone != state.ZBattlefield || oR.Tapped {
		t.Fatalf("precondition: source not on battlefield untapped: %+v", oR)
	}
	oR.ChosenColor = "R"
	gotR := eR.AvailableMana(0)
	if gotR[state.MR] != 1 {
		t.Errorf("recorded R: red = %d, want 1", gotR[state.MR])
	}
	wantR := state.Mana{state.MR: 1}
	if gotR != wantR {
		t.Errorf("recorded R: available = %v, want %v (only R)", gotR, wantR)
	}

	eG := layerEngine(t)
	idG := onBoard(t, eG, 0, src)
	oG := eG.G.Obj(idG)
	if oG.Zone != state.ZBattlefield || oG.Tapped {
		t.Fatalf("precondition: source not on battlefield untapped: %+v", oG)
	}
	oG.ChosenColor = "G"
	gotG := eG.AvailableMana(0)
	wantG := state.Mana{state.MR: 1, state.MG: 1}
	if gotG != wantG {
		t.Fatalf("recorded G: available = %v, want %v (R or G, not all five)", gotG, wantG)
	}
	// The two branches must actually differ, so the assertion cannot be met
	// by a fixed colour set.
	if gotR == gotG {
		t.Fatalf("precondition: recorded R and G produced the same vector %v", gotR)
	}
}
