// Focused test for the snow-covered presence this ticket adds: Roiling Canopy
// fires only when a Forest enters while you control five other Forests, and
// the cause plays a Forest from hand -- so the setup carries the snow-covered
// twins, whose distinct name keeps the played Forest's ref unambiguous.
package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestTriggerSnowForestPresence(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Roiling Canopy"
	it := triggerItem(t, reg, name, "trigger#0.0")
	p0 := it.Scenario.Setup["p0"]
	// Precondition: the gate really is the six-Forest presence floor.
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	if got := card.Faces[0].Triggers[0].ParamStr(cards.PKPresentCompare); got != "GE6" {
		t.Fatalf("precondition: %s PresentCompare$ = %q, want GE6", name, got)
	}
	// The played land is the plain Forest; the setup carries only the
	// snow-covered twin of it.
	played := false
	for _, st := range it.Scenario.Steps {
		if st.Op == "play" && st.Card == "p0:Forest" {
			played = true
		}
	}
	if !played {
		t.Fatalf("%s: no played Forest: %v", name, it.Scenario.Steps)
	}
	forests, snow := 0, 0
	for _, n := range p0.Battlefield {
		switch n {
		case "Forest":
			forests++
		case "Snow-Covered Forest":
			snow++
		}
	}
	if snow < 5 {
		t.Fatalf("%s: battlefield %v carries %d snow Forests, want at least 5", name, p0.Battlefield, snow)
	}
	if forests+snow < 6 {
		t.Fatalf("%s: battlefield %v carries %d Forests, want at least 6", name, p0.Battlefield, forests+snow)
	}
}
