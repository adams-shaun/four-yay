package rules

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A spell's additional Mill cost uses the same completed-mill marker as the
// Mill effect. Real corpus trigger cards exercise both existing match modes.
func millCostTriggerEngine(t *testing.T, triggerNames []string, n int) (*Engine, []state.ObjID, []state.ObjID) {
	t.Helper()
	spell := card(t, "Name:Mill Cost Spell\nManaCost:0\nTypes:Sorcery\n"+
		"A:SP$ Draw | Cost$ Mill<"+strconv.Itoa(n)+"> | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n")
	e := smallLibraryEngine(t, n+1, spell)
	reg := searchTestRegistry(t)
	var triggerIDs []state.ObjID
	for _, name := range triggerNames {
		triggerIDs = append(triggerIDs, seatOnBattlefield(t, e, searchCorpusCard(t, reg, name), 0))
	}
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if len(lib) != n+1 {
		t.Fatalf("precondition: library length = %d, want %d", len(lib), n+1)
	}
	return e, triggerIDs, lib[:n]
}

// queuedMillTriggers returns the number of queued trigger instances for
// source and the TriggerAmount the last one carries. closeMillBatch patches
// the batch's matching-card count into the queued MilledAll trigger's
// TriggerContext.TriggerAmount, so the amount proves the batch latch engaged
// and not merely that the trigger queued.
func queuedMillTriggers(e *Engine, source state.ObjID) (count int, amount int32) {
	for _, tr := range e.pendingTriggers {
		if tr.Source == source {
			count++
			amount = tr.Ctx.TriggerContext.TriggerAmount
		}
	}
	return count, amount
}

func TestMillCostTriggersMilledPerCard(t *testing.T) {
	t.Parallel()
	e, sources, milled := millCostTriggerEngine(t, []string{"Glowing One"}, 1)
	if o := e.G.Obj(sources[0]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Glowing One not on battlefield: %+v", o)
	}
	spellID := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, spellID, "")
	for _, id := range milled {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition: cost-milled card %d is not in graveyard: %+v", id, o)
		}
	}
	if got, amount := queuedMillTriggers(e, sources[0]); got != 1 || amount != 1 {
		t.Fatalf("Glowing One queued trigger count = %d amount = %d, want 1/1 for one cost-milled card", got, amount)
	}
}

func TestMillCostTriggersMilledAllOnceForBatch(t *testing.T) {
	t.Parallel()
	e, sources, milled := millCostTriggerEngine(t, []string{"The Wise Mothman"}, 3)
	if o := e.G.Obj(sources[0]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Mothman not on battlefield: %+v", o)
	}
	spellID := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, spellID, "")
	for _, id := range milled {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition: cost-milled card %d is not in graveyard: %+v", id, o)
		}
	}
	if got, amount := queuedMillTriggers(e, sources[0]); got != 1 || amount != 3 {
		t.Fatalf("Mothman queued trigger count = %d amount = %d, want one MilledAll trigger with the batch amount 3 for a three-card cost mill", got, amount)
	}
}
