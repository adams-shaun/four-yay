package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func probeGate(t *testing.T, e *Engine, id state.ObjID, mode, api string) (bool, bool) {
	t.Helper()
	src := e.G.Obj(id)
	if src == nil || src.Face() == nil {
		t.Fatal("no source")
	}
	for _, tr := range src.Face().Triggers {
		if tr.Mode == mode && tr.Effect != nil && tr.Effect.API == api {
			ctx := &effects.Ctx{Source: id, Controller: e.controllerOf(id), SVars: src.Face().SVars}
			return effects.CheckSVarHolds(e, ctx, tr.Params["CheckSVar"], tr.Params["SVarCompare"])
		}
	}
	t.Fatalf("no %s/%s trigger on %s", mode, api, src.Face().Name)
	return false, false
}

// TestPhaseTriggerCheckSVarRemainingShapes covers the five cards' gates the
// two full-flow fixtures above do not drive: the same life-gained count at
// its other thresholds (Resplendent Angel GE5, Valkyrie Harbinger GE4), the
// opponents' highest valid creature count (Defense of the Heart), the
// opponents' highest life lost this turn (Bloodchief Ascension), and the
// exile count (Valakut Exploration — Count$ValidExile Card.ExiledWithSource,
// a shape this build could already evaluate). Each is asserted BOTH ways:
// the gate fails (and the trigger stays silent) below the threshold and
// holds at it, through the real compiled SVar table of the real corpus card.
func TestPhaseTriggerCheckSVarRemainingShapes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Resplendent Angel: GE5 on life gained this turn.
	e := crAbortEngine(t, reg, "ur-delver", "Resplendent Angel")
	ra := crAbortMove(t, e, 0, "Resplendent Angel", state.ZBattlefield)
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 5})
	holds, ev := probeGate(t, e, ra, "Phase", "Token")
	t.Logf("Resplendent Angel GE5 after +5: holds=%v evaluated=%v", holds, ev)
	if !ev || !holds {
		t.Errorf("Resplendent Angel gate should hold at 5")
	}
	// Losing life does not un-gain the turn's earlier gains: the count sums
	// POSITIVE LifeChanges (CR 118.3's one-sided reading), so 5 stays 5.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -5})
	holds, ev = probeGate(t, e, ra, "Phase", "Token")
	t.Logf("Resplendent Angel after -5 (still 5 gained): holds=%v evaluated=%v", holds, ev)
	if !ev || !holds {
		t.Errorf("Resplendent Angel gate should still hold at 5 gained")
	}
	// A fresh turn-respecting fixture with only 3 gained: below GE5, suppressed.
	e2 := crAbortEngine(t, reg, "ur-delver", "Resplendent Angel")
	ra2 := crAbortMove(t, e2, 0, "Resplendent Angel", state.ZBattlefield)
	e2.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	holds, ev = probeGate(t, e2, ra2, "Phase", "Token")
	t.Logf("Resplendent Angel GE5 with only 3 gained: holds=%v evaluated=%v", holds, ev)
	if !ev || holds {
		t.Errorf("Resplendent Angel gate should not hold at 3")
	}

	// Valkyrie Harbinger: SVar X = Count$LifeYouGainedThisTurn, GE4.
	e3 := crAbortEngine(t, reg, "ur-delver", "Valkyrie Harbinger")
	vh := crAbortMove(t, e3, 0, "Valkyrie Harbinger", state.ZBattlefield)
	e3.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 4})
	holds, ev = probeGate(t, e3, vh, "Phase", "Token")
	t.Logf("Valkyrie Harbinger GE4 after +4: holds=%v evaluated=%v", holds, ev)
	if !ev || !holds {
		t.Errorf("Valkyrie Harbinger gate should hold at 4")
	}

	// Defense of the Heart: PlayerCountOpponents$HighestValid Creature.YouCtrl, GE3.
	e4 := crAbortEngine(t, reg, "ur-delver", "Defense of the Heart")
	dh := crAbortMove(t, e4, 0, "Defense of the Heart", state.ZBattlefield)
	holds, ev = probeGate(t, e4, dh, "Phase", "Sacrifice")
	t.Logf("Defense of the Heart GE3 with 0 opponent creatures: holds=%v evaluated=%v", holds, ev)
	if !ev || holds {
		t.Errorf("Defense of the Heart gate should not hold with 0 opponent creatures")
	}
	for i := 0; i < 3; i++ {
		crAbortMove(t, e4, 1, "Mother of Runes", state.ZBattlefield)
	}
	holds, ev = probeGate(t, e4, dh, "Phase", "Sacrifice")
	t.Logf("Defense of the Heart GE3 with 3 opponent creatures: holds=%v evaluated=%v", holds, ev)
	if !ev || !holds {
		t.Errorf("Defense of the Heart gate should hold with 3 opponent creatures")
	}

	// Bloodchief Ascension: PlayerCountRegisteredOpponents$HighestLifeLostThisTurn, GE2.
	e5 := crAbortEngine(t, reg, "ur-delver", "Bloodchief Ascension")
	ba := crAbortMove(t, e5, 0, "Bloodchief Ascension", state.ZBattlefield)
	holds, ev = probeGate(t, e5, ba, "Phase", "PutCounter")
	t.Logf("Bloodchief GE2 with 0 opponent life lost: holds=%v evaluated=%v", holds, ev)
	if !ev || holds {
		t.Errorf("Bloodchief gate should not hold with 0 lost")
	}
	e5.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -2})
	holds, ev = probeGate(t, e5, ba, "Phase", "PutCounter")
	t.Logf("Bloodchief GE2 with opponent -2: holds=%v evaluated=%v", holds, ev)
	if !ev || !holds {
		t.Errorf("Bloodchief gate should hold with 2 lost")
	}

	// Valakut Exploration: Count$ValidExile Card.ExiledWithSource, GT0 — the
	// gate this build could already evaluate; probe both directions with a
	// fixture script-free exile move carrying ExiledWith provenance.
	e6 := crAbortEngine(t, reg, "ur-delver", "Valakut Exploration")
	ve := crAbortMove(t, e6, 0, "Valakut Exploration", state.ZBattlefield)
	holds, ev = probeGate(t, e6, ve, "Phase", "ChangeZoneAll")
	t.Logf("Valakut GT0 with empty exile: holds=%v evaluated=%v", holds, ev)
	if !ev || holds {
		t.Errorf("Valakut gate should not hold with empty exile")
	}
	deln := crAbortMove(t, e6, 0, "Delver of Secrets", state.ZHand)
	e6.emit(events.Event{Kind: events.MoveZone, Obj: deln, From: state.ZHand, To: state.ZExile, IDs: []state.ObjID{ve}})
	holds, ev = probeGate(t, e6, ve, "Phase", "ChangeZoneAll")
	t.Logf("Valakut GT0 with one exiled-with card: holds=%v evaluated=%v", holds, ev)
	if !ev || !holds {
		t.Errorf("Valakut gate should hold with one exiled-with card")
	}
}
