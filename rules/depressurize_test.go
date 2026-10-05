package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestDepressurizeUsesLayerDerivedPowerForDefinedTargetedQualifier(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	depressurize := mustCorpusCard(t, reg, "Depressurize")
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	e := layerEngine(t)
	e.Advance()
	toMain1(t, e)

	spell := e.G.AddObject(depressurize, 0)
	spell.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{spell.ID})
	target := e.G.AddObject(bears, 1)
	target.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{target.ID})
	e.staticEpoch, e.activeEpoch, e.typesEpoch = -1, -1, -1

	if got := e.G.Obj(target.ID).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: Grizzly Bears zone = %s, want battlefield", got)
	}
	printed := e.G.Obj(target.ID).Face().Power()
	derived := e.Power(target.ID)
	if printed != 2 || derived != 2 {
		t.Fatalf("precondition: Grizzly Bears printed/derived power = %d/%d, want 2/2 before resolution", printed, derived)
	}
	afterPump := derived - 3 // Depressurize's real NumAtt$ -3, with no toughness change.
	if afterPump != -1 || afterPump == int32(printed) {
		t.Fatalf("precondition: printed power %d and post-pump derived power %d must differ; want 2 and -1", printed, afterPump)
	}

	addMana(t, e, 0, "BM") // {1}{B}
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell.ID})
	e.Advance()
	submitTarget(t, e, target.ID)
	passUntilStackEmpty(t, e, 40)

	// The real script's -3/-0 Pump leaves the 2/2 at derived power -1;
	// its immediately chained Defined$ Targeted.powerLE0 must destroy it.
	if got := e.G.Obj(target.ID).Zone; got != state.ZGraveyard {
		t.Fatalf("Depressurize left Grizzly Bears in %s; expected its -1 derived power to satisfy powerLE0", got)
	}
}
