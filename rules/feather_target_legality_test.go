package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestFeatherChoiceOmitsCreatureProtectedFromTriggeredSpell(t *testing.T) {
	t.Parallel()
	e, _, featherID, bear1, bear2, strikeID := featherGame(t, 912)
	protected := onBoard(t, e, 1, "Name:Warded Bear\nManaCost:1G\nTypes:Creature Bear\nPT:2/2\nK:Protection from white\nOracle:x\n")
	if e.G.Obj(protected).Zone != state.ZBattlefield || e.G.Obj(bear1).Zone != state.ZBattlefield {
		t.Fatal("precondition: comparison creatures must be on the battlefield")
	}
	castStrikeAtFeather(t, e, featherID, strikeID)
	d := drainUntilKind(t, e, decision.KChoose)
	if len(d.Options) != 2 {
		t.Fatalf("choice options = %d (%+v), want exactly the two unprotected bears", len(d.Options), d.Options)
	}
	seen1, seen2, sawProtected := false, false, false
	for _, option := range d.Options {
		seen1 = seen1 || option.Obj == bear1
		seen2 = seen2 || option.Obj == bear2
		sawProtected = sawProtected || option.Obj == protected
	}
	if !seen1 || !seen2 || sawProtected {
		t.Fatalf("Feather choice options %+v; bears (%d,%d) must be present and protected bear %d absent", d.Options, bear1, bear2, protected)
	}
	var spellID state.ObjID
	for _, id := range e.G.Stack {
		spell := e.G.Obj(id)
		if spell != nil && spell.Zone == state.ZStack && spell.Face() != nil && spell.Face().Name == "Defiant Strike" {
			spellID = id
			break
		}
	}
	if spellID == 0 {
		t.Fatal("precondition: Defiant Strike is not on the stack")
	}
	if !e.protectedFrom(protected, e.protectionSource(spellID)) {
		t.Fatal("precondition: Defiant Strike must have a different quality from the protected creature")
	}
}
