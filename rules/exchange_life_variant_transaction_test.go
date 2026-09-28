package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestExchangeLifeVariantResumesAfterReplacementChoice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Evra, Halcyon Witness", "Alhammarret's Archive", "Cleric Class")
	archive := searchMoveByName(t, e, "Alhammarret's Archive", state.ZBattlefield)
	cleric := searchMoveByName(t, e, "Cleric Class", state.ZBattlefield)
	evra := searchMoveByName(t, e, "Evra, Halcyon Witness", state.ZBattlefield)
	for _, id := range []state.ObjID{archive, cleric, evra} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d is not on battlefield: %+v", id, o)
		}
	}
	readyExchangeCard(t, e, evra)
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -18})
	oldPower, oldLife := e.Power(evra), e.G.Players[0].Life
	if oldPower != 4 || oldLife != 2 || oldPower == oldLife {
		t.Fatalf("precondition: Evra power=%d life=%d, want distinct 4 and 2", oldPower, oldLife)
	}
	addMana(t, e, 0, "CCCC")
	opt := abilityOption(t, e, evra, 0)
	submitChoices(t, e, opt.Index)
	d := passUntilLifeReplacement(t, e, 20)
	if d.Kind != decision.KReplacement {
		t.Fatalf("pending decision kind=%s, want replacement", d.Kind)
	}
	submitChoices(t, e, d.Options[0].Index)
	if life := e.G.Players[0].Life; life <= oldLife {
		t.Fatalf("replacement did not change life: got %d, started %d", life, oldLife)
	}
	if got := e.Power(evra); got != oldLife {
		t.Fatalf("exchange continuation set Evra power=%d, want prior life %d", got, oldLife)
	}
	if got := exchangeVariantSettersOn(e, evra); got != 1 {
		t.Fatalf("exchange installed %d characteristic setters, want exactly one", got)
	}
}
