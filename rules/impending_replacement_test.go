package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 702.176a's time counters are an entry-characteristic placement. They
// must use the same pre-entry replacement staging as other entry counters.
func TestImpendingEntryTimeCountersApplyDoublingSeason(t *testing.T) {
	t.Parallel()
	season := tokenReplCorpusCard(t, "Doubling Season")
	creature := card(t, impendingGolemSrc)
	e, cfg := tokenReplGame(t, 9876, season, creature)
	seasonID := moveSeededCard(t, e, 0, season, state.ZBattlefield)
	if o := e.G.Obj(seasonID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Doubling Season is not on the battlefield")
	}
	id := findCardObj(t, e, 0, "Impending Test Golem", state.ZHand)
	if id == 0 {
		t.Fatal("precondition: impending Golem is not in hand")
	}
	if _, ok := e.G.Obj(id).Face().KeywordParam("Impending"); !ok {
		t.Fatal("precondition: impending Golem lost its keyword")
	}
	addMana(t, e, 0, "CG")
	submitChoices(t, e, castModeOption(t, e, id, "impended"))
	passUntilStackEmpty(t, e, 40)

	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: impending Golem did not enter the battlefield: %+v", o)
	}
	if o.CastFlags&state.FlagImpending == 0 {
		t.Fatal("precondition: permanent lacks paid-Impending provenance")
	}
	if got := o.Counter("TIME"); got != 4 {
		t.Fatalf("Impending 2 entered with %d TIME counters under Doubling Season, want 4", got)
	}
	foundEntry := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
			foundEntry = true
			if len(ev.Pairs) == 0 {
				t.Fatal("precondition: impending entry did not fold its replacement-adjusted counters atomically")
			}
		}
	}
	if !foundEntry {
		t.Fatal("precondition: no battlefield entry event for Impending Golem")
	}
	replayCheck(t, e, cfg)
}
