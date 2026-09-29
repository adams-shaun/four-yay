package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestGreatestPowerReadsDerivedPowerForComparisonSet(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Grizzly Bears", "Colossal Dreadmaw")
	bear := crAbortMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	big := crAbortMove(t, e, 0, "Colossal Dreadmaw", state.ZBattlefield)
	e.emit(events.Event{Kind: events.ControlChange, Obj: bear, Player: 1})
	e.emit(events.Event{Kind: events.ControlChange, Obj: big, Player: 1})
	if e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(big).Zone != state.ZBattlefield {
		t.Fatal("precondition: both comparison creatures must be on the battlefield")
	}
	if got := e.Power(bear); got != 2 {
		t.Fatalf("precondition: Grizzly Bears power = %d, want printed 2", got)
	}
	if got := e.Power(big); got != 6 {
		t.Fatalf("precondition: Colossal Dreadmaw power = %d, want printed 6", got)
	}
	ctx := &effects.Ctx{Source: big, Controller: 1, TriggerContext: effects.TriggerContext{TriggerCard: big}}
	const spec = "Creature.greatestPowerControlledByCardController"
	if got, ok := effects.EvalCountOK(e, ctx, "Count$ValidSelf "+spec); !ok || got != 1 {
		t.Fatalf("precondition: unpumped Dreadmaw ValidSelf count = %d, resolved=%v; want 1, true", got, ok)
	}
	e.AddContinuous(state.ContinuousEffect{Source: bear, Controller: 1, Layer: LPT, Sub: SubModify,
		Affects: "Creature.Self", AddPower: 5, AddToughness: 5})
	bearPrinted, bigPrinted := e.G.Obj(bear).Face().Power(), e.G.Obj(big).Face().Power()
	if e.Power(bear) != 7 || e.Power(big) != 6 || bearPrinted >= bigPrinted {
		t.Fatalf("precondition: derived/printed ordering must flip: bear derived=%d printed=%d; Dreadmaw derived=%d printed=%d",
			e.Power(bear), bearPrinted, e.Power(big), bigPrinted)
	}
	if got, ok := effects.EvalCountOK(e, ctx, "Count$ValidSelf "+spec); !ok || got != 0 {
		t.Fatalf("pumped Dreadmaw ValidSelf count = %d, resolved=%v; want 0, true", got, ok)
	}
}
