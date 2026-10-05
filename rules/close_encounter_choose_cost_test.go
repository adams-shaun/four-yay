package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestCloseEncounterRaiseCostChooseCard(t *testing.T) {
	t.Parallel()
	e, _ := paidCostEngine(t, []string{"Close Encounter", "Hill Giant"}, []string{"Ancient Brontodon"})
	creature := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZBattlefield)
	spell := paidCostMoveTo(t, e, 0, "Close Encounter", state.ZHand)
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || !o.EffectiveIsCreature() {
		t.Fatalf("precondition: qualifying creature not controlled on battlefield: %+v", o)
	}
	paidCostCast(t, e, spell, "G1")
	d := e.Pending()
	if d != nil && d.Kind == decision.KChoose {
		idx := -1
		for _, o := range d.Options {
			if o.Obj == creature && o.Kind == "choosecost" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("qualifying controlled creature missing from ask: %+v", d.Options)
		}
		submitChoices(t, e, idx)
	}
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("chosen creature moved; ChooseCard selects but does not move it: %+v", o)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("cast did not continue to target choice after paying ChooseCard: %+v", d)
	}
}
